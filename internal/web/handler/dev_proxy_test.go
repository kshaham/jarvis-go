package handler

// Coverage for the operator live-preview reverse proxy (dev_proxy.go). The
// proxy forwards /api/office/dev-proxy/{slot}/* to a slot's announced URL so
// the operator's browser can view a dev server bound to a docker-internal,
// unpublished port.

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// devProxyRouter mounts OfficeDevProxy at the production path so chi populates
// the {slot} and wildcard URL params exactly as the real server does.
func devProxyRouter(ctx *Context) http.Handler {
	r := chi.NewRouter()
	r.Handle("/api/office/dev-proxy/{slot}/*", http.HandlerFunc(ctx.OfficeDevProxy))
	return r
}

func TestOfficeDevProxy_ForwardsUpstreamBody(t *testing.T) {
	// Arrange: a fake dev server and a slot pointing at it (force=true so the
	// localhost upstream is accepted by SetDevInstance).
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hello" {
			t.Errorf("upstream saw path %q, want /hello (prefix not stripped)", r.URL.Path)
		}
		_, _ = io.WriteString(w, "DEV-OK")
	}))
	defer upstream.Close()

	ctx := newTimelineContext(t)
	if err := ctx.OfficeManager.SetDevInstance("app", upstream.URL, 12345, true); err != nil {
		t.Fatalf("SetDevInstance: %v", err)
	}

	// Act
	req := httptest.NewRequest("GET", "/api/office/dev-proxy/app/hello", nil)
	rec := httptest.NewRecorder()
	devProxyRouter(ctx).ServeHTTP(rec, req)

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != "DEV-OK" {
		t.Errorf("body=%q, want DEV-OK", got)
	}
}

func TestOfficeDevProxy_UnknownSlotReturns502(t *testing.T) {
	// Arrange: no slot registered.
	ctx := newTimelineContext(t)

	// Act
	req := httptest.NewRequest("GET", "/api/office/dev-proxy/ghost/", nil)
	rec := httptest.NewRecorder()
	devProxyRouter(ctx).ServeHTTP(rec, req)

	// Assert
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status=%d, want 502 for unknown slot: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficeDevProxy_InvalidSlotReturns400(t *testing.T) {
	// Arrange
	ctx := newTimelineContext(t)

	// Act: a slot name with a disallowed character.
	req := httptest.NewRequest("GET", "/api/office/dev-proxy/bad..slot/", nil)
	rec := httptest.NewRecorder()
	devProxyRouter(ctx).ServeHTTP(rec, req)

	// Assert
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400 for invalid slot: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficeDevProxy_DeadUpstreamReturns502(t *testing.T) {
	// Arrange: a slot pointing at a server we immediately close, so the dial
	// fails and the proxy ErrorHandler fires.
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close()

	ctx := newTimelineContext(t)
	if err := ctx.OfficeManager.SetDevInstance("app", deadURL, 222, true); err != nil {
		t.Fatalf("SetDevInstance: %v", err)
	}

	// Act
	req := httptest.NewRequest("GET", "/api/office/dev-proxy/app/", nil)
	rec := httptest.NewRecorder()
	devProxyRouter(ctx).ServeHTTP(rec, req)

	// Assert
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status=%d, want 502 for dead upstream: %s", rec.Code, rec.Body.String())
	}
}
