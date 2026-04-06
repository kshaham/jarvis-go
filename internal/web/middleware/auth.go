package middleware

import (
	"net/http"
	"strings"

	"github.com/claudeman/jarvis-go/internal/web/auth"
)

// Auth middleware validates session cookies on every request.
// If no password is configured, all requests pass through.
// Exempt paths: /api/health, /api/auth/*, /api/hook-event, and static files.
func Auth(am *auth.Manager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// If no password is configured, auth is disabled -- pass through
			if !am.IsPasswordConfigured() {
				next.ServeHTTP(w, r)
				return
			}

			path := r.URL.Path

			// Exempt paths that must be accessible without authentication
			if isAuthExempt(path) {
				next.ServeHTTP(w, r)
				return
			}

			// Validate session cookie
			if am.ValidateSessionFromRequest(r) {
				next.ServeHTTP(w, r)
				return
			}

			// Unauthorized
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"success":false,"error":"authentication required"}`))
		})
	}
}

// isAuthExempt returns true for paths that do not require authentication.
func isAuthExempt(path string) bool {
	// Health check
	if path == "/api/health" {
		return true
	}
	// All auth endpoints (status, login, logout, setup, change-password)
	if strings.HasPrefix(path, "/api/auth/") || path == "/api/auth" {
		return true
	}
	// Hook events from Claude CLI
	if path == "/api/hook-event" {
		return true
	}
	// Agent callbacks (called by cm CLI from sessions, no browser auth)
	if strings.HasPrefix(path, "/api/office/agent/") {
		return true
	}
	return false
}
