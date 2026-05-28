package middleware

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/kshaham/jarvis/internal/metrics"
)

// RequestLogger logs each HTTP request with method, path, status, duration,
// and correlation ID (set by the RequestID middleware). Also records
// Prometheus counters/histograms for HTTP traffic observability.
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &statusRecorder{ResponseWriter: w, statusCode: 200}
		next.ServeHTTP(wrapped, r)
		duration := time.Since(start)
		reqID := RequestIDFromContext(r.Context())

		// Normalize the route label so high-cardinality fields (session IDs,
		// job IDs, etc.) don't explode Prometheus series. Anything past the
		// second path segment becomes a placeholder.
		route := normalizeRoute(r.URL.Path)
		metrics.HTTPRequests.WithLabelValues(r.Method, route, strconv.Itoa(wrapped.statusCode)).Inc()
		metrics.HTTPDuration.WithLabelValues(route).Observe(duration.Seconds())

		// SSE connections are long-lived; log once at connect time with ID.
		if r.URL.Path == "/api/events" {
			slog.Info("SSE connection", "request_id", reqID, "status", wrapped.statusCode)
			return
		}

		slog.Info("request",
			"request_id", reqID,
			"method", r.Method,
			"path", r.URL.Path,
			"status", wrapped.statusCode,
			"duration", duration.String(),
		)
	})
}

// normalizeRoute collapses variable path segments to "/:id" / "/:name" so the
// HTTPRequests label cardinality stays bounded. Keep the first two segments
// so per-feature latency is still legible (e.g., /api/sessions/:id).
func normalizeRoute(path string) string {
	if len(path) > 80 {
		path = path[:80]
	}
	// Keep /api/sessions/:id etc. -- collapse the third segment if it looks
	// like an ID (long hex/UUID/numeric chunk). This is a lightweight
	// heuristic that's fine for metrics labels.
	segments := splitSegments(path)
	if len(segments) < 3 {
		return path
	}
	for i := 2; i < len(segments); i++ {
		if looksLikeID(segments[i]) {
			segments[i] = ":id"
		}
	}
	return joinSegments(segments)
}

func splitSegments(p string) []string {
	if p == "" || p == "/" {
		return nil
	}
	out := []string{""}
	start := 1
	for i := 1; i < len(p); i++ {
		if p[i] == '/' {
			out = append(out, p[start:i])
			start = i + 1
		}
	}
	out = append(out, p[start:])
	return out
}

func joinSegments(s []string) string {
	if len(s) == 0 {
		return "/"
	}
	out := ""
	for i, seg := range s {
		if i == 0 {
			continue
		}
		out += "/" + seg
	}
	if out == "" {
		return "/"
	}
	return out
}

func looksLikeID(s string) bool {
	// UUIDs, hex IDs, long numeric IDs -- collapse them.
	if len(s) >= 8 {
		hex := 0
		for _, c := range s {
			if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') || c == '-' {
				hex++
			}
		}
		if hex == len(s) {
			return true
		}
	}
	return false
}

type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.statusCode = code
	r.ResponseWriter.WriteHeader(code)
}

// Flush implements http.Flusher for SSE support through middleware.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
