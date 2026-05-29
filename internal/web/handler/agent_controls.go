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

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/web/request"
)

type agentKillRequest struct {
	SessionID string `json:"sessionId"`
	Reason    string `json:"reason"`
}

type agentInjectRequest struct {
	SessionID string `json:"sessionId"`
	Prompt    string `json:"prompt"`
}

// R25 · Operator shifts an agent's working directory mid-run. The
// underlying SDK session's cwd doesn't change (that would require a
// restart); instead we update the agent's bookkeeping field and
// inject a prompt telling the agent to redirect its work. Useful
// when priorities shift — e.g. move a QA engineer from /api to
// /frontend without respawning the whole team.
type agentWorkdirRequest struct {
	SessionID  string `json:"sessionId"`
	WorkingDir string `json:"workingDir"`
	Reason     string `json:"reason"`
}

// R25 · In-place agent restart. Stops the current SDK session, keeps
// the AgentState + memory + learnings + task queue intact, and
// attaches a fresh SDK session under a new session ID. The operator
// reaches for this when an agent's context is bloated but the agent
// itself has accumulated institutional knowledge worth keeping.
type agentRestartRequest struct {
	SessionID string `json:"sessionId"`
	Reason    string `json:"reason"`
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
	result := ctx.OfficeManager.KillAgent(req.SessionID, req.Reason, ctx.stopAgentSessionByID)
	if result.MayBroadcast {
		ctx.SSEBroker.Broadcast("office:agentKilled", map[string]interface{}{
			"sessionId": req.SessionID,
			"agentName": result.AgentName,
			"reason":    result.Reason,
		})
	}
	slog.Info("agent killed by operator", "session", req.SessionID, "name", result.AgentName, "reason", result.Reason, "broadcast", result.MayBroadcast)
	ok(w, map[string]interface{}{"stopped": req.SessionID})
}

// stopAgentSessionByID implements office.StopAgentSessionFunc. Tries
// SDK first (modern path) and falls through to PTY for legacy
// sessions. No-op when SessionManager is nil (test environments).
func (ctx *Context) stopAgentSessionByID(sessionID string) {
	sm := ctx.SessionManager
	if sm == nil {
		return
	}
	if _, ok := sm.GetSDKSession(sessionID); ok {
		_ = sm.StopSDKSession(sessionID)
		return
	}
	if _, ok := sm.GetSession(sessionID); ok {
		_ = sm.StopSession(context.Background(), sessionID)
	}
}

// writeSessionPrompt implements office.SessionPromptFunc. Tries the
// PTY session first (legacy), then the SDK session, returning true
// only when one of them accepted the write. Used by the wrap-up
// fan-out + any other code that needs to push a prompt to a single
// agent regardless of session kind.
func (ctx *Context) writeSessionPrompt(sessionID, prompt string) bool {
	sm := ctx.SessionManager
	if sm == nil {
		return false
	}
	if s, found := sm.GetSession(sessionID); found {
		return s.WriteInkPrompt(prompt)
	}
	if sdk, found := sm.GetSDKSession(sessionID); found {
		return sdk.WriteInkPrompt(prompt)
	}
	return false
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

// OfficeAgentBumpWorkdir shifts one agent's working directory mid-run
// without restarting the session. The SDK session's underlying cwd
// doesn't change (a real cwd change would require a restart — that
// lives in the in-place restart handler); instead we update the
// agent's WorktreePath bookkeeping and inject a redirect prompt so
// the agent knows to work under the new path going forward.
func (ctx *Context) OfficeAgentBumpWorkdir(w http.ResponseWriter, r *http.Request) {
	var req agentWorkdirRequest
	if !request.DecodeAndValidateMax(w, r, &req, 1<<12) {
		return
	}
	if req.SessionID == "" {
		request.WriteError(w, http.StatusBadRequest, "sessionId is required")
		return
	}
	if req.WorkingDir == "" {
		request.WriteError(w, http.StatusBadRequest, "workingDir is required")
		return
	}
	// Validate the target against the spawn allowlist — reuse
	// ResolveWorkingDir so the same safeguards apply as on spawn.
	resolved, err := ctx.ResolveWorkingDir(req.WorkingDir)
	if err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid working directory: "+err.Error())
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

	// R26 concurrency review: WorktreePath is a shared field on
	// AgentState; writing it raw races with any other goroutine
	// reading it. SetWorktreePath wraps the update in the agent
	// mutex and returns the prior value atomically.
	previous := agent.SetWorktreePath(resolved)

	sessions := ctx.OfficeManager.Sessions()
	if sessions == nil {
		request.WriteError(w, http.StatusInternalServerError, "session store unavailable")
		return
	}

	// Build the redirect prompt. The agent's SDK session cwd hasn't
	// moved so they need to explicitly cd into the new path for any
	// further Bash work. Reason is appended if given so future turns
	// have the narrative context.
	reason := req.Reason
	if reason == "" {
		reason = "(no reason given)"
	}
	prompt := "[WORKING DIR CHANGE]\n" +
		"Your assigned working directory has shifted. Start your next turn with `cd " + resolved + "` and perform any further file work, tests, and commits under that directory. Reason: " + reason
	sessions.WriteInkPrompt(req.SessionID, prompt)

	ctx.SSEBroker.Broadcast("office:agentWorkdirChanged", map[string]interface{}{
		"sessionId":  req.SessionID,
		"agentName":  agentName,
		"previous":   previous,
		"workingDir": resolved,
		"reason":     req.Reason,
	})
	slog.Info("operator bumped agent working directory",
		"session", req.SessionID, "name", agentName,
		"previous", previous, "new", resolved, "reason", req.Reason)
	ok(w, map[string]interface{}{"workingDir": resolved})
}

// OfficeAgentRestart tears down one agent's SDK session and attaches
// a fresh one without dropping the AgentState. Memory, learnings,
// task queue, whiteboard note, personality — all survive the
// restart. Session cost/token counters reset (fresh session).
// Returns the new session ID on success.
func (ctx *Context) OfficeAgentRestart(w http.ResponseWriter, r *http.Request) {
	var req agentRestartRequest
	if !request.DecodeAndValidateMax(w, r, &req, 1<<12) {
		return
	}
	if req.SessionID == "" {
		request.WriteError(w, http.StatusBadRequest, "sessionId is required")
		return
	}
	agent := ctx.OfficeManager.GetAgent(req.SessionID)
	if agent == nil {
		request.WriteError(w, http.StatusNotFound, "agent not found")
		return
	}
	member, err := office.BuildRestartTemplateMember(agent)
	if err != nil {
		request.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	agentName := agent.Config.Name

	// Snapshot the working dir from the live SDK session before we
	// stop it — the spawn helper needs it to build the fresh session
	// at the same path. Falls back to agent.WorktreePath when no SDK
	// session lookup is available.
	sdkWorkingDir := ""
	sm := ctx.SessionManager
	if sm != nil {
		if sdk, ok := sm.GetSDKSession(req.SessionID); ok {
			sdkWorkingDir = sdk.WorkingDir
		} else if cx, ok := sm.GetCodexSession(req.SessionID); ok {
			// Codex-harness agent: same restart flow, different backend.
			sdkWorkingDir = cx.WorkingDir
		}
	}
	workingDir := office.ResolveRestartWorkingDir(agent, sdkWorkingDir)
	if workingDir == "" {
		request.WriteError(w, http.StatusInternalServerError,
			office.ErrRestartWorkingDirUnknown.Error())
		return
	}

	// Stop the old SDK session. HandleAgentDeath wouldn't fire
	// post-reassignment (the old session ID is no longer in the
	// agents map by then) — but we stop it BEFORE reassigning so any
	// in-flight EventExit runs against the old ID cleanly.
	if sm != nil {
		_ = sm.StopSDKSession(req.SessionID)
		// Also stop a Codex-backed session if that's what this agent ran on
		// (one of the two is a no-op "not found" — harmless).
		_ = sm.StopCodexSession(req.SessionID)
	}

	spawnReq := spawnTeamRequest{
		WorkingDir: workingDir,
		Mission:    ctx.OfficeManager.GetTeamMission(),
	}
	teamConfigs := ctx.OfficeManager.SnapshotRosterAsTeamMemberConfigs()
	perpetual := ctx.OfficeManager.IsPerpetualMode()
	envVars := ctx.getProviderEnvVars()

	// Respawn path: never opt into hooks-mode here. The previous
	// session already had its .claude/settings.local.json wired (or
	// not) when the team was first spawned; redoing it for a single
	// agent would race with the still-running process and the new
	// session would see a stale URL pointed at the old sessionID.
	// Operators wanting to flip an agent into hooks mode mid-team
	// should kill the team and respawn.
	// Preserve the agent's original harness on restart: pass its recorded
	// provider/model so a Codex agent respawns on Codex (and a Claude agent
	// on Claude). If the resolved provider is a Codex one, getProviderEnvVars
	// above already emitted OPENAI_API_KEY for it.
	newSessionID, mode := spawnAgentSession(ctx, member, spawnReq, envVars, teamConfigs, perpetual, true, nil, agent.Provider, agent.Model, spawnAgentSpec{})
	if newSessionID == "" {
		request.WriteError(w, http.StatusInternalServerError, "failed to create replacement session")
		return
	}

	// Move the existing AgentState onto the new session ID. The
	// agent's memory, task queue, learnings, personality, recovery
	// counters — everything — carries over.
	if !ctx.OfficeManager.ReassignSessionID(req.SessionID, newSessionID) {
		// Extremely unlikely (collision on UUID v4) but don't leak
		// a dangling session if it happens.
		if sm != nil {
			_ = sm.StopSDKSession(newSessionID)
		}
		request.WriteError(w, http.StatusInternalServerError, "failed to reassign session id")
		return
	}

	reason := req.Reason
	if reason == "" {
		reason = "operator"
	}
	ctx.SSEBroker.Broadcast("office:agentRestarted", map[string]interface{}{
		"sessionId":    newSessionID,
		"oldSessionId": req.SessionID,
		"agentName":    agentName,
		"reason":       reason,
		"mode":         mode,
	})
	slog.Info("operator restarted agent in place",
		"name", agentName, "oldSession", req.SessionID,
		"newSession", newSessionID, "reason", reason)
	ok(w, map[string]interface{}{
		"sessionId":    newSessionID,
		"oldSessionId": req.SessionID,
	})
}
