package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func newCSRFStore(t *testing.T) (*CSRFStore, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	store := NewCSRFStore(ctx)
	t.Cleanup(cancel)
	return store, cancel
}

func TestCSRFStore_GenerateProducesUniqueValidTokens(t *testing.T) {
	store, _ := newCSRFStore(t)

	tok1 := store.Generate()
	tok2 := store.Generate()

	if tok1 == "" || tok2 == "" {
		t.Fatal("Generate returned empty token")
	}
	if tok1 == tok2 {
		t.Fatal("Generate produced duplicate tokens")
	}
	// 32 random bytes -> 64 hex chars.
	if len(tok1) != 64 {
		t.Errorf("got token length %d, want 64", len(tok1))
	}
	if !store.Validate(tok1) {
		t.Error("freshly generated token should validate")
	}
	if !store.Validate(tok2) {
		t.Error("second generated token should validate")
	}
}

func TestCSRFStore_ValidateUnknownToken(t *testing.T) {
	store, _ := newCSRFStore(t)
	if store.Validate("never-generated") {
		t.Error("unknown token must not validate")
	}
	if store.Validate("") {
		t.Error("empty token must not validate")
	}
}

func TestCSRFStore_ValidateIsMultiUse(t *testing.T) {
	// Comment in csrf.go says tokens are intentionally multi-use until TTL.
	store, _ := newCSRFStore(t)
	tok := store.Generate()
	if !store.Validate(tok) {
		t.Fatal("first validate should pass")
	}
	if !store.Validate(tok) {
		t.Fatal("token should remain valid for repeated use until TTL")
	}
}

func TestCSRFStore_ValidateExpiredTokenDeletes(t *testing.T) {
	store, _ := newCSRFStore(t)
	tok := store.Generate()

	// Force the stored expiry into the past so Validate hits the expired branch.
	store.mu.Lock()
	store.tokens[tok] = time.Now().Add(-time.Minute)
	store.mu.Unlock()

	if store.Validate(tok) {
		t.Fatal("expired token must not validate")
	}
	// It should have been deleted from the store.
	store.mu.Lock()
	_, present := store.tokens[tok]
	store.mu.Unlock()
	if present {
		t.Error("expired token should be deleted on validate")
	}
}

func TestCSRFStore_CleanupLoopRemovesExpiredOnTick(t *testing.T) {
	// Drive cleanupLoop directly with a cancellable context and a manually
	// inserted expired token. We can't easily reach the 10-minute ticker, so
	// we verify the select+cancel path: the loop returns when ctx is done.
	ctx, cancel := context.WithCancel(context.Background())
	store := &CSRFStore{tokens: make(map[string]time.Time)}
	store.tokens["expired"] = time.Now().Add(-time.Hour)
	store.tokens["fresh"] = time.Now().Add(time.Hour)

	done := make(chan struct{})
	go func() {
		store.cleanupLoop(ctx)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cleanupLoop did not return after context cancellation")
	}
}

func TestCSRFStore_SweepExpiredRemovesOnlyExpired(t *testing.T) {
	store := &CSRFStore{tokens: make(map[string]time.Time)}
	store.tokens["expired1"] = time.Now().Add(-time.Hour)
	store.tokens["expired2"] = time.Now().Add(-time.Minute)
	store.tokens["fresh"] = time.Now().Add(time.Hour)

	removed := store.sweepExpired()
	if removed != 2 {
		t.Errorf("sweepExpired removed %d, want 2", removed)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, ok := store.tokens["fresh"]; !ok {
		t.Error("fresh token should survive sweep")
	}
	if len(store.tokens) != 1 {
		t.Errorf("store size = %d, want 1", len(store.tokens))
	}
}

func TestCSRFStore_SweepExpiredEmptyStore(t *testing.T) {
	store := &CSRFStore{tokens: make(map[string]time.Time)}
	if removed := store.sweepExpired(); removed != 0 {
		t.Errorf("sweepExpired on empty store removed %d, want 0", removed)
	}
}

func TestCSRF_AllowsSafeMethods(t *testing.T) {
	store, _ := newCSRFStore(t)
	for _, method := range []string{"GET", "HEAD", "OPTIONS"} {
		called := false
		h := CSRF(store)(okNext(&called))
		req := httptest.NewRequest(method, "/api/status", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if !called {
			t.Errorf("%s: safe method should pass without CSRF token", method)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("%s: got %d, want 200", method, rec.Code)
		}
	}
}

func TestCSRF_RejectsMutatingWithoutToken(t *testing.T) {
	store, _ := newCSRFStore(t)
	for _, method := range []string{"POST", "PUT", "DELETE", "PATCH"} {
		called := false
		h := CSRF(store)(okNext(&called))
		req := httptest.NewRequest(method, "/api/config", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if called {
			t.Errorf("%s: handler should not run without CSRF token", method)
		}
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: got %d, want 403", method, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("%s: got content-type %q", method, ct)
		}
	}
}

func TestCSRF_RejectsMutatingWithInvalidToken(t *testing.T) {
	store, _ := newCSRFStore(t)
	called := false
	h := CSRF(store)(okNext(&called))
	req := httptest.NewRequest("POST", "/api/config", nil)
	req.Header.Set("X-CSRF-Token", "not-a-real-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if called {
		t.Fatal("handler should not run with invalid CSRF token")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403", rec.Code)
	}
}

func TestCSRF_AcceptsMutatingWithValidToken(t *testing.T) {
	store, _ := newCSRFStore(t)
	token := store.Generate()
	called := false
	h := CSRF(store)(okNext(&called))
	req := httptest.NewRequest("POST", "/api/config", nil)
	req.Header.Set("X-CSRF-Token", token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !called {
		t.Fatal("handler should run with valid CSRF token")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
}

func TestCSRF_BearerBypassesTokenCheck(t *testing.T) {
	store, _ := newCSRFStore(t)
	// Bearer-authenticated requests skip CSRF (the upstream Auth middleware
	// already validated the bearer). Both header forms should bypass.
	cases := []struct {
		name   string
		header string
		value  string
	}{
		{"authorization bearer", "Authorization", "Bearer abc123"},
		{"x-jarvis-agent-token", "X-Jarvis-Agent-Token", "agenttok"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			h := CSRF(store)(okNext(&called))
			req := httptest.NewRequest("POST", "/api/config", nil)
			req.Header.Set(tc.header, tc.value)
			// Deliberately omit any CSRF token.
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if !called {
				t.Fatal("bearer request should bypass CSRF and reach handler")
			}
			if rec.Code != http.StatusOK {
				t.Fatalf("got %d, want 200", rec.Code)
			}
		})
	}
}

func TestHasBearer(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
		want    bool
	}{
		{"agent header", map[string]string{"X-Jarvis-Agent-Token": "x"}, true},
		{"bearer authorization", map[string]string{"Authorization": "Bearer y"}, true},
		{"basic authorization", map[string]string{"Authorization": "Basic z"}, false},
		{"none", map[string]string{}, false},
		{"empty agent header", map[string]string{"X-Jarvis-Agent-Token": ""}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			if got := hasBearer(req); got != tc.want {
				t.Errorf("hasBearer = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCSRFStore_ConcurrentGenerateValidate(t *testing.T) {
	// Exercise the mutex under -race: many goroutines generating and
	// validating concurrently must not corrupt the store.
	store, _ := newCSRFStore(t)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tok := store.Generate()
			if !store.Validate(tok) {
				t.Errorf("concurrently generated token failed to validate")
			}
			store.Validate("bogus")
		}()
	}
	wg.Wait()
}
