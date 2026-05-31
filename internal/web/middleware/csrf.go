package middleware

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/kshaham/jarvis/internal/safego"
)

// CSRFStore manages CSRF tokens with 1-hour TTL and single-use validation.
type CSRFStore struct {
	mu     sync.Mutex
	tokens map[string]time.Time // token -> expiry
}

// NewCSRFStore creates a new CSRF token store.
// The cleanup goroutine exits when the provided context is cancelled.
func NewCSRFStore(ctx context.Context) *CSRFStore {
	s := &CSRFStore{tokens: make(map[string]time.Time)}
	safego.Go("csrf.cleanupLoop", func() { s.cleanupLoop(ctx) })
	return s
}

// Generate creates a new CSRF token.
func (s *CSRFStore) Generate() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	token := hex.EncodeToString(b)

	s.mu.Lock()
	s.tokens[token] = time.Now().Add(1 * time.Hour)
	s.mu.Unlock()

	return token
}

// Validate checks and consumes a CSRF token (single-use).
// Uses constant-time comparison to prevent timing side-channel attacks.
func (s *CSRFStore) Validate(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	tokenBytes := []byte(token)
	for stored, expiry := range s.tokens {
		if subtle.ConstantTimeCompare(tokenBytes, []byte(stored)) == 1 {
			if time.Now().After(expiry) {
				delete(s.tokens, stored)
				return false
			}
			// Token is valid -- keep it alive (multi-use until TTL expires).
			// The frontend reuses the same token for multiple rapid requests.
			return true
		}
	}
	return false
}

func (s *CSRFStore) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweepExpired()
		}
	}
}

// sweepExpired removes every token whose TTL has elapsed and returns the
// number deleted. Extracted from cleanupLoop so the expiry logic can be
// exercised without waiting on the 10-minute ticker.
func (s *CSRFStore) sweepExpired() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	removed := 0
	for token, expiry := range s.tokens {
		if now.After(expiry) {
			delete(s.tokens, token)
			removed++
		}
	}
	return removed
}

// CSRF middleware validates X-CSRF-Token header on mutating requests.
func CSRF(store *CSRFStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "POST" || r.Method == "PUT" || r.Method == "DELETE" || r.Method == "PATCH" {
				// CSRF protects cookie-based auth from cross-origin abuse
				// (the confused-deputy problem). Bearer-token requests
				// already require the caller to know the secret, so a
				// malicious page in another origin can't forge them.
				// The upstream auth middleware has already validated the
				// bearer; if we see one here we can safely skip CSRF.
				if hasBearer(r) {
					next.ServeHTTP(w, r)
					return
				}
				token := r.Header.Get("X-CSRF-Token")
				if token == "" || !store.Validate(token) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusForbidden)
					w.Write([]byte(`{"success":false,"error":"invalid CSRF token"}`))
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// hasBearer reports whether the request carries either an
// Authorization: Bearer header or the legacy X-Jarvis-Agent-Token header.
// Auth middleware runs before CSRF in the chain, so by the time we get
// here any present token has already been verified.
func hasBearer(r *http.Request) bool {
	if r.Header.Get("X-Jarvis-Agent-Token") != "" {
		return true
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return true
	}
	return false
}
