package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

// requestIDHeader is both accepted from upstream and echoed back. Matches
// the de-facto convention used by nginx (sometimes capitalized X-Request-Id);
// Go's http.Header is canonicalized so we only need one constant here.
const requestIDHeader = "X-Request-ID"

// requestIDCtxKey is the unexported context key type. Handlers retrieve the
// ID via RequestIDFromContext.
type requestIDCtxKey struct{}

// RequestID generates or propagates a correlation ID for every incoming
// request. An incoming X-Request-ID header is honored so upstream proxies
// can preserve trace context; if absent, a fresh 16-char hex ID is minted.
// The ID is:
//
//   - echoed in the response header for the client to capture,
//   - stored in the request context for downstream handlers / slog, and
//   - accessible via RequestIDFromContext(ctx).
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestIDHeader)
		if id == "" {
			id = newRequestID()
		}
		w.Header().Set(requestIDHeader, id)
		ctx := context.WithValue(r.Context(), requestIDCtxKey{}, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestIDFromContext returns the correlation ID attached by the
// RequestID middleware, or the empty string when not present.
func RequestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDCtxKey{}).(string); ok {
		return v
	}
	return ""
}

// newRequestID returns 16 hex chars (8 random bytes). Short enough to fit
// comfortably in log lines; long enough to collision-resist within a session.
func newRequestID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Fall back to the zero value rather than panicking mid-request.
		return "00000000"
	}
	return hex.EncodeToString(b)
}
