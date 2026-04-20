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

	"github.com/claudeman/jarvis-go/internal/office"
	"github.com/claudeman/jarvis-go/internal/teamtemplates"
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
	agent := ctx.OfficeManager.GetAgent(req.SessionID)
	agentName := req.SessionID
	if agent != nil && agent.Config != nil {
		agentName = agent.Config.Name
	}

	// R21 code review (third pass): claim the stop-broadcast slot
	// before doing work. If checkForWedgedWorkingAgents raced us and
	// already fired office:agentIdleStopped, we still stop the
	// session + unregister, but skip the duplicate office:agentKilled
	// toast so the dashboard doesn't show both reasons for one stop.
	mayBroadcast := agent != nil && agent.ClaimStopBroadcast()

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
	if mayBroadcast {
		ctx.SSEBroker.Broadcast("office:agentKilled", map[string]interface{}{
			"sessionId": req.SessionID,
			"agentName": agentName,
			"reason":    reason,
		})
	}
	slog.Info("agent killed by operator", "session", req.SessionID, "name", agentName, "reason", reason, "broadcast", mayBroadcast)
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

	previous := agent.WorktreePath
	agent.WorktreePath = resolved

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
	if agent.Config == nil {
		request.WriteError(w, http.StatusInternalServerError, "agent has no config (cannot rebuild kickstart)")
		return
	}
	agentName := agent.Config.Name

	// Snapshot the working dir from the live SDK session before we
	// stop it — the spawn helper needs it to build the fresh session
	// at the same path.
	workingDir := ""
	sm := ctx.SessionManager
	if sm != nil {
		if sdk, ok := sm.GetSDKSession(req.SessionID); ok {
			workingDir = sdk.WorkingDir
		}
	}
	if workingDir == "" {
		// Fallback: agent.WorktreePath is set by R25's workdir bump
		// and by spawnAgentSession. Either way it's a safe fallback.
		workingDir = agent.WorktreePath
	}
	if workingDir == "" {
		request.WriteError(w, http.StatusInternalServerError,
			"cannot determine working directory for restart")
		return
	}

	// Stop the old SDK session. HandleAgentDeath wouldn't fire
	// post-reassignment (the old session ID is no longer in the
	// agents map by then) — but we stop it BEFORE reassigning so any
	// in-flight EventExit runs against the old ID cleanly.
	if sm != nil {
		_ = sm.StopSDKSession(req.SessionID)
	}

	// Build a synthetic spawn request for this single member. Only
	// the fields spawnAgentSession actually reads are populated.
	member := teamtemplates.TeamTemplateMember{
		ID:           agent.ConfigID,
		Name:         agent.Config.Name,
		Role:         agent.Config.Role,
		Tier:         agent.Config.Tier,
		Color:        agent.Config.Color,
		Personality:  agent.Config.Personality,
		SystemPrompt: agent.Config.SystemPrompt,
		Skills:       agent.Config.Skills,
	}
	// Mission: use the current team mission so the restarted agent
	// has the same macro-goal its teammates have.
	mission := ctx.OfficeManager.GetTeamMission()
	spawnReq := spawnTeamRequest{
		WorkingDir: workingDir,
		Mission:    mission,
	}
	teamConfigs := buildCurrentTeamConfigs(ctx)
	perpetual := ctx.OfficeManager.IsPerpetualMode()
	envVars := ctx.getProviderEnvVars()

	newSessionID, mode := spawnAgentSession(ctx, member, spawnReq, envVars, teamConfigs, perpetual, true, nil)
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
		"sessionId":     newSessionID,
		"oldSessionId":  req.SessionID,
		"agentName":     agentName,
		"reason":        reason,
		"mode":          mode,
	})
	slog.Info("operator restarted agent in place",
		"name", agentName, "oldSession", req.SessionID,
		"newSession", newSessionID, "reason", reason)
	ok(w, map[string]interface{}{
		"sessionId":    newSessionID,
		"oldSessionId": req.SessionID,
	})
}

// buildCurrentTeamConfigs snapshots the current team's
// TeamMemberConfig list, used by spawnAgentSession when building the
// new kickstart so the restarted agent knows who their teammates are.
func buildCurrentTeamConfigs(ctx *Context) []*office.TeamMemberConfig {
	roster := ctx.OfficeManager.GetRoster()
	out := make([]*office.TeamMemberConfig, 0, len(roster))
	for _, a := range roster {
		if a == nil || a.Config == nil {
			continue
		}
		out = append(out, a.Config)
	}
	return out
}
