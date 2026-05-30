package office

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kshaham/jarvis/internal/library"
	"github.com/kshaham/jarvis/internal/office/spawnpayload"
	"github.com/kshaham/jarvis/internal/teamtemplates"
)

// Spawn-orchestrator migration
// ============================
//
// This file is the destination for the team-spawn business logic that
// currently lives in internal/web/handler/office.go
// (`spawnTeamFromRequest`, ~550 LOC at handler/office.go:1013).
//
// The handler today owns BOTH HTTP shape (decoding the request,
// writing the response) AND the spawn business logic (resolving the
// template, building per-member configs, computing env vars,
// distributing the Z.AI rate-limit pool, spawning each session,
// broadcasting SSE events, persisting team state). The right shape:
//
//   handler.OfficeSpawnTeam(w, r) {
//       var req spawnpayload.SpawnTeamRequest
//       if err := decode(r, &req); err != nil { writeError; return }
//       resp, err := orch.SpawnTeam(r.Context(), req)
//       if err != nil { writeError; return }
//       writeJSON(w, resp)
//   }
//
//   orch := office.NewOrchestrator(officeManager, sessionManager,
//                                  sseBroker, stateStore,
//                                  agentTokenManager, ...)
//   orch.SpawnTeam(ctx, req) (*SpawnTeamResponse, error)
//
// The Orchestrator interface below is the seam. The implementation
// will land incrementally:
//
//   1. (done) Move types to internal/office/spawnpayload.
//   2. Define Orchestrator interface + a no-op stub here.
//   3. Build the concrete implementation by porting one helper at a
//      time from handler/office.go:
//        - resolveTemplate (template_id + memberOverrides → final
//          member list)
//        - buildPerMemberConfigs (member list → []*TeamMemberConfig)
//        - distributeZAIPool (when provider=z.ai, allocate rate-limit
//          tier per member)
//        - spawnLoop (the for-loop that calls spawnAgentSession per
//          member with the right stagger)
//        - postSpawnBroadcast (SSE office:teamSpawned + persistTeam)
//   4. Switch handler to call orch.SpawnTeam; keep the old function
//      under a build tag for one release as a rollback path.
//   5. Delete the old function once orch.SpawnTeam has been live for
//      a release without regression.
//
// The spawn_agent.go file in internal/web/handler already isolates
// `spawnAgentSession` + `spawnAgentSessionWithHooks` — those move into
// internal/office last (step 4/5) since they're the most coupled to
// the handler's Context type. The Orchestrator constructor will take
// the dependencies that Context holds today (SessionManager,
// SSEBroker, OfficeManager, StateStore, AgentTokenManager) and pass
// them through to the agent-spawn helpers as plain function arguments.

// SpawnTeamResponse mirrors what handler.OfficeSpawnTeam writes back
// today. Kept narrow so handler can JSON-encode it directly without
// shape translation.
type SpawnTeamResponse struct {
	Success     bool     `json:"success"`
	TeamName    string   `json:"teamName,omitempty"`
	SessionIDs  []string `json:"sessionIds,omitempty"`
	MemberCount int      `json:"memberCount,omitempty"`
	Mode        string   `json:"mode,omitempty"`
	WorkingDir  string   `json:"workingDir,omitempty"`
	Warning     string   `json:"warning,omitempty"`
}

// Orchestrator owns the team-spawn business logic. The handler
// becomes a thin shell once the implementation lands here.
//
// Methods take a context.Context so an operator-cancelled request
// (browser refresh during a long spawn) propagates cancellation into
// any pending subprocess setup.
type Orchestrator interface {
	// SpawnTeam resolves the request into a concrete team
	// composition and spawns each member's session in order,
	// honouring per-member spawn delays. Returns a response shape
	// the handler can JSON-encode directly. Errors here are
	// terminal (template not found, working dir denied, etc.) and
	// the handler should map them to HTTP 4xx / 5xx.
	SpawnTeam(ctx context.Context, req spawnpayload.SpawnTeamRequest) (*SpawnTeamResponse, error)
}

// FinalizeTeamSpawn wires up the post-spawn-loop state on a Manager
// after at least one agent has successfully registered. Order
// matters and is documented inline below — call sites must NOT
// reorder without thinking through the SSE consumer contracts.
//
// Idempotent only in the no-op sense: calling twice with the same
// args produces the same observable state. Calling with different
// args silently overwrites (typical Manager-setter semantics).
//
// Pulled out of handler.spawnTeamFromRequest so the eventual
// Orchestrator.SpawnTeam can call it directly without going through
// Context. The handler shell shrinks from six Manager calls to one.
func (m *Manager) FinalizeTeamSpawn(req spawnpayload.SpawnTeamRequest, templateUsed string, perpetual bool) {
	// Record the spawn context BEFORE SetTeamSpawned(true) so any
	// consumer listening on that signal already sees the correct
	// template + working dir on the "current team" banner.
	m.SetSpawnContext(templateUsed, req.WorkingDir)

	// Optional GitHub PR linkage so external tooling can map the PR
	// back to this team. Zero clears any prior linkage.
	m.SetTeamPRNumber(req.PRNumber)

	// Kick off the codegraph index init in the background so agents
	// have an `mcp__codegraph__*` tool surface ready by the time
	// they need it. Fires once per workingDir (de-duped inside
	// EnsureCodegraphIndex via sync.Once). Failure is non-fatal —
	// agents fall back to grep + read.
	if !spawnpayload.CodegraphOptOut(req) {
		if err := EnsureCodegraphIndex(context.Background(), req.WorkingDir); err != nil {
			slog.Debug("codegraph index skipped", "dir", req.WorkingDir, "err", err)
		}
	}

	// Flip the spawned flag (also triggers StartIdleMonitoring).
	// The flag itself is already true from TrySetTeamSpawning at
	// the start of the spawn flow; this call is what starts the
	// monitoring goroutines.
	m.SetTeamSpawned(true)

	// Apply the rest of the operator's intent.
	if req.Mission != "" {
		m.SetTeamMission(req.Mission)
	}
	m.SetPerpetualMode(perpetual)
}

// Sentinel errors returned by TriageTaskBetweenAgents. Distinct
// per failure mode so the handler can map each to the right 404
// message without string-matching.
var (
	ErrTriageSourceNotFound = errors.New("source agent not found")
	ErrTriageTargetNotFound = errors.New("target agent not found")
	ErrTriageTaskNotFound   = errors.New("task not found")
)

// TriageTaskBetweenAgents moves a task from one agent's queue to
// another's, resetting status + assignment metadata. Returns the
// source + target AgentState pointers so the caller can rebuild
// member payloads for the SSE broadcasts without a second lookup.
//
// Failure modes return distinct sentinel errors — caller writes
// 404 with the error message for each.
//
// The operation is NOT atomic across both queues (RemoveTask +
// AddTask are two separate Manager calls); if the second fails
// the source has already lost the task. Acceptable given Manager's
// in-memory model — a panic between calls would lose the task
// anyway, and there's no transaction shape to lean on.
func (m *Manager) TriageTaskBetweenAgents(sourceSessionID, targetSessionID, taskID string) (source, target *AgentState, err error) {
	source = m.GetAgentAndTouch(sourceSessionID)
	if source == nil {
		return nil, nil, ErrTriageSourceNotFound
	}
	target = m.GetAgent(targetSessionID)
	if target == nil {
		return nil, nil, ErrTriageTargetNotFound
	}
	task := source.RemoveTask(taskID)
	if task == nil {
		return nil, nil, ErrTriageTaskNotFound
	}
	// Deep-copy before stamping the fields: GetTasks hands out the *Task
	// pointers (only the slice is copied), so a recovery-monitor / handler
	// goroutine may still hold this pointer and read task.Status concurrently.
	// Mutating a private copy (as HandleAgentDeath does) avoids that data race;
	// the source no longer exposes the original after RemoveTask.
	moved := *task
	moved.Status = TaskPending
	moved.AssignedAt = time.Now()
	moved.Source = "triage"
	target.AddTask(&moved)
	return source, target, nil
}

// SnapshotRosterAsTemplateMembers projects the live roster into a
// slice of TeamTemplateMember entries suitable for persisting as a
// custom template. Personality + system-prompt overrides (set
// mid-run via OfficePersonalityOverride) take precedence over the
// template-time values so the saved template captures the team as
// it currently runs, not as it was originally spawned.
//
// Used by OfficeTemplateFromTeam ("save current team as template")
// and serves as the canonical projection any future "export team"
// flow should call.
//
// Pure modulo the GetRoster + per-agent reads. Returns an empty
// slice (not nil) on an empty roster.
func (m *Manager) SnapshotRosterAsTemplateMembers() []teamtemplates.TeamTemplateMember {
	roster := m.GetRoster()
	members := make([]teamtemplates.TeamTemplateMember, 0, len(roster))
	for _, agent := range roster {
		if agent == nil || agent.Config == nil {
			continue
		}
		cfg := agent.Config
		m := teamtemplates.TeamTemplateMember{
			ID:    cfg.ID,
			Name:  cfg.Name,
			Role:  cfg.Role,
			Tier:  cfg.Tier,
			Color: cfg.Color,
		}
		if po := agent.GetPersonalityOverride(); po != nil {
			m.Personality = po.Personality
			m.SystemPrompt = po.SystemPrompt
		} else {
			m.Personality = cfg.Personality
			m.SystemPrompt = cfg.SystemPrompt
		}
		m.Skills = cfg.Skills
		m.Voice = agent.GetVoice() // locked read: Voice is mutated by SetVoice
		members = append(members, m)
	}
	return members
}

// SessionPromptFunc is the per-session prompt-delivery callback
// the wrap-up fan-out uses. Returns true when the prompt was
// delivered (either to a PTY session or an SDK session). The
// handler's implementation tries both kinds in order and falls
// back to "missed" when neither exists for the given sessionID.
//
// Lifted via callback so the office package doesn't need to
// import pty.
type SessionPromptFunc func(sessionID, prompt string) bool

// WrapUpPrompt is the canonical wrap-up instruction blast sent to
// every agent when the team enters graceful-shutdown mode. Held
// here (not in handler/office.go) because it's an operational
// artifact — the wording is the contract with each agent's behavior
// during wrap-up, not an HTTP concern. Edit deliberately: agents
// read this verbatim and any wording drift changes their behavior.
const WrapUpPrompt = `[WRAP-UP INITIATED] The team is wrapping up. Follow these steps:

1. FINISH your current task -- complete what you're working on right now
2. COMMIT all changes with a clear commit message
3. SAVE your work context: cm memory write "<summary of what you did, current state, and any unfinished work>"
4. DO NOT start any new tasks from your queue
5. DO NOT pick up any new work
6. Report your final status: cm task complete <taskId>
7. Once you've committed, saved memory, and reported status, stop working

This is a graceful shutdown. Take the time to finish cleanly, but do not begin anything new.`

// FanOutWrapUpPrompt walks the roster, attempts to deliver the
// wrap-up prompt to each agent's session via the callback, AND
// cancels every Pending/Acknowledged task to enforce the "no new
// work" half of the wrap-up contract. Agents whose session can't
// be reached land on missedAgentNames (logged at warn level by
// the caller). In-progress tasks are left alone — the prompt
// itself tells agents to finish what they're working on.
//
// Returns:
//   - notifiedSessionIDs: the session IDs that received the prompt
//   - missedAgentNames: human-readable names for any agents whose
//     prompt delivery failed (lookup by Config.Name when set)
//   - cancelledTasks: total queued tasks pushed to TaskFailed
//
// Pure given a deterministic deliver callback. Manager state IS
// mutated (UpdateTaskStatus) — the wrap-up prompt's "no new work"
// guarantee depends on this backstop firing whether or not the
// prompt reaches each session.
func (m *Manager) FanOutWrapUpPrompt(prompt string, deliver SessionPromptFunc) (notifiedSessionIDs, missedAgentNames []string, cancelledTasks int) {
	roster := m.GetRoster()
	notifiedSessionIDs = make([]string, 0, len(roster))
	missedAgentNames = make([]string, 0)

	for _, agent := range roster {
		delivered := false
		if deliver != nil {
			delivered = deliver(agent.SessionID, prompt)
		}
		if delivered {
			notifiedSessionIDs = append(notifiedSessionIDs, agent.SessionID)
		} else {
			name := agent.SessionID
			if agent.Config != nil {
				name = agent.Config.Name
			}
			missedAgentNames = append(missedAgentNames, name)
		}

		// Cancel queued (not-yet-started) tasks. TaskInProgress is
		// deliberately left alone so agents finish current work —
		// step 1 of the wrap-up prompt.
		for _, t := range agent.GetTasksCopy() {
			if t.Status == TaskPending || t.Status == TaskAcknowledged {
				if agent.UpdateTaskStatus(t.ID, TaskFailed) {
					cancelledTasks++
				}
			}
		}
	}
	return notifiedSessionIDs, missedAgentNames, cancelledTasks
}

// nextTaskPromptFormat is the template injected into an agent's
// session when a previous task completes and another task is
// already in the queue. The prompt nudges the agent to
// acknowledge + start the next task without a manual operator
// intervention.
//
// Two %s pairs: title (for the human-readable header line) and
// taskID twice (cm acks + cm start both take the same ID).
const nextTaskPromptFormat = "[NEXT TASK] %s\nAcknowledge and start: cm task ack %s && cm task start %s"

// PromptNextTask injects the standard next-task prompt into the
// given agent's session so it picks up the queued task without an
// operator nudge. No-op when the SessionStore is nil (test
// environments without a session manager).
//
// Appends a one-hit "similar past task" snippet pulled from the
// knowledge library when one is available. The snippet is bounded
// (one hit, ~200 tokens) so the next-task prompt stays focused;
// agents wanting more depth pull it via the PageIndex MCP.
//
// Pure orchestration: formats the prompt + writes it through the
// SessionStore. Caller decides whether to invoke from a goroutine
// (the handler does, to avoid blocking the HTTP response).
func (m *Manager) PromptNextTask(sessionID, taskTitle, taskID string) {
	sessions := m.Sessions()
	if sessions == nil {
		return
	}
	prompt := fmt.Sprintf(nextTaskPromptFormat, taskTitle, taskID, taskID)
	// Append acceptance criteria checklist when the task has one.
	if agent := m.GetAgent(sessionID); agent != nil {
		for _, t := range agent.GetTasksCopy() {
			if t != nil && t.ID == taskID {
				if section := renderTaskCriteriaSection(t); section != "" {
					prompt = prompt + "\n\n" + section
				}
				break
			}
		}
	}
	if hint := m.nextTaskHint(sessionID, taskID, taskTitle); hint != "" {
		prompt = prompt + "\n\n" + hint
	}
	if avoid := m.nextTaskAntiPatternHint(sessionID, taskID, taskTitle); avoid != "" {
		prompt = prompt + "\n\n" + avoid
	}
	sessions.WriteInkPrompt(sessionID, prompt)
}

// nextTaskHint returns a small library-derived hint snippet for the
// PromptNextTask call. Looks up the agent + task by IDs so we can
// query with the right role / labels / working dir. Returns empty
// string when no hit clears the relevance threshold.
//
// One hit only — keeps the next-task prompt focused. Agents can
// always pull more via mcp__pageindex__get_page_content.
func (m *Manager) nextTaskHint(sessionID, taskID, taskTitle string) string {
	agent := m.GetAgent(sessionID)
	if agent == nil || agent.Config == nil {
		return ""
	}
	labels := agent.GetTaskLabels(taskID) // locked copy (Labels mutated under a.mu)
	hits, err := library.Search(library.Query{
		AgentRole:  agent.Config.Role,
		TaskTitle:  taskTitle,
		TaskLabels: labels,
		WorkingDir: agent.GetWorktreePath(),
		K:          1,
	})
	if err != nil || len(hits) == 0 {
		return ""
	}
	h := hits[0]
	var b strings.Builder
	b.WriteString("[SIMILAR PAST TASK] ")
	fmt.Fprintf(&b, "outcome %.2f — %s", h.OutcomeScore, h.TaskTitle)
	if h.ApproachSummary != "" {
		b.WriteString("\nApproach: ")
		b.WriteString(h.ApproachSummary)
	}
	fmt.Fprintf(&b, "\nFull doc: `%s` (read via mcp__pageindex__get_page_content)", h.Path)
	return b.String()
}

// nextTaskAntiPatternHint is the failure-mode analog of
// nextTaskHint. Looks up the agent + task by IDs, queries
// library.SearchAntiPatterns with the same role/labels/dir, returns
// the top-1 anti-pattern as a "[AVOID]" snippet. Empty string
// when no match clears the relevance threshold — the kickstart
// path remains the primary surface for these hints; this is the
// per-task moment reminder.
//
// One hit cap (same as nextTaskHint) keeps the next-task prompt
// focused. The two-hit cap on bullets keeps the prompt budget tight.
func (m *Manager) nextTaskAntiPatternHint(sessionID, taskID, taskTitle string) string {
	agent := m.GetAgent(sessionID)
	if agent == nil || agent.Config == nil {
		return ""
	}
	labels := agent.GetTaskLabels(taskID) // locked copy (Labels mutated under a.mu)
	hits, err := library.SearchAntiPatterns(library.Query{
		AgentRole:  agent.Config.Role,
		TaskTitle:  taskTitle,
		TaskLabels: labels,
		WorkingDir: agent.GetWorktreePath(),
		K:          1,
	})
	if err != nil || len(hits) == 0 {
		return ""
	}
	h := hits[0]
	var b strings.Builder
	b.WriteString("[AVOID] ")
	title := h.Title
	if title == "" {
		title = h.Slug
	}
	b.WriteString(title)
	if h.WhyItFails != "" {
		b.WriteString("\nWhy it fails: ")
		b.WriteString(h.WhyItFails)
	}
	for i, avoid := range h.WhatToAvoid {
		if i >= 2 {
			break
		}
		b.WriteString("\n- ")
		b.WriteString(avoid)
	}
	fmt.Fprintf(&b, "\nFull doc: `%s` (read via mcp__pageindex__get_page_content)", h.Path)
	return b.String()
}

// ReleaseTeamSpawnSlot rolls back the atomic spawn-slot claim made
// by TrySetTeamSpawning when the spawn loop failed to register any
// agents. Without this rollback, the team sits in
// "spawned-with-zero-agents" limbo and the operator can't retry
// the spawn without restarting jarvis.
//
// Symmetric with TrySetTeamSpawning by intent — both manipulate
// the same `teamSpawned` flag, but pairing them as named methods
// (Try-then-Release vs. plain SetTeamSpawned(false)) makes the
// atomic-claim discipline obvious at call sites.
//
// Today this is a thin wrapper around SetTeamSpawned(false). Kept
// distinct from SetTeamSpawned so the failure-rollback path can
// gain additional cleanup (e.g., clearing partial registrations)
// without affecting the normal teardown flow.
func (m *Manager) ReleaseTeamSpawnSlot() {
	m.SetTeamSpawned(false)
}

// RegisterAgentWithProvider registers a freshly-spawned agent then
// immediately records its resolved provider + model and session
// mode on the corresponding AgentState. Combines what used to be
// three handler-side calls (RegisterAgent + GetAgent + SetProvider
// + SetSessionMode) into one Manager method so the spawn loop
// doesn't need to walk the registry twice per member.
//
// sessionMode normalisation: "pty" is rewritten to "legacy" before
// it lands on AgentState. The dashboard reads the label, and a
// fresh operator finds "legacy" more meaningful than the technical
// abbreviation "pty".
//
// Returns the resulting AgentState (nil if RegisterAgent failed
// somehow, though current Manager semantics make that impossible).
func (m *Manager) RegisterAgentWithProvider(
	sessionID string,
	config *TeamMemberConfig,
	provider, model string,
	sessionMode string,
) *AgentState {
	if config == nil {
		return nil
	}
	m.RegisterAgent(sessionID, config.ID, config)
	if sessionMode == "pty" {
		sessionMode = "legacy"
	}
	agent := m.GetAgent(sessionID)
	if agent != nil {
		agent.SetProvider(provider, model)
		agent.SetSessionMode(sessionMode)
	}
	return agent
}

// StopAgentSessionFunc is the per-agent callback TeardownTeam
// invokes inside its roster loop. The implementation is handler-side
// because it touches the pty.SessionManager (which the office
// package deliberately does not import). A typical impl writes a
// shutdown message into the session, stops the subprocess, and
// cleans up any handler-side session resources.
//
// Passing nil is allowed for tests / call sites that only want the
// Manager bookkeeping (NotifyEvent, RecordExperiment, etc.) without
// touching any sessions.
type StopAgentSessionFunc func(sessionID string)

// TeardownTeam reverses everything FinalizeTeamSpawn did, plus
// records an experiment snapshot for offline analysis, plus stops
// every per-agent session via the supplied callback. Order matches
// the handler's prior inline implementation:
//
//  1. NotifyOfficeEvent("Team Shutting Down") so the UI sees the
//     shutdown intent before any session goes silent.
//  2. BuildExperimentRecord + RecordExperiment to capture metrics
//     before agent state vanishes.
//  3. StopIdleMonitoring so the recovery goroutine doesn't fire
//     mid-teardown and re-prompt a session that's already closing.
//  4. Per-agent stop via the callback, then UnregisterAgent for
//     each so the office state matches the session state.
//  5. SetTeamSpawned(false) flips the spawned flag last so any
//     "is the team alive?" check still sees true until cleanup
//     completes.
//
// Idempotent on the Manager fields (calling again after all agents
// have been unregistered is a no-op for the per-agent loop; the
// other steps run again harmlessly).
func (m *Manager) TeardownTeam(stopAgent StopAgentSessionFunc) {
	m.NotifyOfficeEvent("Team Shutting Down", "")

	// Capture an experiment snapshot before SetTeamSpawned(false)
	// blanks the spawn timestamp.
	spawnedAt := m.GetTeamSpawnedAt()
	record := m.BuildExperimentRecord(spawnedAt)
	if err := RecordExperiment(record); err != nil {
		slog.Warn("Failed to record experiment", "err", err)
	}

	// Stop the recovery / standup goroutines so they don't fire
	// mid-teardown.
	m.StopIdleMonitoring()

	// Per-agent shutdown. The callback does the session-stop work
	// (handler-side because it touches pty.SessionManager); we
	// unregister the agent on the Manager side either way.
	for _, agent := range m.GetRoster() {
		if stopAgent != nil {
			stopAgent(agent.SessionID)
		}
		m.UnregisterAgent(agent.SessionID)
	}

	m.SetTeamSpawned(false)
}

// KillAgentResult bundles the fields the handler needs for its SSE
// broadcast + log line after a single-agent kill. Kept narrow so the
// handler doesn't need to re-derive agent name or normalise the reason
// string itself.
type KillAgentResult struct {
	// AgentName is the human-readable name from Config.Name when
	// available, falling back to the sessionID otherwise. Always
	// non-empty.
	AgentName string

	// MayBroadcast is true when this caller won the
	// ClaimStopBroadcast race and should fire office:agentKilled.
	// False when checkForWedgedWorkingAgents already fired
	// office:agentIdleStopped for the same stop.
	MayBroadcast bool

	// Reason is the request reason with the default ("operator")
	// substituted in when the request omitted one. Handler uses it
	// directly in the SSE payload + log line.
	Reason string

	// AgentFound reports whether GetAgent returned a state. False
	// means the kill still ran (the session-stop + UnregisterAgent
	// calls are safe on unknown IDs) but the dashboard had no card
	// to flash anyway.
	AgentFound bool
}

// KillAgent stops one agent session, unregisters it from the office,
// and returns the metadata the handler needs to broadcast +
// log the result. The session-stop step is delegated to the supplied
// callback (handler-side because it touches pty.SessionManager).
//
// ClaimStopBroadcast race: when checkForWedgedWorkingAgents already
// fired office:agentIdleStopped, the operator-side broadcast is
// suppressed so the dashboard doesn't show two reasons for one stop.
// The session-stop + UnregisterAgent calls still run either way —
// idempotency on those sides means losing the race is harmless.
//
// reason normalisation: empty → "operator" so the SSE payload always
// carries a usable label. Pre-2026 dashboards depend on this.
func (m *Manager) KillAgent(sessionID, reason string, stopAgent StopAgentSessionFunc) KillAgentResult {
	agent := m.GetAgent(sessionID)
	agentName := sessionID
	if agent != nil && agent.Config != nil {
		agentName = agent.Config.Name
	}

	mayBroadcast := agent != nil && agent.ClaimStopBroadcast()

	if stopAgent != nil {
		stopAgent(sessionID)
	}
	m.UnregisterAgent(sessionID)

	if reason == "" {
		reason = "operator"
	}
	return KillAgentResult{
		AgentName:    agentName,
		MayBroadcast: mayBroadcast,
		Reason:       reason,
		AgentFound:   agent != nil,
	}
}

// ErrAgentMissingConfig signals OfficeAgentRestart that the live
// AgentState lacks the Config pointer it needs to rebuild a
// kickstart. Should be impossible in practice; surfaced so the
// handler can map it to HTTP 500.
var ErrAgentMissingConfig = errors.New("agent has no config (cannot rebuild kickstart)")

// ErrRestartWorkingDirUnknown signals that no working directory
// could be determined for the restart (neither the live SDK session
// nor the agent's WorktreePath was set). Restart cannot proceed
// without one — the new SDK session needs a cwd.
var ErrRestartWorkingDirUnknown = errors.New("cannot determine working directory for restart")

// BuildRestartTemplateMember projects an existing AgentState into a
// teamtemplates.TeamTemplateMember suitable for feeding back into
// the spawn pipeline as a single-member restart. Preserves
// personality + system prompt from the agent's Config (NOT from any
// runtime personality-override) so the restarted session boots from
// the same template-time identity it was created under.
//
// Returns ErrAgentMissingConfig when agent.Config is nil — the
// caller (handler) maps that to HTTP 500 since rebuilding without a
// config would lose the agent's identity.
func BuildRestartTemplateMember(agent *AgentState) (teamtemplates.TeamTemplateMember, error) {
	if agent == nil || agent.Config == nil {
		return teamtemplates.TeamTemplateMember{}, ErrAgentMissingConfig
	}
	cfg := agent.Config
	return teamtemplates.TeamTemplateMember{
		ID:           cfg.ID,
		Name:         cfg.Name,
		Role:         cfg.Role,
		Tier:         cfg.Tier,
		Color:        cfg.Color,
		Personality:  cfg.Personality,
		Voice:        agent.GetVoice(), // locked read: Voice is mutated by SetVoice
		SystemPrompt: cfg.SystemPrompt,
		Skills:       cfg.Skills,
	}, nil
}

// ResolveRestartWorkingDir picks the working directory for a
// restart. Priority:
//  1. sdkWorkingDir — the live SDK session's cwd. Caller looks it up
//     before invoking us because the SessionManager dependency lives
//     in handler.
//  2. agent.GetWorktreePath() — falls back to the bookkeeping field
//     R25's workdir-bump writes to. Same field spawnAgentSession
//     sets at spawn time, so it's always populated for a healthy
//     agent.
//
// Returns "" only when both sources are empty (genuinely missing
// path); caller maps that to ErrRestartWorkingDirUnknown.
func ResolveRestartWorkingDir(agent *AgentState, sdkWorkingDir string) string {
	if sdkWorkingDir != "" {
		return sdkWorkingDir
	}
	if agent == nil {
		return ""
	}
	return agent.GetWorktreePath()
}

// SnapshotRosterAsTeamMemberConfigs returns a flat slice of the
// live roster's TeamMemberConfig pointers, skipping nil entries.
// Used by the restart flow to seed the new agent's kickstart with
// its teammates' identities so it knows the rest of the team's
// names + roles on first turn.
//
// Mirror of the handler's buildCurrentTeamConfigs but on Manager so
// the restart path doesn't need a *Context pointer.
func (m *Manager) SnapshotRosterAsTeamMemberConfigs() []*TeamMemberConfig {
	roster := m.GetRoster()
	out := make([]*TeamMemberConfig, 0, len(roster))
	for _, a := range roster {
		if a == nil || a.Config == nil {
			continue
		}
		out = append(out, a.Config)
	}
	return out
}

// NewOrchestrator is the real constructor (not implemented yet). When
// the migration lands this will take the dependencies the handler's
// Context holds today: SessionManager, SSEBroker, OfficeManager,
// StateStore, AgentTokenManager, ToolPolicyStore. Signature
// intentionally undefined — adding it now would invite premature
// importing.
//
// TODO(spawn-orchestrator): build the constructor + concrete impl
// per the migration plan in this file's package comment.
