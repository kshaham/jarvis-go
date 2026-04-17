package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/claudeman/jarvis-go/internal/notifications"
	"github.com/claudeman/jarvis-go/internal/web/request"
)

// ListNotificationRoutes returns every persisted route.
// GET /api/notifications/routes
func (ctx *Context) ListNotificationRoutes(w http.ResponseWriter, r *http.Request) {
	if ctx.Notifications == nil {
		request.WriteError(w, http.StatusServiceUnavailable, "notifications not initialized")
		return
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"routes": ctx.Notifications.List(),
	})
}

// GetNotificationRoute returns a single route.
// GET /api/notifications/routes/{id}
func (ctx *Context) GetNotificationRoute(w http.ResponseWriter, r *http.Request) {
	if ctx.Notifications == nil {
		request.WriteError(w, http.StatusServiceUnavailable, "notifications not initialized")
		return
	}
	route := ctx.Notifications.Get(chi.URLParam(r, "id"))
	if route == nil {
		request.WriteCodedError(w, http.StatusNotFound, request.ErrCodeNotFound, "route not found")
		return
	}
	request.WriteJSON(w, http.StatusOK, route)
}

// CreateNotificationRoute creates a new route.
// POST /api/notifications/routes
func (ctx *Context) CreateNotificationRoute(w http.ResponseWriter, r *http.Request) {
	if ctx.Notifications == nil {
		request.WriteError(w, http.StatusServiceUnavailable, "notifications not initialized")
		return
	}
	var route notifications.Route
	if !request.DecodeAndValidate(w, r, &route) {
		return
	}
	if route.Name == "" {
		request.WriteCodedError(w, http.StatusBadRequest, request.ErrCodeInvalidInput, "name is required")
		return
	}
	if route.Match.EventPattern == "" {
		request.WriteCodedError(w, http.StatusBadRequest, request.ErrCodeInvalidInput, "match.eventPattern is required")
		return
	}
	if len(route.Channels) == 0 {
		request.WriteCodedError(w, http.StatusBadRequest, request.ErrCodeInvalidInput, "at least one channel is required")
		return
	}
	for _, ch := range route.Channels {
		if !validChannelType(ch.Type) {
			request.WriteCodedError(w, http.StatusBadRequest, request.ErrCodeInvalidInput, "channel.type must be telegram, webhook, or log")
			return
		}
		if ch.Target == "" {
			request.WriteCodedError(w, http.StatusBadRequest, request.ErrCodeInvalidInput, "channel.target is required")
			return
		}
	}
	created := ctx.Notifications.Create(route)
	request.WriteJSON(w, http.StatusCreated, created)
}

// UpdateNotificationRoute replaces a route's fields.
// PUT /api/notifications/routes/{id}
func (ctx *Context) UpdateNotificationRoute(w http.ResponseWriter, r *http.Request) {
	if ctx.Notifications == nil {
		request.WriteError(w, http.StatusServiceUnavailable, "notifications not initialized")
		return
	}
	id := chi.URLParam(r, "id")
	var route notifications.Route
	if !request.DecodeAndValidate(w, r, &route) {
		return
	}
	updated, err := ctx.Notifications.Update(id, route)
	if err == notifications.ErrNotFound {
		request.WriteCodedError(w, http.StatusNotFound, request.ErrCodeNotFound, "route not found")
		return
	}
	if err != nil {
		request.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	request.WriteJSON(w, http.StatusOK, updated)
}

// DeleteNotificationRoute removes a route.
// DELETE /api/notifications/routes/{id}
func (ctx *Context) DeleteNotificationRoute(w http.ResponseWriter, r *http.Request) {
	if ctx.Notifications == nil {
		request.WriteError(w, http.StatusServiceUnavailable, "notifications not initialized")
		return
	}
	if err := ctx.Notifications.Delete(chi.URLParam(r, "id")); err != nil {
		if err == notifications.ErrNotFound {
			request.WriteCodedError(w, http.StatusNotFound, request.ErrCodeNotFound, "route not found")
			return
		}
		request.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{"deleted": true})
}

// TestNotificationRoute fires a synthetic event through the route so
// operators can verify a new configuration end-to-end without waiting for
// a real event to fire.
// POST /api/notifications/routes/{id}/test
func (ctx *Context) TestNotificationRoute(w http.ResponseWriter, r *http.Request) {
	if ctx.NotificationsRouter == nil {
		request.WriteError(w, http.StatusServiceUnavailable, "notifications router not initialized")
		return
	}
	id := chi.URLParam(r, "id")
	payload := map[string]interface{}{
		"test":      true,
		"message":   "synthetic test event",
		"timestamp": r.Context().Value("requestTime"),
	}
	if err := ctx.NotificationsRouter.Test(id, "notifications:test", payload); err != nil {
		if err == notifications.ErrNotFound {
			request.WriteCodedError(w, http.StatusNotFound, request.ErrCodeNotFound, "route not found")
			return
		}
		request.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{"dispatched": true})
}

func validChannelType(t string) bool {
	switch t {
	case notifications.ChannelTelegram, notifications.ChannelWebhook, notifications.ChannelLog:
		return true
	}
	return false
}
