package handler

// Round-3 branch fills for the notification-route handlers:
//   - CreateNotificationRoute nil-store 503 (the only mutator missing it)
//   - Create + Update decode-failure 400 arms (malformed JSON body)

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateNotificationRoute_NilStore503(t *testing.T) {
	ctx := &Context{} // Notifications is nil
	req := httptest.NewRequest("POST", "/api/notifications/routes", strings.NewReader(`{"name":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.CreateNotificationRoute(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 with nil store, got %d", rec.Code)
	}
}

func TestCreateNotificationRoute_BadJSON400(t *testing.T) {
	ctx := newNotifContext(t)
	req := httptest.NewRequest("POST", "/api/notifications/routes", strings.NewReader("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.CreateNotificationRoute(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on malformed Create body, got %d", rec.Code)
	}
}

func TestUpdateNotificationRoute_BadJSON400(t *testing.T) {
	ctx := newNotifContext(t)
	req := httptest.NewRequest("PUT", "/api/notifications/routes/r1", strings.NewReader("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	req = withURLParam(req, "id", "r1")
	rec := httptest.NewRecorder()
	ctx.UpdateNotificationRoute(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on malformed Update body, got %d", rec.Code)
	}
}
