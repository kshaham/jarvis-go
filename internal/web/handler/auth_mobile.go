package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/claudeman/jarvis-go/internal/web/auth"
	"github.com/claudeman/jarvis-go/internal/web/request"
)

// Mobile-app authentication. These endpoints sit alongside the session-cookie
// flow in monitoring_extended.go but issue per-device bearer tokens that the
// React Native client stores in iOS Keychain / Android Keystore. The middleware
// in internal/web/middleware/auth.go accepts these tokens on the same routes
// that today accept session cookies, so the mobile app can drive the entire
// office surface area.

const maxMobileDeviceNameLen = 80

// AuthMobileLogin issues a new mobile bearer token for the given device.
// POST /api/auth/mobile/login
// Body:   {"password": "...", "deviceName": "My iPhone"}
// Reply:  {"success": true, "token": "...", "deviceId": "...", "expiresAt": <unix-ms>}
//
// Rate-limited per source IP using the same backoff as the existing login
// flow (5 failures -> 5m block, 10 -> 30m). Token is returned exactly once.
func (ctx *Context) AuthMobileLogin(w http.ResponseWriter, r *http.Request) {
	if ctx.MobileTokenMgr == nil {
		request.WriteError(w, http.StatusServiceUnavailable, "mobile auth not initialised")
		return
	}

	ip := auth.ClientIP(r)
	blocked, retryAfterMs := ctx.AuthManager.IsRateLimited(ip)
	if blocked {
		request.WriteJSON(w, http.StatusTooManyRequests, map[string]interface{}{
			"success":      false,
			"error":        "Too many failed attempts",
			"retryAfterMs": retryAfterMs,
		})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	var body struct {
		Password   string `json:"password"`
		DeviceName string `json:"deviceName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.Password == "" {
		request.WriteJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "Password is required",
		})
		return
	}
	if len(body.Password) > 128 {
		request.WriteJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "Invalid password",
		})
		return
	}
	deviceName := strings.TrimSpace(body.DeviceName)
	if len(deviceName) > maxMobileDeviceNameLen {
		deviceName = deviceName[:maxMobileDeviceNameLen]
	}

	valid, err := ctx.AuthManager.VerifyPassword(body.Password)
	if err != nil {
		slog.Error("Mobile login: password verification failed", "error", err)
		request.WriteError(w, http.StatusInternalServerError, "authentication error")
		return
	}
	if !valid {
		rateLimited, retryMs := ctx.AuthManager.RecordFailedAttempt(ip)
		if rateLimited {
			request.WriteJSON(w, http.StatusTooManyRequests, map[string]interface{}{
				"success":      false,
				"error":        "Too many failed attempts",
				"retryAfterMs": retryMs,
			})
			return
		}
		request.WriteJSON(w, http.StatusUnauthorized, map[string]interface{}{
			"success": false,
			"error":   "Invalid password",
		})
		return
	}

	ctx.AuthManager.ResetRateLimit(ip)
	issued, err := ctx.MobileTokenMgr.Issue(deviceName, 0)
	if err != nil {
		slog.Error("Mobile login: token issue failed", "error", err)
		request.WriteError(w, http.StatusInternalServerError, "failed to issue token")
		return
	}

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"token":     issued.Token,
		"deviceId":  issued.DeviceID,
		"expiresAt": issued.ExpiresAt,
	})
}

// AuthMobileLogout revokes the bearer token presented by the caller. The
// device drops its stored token after a successful call. Operators can
// also revoke tokens via DELETE /api/auth/mobile/tokens/{deviceId} if a
// phone is lost.
// POST /api/auth/mobile/logout
// Auth:   Authorization: Bearer <mobile-token>
// Reply:  {"success": true, "revoked": true|false}
func (ctx *Context) AuthMobileLogout(w http.ResponseWriter, r *http.Request) {
	if ctx.MobileTokenMgr == nil {
		request.WriteError(w, http.StatusServiceUnavailable, "mobile auth not initialised")
		return
	}
	presented := bearerToken(r)
	if presented == "" {
		request.WriteJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "Authorization: Bearer <token> header required",
		})
		return
	}
	revoked := ctx.MobileTokenMgr.RevokeByToken(presented)
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"revoked": revoked,
	})
}

// AuthMobileListTokens lists every registered mobile device. The raw
// tokens are never returned; only metadata for the operator's Settings UI.
// GET /api/auth/mobile/tokens
// Auth:   session cookie OR mobile bearer token (the device can see its
//
//	own row plus everyone else's -- low blast radius since
//	metadata only).
func (ctx *Context) AuthMobileListTokens(w http.ResponseWriter, r *http.Request) {
	if ctx.MobileTokenMgr == nil {
		request.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"tokens":  []interface{}{},
		})
		return
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"tokens":  ctx.MobileTokenMgr.List(),
		"now":     time.Now().UnixMilli(),
	})
}

// AuthMobileRevokeToken revokes the token for a specific device. Idempotent.
// DELETE /api/auth/mobile/tokens/{deviceId}
// Auth:   session cookie OR mobile bearer token (a phone can revoke its
//
//	own access).
func (ctx *Context) AuthMobileRevokeToken(w http.ResponseWriter, r *http.Request) {
	if ctx.MobileTokenMgr == nil {
		request.WriteError(w, http.StatusServiceUnavailable, "mobile auth not initialised")
		return
	}
	deviceID := strings.TrimSpace(chi.URLParam(r, "deviceId"))
	if deviceID == "" {
		request.WriteError(w, http.StatusBadRequest, "deviceId is required")
		return
	}
	revoked := ctx.MobileTokenMgr.Revoke(deviceID)
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"revoked": revoked,
	})
}

// bearerToken extracts a token from either Authorization: Bearer or
// X-Jarvis-Agent-Token. Mirrors middleware.bearerOrAgentHeader but lives
// here so the handler is self-contained.
func bearerToken(r *http.Request) string {
	if v := r.Header.Get("X-Jarvis-Agent-Token"); v != "" {
		return v
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	return ""
}
