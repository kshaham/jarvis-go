package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRecovery_CatchesPanicAndReturns500(t *testing.T) {
	buf := captureSlog(t)

	h := Recovery(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))
	req := httptest.NewRequest("GET", "/api/explode", nil)
	rec := httptest.NewRecorder()

	// Must not propagate the panic.
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "internal server error") {
		t.Errorf("body = %q, want it to mention internal server error", rec.Body.String())
	}
	// The panic should be logged with the path.
	logged := buf.String()
	if !strings.Contains(logged, "panic recovered") {
		t.Errorf("expected 'panic recovered' log entry, got %q", logged)
	}
	if !strings.Contains(logged, "/api/explode") {
		t.Errorf("expected request path in log, got %q", logged)
	}
}

func TestRecovery_PassesThroughWhenNoPanic(t *testing.T) {
	called := false
	h := Recovery(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("fine"))
	}))
	req := httptest.NewRequest("GET", "/api/ok", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !called {
		t.Fatal("handler should run normally")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "fine" {
		t.Errorf("body = %q", rec.Body.String())
	}
}

func TestRecovery_PanicWithErrorValue(t *testing.T) {
	captureSlog(t)
	h := Recovery(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(http.ErrAbortHandler.Error() + "-ish")
	}))
	req := httptest.NewRequest("POST", "/api/x", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}
