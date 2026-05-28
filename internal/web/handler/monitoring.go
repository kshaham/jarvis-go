package handler

import (
	"net/http"
	"runtime"
	"time"

	"github.com/kshaham/jarvis/internal/web/request"
)

var startTime = time.Now()

// Health returns a simple liveness check.
func (ctx *Context) Health(w http.ResponseWriter, r *http.Request) {
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"status": "ok",
		"uptime": time.Since(startTime).String(),
	})
}

// Ready returns a readiness check. Unlike Health (liveness), this verifies
// that critical dependencies are alive. Returns 503 if degraded so
// orchestrators (k8s, systemd) can stop routing traffic.
func (ctx *Context) Ready(w http.ResponseWriter, r *http.Request) {
	failing := make([]string, 0)
	if ctx.StateStore == nil {
		failing = append(failing, "stateStore")
	}
	if ctx.SSEBroker == nil {
		failing = append(failing, "sseBroker")
	}
	if ctx.SessionManager == nil {
		failing = append(failing, "sessionManager")
	}
	goroutines := runtime.NumGoroutine()
	if goroutines > 5000 {
		failing = append(failing, "goroutineCount")
	}

	status := http.StatusOK
	ready := true
	if len(failing) > 0 {
		status = http.StatusServiceUnavailable
		ready = false
	}

	request.WriteJSON(w, status, map[string]interface{}{
		"ready":      ready,
		"failing":    failing,
		"goroutines": goroutines,
		"uptime":     time.Since(startTime).String(),
	})
}

// Status returns full server status matching the Node.js response shape.
// The frontend calls data.sessions.find() so sessions MUST be an array.
func (ctx *Context) Status(w http.ResponseWriter, r *http.Request) {
	sessions := ctx.SessionManager.GetAllSessions()
	sessionList := make([]sessionJSON, 0, len(sessions))
	for _, s := range sessions {
		sessionList = append(sessionList, sessionToJSON(s))
	}

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"status":     "running",
		"version":    "0.2.0-go",
		"uptime":     time.Since(startTime).String(),
		"sessions":   sessionList,
		"goVersion":  runtime.Version(),
		"goroutines": runtime.NumGoroutine(),
	})
}

// CSRFToken generates and returns a new CSRF token.
func (ctx *Context) CSRFToken(w http.ResponseWriter, r *http.Request) {
	token := ctx.CSRFStore.Generate()
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"token":   token,
	})
}
