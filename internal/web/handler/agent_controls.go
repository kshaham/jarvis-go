package handler

// R18 · Operator-facing controls over individual agents. Complements
// the existing team-wide /shutdown-team endpoint with surgical tools:
//
//   POST /api/office/agent/kill    — stop one agent by sessionID
//   POST /api/office/agent/inject  — send a corrective prompt to one
//                                    agent without killing the session
//
// Both routes live in the browser-auth group so operators click a UI
// button; agents themselves can't kill peers or reprogram them.

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/claudeman/jarvis-go/internal/web/request"
)

type agentKillRequest struct {
	SessionID string `json:"sessionId"`
	Reason    string `json:"reason"`
}

type agentInjectRequest struct {
	SessionID string `json:"sessionId"`
	Prompt    string `json:"prompt"`
}

// maxInjectPromptBytes caps operator-supplied prompt size. Keeps
// payloads reasonable and prevents accidental codebase pastes.
const maxInjectPromptBytes = 4 * 1024

// maxInjectBodyBytes is the outer JSON-body limit the decoder enforces
// BEFORE we see req.Prompt. Just enough headroom for the envelope
// (sessionId + key names + quoting) — previously 1024 bytes which was
// 4× more slack than needed. R21 code review tightened this.
const maxInjectBodyBytes = maxInjectPromptBytes + 256

// OfficeAgentKill stops a single agent session. Surgical alternative to
// /shutdown-team for rogue/runaway agents. Fires office:agentKilled
// with the reason so the dashboard can flash the card before removing.
func (ctx *Context) OfficeAgentKill(w http.ResponseWriter, r *http.Request) {
	var req agentKillRequest
	if !request.DecodeAndValidateMax(w, r, &req, 1<<12) {
		return
	}
	if req.SessionID == "" {
		request.WriteError(w, http.StatusBadRequest, "sessionId is required")
		return
	}
	agent := ctx.OfficeManager.GetAgent(req.SessionID)
	agentName := req.SessionID
	if agent != nil && agent.Config != nil {
		agentName = agent.Config.Name
	}

	// Stop the underlying session. Try SDK first (most agents); fall
	// through to PTY for the handful of legacy sessions.
	sm := ctx.SessionManager
	if sm != nil {
		if _, ok := sm.GetSDKSession(req.SessionID); ok {
			_ = sm.StopSDKSession(req.SessionID)
		} else if _, ok := sm.GetSession(req.SessionID); ok {
			_ = sm.StopSession(context.Background(), req.SessionID)
		}
	}

	// Unregister from the office — triggers task reassignment + dev
	// instance cleanup via HandleAgentDeath if this was the last lead.
	ctx.OfficeManager.UnregisterAgent(req.SessionID)

	reason := req.Reason
	if reason == "" {
		reason = "operator"
	}
	ctx.SSEBroker.Broadcast("office:agentKilled", map[string]interface{}{
		"sessionId": req.SessionID,
		"agentName": agentName,
		"reason":    reason,
	})
	slog.Info("agent killed by operator", "session", req.SessionID, "name", agentName, "reason", reason)
	ok(w, map[string]interface{}{"stopped": req.SessionID})
}

// OfficeAgentInjectPrompt sends a corrective prompt to one agent. The
// prompt is appended to the agent's current context (it does NOT reset
// the session); the agent's next turn will include it. Useful when an
// agent is drifting off-goal and the operator wants to nudge them
// without killing the accumulated context.
func (ctx *Context) OfficeAgentInjectPrompt(w http.ResponseWriter, r *http.Request) {
	var req agentInjectRequest
	if !request.DecodeAndValidateMax(w, r, &req, maxInjectBodyBytes) {
		return
	}
	if req.SessionID == "" {
		request.WriteError(w, http.StatusBadRequest, "sessionId is required")
		return
	}
	if req.Prompt == "" {
		request.WriteError(w, http.StatusBadRequest, "prompt is required")
		return
	}
	if len(req.Prompt) > maxInjectPromptBytes {
		request.WriteError(w, http.StatusBadRequest, "prompt exceeds 4KB limit")
		return
	}
	agent := ctx.OfficeManager.GetAgent(req.SessionID)
	if agent == nil {
		request.WriteError(w, http.StatusNotFound, "agent not found")
		return
	}
	agentName := req.SessionID
	if agent.Config != nil {
		agentName = agent.Config.Name
	}

	// Prefix so the agent knows this came from the operator and wasn't a
	// teammate message. Avoids ambiguity in the agent's reasoning.
	decorated := "[OPERATOR PROMPT]\n" + req.Prompt

	sessions := ctx.OfficeManager.Sessions()
	if sessions == nil {
		request.WriteError(w, http.StatusInternalServerError, "session store unavailable")
		return
	}
	sessions.WriteInkPrompt(req.SessionID, decorated)

	ctx.SSEBroker.Broadcast("office:agentPromptInjected", map[string]interface{}{
		"sessionId": req.SessionID,
		"agentName": agentName,
		"bytes":     len(req.Prompt),
	})
	slog.Info("operator injected prompt", "session", req.SessionID, "name", agentName, "bytes", len(req.Prompt))
	ok(w, map[string]interface{}{"injected": true})
}
