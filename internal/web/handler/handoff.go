package handler

// R29 · HTTP handlers for structured task handoffs.
//
// Three agent-auth endpoints (initiate, accept, reject) and two
// operator-auth endpoints (list, force-resolve). Each validates caller
// identity where relevant — senders must own the task they're handing
// off, and receivers can only accept/reject handoffs addressed to
// them. Operators use the force variants to unblock stuck handoffs
// when an agent is unresponsive.

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/web/request"
)

type handoffInitiateRequest struct {
	SessionID  string `json:"sessionId"`
	TaskID     string `json:"taskId"`
	ToConfigID string `json:"toConfigId"`
	Context    string `json:"context"`
}

type handoffResolveRequest struct {
	SessionID string `json:"sessionId"`
	TaskID    string `json:"taskId"`
	Reason    string `json:"reason,omitempty"` // only used by reject
}

type handoffForceRequest struct {
	TaskID string `json:"taskId"`
	Reason string `json:"reason,omitempty"` // only used by force-reject
}

// OfficeAgentHandoffInitiate starts a handoff from the caller to
// another agent identified by toConfigId.
// POST /api/office/task/handoff
func (ctx *Context) OfficeAgentHandoffInitiate(w http.ResponseWriter, r *http.Request) {
	if ctx.OfficeManager.IsWrappingUp() {
		request.WriteError(w, http.StatusConflict, "team is wrapping up, no new handoffs accepted")
		return
	}
	var req handoffInitiateRequest
	if !decodeBody(w, r, &req) {
		return
	}
	agent := ctx.OfficeManager.GetAgentAndTouch(req.SessionID)
	if agent == nil {
		request.WriteError(w, http.StatusNotFound, "agent not found")
		return
	}
	h, err := ctx.OfficeManager.InitiateHandoff(req.SessionID, req.TaskID, req.ToConfigID, req.Context)
	if err != nil {
		// Map validation errors to 400; everything else 500.
		status := statusForHandoffError(err)
		request.WriteError(w, status, err.Error())
		return
	}
	ok(w, map[string]interface{}{"handoff": h})
}

// OfficeAgentHandoffAccept accepts a pending handoff addressed to the
// caller. POST /api/office/task/handoff/accept
func (ctx *Context) OfficeAgentHandoffAccept(w http.ResponseWriter, r *http.Request) {
	var req handoffResolveRequest
	if !decodeBody(w, r, &req) {
		return
	}
	agent := ctx.OfficeManager.GetAgentAndTouch(req.SessionID)
	if agent == nil {
		request.WriteError(w, http.StatusNotFound, "agent not found")
		return
	}
	h, err := ctx.OfficeManager.AcceptHandoff(req.SessionID, req.TaskID)
	if err != nil {
		request.WriteError(w, statusForHandoffError(err), err.Error())
		return
	}
	ok(w, map[string]interface{}{"handoff": h})
}

// OfficeAgentHandoffCancel retracts a pending handoff. Only the
// sender of the handoff can call this; resolveHandoffPair inside
// CancelHandoff enforces the ownership check and returns "not your
// handoff" -> 403 otherwise.
// POST /api/office/task/handoff/cancel
func (ctx *Context) OfficeAgentHandoffCancel(w http.ResponseWriter, r *http.Request) {
	var req handoffResolveRequest
	if !decodeBody(w, r, &req) {
		return
	}
	agent := ctx.OfficeManager.GetAgentAndTouch(req.SessionID)
	if agent == nil {
		request.WriteError(w, http.StatusNotFound, "agent not found")
		return
	}
	h, err := ctx.OfficeManager.CancelHandoff(req.SessionID, req.TaskID)
	if err != nil {
		request.WriteError(w, statusForHandoffError(err), err.Error())
		return
	}
	ok(w, map[string]interface{}{"handoff": h})
}

// OfficeAgentHandoffReject rejects a pending handoff.
// POST /api/office/task/handoff/reject
func (ctx *Context) OfficeAgentHandoffReject(w http.ResponseWriter, r *http.Request) {
	var req handoffResolveRequest
	if !decodeBody(w, r, &req) {
		return
	}
	agent := ctx.OfficeManager.GetAgentAndTouch(req.SessionID)
	if agent == nil {
		request.WriteError(w, http.StatusNotFound, "agent not found")
		return
	}
	reason := req.Reason
	if reason == "" {
		reason = "declined"
	}
	h, err := ctx.OfficeManager.RejectHandoff(req.SessionID, req.TaskID, reason)
	if err != nil {
		request.WriteError(w, statusForHandoffError(err), err.Error())
		return
	}
	ok(w, map[string]interface{}{"handoff": h})
}

// OfficeListHandoffs returns pending handoffs + recent audit history.
// GET /api/office/handoffs
func (ctx *Context) OfficeListHandoffs(w http.ResponseWriter, r *http.Request) {
	ok(w, map[string]interface{}{
		"pending": ctx.OfficeManager.ListPendingHandoffs(),
		"history": ctx.OfficeManager.GetHandoffHistory(),
	})
}

// OfficeHandoffForceAccept force-accepts a pending handoff on the
// operator's behalf. POST /api/office/handoffs/force-accept
func (ctx *Context) OfficeHandoffForceAccept(w http.ResponseWriter, r *http.Request) {
	var req handoffForceRequest
	if !decodeBody(w, r, &req) {
		return
	}
	h, err := ctx.OfficeManager.ForceAcceptHandoff(req.TaskID)
	if err != nil {
		slog.Warn("handoff force-accept failed", "taskId", req.TaskID, "err", err)
		request.WriteError(w, statusForHandoffError(err), err.Error())
		return
	}
	ok(w, map[string]interface{}{"handoff": h})
}

// OfficeHandoffForceReject force-rejects a pending handoff on the
// operator's behalf. POST /api/office/handoffs/force-reject
func (ctx *Context) OfficeHandoffForceReject(w http.ResponseWriter, r *http.Request) {
	var req handoffForceRequest
	if !decodeBody(w, r, &req) {
		return
	}
	reason := req.Reason
	if reason == "" {
		reason = "operator-override"
	}
	h, err := ctx.OfficeManager.ForceRejectHandoff(req.TaskID, reason)
	if err != nil {
		slog.Warn("handoff force-reject failed", "taskId", req.TaskID, "err", err)
		request.WriteError(w, statusForHandoffError(err), err.Error())
		return
	}
	ok(w, map[string]interface{}{"handoff": h})
}

// statusForHandoffError maps the text of a Manager error to an HTTP
// status code. Shared by every handoff endpoint so the mapping stays
// consistent.
func statusForHandoffError(err error) int {
	msg := err.Error()
	// Exact matches first — ownership / not-found / limit are the cases
	// callers most often need to distinguish.
	switch msg {
	case "sender agent not found", "task not found", "no pending handoff for task",
		"agent not found", "receiver not in roster", "handoff references missing agent":
		return http.StatusNotFound
	case "not your handoff":
		return http.StatusForbidden
	case fmt.Sprintf("too many pending handoffs (limit %d)", office.HandoffMaxPendingPerSender):
		return http.StatusTooManyRequests
	}
	// Substring matches for the validation cluster — cheaper than
	// exhaustively enumerating every wrapped message.
	switch {
	case strings.Contains(msg, "context too large"),
		strings.Contains(msg, "cannot hand off to self"),
		strings.Contains(msg, "must be in_progress"),
		strings.Contains(msg, "already has a pending"):
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}
