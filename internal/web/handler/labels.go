package handler

// R36 · Task-labels endpoints.
//
//   GET  /api/office/labels            -- distinct labels (autocomplete)
//   GET  /api/office/labels/rollups    -- per-label counts + cost
//   POST /api/office/agent/task/labels -- agent adds/removes labels
//
// Self-add already exists and now accepts an optional `labels` field
// on the request body (handled in office.go:OfficeAgentTaskSelfAdd).

import (
	"net/http"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/web/request"
)

// OfficeGetLabels returns the distinct label set across the live
// roster. Powers the filter-chip dropdown + autocomplete.
// GET /api/office/labels
func (ctx *Context) OfficeGetLabels(w http.ResponseWriter, r *http.Request) {
	labels := ctx.OfficeManager.GetAllLabels()
	if labels == nil {
		labels = []string{}
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{"labels": labels})
}

// OfficeGetLabelRollups returns per-label aggregates sorted by total
// cost descending. GET /api/office/labels/rollups
func (ctx *Context) OfficeGetLabelRollups(w http.ResponseWriter, r *http.Request) {
	rollups := ctx.OfficeManager.GetLabelRollups()
	if rollups == nil {
		rollups = []office.LabelRollup{}
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{"rollups": rollups})
}

type agentTaskLabelsRequest struct {
	SessionID string   `json:"sessionId"`
	TaskID    string   `json:"taskId"`
	Add       []string `json:"add,omitempty"`
	Remove    []string `json:"remove,omitempty"`
}

// OfficeAgentTaskLabels handles add/remove operations on a single
// task's labels. Body: {sessionId, taskId, add[], remove[]}. All
// adds + removes are applied atomically-ish (not transactional, but
// errors short-circuit) so partial failures leave the server in a
// well-defined state.
// POST /api/office/agent/task/labels
func (ctx *Context) OfficeAgentTaskLabels(w http.ResponseWriter, r *http.Request) {
	var req agentTaskLabelsRequest
	if !decodeBody(w, r, &req) {
		return
	}
	agent := ctx.OfficeManager.GetAgentAndTouch(req.SessionID)
	if agent == nil {
		request.WriteError(w, http.StatusNotFound, "agent not found")
		return
	}
	if req.TaskID == "" {
		request.WriteError(w, http.StatusBadRequest, "taskId is required")
		return
	}
	// Validate adds before mutating anything -- saves a confusing
	// partial-success state.
	for _, raw := range req.Add {
		if err := office.ValidateTaskLabel(office.NormalizeTaskLabel(raw)); err != nil {
			request.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	for _, raw := range req.Remove {
		if err := agent.RemoveLabel(req.TaskID, raw); err != nil {
			request.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	for _, raw := range req.Add {
		if err := agent.AddLabel(req.TaskID, raw); err != nil {
			request.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	// Broadcast so every dashboard client refreshes. Reuse the
	// generic task-updated event -- existing listeners already
	// refetch on it.
	ctx.emit("office:taskUpdated", map[string]interface{}{
		"sessionId": req.SessionID,
		"taskId":    req.TaskID,
	})
	// Return the current label list for the client's immediate use.
	var current []string
	for _, t := range agent.GetTasks() {
		if t.ID == req.TaskID {
			current = append([]string(nil), t.Labels...)
			break
		}
	}
	ok(w, map[string]interface{}{"labels": current})
}
