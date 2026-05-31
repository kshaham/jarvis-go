package handler

// Coverage for notifications.go: the route CRUD + test-fire endpoints.
// These handlers are pure-ish (a JSON Store backed by a temp file) so we
// exercise every status code: 503 when the store is nil, 400 validation
// failures, 404 not-found, and the happy 200/201 paths.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/kshaham/jarvis/internal/notifications"
	"github.com/kshaham/jarvis/internal/web/middleware"
	"github.com/kshaham/jarvis/internal/web/sse"
)

// newNotifContext builds a Context with a real (temp-file backed)
// notifications Store + Router so every handler path is reachable.
func newNotifContext(t *testing.T) *Context {
	t.Helper()
	store := notifications.NewStore(filepath.Join(t.TempDir(), "notifications.json"))
	router := notifications.NewRouter(store, nil)
	return &Context{
		SSEBroker:           sse.NewBroker(100, 10),
		CSRFStore:           middleware.NewCSRFStore(context.Background()),
		Notifications:       store,
		NotificationsRouter: router,
	}
}

// withURLParam attaches a chi URL param to a request so handlers that read
// chi.URLParam(r, "id") see the value.
func withURLParam(req *http.Request, key, val string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, val)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func validRoute(name string) notifications.Route {
	return notifications.Route{
		Name:    name,
		Enabled: true,
		Match:   notifications.MatchRule{EventPattern: "office:*"},
		Channels: []notifications.ChannelSpec{
			{Type: notifications.ChannelLog, Target: "audit"},
		},
	}
}

func TestListNotificationRoutes_Empty(t *testing.T) {
	t.Parallel()
	ctx := newNotifContext(t)
	req := httptest.NewRequest("GET", "/api/notifications/routes", nil)
	rec := httptest.NewRecorder()
	ctx.ListNotificationRoutes(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp struct {
		Routes []notifications.Route `json:"routes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Routes) != 0 {
		t.Errorf("expected empty routes, got %d", len(resp.Routes))
	}
}

func TestListNotificationRoutes_NilStore503(t *testing.T) {
	t.Parallel()
	ctx := &Context{}
	req := httptest.NewRequest("GET", "/api/notifications/routes", nil)
	rec := httptest.NewRecorder()
	ctx.ListNotificationRoutes(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestCreateNotificationRoute_Happy(t *testing.T) {
	t.Parallel()
	ctx := newNotifContext(t)
	rec := postJSON(t, ctx.CreateNotificationRoute, "/api/notifications/routes", validRoute("alpha"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var created notifications.Route
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.ID == "" {
		t.Error("expected a generated ID")
	}
	if created.Name != "alpha" {
		t.Errorf("name=%q, want alpha", created.Name)
	}
	// And it should now be listed.
	if len(ctx.Notifications.List()) != 1 {
		t.Errorf("store should hold 1 route, has %d", len(ctx.Notifications.List()))
	}
}

func TestCreateNotificationRoute_MissingName(t *testing.T) {
	t.Parallel()
	ctx := newNotifContext(t)
	r := validRoute("")
	rec := postJSON(t, ctx.CreateNotificationRoute, "/api/notifications/routes", r)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing name, got %d", rec.Code)
	}
}

func TestCreateNotificationRoute_MissingEventPattern(t *testing.T) {
	t.Parallel()
	ctx := newNotifContext(t)
	r := validRoute("beta")
	r.Match.EventPattern = ""
	rec := postJSON(t, ctx.CreateNotificationRoute, "/api/notifications/routes", r)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing eventPattern, got %d", rec.Code)
	}
}

func TestCreateNotificationRoute_NoChannels(t *testing.T) {
	t.Parallel()
	ctx := newNotifContext(t)
	r := validRoute("gamma")
	r.Channels = nil
	rec := postJSON(t, ctx.CreateNotificationRoute, "/api/notifications/routes", r)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for no channels, got %d", rec.Code)
	}
}

func TestCreateNotificationRoute_BadChannelType(t *testing.T) {
	t.Parallel()
	ctx := newNotifContext(t)
	r := validRoute("delta")
	r.Channels = []notifications.ChannelSpec{{Type: "carrier-pigeon", Target: "x"}}
	rec := postJSON(t, ctx.CreateNotificationRoute, "/api/notifications/routes", r)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad channel type, got %d", rec.Code)
	}
}

func TestCreateNotificationRoute_MissingChannelTarget(t *testing.T) {
	t.Parallel()
	ctx := newNotifContext(t)
	r := validRoute("eps")
	r.Channels = []notifications.ChannelSpec{{Type: notifications.ChannelWebhook, Target: ""}}
	rec := postJSON(t, ctx.CreateNotificationRoute, "/api/notifications/routes", r)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing channel target, got %d", rec.Code)
	}
}

func TestGetNotificationRoute_FoundAndNotFound(t *testing.T) {
	t.Parallel()
	ctx := newNotifContext(t)
	created := ctx.Notifications.Create(validRoute("zeta"))

	// Found.
	req := withURLParam(httptest.NewRequest("GET", "/api/notifications/routes/"+created.ID, nil), "id", created.ID)
	rec := httptest.NewRecorder()
	ctx.GetNotificationRoute(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var got notifications.Route
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.ID != created.ID {
		t.Errorf("id=%q, want %q", got.ID, created.ID)
	}

	// Not found.
	req2 := withURLParam(httptest.NewRequest("GET", "/api/notifications/routes/nope", nil), "id", "nope")
	rec2 := httptest.NewRecorder()
	ctx.GetNotificationRoute(rec2, req2)
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec2.Code)
	}
}

func TestUpdateNotificationRoute_HappyAndNotFound(t *testing.T) {
	t.Parallel()
	ctx := newNotifContext(t)
	created := ctx.Notifications.Create(validRoute("eta"))

	// Happy update.
	upd := validRoute("eta-renamed")
	buf, _ := json.Marshal(upd)
	req := withURLParam(httptest.NewRequest("PUT", "/api/notifications/routes/"+created.ID, bytes.NewReader(buf)), "id", created.ID)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.UpdateNotificationRoute(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got notifications.Route
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Name != "eta-renamed" {
		t.Errorf("name=%q, want eta-renamed", got.Name)
	}

	// Not found.
	buf2, _ := json.Marshal(upd)
	req2 := withURLParam(httptest.NewRequest("PUT", "/api/notifications/routes/ghost", bytes.NewReader(buf2)), "id", "ghost")
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	ctx.UpdateNotificationRoute(rec2, req2)
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec2.Code)
	}
}

func TestDeleteNotificationRoute_HappyAndNotFound(t *testing.T) {
	t.Parallel()
	ctx := newNotifContext(t)
	created := ctx.Notifications.Create(validRoute("theta"))

	req := withURLParam(httptest.NewRequest("DELETE", "/api/notifications/routes/"+created.ID, nil), "id", created.ID)
	rec := httptest.NewRecorder()
	ctx.DeleteNotificationRoute(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ctx.Notifications.Get(created.ID) != nil {
		t.Error("route should be gone from store")
	}

	// Delete again -> 404.
	req2 := withURLParam(httptest.NewRequest("DELETE", "/api/notifications/routes/"+created.ID, nil), "id", created.ID)
	rec2 := httptest.NewRecorder()
	ctx.DeleteNotificationRoute(rec2, req2)
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on second delete, got %d", rec2.Code)
	}
}

func TestTestNotificationRoute_HappyAndNotFound(t *testing.T) {
	t.Parallel()
	ctx := newNotifContext(t)
	created := ctx.Notifications.Create(validRoute("iota"))

	req := withURLParam(httptest.NewRequest("POST", "/api/notifications/routes/"+created.ID+"/test", nil), "id", created.ID)
	rec := httptest.NewRecorder()
	ctx.TestNotificationRoute(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["dispatched"] != true {
		t.Errorf("expected dispatched=true, got %v", resp["dispatched"])
	}

	// Unknown route -> 404.
	req2 := withURLParam(httptest.NewRequest("POST", "/api/notifications/routes/missing/test", nil), "id", "missing")
	rec2 := httptest.NewRecorder()
	ctx.TestNotificationRoute(rec2, req2)
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec2.Code)
	}
}

func TestTestNotificationRoute_NilRouter503(t *testing.T) {
	t.Parallel()
	ctx := &Context{} // no NotificationsRouter
	req := withURLParam(httptest.NewRequest("POST", "/api/notifications/routes/x/test", nil), "id", "x")
	rec := httptest.NewRecorder()
	ctx.TestNotificationRoute(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestValidChannelType(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		notifications.ChannelTelegram: true,
		notifications.ChannelWebhook:  true,
		notifications.ChannelLog:      true,
		notifications.ChannelExpoPush: false, // expo isn't accepted by the create validator
		"bogus":                       false,
		"":                            false,
	}
	for in, want := range cases {
		if got := validChannelType(in); got != want {
			t.Errorf("validChannelType(%q)=%v, want %v", in, got, want)
		}
	}
}
