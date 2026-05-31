package middleware

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeaders_AlwaysSetBaseHeaders(t *testing.T) {
	called := false
	h := SecurityHeaders(false)(okNext(&called))

	req := httptest.NewRequest("GET", "/api/status", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !called {
		t.Fatal("next handler should run")
	}
	hdr := rec.Header()
	checks := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
	}
	for k, want := range checks {
		if got := hdr.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	csp := hdr.Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("CSP header missing")
	}
	for _, frag := range []string{"default-src 'self'", "frame-ancestors 'none'", "base-uri 'self'"} {
		if !strings.Contains(csp, frag) {
			t.Errorf("CSP missing fragment %q; got %q", frag, csp)
		}
	}
}

func TestSecurityHeaders_NoHSTSOnPlainHTTP(t *testing.T) {
	called := false
	h := SecurityHeaders(false)(okNext(&called))
	req := httptest.NewRequest("GET", "/api/status", nil) // no TLS, no proxy header
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("HSTS should be absent on plain HTTP, got %q", got)
	}
}

func TestSecurityHeaders_HSTSOnTLS(t *testing.T) {
	called := false
	h := SecurityHeaders(true)(okNext(&called))
	req := httptest.NewRequest("GET", "/api/status", nil)
	req.TLS = &tls.ConnectionState{} // simulate a TLS connection
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	got := rec.Header().Get("Strict-Transport-Security")
	if got == "" {
		t.Fatal("expected HSTS header on TLS request")
	}
	if !strings.Contains(got, "max-age=31536000") || !strings.Contains(got, "includeSubDomains") {
		t.Errorf("unexpected HSTS value %q", got)
	}
}

func TestSecurityHeaders_HSTSOnForwardedProtoHTTPS(t *testing.T) {
	called := false
	h := SecurityHeaders(false)(okNext(&called))
	req := httptest.NewRequest("GET", "/api/status", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Strict-Transport-Security"); got == "" {
		t.Error("expected HSTS header when behind https reverse proxy")
	}
}

func TestSecurityHeaders_ForwardedProtoHTTPNoHSTS(t *testing.T) {
	called := false
	h := SecurityHeaders(false)(okNext(&called))
	req := httptest.NewRequest("GET", "/api/status", nil)
	req.Header.Set("X-Forwarded-Proto", "http")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("X-Forwarded-Proto=http must not advertise HSTS, got %q", got)
	}
}

func TestSecurityHeaders_PassesThroughStatusAndBody(t *testing.T) {
	h := SecurityHeaders(false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("payload"))
	}))
	req := httptest.NewRequest("GET", "/api/status", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Errorf("status = %d, want 202", rec.Code)
	}
	if rec.Body.String() != "payload" {
		t.Errorf("body = %q", rec.Body.String())
	}
}
