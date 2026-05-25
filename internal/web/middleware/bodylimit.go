package middleware

import (
	"net/http"
	"strings"
)

// DefaultMaxBodyBytes caps every non-SSE request body at 10 MB. Large
// enough for whiteboard uploads, screenshot evidence, and snapshot
// restore bodies; small enough that a 1 GB POST can't OOM the server
// before per-handler readers kick in.
const DefaultMaxBodyBytes = 10 << 20

// sseExemptPrefixes are paths that need unbounded request bodies
// because they keep long-lived streams open. Add to this list only
// when a new SSE / streaming endpoint lands.
var sseExemptPrefixes = []string{
	"/api/events",
	"/api/sse",
}

// BodyLimit returns middleware that caps the request body at `max`
// bytes via http.MaxBytesReader. SSE-style endpoints are exempt
// because their "body" is the client-side keepalive channel and
// truncating it would close the stream prematurely.
//
// Per-handler readers (e.g. claudehooks' 1 MB readLimited) still
// apply on top; this middleware is the outermost ceiling.
func BodyLimit(max int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isSSEExempt(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, max)
			}
			next.ServeHTTP(w, r)
		})
	}
}

func isSSEExempt(path string) bool {
	for _, p := range sseExemptPrefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}
