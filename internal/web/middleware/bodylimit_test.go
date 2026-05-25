package middleware

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBodyLimit_UnderCap(t *testing.T) {
	called := false
	h := BodyLimit(1024)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if len(body) != 512 {
			t.Fatalf("got %d bytes, want 512", len(body))
		}
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("POST", "/api/anything", bytes.NewReader(make([]byte, 512)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !called {
		t.Fatal("handler not called")
	}
}

func TestBodyLimit_OverCapFailsRead(t *testing.T) {
	// http.MaxBytesReader signals overflow by failing the read inside
	// the handler with a *http.MaxBytesError. Handlers like decodeBody
	// in the office package translate that into a 413 response. The
	// middleware's contract is "wrap the body so reads beyond max fail";
	// the handler decides the status.
	var readErr error
	h := BodyLimit(64)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
	}))
	req := httptest.NewRequest("POST", "/api/anything", bytes.NewReader(make([]byte, 4096)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if readErr == nil {
		t.Fatal("expected read to fail when body exceeds cap")
	}
	var mbe *http.MaxBytesError
	if !errors.As(readErr, &mbe) {
		t.Fatalf("expected *http.MaxBytesError, got %T: %v", readErr, readErr)
	}
}

func TestBodyLimit_SSEPathExempt(t *testing.T) {
	// SSE paths must not have their body wrapped; passing a huge body to
	// /api/events should reach the handler intact (no 413).
	called := false
	h := BodyLimit(64)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read should not error for SSE path: %v", err)
		}
		if len(body) != 4096 {
			t.Fatalf("SSE path body got %d, want 4096", len(body))
		}
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("POST", "/api/events?session=foo", bytes.NewReader(make([]byte, 4096)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !called {
		t.Fatal("SSE handler not called")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("SSE path: expected 200, got %d", rec.Code)
	}
}

func TestBodyLimit_NilBodyNoCrash(t *testing.T) {
	h := BodyLimit(64)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("GET", "/api/health", strings.NewReader(""))
	req.Body = nil
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for nil body, got %d", rec.Code)
	}
}
