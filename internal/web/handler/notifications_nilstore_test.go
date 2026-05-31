package handler

// Coverage for the nil-store 503 guards on the notification-route handlers
// that didn't already have one tested.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNotificationRoutes_NilStore503(t *testing.T) {
	ctx := &Context{} // Notifications is nil

	cases := []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request)
		method  string
		body    string
	}{
		{"GetNotificationRoute", ctx.GetNotificationRoute, "GET", ""},
		{"UpdateNotificationRoute", ctx.UpdateNotificationRoute, "PUT", `{"name":"x"}`},
		{"DeleteNotificationRoute", ctx.DeleteNotificationRoute, "DELETE", ""},
		{"TestNotificationRoute", ctx.TestNotificationRoute, "POST", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var req *http.Request
			if c.body != "" {
				req = httptest.NewRequest(c.method, "/api/notifications/routes/r1", strings.NewReader(c.body))
				req.Header.Set("Content-Type", "application/json")
			} else {
				req = httptest.NewRequest(c.method, "/api/notifications/routes/r1", nil)
			}
			req = withURLParam(req, "id", "r1")
			rec := httptest.NewRecorder()
			c.handler(rec, req)
			if rec.Code != http.StatusServiceUnavailable {
				t.Errorf("%s: expected 503 with nil store, got %d", c.name, rec.Code)
			}
		})
	}
}
