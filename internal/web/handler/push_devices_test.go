package handler

// Coverage for push_devices.go: mobile push-device (de)registration.
// Registration translates an Expo token into one notification Route per
// default event pattern; re-registration with the same deviceId updates
// in place. We assert the route count, idempotency, validation failures,
// and the mobileNameFor helper.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kshaham/jarvis/internal/notifications"
	"github.com/kshaham/jarvis/internal/web/middleware"
	"github.com/kshaham/jarvis/internal/web/sse"
)

const validExpoToken = "ExponentPushToken[abcdefghijklmnop1234]"

func newPushContext(t *testing.T) *Context {
	t.Helper()
	store := notifications.NewStore(filepath.Join(t.TempDir(), "notifications.json"))
	return &Context{
		SSEBroker:     sse.NewBroker(100, 10),
		CSRFStore:     middleware.NewCSRFStore(context.Background()),
		Notifications: store,
	}
}

func TestRegisterPushDevice_FirstRegistration(t *testing.T) {
	ctx := newPushContext(t)
	rec := postJSON(t, ctx.RegisterPushDevice, "/api/notifications/devices", map[string]interface{}{
		"token":      validExpoToken,
		"deviceId":   "dev-1",
		"deviceName": "Pixel",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp registerDeviceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Success {
		t.Error("expected success=true")
	}
	// One route per default event pattern.
	if resp.Events != len(defaultMobileEventPatterns) {
		t.Errorf("events=%d, want %d", resp.Events, len(defaultMobileEventPatterns))
	}
	if got := len(ctx.Notifications.List()); got != len(defaultMobileEventPatterns) {
		t.Errorf("store has %d routes, want %d", got, len(defaultMobileEventPatterns))
	}
	// Every created route should carry the mobile prefix in its name.
	for _, route := range ctx.Notifications.List() {
		if !strings.HasPrefix(route.Name, mobileRoutePrefix+"dev-1") {
			t.Errorf("route name missing mobile prefix: %q", route.Name)
		}
		if route.Channels[0].Type != notifications.ChannelExpoPush {
			t.Errorf("expected expo channel, got %q", route.Channels[0].Type)
		}
	}
}

func TestRegisterPushDevice_Idempotent(t *testing.T) {
	ctx := newPushContext(t)
	// First registration creates N routes.
	postJSON(t, ctx.RegisterPushDevice, "/api/notifications/devices", map[string]interface{}{
		"token":    validExpoToken,
		"deviceId": "dev-2",
	})
	firstCount := len(ctx.Notifications.List())

	// Re-register the same device with a new token -> updates in place,
	// does NOT add more routes (the handler finds the existing prefix
	// match and updates the first one).
	newToken := "ExponentPushToken[ZZZZZZZZZZZZZZZZ9999]"
	rec := postJSON(t, ctx.RegisterPushDevice, "/api/notifications/devices", map[string]interface{}{
		"token":      newToken,
		"deviceId":   "dev-2",
		"deviceName": "Renamed",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	// No new routes should have been created.
	if got := len(ctx.Notifications.List()); got != firstCount {
		t.Errorf("re-registration changed route count: %d -> %d", firstCount, got)
	}
	// At least one route should now carry the new token.
	foundNewToken := false
	for _, route := range ctx.Notifications.List() {
		for _, ch := range route.Channels {
			if ch.Target == newToken {
				foundNewToken = true
			}
		}
	}
	if !foundNewToken {
		t.Error("expected at least one route updated to the new token")
	}
}

func TestRegisterPushDevice_CustomEvents(t *testing.T) {
	ctx := newPushContext(t)
	rec := postJSON(t, ctx.RegisterPushDevice, "/api/notifications/devices", map[string]interface{}{
		"token":    validExpoToken,
		"deviceId": "dev-3",
		"events":   []string{"office:taskAssigned"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp registerDeviceResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Events != 1 {
		t.Errorf("events=%d, want 1 for single custom pattern", resp.Events)
	}
}

func TestRegisterPushDevice_InvalidToken(t *testing.T) {
	ctx := newPushContext(t)
	rec := postJSON(t, ctx.RegisterPushDevice, "/api/notifications/devices", map[string]interface{}{
		"token":    "not-an-expo-token",
		"deviceId": "dev-4",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid token, got %d", rec.Code)
	}
}

func TestRegisterPushDevice_MissingDeviceID(t *testing.T) {
	ctx := newPushContext(t)
	rec := postJSON(t, ctx.RegisterPushDevice, "/api/notifications/devices", map[string]interface{}{
		"token": validExpoToken,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing deviceId, got %d", rec.Code)
	}
}

func TestRegisterPushDevice_InvalidJSON(t *testing.T) {
	ctx := newPushContext(t)
	req := httptest.NewRequest("POST", "/api/notifications/devices", strings.NewReader("{not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.RegisterPushDevice(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed JSON, got %d", rec.Code)
	}
}

func TestRegisterPushDevice_NilStore503(t *testing.T) {
	ctx := &Context{}
	rec := postJSON(t, ctx.RegisterPushDevice, "/api/notifications/devices", map[string]interface{}{
		"token":    validExpoToken,
		"deviceId": "dev-5",
	})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestUnregisterPushDevice_RemovesRoutes(t *testing.T) {
	ctx := newPushContext(t)
	// Register, then unregister, and confirm all its routes are gone.
	postJSON(t, ctx.RegisterPushDevice, "/api/notifications/devices", map[string]interface{}{
		"token":    validExpoToken,
		"deviceId": "dev-6",
	})
	registered := len(ctx.Notifications.List())
	if registered == 0 {
		t.Fatal("setup: expected routes after register")
	}

	req := withURLParam(httptest.NewRequest("DELETE", "/api/notifications/devices/dev-6", nil), "deviceId", "dev-6")
	rec := httptest.NewRecorder()
	ctx.UnregisterPushDevice(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if int(resp["removed"].(float64)) != registered {
		t.Errorf("removed=%v, want %d", resp["removed"], registered)
	}
	if len(ctx.Notifications.List()) != 0 {
		t.Errorf("store should be empty, has %d", len(ctx.Notifications.List()))
	}
}

func TestUnregisterPushDevice_MissingID(t *testing.T) {
	ctx := newPushContext(t)
	req := withURLParam(httptest.NewRequest("DELETE", "/api/notifications/devices/", nil), "deviceId", "")
	rec := httptest.NewRecorder()
	ctx.UnregisterPushDevice(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty deviceId, got %d", rec.Code)
	}
}

func TestUnregisterPushDevice_NilStore503(t *testing.T) {
	ctx := &Context{}
	req := withURLParam(httptest.NewRequest("DELETE", "/api/notifications/devices/x", nil), "deviceId", "x")
	rec := httptest.NewRecorder()
	ctx.UnregisterPushDevice(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestMobileNameFor(t *testing.T) {
	got := mobileNameFor("dev-9", "My Phone")
	want := "mobile:dev-9 (My Phone)"
	if got != want {
		t.Errorf("mobileNameFor=%q, want %q", got, want)
	}
}
