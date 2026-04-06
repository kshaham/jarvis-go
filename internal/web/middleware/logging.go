package middleware

import (
	"log/slog"
	"net/http"
	"time"
)

// RequestLogger logs each HTTP request with method, path, status, and duration.
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &statusRecorder{ResponseWriter: w, statusCode: 200}
		next.ServeHTTP(wrapped, r)
		duration := time.Since(start)

		// Log SSE connections briefly (they are long-lived, only log once on connect)
		if r.URL.Path == "/api/events" {
			slog.Info("SSE connection", "status", wrapped.statusCode)
			return
		}

		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", wrapped.statusCode,
			"duration", duration.String(),
		)
	})
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
