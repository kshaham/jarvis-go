package handler

import (
	"encoding/json"
	"net/http"

	"github.com/kshaham/jarvis/internal/claudehooks"
	"github.com/kshaham/jarvis/internal/web/request"
)

// Operator-facing endpoints for the PreToolUse tool-policy. GET shows
// the active policy; PUT replaces it (validates regexes before
// swapping). The store handles persistence + atomic writes; this
// handler is a thin shell.

// GetToolPolicy returns the active policy as JSON.
// GET /api/office/tool-policy
func (ctx *Context) GetToolPolicy(w http.ResponseWriter, r *http.Request) {
	if ctx.ToolPolicy == nil {
		request.WriteJSON(w, http.StatusOK, claudehooks.DefaultToolPolicy)
		return
	}
	request.WriteJSON(w, http.StatusOK, ctx.ToolPolicy.Snapshot())
}

// PutToolPolicy replaces the active policy. Returns 400 if any rule
// has an invalid regex.
// PUT /api/office/tool-policy
func (ctx *Context) PutToolPolicy(w http.ResponseWriter, r *http.Request) {
	if ctx.ToolPolicy == nil {
		request.WriteError(w, http.StatusServiceUnavailable, "tool policy store not initialised")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	var body claudehooks.ToolPolicy
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := ctx.ToolPolicy.Replace(body); err != nil {
		request.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"policy":  ctx.ToolPolicy.Snapshot(),
	})
}
