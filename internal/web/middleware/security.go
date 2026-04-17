package middleware

import "net/http"

// contentSecurityPolicy is the CSP applied to every response. Notes:
//   - script-src 'self' plus the xterm + search addon CDN (loaded by index.html)
//   - 'unsafe-inline' on scripts is needed because several onclick="..." and
//     inline <script> blocks populate UI state; tightening requires a
//     frontend refactor that's out of scope for this hardening pass.
//   - style-src 'self' 'unsafe-inline' because many inline style attributes
//     drive dynamic UI (session colors, tab states).
//   - connect-src self plus wss/ws for SSE + Gemini Live WebSocket.
//   - img-src allows data: for icons and blob: for generated images.
//   - frame-ancestors 'none' (plus existing X-Frame-Options) forbids embedding.
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net; " +
	"style-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net; " +
	"connect-src 'self' wss: ws: https://generativelanguage.googleapis.com; " +
	"img-src 'self' data: blob:; " +
	"font-src 'self' data:; " +
	"frame-ancestors 'none'; " +
	"base-uri 'self'"

// SecurityHeaders adds standard security headers to all responses.
// HSTS is set only on requests that actually arrived over TLS, so the
// parallel HTTP listener doesn't advertise HSTS (browsers would ignore
// it anyway, but it's cleaner to gate per-request).
func SecurityHeaders(_ bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
			w.Header().Set("Content-Security-Policy", contentSecurityPolicy)
			if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
				w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}
