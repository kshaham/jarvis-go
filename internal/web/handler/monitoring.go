package handler

import (
	"net/http"
	"runtime"
	"time"

	"github.com/claudeman/jarvis-go/internal/web/request"
)

var startTime = time.Now()

// Health returns a simple health check.
func (ctx *Context) Health(w http.ResponseWriter, r *http.Request) {
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"status": "ok",
		"uptime": time.Since(startTime).String(),
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
