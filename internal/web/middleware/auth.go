package middleware

import (
	"net/http"
	"strings"

	"github.com/claudeman/jarvis-go/internal/web/auth"
)

// Auth middleware validates session cookies on every request.
// If no password is configured, all requests pass through.
// Exempt paths: /api/health, /api/auth/*, and static files. /api/hook-event
// is exempt here but guarded by HMAC signature verification in its handler.
// /api/office/agent/* is NOT exempt; it's guarded by AgentTokenAuth below.
//
// Agents running inside spawned sessions also have a valid agent token
// (JARVIS_AGENT_TOKEN) but no session cookie. jarvis-mcp calls routes like
// /api/status and /api/config from within those sessions, so we accept the
// agent token as a valid alternative form of authentication on any route
// that Auth guards. This mirrors what the `cm` CLI already does.
func Auth(am *auth.Manager) func(http.Handler) http.Handler {
	return AuthWithAgentToken(am, nil)
}

// AuthWithAgentToken is like Auth but additionally accepts a valid agent
// token as authentication. Pass nil for atm to get the strict cookie-only
// behavior.
func AuthWithAgentToken(am *auth.Manager, atm *auth.AgentTokenManager) func(http.Handler) http.Handler {
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

			// Fallback: accept a valid agent token so jarvis-mcp tools that
			// touch general status/config routes work inside spawned sessions.
			if atm != nil {
				presented := r.Header.Get("X-Jarvis-Agent-Token")
				if presented == "" {
					if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
						presented = strings.TrimPrefix(h, "Bearer ")
					}
				}
				if atm.Validate(presented) {
					next.ServeHTTP(w, r)
					return
				}
			}

			// Unauthorized
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"success":false,"error":"authentication required"}`))
		})
	}
}

// AgentTokenAuth requires a valid X-Jarvis-Agent-Token header (or Bearer
// token). Used to protect the /api/office/agent/* endpoints that the `cm`
// CLI calls from inside spawned sessions. The token is generated once on
// server startup and piped into sessions via JARVIS_AGENT_TOKEN env var,
// so legitimate agent traffic carries it and external callers cannot.
func AgentTokenAuth(atm *auth.AgentTokenManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			presented := r.Header.Get("X-Jarvis-Agent-Token")
			if presented == "" {
				if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
					presented = strings.TrimPrefix(h, "Bearer ")
				}
			}
			if atm.Validate(presented) {
				next.ServeHTTP(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"success":false,"error":"agent token required"}`))
		})
	}
}

// isAuthExempt returns true for paths that do not require authentication.
func isAuthExempt(path string) bool {
	// Health + readiness checks
	if path == "/api/health" || path == "/api/ready" {
		return true
	}
	// All auth endpoints (status, login, logout, setup, change-password)
	if strings.HasPrefix(path, "/api/auth/") || path == "/api/auth" {
		return true
	}
	// Hook events from Claude CLI -- HMAC-signed in handler, not cookie-auth.
	if path == "/api/hook-event" {
		return true
	}
	return false
}
