package handler

// Round 5 -- residual branches in RegisterPushDevice:
//   - the empty-EventPattern repair arm (an already-existing mobile route
//     whose Match pattern was cleared gets reset to "*").
//   - the Update error arm (time-of-check/time-of-use: the matched route
//     vanishes between List and Update), driven through the
//     updateNotificationRoute seam.

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/kshaham/jarvis/internal/notifications"
)

func TestRegisterPushDevice_RepairsEmptyEventPattern(t *testing.T) {
	t.Parallel()
	ctx := newPushContext(t)
	// Pre-seed a mobile route for dev-7 whose Match pattern is empty --
	// simulating a route the operator cleared through the Routes UI.
	seed := ctx.Notifications.Create(notifications.Route{
		Name:    mobileNameFor("dev-7", "Old"),
		Enabled: false,
		Match:   notifications.MatchRule{EventPattern: ""},
		Channels: []notifications.ChannelSpec{{
			Type:   notifications.ChannelExpoPush,
			Target: "ExponentPushToken[OLDOLDOLDOLDOLD0000]",
		}},
	})

	rec := postJSON(t, ctx.RegisterPushDevice, "/api/notifications/devices", map[string]interface{}{
		"token":      validExpoToken,
		"deviceId":   "dev-7",
		"deviceName": "New",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp registerDeviceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.RouteID != seed.ID {
		t.Errorf("expected update of seeded route %q, got routeId %q", seed.ID, resp.RouteID)
	}
	// The empty pattern should have been repaired to "*".
	got := ctx.Notifications.Get(seed.ID)
	if got == nil {
		t.Fatal("seeded route disappeared")
	}
	if got.Match.EventPattern != "*" {
		t.Errorf("EventPattern=%q, want %q (repaired)", got.Match.EventPattern, "*")
	}
	if !got.Enabled {
		t.Error("expected route re-enabled after re-registration")
	}
	if got.Channels[0].Target != validExpoToken {
		t.Errorf("token not refreshed: got %q", got.Channels[0].Target)
	}
}

func TestRegisterPushDevice_UpdateErrorReturns500(t *testing.T) {
	ctx := newPushContext(t)
	// Seed an existing mobile route so the update branch is taken.
	ctx.Notifications.Create(notifications.Route{
		Name:    mobileNameFor("dev-8", "Phone"),
		Enabled: true,
		Match:   notifications.MatchRule{EventPattern: "office:taskAssigned"},
		Channels: []notifications.ChannelSpec{{
			Type:   notifications.ChannelExpoPush,
			Target: "ExponentPushToken[SEEDSEEDSEEDSEED1111]",
		}},
	})

	// Force the TOCTOU failure: the matched route is gone by Update time.
	orig := updateNotificationRoute
	t.Cleanup(func() { updateNotificationRoute = orig })
	updateNotificationRoute = func(_ *notifications.Store, _ string, _ notifications.Route) (*notifications.Route, error) {
		return nil, notifications.ErrNotFound
	}

	rec := postJSON(t, ctx.RegisterPushDevice, "/api/notifications/devices", map[string]interface{}{
		"token":      validExpoToken,
		"deviceId":   "dev-8",
		"deviceName": "Phone",
	})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on update error, got %d: %s", rec.Code, rec.Body.String())
	}
	if !errors.Is(notifications.ErrNotFound, notifications.ErrNotFound) {
		t.Fatal("sanity")
	}
}
