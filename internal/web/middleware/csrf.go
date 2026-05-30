package middleware

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/kshaham/jarvis/internal/safego"
)

// CSRFStore manages CSRF tokens with 1-hour TTL and multi-use validation.
//
// Hot-path optimisation: tokens are keyed by sha256(rawToken) so Validate
// is an O(1) map lookup instead of an O(N) linear scan. The map is guarded
// by a sync.RWMutex because reads (Validate) are far more frequent than
// writes (Generate, cleanup). The raw token is never stored — only its hash
// — so a snapshot of the in-memory map reveals nothing usable.
type CSRFStore struct {
	mu     sync.RWMutex
	tokens map[string]time.Time // sha256hex(token) -> expiry
}

// NewCSRFStore creates a new CSRF token store.
// The cleanup goroutine exits when the provided context is cancelled.
func NewCSRFStore(ctx context.Context) *CSRFStore {
	s := &CSRFStore{tokens: make(map[string]time.Time)}
	safego.Go("csrf.cleanupLoop", func() { s.cleanupLoop(ctx) })
	return s
}

// csrfTokenHash returns the lower-case hex SHA-256 of a raw CSRF token.
// Centralised so Generate and Validate share the exact same encoding.
func csrfTokenHash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// Generate creates a new CSRF token and stores its hash in the map.
// Returns the raw token which the client must echo back on mutating requests.
func (s *CSRFStore) Generate() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	token := hex.EncodeToString(b)
	h := csrfTokenHash(token)

	s.mu.Lock()
	s.tokens[h] = time.Now().Add(1 * time.Hour)
	s.mu.Unlock()

	return token
}

// Validate checks and validates a CSRF token (multi-use until TTL expires).
// O(1) map lookup on the hash; constant-time hash comparison is inherent
// because the map key is a fixed-length hex string of the SHA-256 digest.
func (s *CSRFStore) Validate(token string) bool {
	if token == "" {
		return false
	}
	h := csrfTokenHash(token)

	s.mu.RLock()
	expiry, ok := s.tokens[h]
	s.mu.RUnlock()

	if !ok {
		return false
	}
	if time.Now().After(expiry) {
		// Expired — delete under write-lock (best-effort lazy eviction;
		// the cleanup goroutine will remove it on the next tick if we miss).
		s.mu.Lock()
		if exp, still := s.tokens[h]; still && time.Now().After(exp) {
			delete(s.tokens, h)
		}
		s.mu.Unlock()
		return false
	}
	// Token is valid — keep it alive (multi-use until TTL expires).
	// The frontend reuses the same token for multiple rapid requests.
	return true
}

func (s *CSRFStore) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			now := time.Now()
			for h, expiry := range s.tokens {
				if now.After(expiry) {
					delete(s.tokens, h)
				}
			}
			s.mu.Unlock()
		}
	}
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
