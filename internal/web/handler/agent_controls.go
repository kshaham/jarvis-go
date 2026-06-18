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
	"errors"
	"fmt"
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

// agentPeerRestartRequest is the agent-callable peer-restart payload. The
// Guardian (or any teammate) refers to the target by display name or member id;
// the handler resolves it to a ConfigID. Reason is optional and surfaced in the
// broadcast for audit.
type agentPeerRestartRequest struct {
	Name     string `json:"name"`
	MemberID string `json:"memberId"`
	Reason   string `json:"reason"`
}

// agentControlRestartRequest is the operator/agent-callable restart payload for
// the dual-auth /api/office/agent-control/restart endpoint. The caller names the
// member by display name or member id; the handler resolves it to a ConfigID and
// restarts in place if live, or respawns from the snapshot if dead.
type agentControlRestartRequest struct {
	Name     string `json:"name"`
	MemberID string `json:"memberId"`
	Reason   string `json:"reason"`
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
		ctx.emit("office:agentKilled", map[string]interface{}{
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
	if _, ok := sm.GetCodexSession(sessionID); ok {
		_ = sm.StopCodexSession(sessionID)
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
	if cx, found := sm.GetCodexSession(sessionID); found {
		return cx.WriteInkPrompt(prompt)
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
	if !sessions.WriteInkPrompt(req.SessionID, decorated) {
		// The session isn't accepting prompts (stopped/unknown id) — don't
		// report success for a prompt that was never delivered.
		request.WriteError(w, http.StatusConflict, "agent session is not accepting prompts")
		return
	}

	ctx.emit("office:agentPromptInjected", map[string]interface{}{
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
	// The workdir bookkeeping (SetWorktreePath) already applied above, so don't
	// fail the request; but surface it if the redirect prompt couldn't be
	// delivered (the agent won't know to cd into the new path).
	if !sessions.WriteInkPrompt(req.SessionID, prompt) {
		slog.Warn("workdir bump: redirect prompt not delivered (session not accepting prompts)",
			"session", req.SessionID, "name", agentName)
	}

	ctx.emit("office:agentWorkdirChanged", map[string]interface{}{
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

// restartLiveAgent tears down one LIVE agent's SDK/Codex session and attaches
// a fresh one without dropping the AgentState. Memory, learnings, task queue,
// whiteboard note, personality — all survive the restart; session cost/token
// counters reset (fresh session). Returns the new session ID, the agent's
// display name, and the spawn mode. The error is one of the office sentinels
// (ErrAgentMissingConfig / ErrRestartWorkingDirUnknown) or a generic spawn
// failure; the caller maps it to the right HTTP status.
//
// Factored out of OfficeAgentRestart so the agent-callable peer-restart
// endpoint can reuse the exact same in-place restart flow for a teammate that
// is still alive but stuck.
func (ctx *Context) restartLiveAgent(sessionID, reason string) (newSessionID, agentName, mode string, err error) {
	agent := ctx.OfficeManager.GetAgent(sessionID)
	if agent == nil {
		return "", "", "", office.ErrAgentMissingConfig
	}
	member, mErr := office.BuildRestartTemplateMember(agent)
	if mErr != nil {
		return "", "", "", mErr
	}
	agentName = agent.Config.Name

	// Snapshot the working dir from the live SDK/Codex session before we stop
	// it — the spawn helper needs it to build the fresh session at the same
	// path. Falls back to agent.WorktreePath when no live session lookup hits.
	sdkWorkingDir := ""
	sm := ctx.SessionManager
	if sm != nil {
		if sdk, ok := sm.GetSDKSession(sessionID); ok {
			sdkWorkingDir = sdk.WorkingDir
		} else if cx, ok := sm.GetCodexSession(sessionID); ok {
			sdkWorkingDir = cx.WorkingDir
		}
	}
	workingDir := office.ResolveRestartWorkingDir(agent, sdkWorkingDir)
	if workingDir == "" {
		return "", "", "", office.ErrRestartWorkingDirUnknown
	}

	// Stop the old session BEFORE reassigning so any in-flight EventExit runs
	// against the old ID cleanly. One of the two stops is a harmless no-op.
	if sm != nil {
		_ = sm.StopSDKSession(sessionID)
		_ = sm.StopCodexSession(sessionID)
	}

	spawnReq := spawnTeamRequest{
		WorkingDir: workingDir,
		Mission:    ctx.OfficeManager.GetTeamMission(),
	}
	teamConfigs := ctx.OfficeManager.SnapshotRosterAsTeamMemberConfigs()
	perpetual := ctx.OfficeManager.IsPerpetualMode()
	envVars := ctx.getProviderEnvVars()

	// Respawn path: never opt into hooks-mode here (see OfficeAgentRestart's
	// original note). Preserve the agent's recorded provider/model so a Codex
	// agent respawns on Codex and a Claude agent on Claude.
	newSessionID, mode = spawnAgentSession(ctx, member, spawnReq, envVars, teamConfigs, perpetual, true, nil, agent.Provider, agent.Model, spawnAgentSpec{})
	if newSessionID == "" {
		return "", "", "", fmt.Errorf("failed to create replacement session")
	}

	if !ctx.OfficeManager.ReassignSessionID(sessionID, newSessionID) {
		// UUID-v4 collision (effectively impossible) — don't leak the dangling
		// replacement. Stop both backends since the replacement may be Codex.
		if sm != nil {
			_ = sm.StopSDKSession(newSessionID)
			_ = sm.StopCodexSession(newSessionID)
		}
		return "", "", "", fmt.Errorf("failed to reassign session id")
	}
	return newSessionID, agentName, mode, nil
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
	if ctx.OfficeManager.GetAgent(req.SessionID) == nil {
		request.WriteError(w, http.StatusNotFound, "agent not found")
		return
	}

	reason := req.Reason
	if reason == "" {
		reason = "operator"
	}
	newSessionID, agentName, mode, err := ctx.restartLiveAgent(req.SessionID, reason)
	if err != nil {
		request.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	ctx.emit("office:agentRestarted", map[string]interface{}{
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

// OfficeAgentPeerRestart lets an agent (the Guardian watchdog, or any teammate)
// restart a peer that is stuck or has died. The caller names the peer by
// display name or member id; the handler resolves it to a ConfigID and then:
//
//   - LIVE peer (still on the roster): reuse the in-place restartLiveAgent
//     flow — same path as the operator's /agent/restart, preserving the peer's
//     memory, task queue, and learnings under a fresh session.
//   - DEAD peer (no live session): rebuild it from the spawn-time snapshot via
//     the manager's PrepareMemberRespawn (which enforces a respawn backoff
//     cap), spawn a fresh session at the recorded workdir, and RegisterAgent.
//
// Lives in the agent-token route group so spawned agents can call it; the
// operator-only kill/restart endpoints stay in the CSRF group.
func (ctx *Context) OfficeAgentPeerRestart(w http.ResponseWriter, r *http.Request) {
	var req agentPeerRestartRequest
	if !request.DecodeAndValidateMax(w, r, &req, 1<<12) {
		return
	}
	target := req.MemberID
	if target == "" {
		target = req.Name
	}
	if target == "" {
		request.WriteError(w, http.StatusBadRequest, "name or memberId is required")
		return
	}

	configID := ctx.OfficeManager.ResolveMemberConfigID(target)
	if configID == "" {
		request.WriteError(w, http.StatusNotFound, "no such teammate: "+target)
		return
	}

	reason := req.Reason
	if reason == "" {
		reason = "peer-restart"
	}

	// LIVE peer: reuse the in-place restart flow.
	if live := ctx.OfficeManager.GetAgentByConfig(configID); live != nil {
		oldSessionID := live.SessionID()
		newSessionID, agentName, mode, err := ctx.restartLiveAgent(oldSessionID, reason)
		if err != nil {
			request.WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		ctx.emit("office:agentRestarted", map[string]interface{}{
			"sessionId":    newSessionID,
			"oldSessionId": oldSessionID,
			"agentName":    agentName,
			"reason":       reason,
			"mode":         mode,
			"via":          "peer",
		})
		slog.Info("peer restarted live agent in place",
			"name", agentName, "oldSession", oldSessionID,
			"newSession", newSessionID, "reason", reason)
		ok(w, map[string]interface{}{
			"sessionId":    newSessionID,
			"oldSessionId": oldSessionID,
			"restarted":    "live",
		})
		return
	}

	// DEAD peer: rebuild from the spawn snapshot (backoff-capped).
	if err := ctx.respawnDeadMember(configID); err != nil {
		switch {
		case errors.Is(err, office.ErrRespawnCapReached):
			request.WriteError(w, http.StatusTooManyRequests,
				"respawn cap reached for "+target+"; escalate to the lead instead")
		case errors.Is(err, office.ErrMemberNotRespawnable):
			request.WriteError(w, http.StatusNotFound,
				"no spawn snapshot for "+target+" (cannot respawn)")
		default:
			request.WriteError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	live := ctx.OfficeManager.GetAgentByConfig(configID)
	newSessionID := ""
	respawnedName := target
	if live != nil {
		newSessionID = live.SessionID()
		if live.Config != nil {
			respawnedName = live.Config.Name
		}
	}
	ctx.emit("office:agentRestarted", map[string]interface{}{
		"sessionId": newSessionID,
		"agentName": respawnedName,
		"reason":    reason,
		"via":       "peer-respawn",
	})
	slog.Info("peer respawned dead member",
		"name", respawnedName, "newSession", newSessionID, "reason", reason)
	ok(w, map[string]interface{}{
		"sessionId": newSessionID,
		"restarted": "respawned",
	})
}

// respawnDeadMember rebuilds a DEAD member (no live session) from the spawn-time
// snapshot. It is the single source of truth for the dead-member respawn flow,
// shared by the agent-callable peer-restart endpoint, the operator-facing
// agent-control restart endpoint, and the backend auto-respawn watchdog (wired
// via Manager.SetRespawnMemberFunc).
//
// Returns office.ErrRespawnCapReached / office.ErrMemberNotRespawnable verbatim
// so callers can map them to the right HTTP status; any other error is a generic
// spawn failure.
// RespawnDeadMember is the exported entry point the office manager's auto-respawn
// watchdog is wired to via SetRespawnMemberFunc (server.go). It delegates to the
// unexported respawnDeadMember so cross-package wiring works while in-package
// callers (peer-restart, agent-control restart) keep using the lowercase method.
func (ctx *Context) RespawnDeadMember(configID string) error {
	return ctx.respawnDeadMember(configID)
}

func (ctx *Context) respawnDeadMember(configID string) error {
	plan, err := ctx.OfficeManager.PrepareMemberRespawn(configID)
	if err != nil {
		return err
	}

	spawnReq := spawnTeamRequest{
		WorkingDir: plan.WorkingDir,
		Mission:    ctx.OfficeManager.GetTeamMission(),
	}
	teamConfigs := ctx.OfficeManager.SnapshotRosterAsTeamMemberConfigs()
	envVars := ctx.getProviderEnvVars()
	// If the recorded provider is a Codex one, make sure OPENAI_API_KEY is in
	// the env block (getProviderEnvVars emits the team default; rebuild for the
	// member's recorded provider when it differs and lacks the key).
	if office.HarnessFor(plan.Provider) == "codex" && envVars["OPENAI_API_KEY"] == "" {
		envVars = ctx.getProviderEnvVarsFor(plan.Provider, plan.Model, "")
	}

	newSessionID, mode := spawnAgentSession(ctx, plan.Member, spawnReq, envVars, teamConfigs, plan.Perpetual, true, nil, plan.Provider, plan.Model, spawnAgentSpec{})
	if newSessionID == "" {
		return fmt.Errorf("failed to respawn dead member %s", plan.Member.Name)
	}

	// Register the fresh session under the member's config. A dead member has
	// no surviving AgentState, so this re-creates one (memory on disk is keyed
	// by ConfigID + workingDir, so kickstart still reloads the member's notes).
	cfg := &office.TeamMemberConfig{
		ID:           plan.Member.ID,
		Name:         plan.Member.Name,
		Role:         plan.Member.Role,
		Tier:         plan.Member.Tier,
		Color:        plan.Member.Color,
		Personality:  plan.Member.Personality,
		Voice:        plan.Member.Voice,
		Skills:       plan.Member.Skills,
		SystemPrompt: plan.Member.SystemPrompt,
	}
	ctx.OfficeManager.RegisterAgentWithProvider(newSessionID, cfg, plan.Provider, plan.Model, mode)
	return nil
}

// OfficeAgentControlRestart is the dual-auth (operator session OR agent token)
// manual restart of one team member by name or member id. It mirrors the
// peer-restart routing: a LIVE member is restarted in place (preserving memory,
// learnings, and task queue under a fresh session), while a DEAD member is
// respawned from the spawn-time snapshot. Lives in the AuthWithTokens group so
// the operator can trigger it from the UI and agents/mobile can call it with a
// bearer token without CSRF.
func (ctx *Context) OfficeAgentControlRestart(w http.ResponseWriter, r *http.Request) {
	var req agentControlRestartRequest
	if !request.DecodeAndValidateMax(w, r, &req, 1<<12) {
		return
	}
	target := req.MemberID
	if target == "" {
		target = req.Name
	}
	if target == "" {
		request.WriteError(w, http.StatusBadRequest, "name or memberId is required")
		return
	}

	configID := ctx.OfficeManager.ResolveMemberConfigID(target)
	if configID == "" {
		request.WriteError(w, http.StatusNotFound, "no such teammate: "+target)
		return
	}

	reason := req.Reason
	if reason == "" {
		reason = "operator-restart"
	}

	// LIVE member: restart in place.
	if live := ctx.OfficeManager.GetAgentByConfig(configID); live != nil {
		oldSessionID := live.SessionID()
		newSessionID, agentName, mode, err := ctx.restartLiveAgent(oldSessionID, reason)
		if err != nil {
			request.WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		ctx.emit("office:agentRestarted", map[string]interface{}{
			"sessionId":    newSessionID,
			"oldSessionId": oldSessionID,
			"agentName":    agentName,
			"reason":       reason,
			"mode":         mode,
			"via":          "agent-control",
		})
		slog.Info("agent-control restarted live agent in place",
			"name", agentName, "oldSession", oldSessionID,
			"newSession", newSessionID, "reason", reason)
		ok(w, map[string]interface{}{
			"sessionId":    newSessionID,
			"oldSessionId": oldSessionID,
			"restarted":    "live",
		})
		return
	}

	// DEAD member: respawn from the spawn snapshot (backoff-capped).
	if err := ctx.respawnDeadMember(configID); err != nil {
		switch {
		case errors.Is(err, office.ErrRespawnCapReached):
			request.WriteError(w, http.StatusTooManyRequests,
				"respawn cap reached for "+target+"; escalate to the lead instead")
		case errors.Is(err, office.ErrMemberNotRespawnable):
			request.WriteError(w, http.StatusNotFound,
				"no spawn snapshot for "+target+" (cannot respawn)")
		default:
			request.WriteError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	live := ctx.OfficeManager.GetAgentByConfig(configID)
	newSessionID := ""
	respawnedName := target
	if live != nil {
		newSessionID = live.SessionID()
		if live.Config != nil {
			respawnedName = live.Config.Name
		}
	}
	ctx.emit("office:agentRestarted", map[string]interface{}{
		"sessionId": newSessionID,
		"agentName": respawnedName,
		"reason":    reason,
		"via":       "agent-control-respawn",
	})
	slog.Info("agent-control respawned dead member",
		"name", respawnedName, "newSession", newSessionID, "reason", reason)
	ok(w, map[string]interface{}{
		"sessionId": newSessionID,
		"restarted": "respawned",
	})
}
