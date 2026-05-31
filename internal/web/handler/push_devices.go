package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/kshaham/jarvis/internal/notifications"
	"github.com/kshaham/jarvis/internal/web/request"
)

// Mobile push-notification device registration.
//
// The mobile app calls POST /api/notifications/devices on first launch
// after the user grants notification permission. Body: {token, deviceId,
// deviceName, events?}. We translate the device into a NotificationRoute
// with a single ExpoPush channel so the existing dispatcher (configured
// by routes) does the actual delivery. Routes created this way are
// tagged in their Name with the mobile deviceId so registration is
// idempotent -- re-registering with the same deviceId updates the
// existing route's token in place instead of creating duplicates.

// expoTokenRe matches ExponentPushToken[xxx] tokens. Defence against
// rows in the routes file containing arbitrary URLs that would then get
// POSTed to https://exp.host/...
var expoTokenRe = regexp.MustCompile(`^ExponentPushToken\[[A-Za-z0-9_-]{16,}\]$`)

// defaultMobileEventPatterns is what we subscribe a freshly registered
// device to. Curated for "things you actually want to know about" -- not
// every SSE event. Operators can edit through the Routes UI on the web
// dashboard if they want more or fewer.
var defaultMobileEventPatterns = []string{
	"office:wrapUpInitiated",
	"office:agentStuck",
	"office:teamPaused",
	"office:taskAssigned",
	"office:standupStarted",
}

const mobileRoutePrefix = "mobile:"

// updateNotificationRoute is a seam over Store.Update so the rare
// time-of-check/time-of-use failure (the matched route is deleted between
// List and Update) can be exercised deterministically in tests. In
// production it forwards straight to the store.
var updateNotificationRoute = func(store *notifications.Store, id string, route notifications.Route) (*notifications.Route, error) {
	return store.Update(id, route)
}

type registerDeviceRequest struct {
	Token      string   `json:"token"`
	DeviceID   string   `json:"deviceId"`
	DeviceName string   `json:"deviceName"`
	Events     []string `json:"events,omitempty"`
}

type registerDeviceResponse struct {
	Success bool   `json:"success"`
	RouteID string `json:"routeId"`
	Events  int    `json:"events"`
}

// RegisterPushDevice creates or updates a notification route for one
// mobile device. POST /api/notifications/devices.
func (ctx *Context) RegisterPushDevice(w http.ResponseWriter, r *http.Request) {
	if ctx.Notifications == nil {
		request.WriteError(w, http.StatusServiceUnavailable, "notifications store not available")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<14)
	var body registerDeviceRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	body.Token = strings.TrimSpace(body.Token)
	body.DeviceID = strings.TrimSpace(body.DeviceID)
	body.DeviceName = strings.TrimSpace(body.DeviceName)
	if !expoTokenRe.MatchString(body.Token) {
		request.WriteError(w, http.StatusBadRequest, "invalid Expo push token")
		return
	}
	if body.DeviceID == "" {
		request.WriteError(w, http.StatusBadRequest, "deviceId is required")
		return
	}
	if body.DeviceName == "" {
		body.DeviceName = "Mobile device"
	}

	events := body.Events
	if len(events) == 0 {
		events = defaultMobileEventPatterns
	}

	// Build the channels: one ExpoPush spec per matching event pattern
	// would duplicate the dispatch; instead, we create one ExpoPush
	// channel per route and fan out events with a single shared route
	// per device. Operators can edit later.
	channels := []notifications.ChannelSpec{{
		Type:   notifications.ChannelExpoPush,
		Target: body.Token,
	}}

	// Find any existing route for this deviceId by name prefix.
	wanted := mobileRoutePrefix + body.DeviceID
	for _, route := range ctx.Notifications.List() {
		if strings.HasPrefix(route.Name, wanted) {
			updated := *route
			updated.Name = mobileNameFor(body.DeviceID, body.DeviceName)
			updated.Enabled = true
			updated.Channels = channels
			// Keep their existing Match pattern -- they may have
			// edited it through the Routes UI. Only overwrite if it's
			// still the default we set.
			if updated.Match.EventPattern == "" {
				updated.Match.EventPattern = "*"
			}
			if _, err := updateNotificationRoute(ctx.Notifications, route.ID, updated); err != nil {
				slog.Warn("notifications: failed to refresh mobile route", "error", err, "deviceId", body.DeviceID)
				request.WriteError(w, http.StatusInternalServerError, "failed to update route")
				return
			}
			request.WriteJSON(w, http.StatusOK, registerDeviceResponse{
				Success: true,
				RouteID: route.ID,
				Events:  len(events),
			})
			return
		}
	}

	// First registration for this device: create one route per default
	// event pattern, all pointing at the same Expo token. Multiple
	// routes are simpler than a single route with multi-pattern
	// matching (which the existing engine doesn't support).
	created := 0
	var lastID string
	for _, pattern := range events {
		route := notifications.Route{
			Name:    mobileNameFor(body.DeviceID, body.DeviceName) + " · " + pattern,
			Enabled: true,
			Match: notifications.MatchRule{
				EventPattern: pattern,
			},
			Channels: channels,
		}
		out := ctx.Notifications.Create(route)
		lastID = out.ID
		created++
	}

	request.WriteJSON(w, http.StatusOK, registerDeviceResponse{
		Success: true,
		RouteID: lastID,
		Events:  created,
	})
}

// UnregisterPushDevice deletes every route whose Name matches the
// mobile:<deviceId> prefix. DELETE /api/notifications/devices/{deviceId}.
func (ctx *Context) UnregisterPushDevice(w http.ResponseWriter, r *http.Request) {
	if ctx.Notifications == nil {
		request.WriteError(w, http.StatusServiceUnavailable, "notifications store not available")
		return
	}
	deviceID := strings.TrimSpace(chi.URLParam(r, "deviceId"))
	if deviceID == "" {
		request.WriteError(w, http.StatusBadRequest, "deviceId is required")
		return
	}
	wanted := mobileRoutePrefix + deviceID
	removed := 0
	for _, route := range ctx.Notifications.List() {
		if strings.HasPrefix(route.Name, wanted) {
			if err := ctx.Notifications.Delete(route.ID); err == nil {
				removed++
			}
		}
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"removed": removed,
	})
}

func mobileNameFor(deviceID, deviceName string) string {
	return mobileRoutePrefix + deviceID + " (" + deviceName + ")"
}
