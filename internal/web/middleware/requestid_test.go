package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestID_GeneratesWhenAbsent(t *testing.T) {
	var captured string
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = RequestIDFromContext(r.Context())
	}))

	req := httptest.NewRequest("GET", "/api/health", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if captured == "" {
		t.Fatal("expected generated request ID in context")
	}
	if rr.Header().Get("X-Request-ID") != captured {
		t.Errorf("expected response header to match context ID; got %q vs %q",
			rr.Header().Get("X-Request-ID"), captured)
	}
	if len(captured) != 16 {
		t.Errorf("expected 16-char ID, got %d chars", len(captured))
	}
}

func TestRequestID_HonorsIncomingHeader(t *testing.T) {
	const incoming = "upstream-trace-id-abc"
	var captured string
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = RequestIDFromContext(r.Context())
	}))

	req := httptest.NewRequest("GET", "/api/health", nil)
	req.Header.Set("X-Request-ID", incoming)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if captured != incoming {
		t.Errorf("expected incoming ID preserved; got %q", captured)
	}
	if rr.Header().Get("X-Request-ID") != incoming {
		t.Errorf("response header should echo incoming ID")
	}
}
