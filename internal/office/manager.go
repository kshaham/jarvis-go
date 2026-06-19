package office

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	urlPkg "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/kshaham/jarvis/internal/ralph"
	"github.com/kshaham/jarvis/internal/safego"
)

// execCommand + execExitError indirect the os/exec types so tests can
// swap them (not used today, but keeps the code testable).
var execCommand = exec.Command

type execExitError = exec.ExitError

// sysKill indirects syscall.Kill so process-tree teardown logic can be
// exercised in tests against fake PIDs without signaling real processes.
var sysKill = syscall.Kill

// Manager is the central coordinator for the Virtual Office.
type Manager struct {
	mu sync.RWMutex

	agents      map[string]*AgentState // sessionId -> state
	configIndex map[string]string      // configId -> sessionId

	// spawnedMembers snapshots each member's config at spawn time keyed by
	// ConfigID, so a DEAD member (no live session in the agents map) can be
	// rebuilt and respawned by the Guardian / peer-restart flow. Populated in
	// FinalizeTeamSpawn from the live roster; cleared on team teardown.
	spawnedMembers map[string]*spawnedMemberRecord
	// respawnHistory tracks per-member respawn timestamps so RespawnMemberByID
	// can enforce a backoff cap (max respawnCapCount within respawnCapWindow)
	// and refuse to loop forever on a member that keeps dying.
	respawnHistory map[string][]time.Time
	// turnCapDeaths flags, per member ConfigID, that the member's most recent
	// death was a clean per-session turn-cap rollover rather than a crash.
	// Set by MarkTurnCapDeath (from the event forwarder when EventExit carries
	// ExitReasonTurnCap) and consumed by PrepareMemberRespawn, which skips the
	// respawn-cap increment for a turn-cap rollover so legitimate steady-state
	// work can't permanently retire a perpetual member. A genuine crash flap
	// leaves the flag unset and still trips the cap.
	turnCapDeaths map[string]bool

	// blockedTaskCount is the team-wide count of tasks in TaskBlocked state.
	// Maintained atomically so the statusline handler can read it in O(1)
	// without walking all agents*tasks. Updated by NotifyTaskBlocked and
	// NotifyTaskUnblocked, which are called from applyTaskStatusLocked and
	// RemoveTask respectively.
	blockedTaskCount atomic.Int64

	fileLocks map[string]*FileLock
	meetings  map[string]*Meeting
	bugTasks  map[string]*BugTask
	messages  []OfficeMessage

	standup *StandupState

	sessions    SessionStore
	broadcast   BroadcastFunc
	auditWriter *AuditWriter
	metrics     *Metrics
	config      *Config

	// respawnMemberFn is injected by the web handler at startup (same pattern
	// as broadcast). It rebuilds a DEAD member's session from the spawn-time
	// snapshot — the office package can't reach the handler's pty.SessionManager
	// directly, so the callback bridges into respawnDeadMember. nil until wired;
	// the auto-respawn watchdog (checkForExitedMembers) no-ops while nil so a
	// manager constructed in a test without the callback is inert.
	respawnMemberFn func(configID string) error

	teamSpawned   bool
	wrappingUp    bool
	perpetualMode bool
	// perpetualDeadline is the wall-clock instant at which perpetual mode
	// auto-wraps-up (teamSpawnedAt + PerpetualDurationHours). Zero means
	// unlimited (the historical default). Set by SetPerpetualDurationHours.
	perpetualDeadline time.Time
	// perpetualDurationWrappedUp latches true once the duration deadline has
	// fired an auto-wrap-up, so checkPerpetualDurationDeadline never
	// re-triggers the wrap-up fan-out on subsequent ticks.
	perpetualDurationWrappedUp bool
	// sevenStepMode is the additive 7-step engineering discipline overlay
	// (agent-skills SDLC). Orthogonal to perpetualMode -- it governs HOW each
	// task is executed, not whether agents self-replenish.
	sevenStepMode bool
	// eccDisabled mirrors the spawn-time DisableEcc flag so non-spawn paths
	// (e.g. the onboarding endpoint, which has no request in hand) can filter
	// ECC-sourced skill recommendations a team opted out of.
	eccDisabled        bool
	teamSpawnedAt      time.Time
	wrapUpStartedAt    time.Time
	autoStandupEnabled bool
	curiosityEnabled   bool

	// gateFactory is a test seam for the objective verify gate. nil in
	// production (objectiveGate falls back to a BasicCommandGate built from
	// Config.VerifyCommand). Tests inject a fast stub so loop logic can be
	// exercised without running a real test suite.
	gateFactory func() ralph.ObjectiveGate

	// criteriaEvaluator is a test seam for the independent maker/checker
	// evaluator. nil in production (falls back to EvaluateAcceptanceCriteria,
	// which shells out to a fresh `claude` process). Lets tests exercise the
	// autonomous verification path without a live model call.
	criteriaEvaluator func(ctx context.Context, worktreeDir, summary string, criteria []string) (CriteriaVerdict, error)

	// memifyLastRunAt gates the knowledge-library memify sweep so
	// it fires roughly every memifyInterval instead of every idle
	// tick. Zero on a fresh manager — the first eligible tick runs
	// it.
	memifyLastRunAt time.Time
	// R25 · When paused, the recovery ticker skips nudging, replenishment,
	// auto-standup, and auto-brainstorms. In-flight agent turns complete
	// normally — pause is "no new input", not "suspend mid-step". Unpause
	// restores normal behavior without requiring a respawn.
	paused      bool
	pausedAt    time.Time
	pauseReason string
	teamMission string
	// projectInvariants holds the ordered list of project-wide invariants that
	// are injected into every agent prompt. Persisted to
	// ~/.jarvis/office-memory/project-invariants.json.
	projectInvariants []string
	// Dev instances keyed by slot name (default slot is "app"; fullstack
	// teams can have e.g. "frontend" + "api"). One dev server per slot.
	// Persisted individually at ~/.jarvis/office-memory/team-dev-<slot>.json.
	// All legacy getters (GetTeamDevURL etc.) route through the "app" slot
	// so R13/R14 callers keep working without changes.
	teamDevInstances map[string]*devInstance
	// Which template the current team was spawned from + its resolved
	// working directory. Exposed in the status payload so the UI can show
	// a "current team" banner while a team is running.
	lastTemplateID string
	lastWorkingDir string
	// R3.6 · Optional PR linkage. When the operator spawned the team
	// via the "work on PR #N" flow, the PR number lives here so the
	// dashboard / mobile / external tooling can map PR -> session IDs
	// (drives `claude --from-pr <n>` resume hints). Zero = no PR
	// linkage. Persisted on the in-memory team state only — recreated
	// on next spawn rather than disk-persisted because PRs are
	// short-lived and the linkage rarely outlives a team session.
	teamPRNumber      int
	lastBrainstormAt  time.Time
	lastWaterCoolerAt time.Time
	triageAttempts    map[string]int       // taskID -> triage attempt count
	lastTriageAt      map[string]time.Time // taskID -> last triage prompt time
	// Entries in the maps above are reclaimed by SweepTriageMaps (periodic,
	// against the live set of Blocked task IDs) and wholesale on
	// SetTeamSpawned(false), so they don't grow without bound. (PruneTriage is a
	// per-task helper available for callers that hold a Manager reference; the
	// AgentState status path can't call it directly since AgentState has no
	// Manager back-reference -- the sweep is the actual reclamation path.)
	lastRecoveryAt map[string]time.Time // sessionID -> last recovery prompt time (backoff)

	// autoGateInFlight guards against launching overlapping objective-gate
	// verify runs for the same session during gated auto-complete. Keyed by
	// sessionID. A verify command (e.g. `go test ./...`) is expensive, so we
	// never stack two for one agent.
	autoGateInFlight map[string]bool

	// lastLowAcceptWarnAt throttles the office:lowAcceptanceRate broadcast so a
	// persistently low accept rate doesn't spam the dashboard every sweep.
	lastLowAcceptWarnAt time.Time

	// Security-gate state (loop-engineering requirement I). lastSecurityScanAt
	// gates the bounded secret scan to a cooldown; securityBlocked latches when
	// the last scan found committed secrets, suppressing replenishment until a
	// clean scan clears it.
	lastSecurityScanAt time.Time
	securityBlocked    bool

	// Headroom failure-mining sweep (opt-in). headroomLearnEnabled gates the
	// `headroom learn` sweep that writes corrections into the project CLAUDE.md;
	// headroomLearnLastRunAt is the wall-clock gate timer.
	headroomLearnEnabled   bool
	headroomLearnLastRunAt time.Time

	// Q&A primitive (cm ask). Populated lazily by AskQuestion / AnswerQuestion
	// in questions.go. pendingQuestions is keyed by question ID;
	// lastAskAt enforces a per-asker, per-question-content cooldown so a
	// stuck agent can't pin a teammate by re-asking the same question on
	// every turn.
	pendingQuestions map[string]*Question
	lastAskAt        map[string]time.Time

	// daemonURL is the base URL spawned `claude` processes use to reach
	// this daemon's /api/claude/hooks/{sessionID}/{event} endpoints.
	// Set by SetDaemonURL during server bootstrap; baked into the
	// generated .claude/settings.local.json per spawn (see worktree.go).
	// Empty means hooks-mode spawns will skip settings injection and
	// fall back to legacy ModeClaude behaviour.
	daemonURL string

	idleTicker   *time.Ticker
	monitorDone  chan struct{}
	standupTimer *time.Ticker
	standupDone  chan struct{}

	// monitorChecks is a test seam for the idle-monitor tick's panic
	// wrapper: nil in production (defaultMonitorChecks runs), but tests
	// can inject a panicking sweep to exercise the recover() path.
	monitorChecks func()

	events chan OfficeEvent

	// R29 · Handoff audit log. Lazily initialised on first resolve so
	// tests that reset HOME don't need a global-state dance.
	handoffOnce sync.Once
	handoff     *handoffStore

	// R35 · Lead-reply callback for the Telegram two-way chat.
	// Installed by the Telegram handler; invoked on every SDK
	// turn-complete text event so the handler can forward the
	// lead's response to a pending chat.
	leadReplyCb func(sessionID, text string)

	// Telegram integration
	telegramNotifier TelegramNotifier
	telegramEnabled  bool
	updateInterval   time.Duration
	updateTicker     *time.Ticker
	updateStopCh     chan struct{}
	updateStopOnce   *sync.Once
	updateWg         sync.WaitGroup
	// telegramLifecycleMu serializes StopTelegramUpdates and
	// startPeriodicUpdates so a start's updateWg.Add(1) can never run
	// concurrently with a stop's updateWg.Wait() (which would be a WaitGroup
	// misuse / data race under concurrent SetTelegramConfig). Separate from
	// m.mu so the up-to-5s Wait doesn't block the manager.
	telegramLifecycleMu sync.Mutex
	lastTelegramUpdate  time.Time
	pendingLeaderQuery  *PendingLeaderQuery
}

// OfficeMessage represents a team message.
type OfficeMessage struct {
	ID        string    `json:"id"`
	From      string    `json:"fromSessionId"`
	To        string    `json:"toSessionId,omitempty"`
	FromName  string    `json:"fromName,omitempty"`
	ToName    string    `json:"toName,omitempty"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// Meeting represents a brainstorm or ad-hoc meeting.
type Meeting struct {
	ID           string    `json:"id"`
	Type         string    `json:"type"`
	Participants []string  `json:"participants"`
	StartedAt    time.Time `json:"startedAt"`
	Active       bool      `json:"active"`
	// FacilitatorSessionID is set when the brainstorm flow picks its
	// lead speaker (brainstorm.go). Persisted on the meeting so
	// HandleAgentDeath can detect "facilitator died mid-meeting" and
	// fire office:meetingAborted instead of letting participants sit
	// on their waitSlots until the 5-min timeout.
	FacilitatorSessionID string `json:"facilitatorSessionId,omitempty"`
}

// BugTask represents a bug filed by Lint or other agents.
type BugTask struct {
	ID          string    `json:"id"`
	ReportedBy  string    `json:"reportedBy"`
	AssignedTo  string    `json:"assignedTo"`
	File        string    `json:"file"`
	Line        int       `json:"line,omitempty"`
	Description string    `json:"description"`
	Priority    string    `json:"priority"`
	Category    string    `json:"category"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
	ResolvedAt  time.Time `json:"resolvedAt,omitempty"`
}

// StandupState tracks an active standup meeting.
type StandupState struct {
	ID           string
	Active       bool
	Participants []string
	Responses    map[string]string
	Transcript   []StandupMessage
	StartedAt    time.Time
	CurrentIndex int
}

// NewManager creates a new office manager.
func NewManager(sessions SessionStore, broadcast BroadcastFunc, config *Config) *Manager {
	if config == nil {
		config = DefaultConfig()
	}
	mgr := &Manager{
		agents:             make(map[string]*AgentState),
		configIndex:        make(map[string]string),
		spawnedMembers:     make(map[string]*spawnedMemberRecord),
		respawnHistory:     make(map[string][]time.Time),
		turnCapDeaths:      make(map[string]bool),
		fileLocks:          make(map[string]*FileLock),
		meetings:           make(map[string]*Meeting),
		bugTasks:           make(map[string]*BugTask),
		messages:           make([]OfficeMessage, 0),
		sessions:           sessions,
		broadcast:          broadcast,
		auditWriter:        NewAuditWriter(config.AuditMaxEntries),
		metrics:            NewMetrics(),
		config:             config,
		autoStandupEnabled: true,
		curiosityEnabled:   true,
		triageAttempts:     make(map[string]int),
		lastTriageAt:       make(map[string]time.Time),
		lastRecoveryAt:     make(map[string]time.Time),
		autoGateInFlight:   make(map[string]bool),
		pendingQuestions:   make(map[string]*Question),
		lastAskAt:          make(map[string]time.Time),
		events:             make(chan OfficeEvent, 64),
	}
	mgr.loadPersistedMission()
	mgr.loadPersistedInvariants()
	mgr.LoadPersistedMetrics()
	mgr.loadPersistedMessages()
	return mgr
}

// RegisterAgent adds a team member to the office.
func (m *Manager) RegisterAgent(sessionID, configID string, config *TeamMemberConfig) {
	agent := NewAgentState(sessionID, configID, config)
	agent.LastPromptSentAt = time.Now() // Mark spawn time so stuck detection can track it
	agent.manager = m                   // backlink so applyTaskStatusLocked can notify blockedTaskCount

	m.mu.Lock()
	m.agents[sessionID] = agent
	m.configIndex[configID] = sessionID
	m.mu.Unlock()

	// Resume the perpetual-loop hard-stop budget if a prior run persisted it.
	// No-op (loadLoopState ok=false) when there's no saved state, so a fresh
	// agent starts at zero. Without this the persisted iteration count is
	// write-only and the cap silently resets on every daemon restart.
	m.RestoreLoopState(agent)

	m.auditLog("office:memberJoined", sessionID, config.Name, nil)
	m.emitEvent(MemberJoinedEvent{ConfigID: configID, SessionID: sessionID, Name: config.Name})

	slog.Info("Agent registered", "name", config.Name, "session", sessionID)
}

// HandleAgentDeath handles a dead agent session: reassigns pending tasks,
// unregisters the agent, and broadcasts the death event.
func (m *Manager) HandleAgentDeath(sessionID string) {
	agent := m.GetAgent(sessionID)
	if agent == nil {
		return
	}
	// Idempotency: HandleAgentDeath races in from the event-forwarder EventExit,
	// the idle-monitor health check, and the claude-hooks SessionEnd dispatcher
	// for the same sessionID. The GetAgent!=nil check above is non-atomic (the
	// lock is released before UnregisterAgent below), so without this guard
	// concurrent callers would each reassign every pending task (doubling the
	// strategic agent's queue) and twice SIGTERM the dev-instance process tree.
	// Only the first caller runs the side effects.
	if !agent.ClaimDeath() {
		return
	}

	agentName := ""
	if agent.Config != nil {
		agentName = agent.Config.Name
	}
	slog.Warn("Agent session died", "name", agentName, "session", sessionID)

	// Reassign pending/acknowledged tasks to a strategic-tier agent (deep copy to avoid races).
	tasks := agent.GetTasks()
	for _, task := range tasks {
		if task.Status == TaskPending || task.Status == TaskAcknowledged {
			copied := *task
			copied.Source = "reassigned"
			m.mu.RLock()
			for _, other := range m.agents {
				if other.SessionID() != sessionID && other.Config != nil && other.Config.Tier == "strategic" {
					other.AddTask(&copied)
					slog.Info("Reassigned task from dead agent", "task", copied.Title, "to", other.Config.Name)
					break
				}
			}
			m.mu.RUnlock()
		}
	}

	// If the dead agent was a strategic-tier lead AND the team has no
	// other living strategic agent to hand off to, their dev instance
	// is orphaned — kill it so teammates don't keep hitting a dead URL.
	// We leave the URL alive if another lead can take over.
	wasLead := agent.Config != nil && agent.Config.Tier == "strategic"
	if wasLead {
		m.mu.RLock()
		otherLeadAlive := false
		for _, other := range m.agents {
			if other.SessionID() != sessionID && other.Config != nil && other.Config.Tier == "strategic" {
				otherLeadAlive = true
				break
			}
		}
		m.mu.RUnlock()
		if !otherLeadAlive {
			safego.Go("office.clearTeamDevInstance", func() { m.ClearTeamDevInstance(true) })
		}
	}

	// R19 · If this agent was facilitating a live meeting, abort it
	// cleanly so participants unblock immediately instead of sitting
	// on their waitSlots until the per-speaker timeout. Runs BEFORE
	// UnregisterAgent so the meeting scan still sees the mapping.
	m.abortMeetingsFacilitatedBy(sessionID, agentName, "facilitator_died")

	m.UnregisterAgent(sessionID)
	m.broadcast("office:agentDied", map[string]interface{}{
		"sessionId": sessionID, "name": agentName,
	})
}

// abortMeetingsFacilitatedBy marks any live meetings whose facilitator
// matches sessionID as aborted, delivers a sentinel to every
// participant's waitSlots so they unblock, and broadcasts
// office:meetingAborted. No retry — operator restarts manually if
// needed. Called from HandleAgentDeath.
func (m *Manager) abortMeetingsFacilitatedBy(sessionID, facilitatorName, reason string) {
	// Snapshot the meeting IDs to abort under the lock, then release
	// so we can acquire agent locks below without lock ordering
	// issues.
	m.mu.Lock()
	var toAbort []*Meeting
	for _, mtg := range m.meetings {
		if mtg != nil && mtg.Active && mtg.FacilitatorSessionID == sessionID {
			toAbort = append(toAbort, mtg)
			mtg.Active = false
		}
	}
	m.mu.Unlock()

	for _, mtg := range toAbort {
		// R21 code review: gate sentinel delivery by meeting type.
		// Before: we delivered to BrainstormSlot AND StandupSlot for
		// every participant, which could spuriously abort an
		// unrelated concurrent meeting the participant was in. Now
		// we only poke the slot type matching the dying meeting.
		// The waitSlot drop-guards cover the case where the slot
		// was already finished.
		for _, pid := range mtg.Participants {
			a := m.GetAgent(pid)
			if a == nil {
				continue
			}
			a.mu.Lock()
			bslot := a.BrainstormSlot
			sslot := a.StandupSlot
			a.mu.Unlock()
			// Note: standups don't go through m.meetings at all —
			// StartStandup uses a separate m.standup state and is
			// never scanned here. So "standup" can't show up as
			// mtg.Type in practice; the default branch handles any
			// future meeting type the same way we handle brainstorm.
			switch mtg.Type {
			case "brainstorm":
				if bslot != nil {
					bslot.deliver(meetingAbortedSentinel)
				}
			default:
				// Unknown / future meeting type — best-effort poke
				// of both slots so we fail open rather than leave
				// participants hanging.
				if bslot != nil {
					bslot.deliver(meetingAbortedSentinel)
				}
				if sslot != nil {
					sslot.deliver(meetingAbortedSentinel)
				}
			}
		}

		slog.Warn("Meeting aborted: facilitator died",
			"meetingId", mtg.ID, "type", mtg.Type, "facilitator", facilitatorName)
		m.broadcast("office:meetingAborted", map[string]interface{}{
			"meetingId":            mtg.ID,
			"type":                 mtg.Type,
			"reason":               reason,
			"facilitator":          facilitatorName,
			"facilitatorSessionId": sessionID,
		})
	}
}

// meetingAbortedSentinel is the special string delivered to a
// participant's waitSlot when the facilitator dies. The reader in
// waitForBrainstormResponse / waitForStandupResponse recognizes it
// and returns a user-facing "meeting aborted" fallback instead of
// treating it as a real response.
const meetingAbortedSentinel = "__OFFICE_MEETING_ABORTED__"

// ReassignSessionID rekeys an existing agent to a new session ID
// without losing its memory, task queue, learnings, or any other
// AgentState fields. Used by R25's in-place restart: the operator
// can kill + respawn one agent's SDK session while keeping the
// institutional knowledge intact.
//
// Also releases any file locks owned by the old session ID (they'd
// otherwise stay keyed on the dead session) and updates the configIndex
// so GetAgentByConfig keeps returning the same AgentState.
//
// Returns true if the reassignment happened. false means the old
// session wasn't registered (nothing to reassign) or the new session
// ID was already taken (shouldn't happen with UUID v4s; guard anyway).
func (m *Manager) ReassignSessionID(oldSessionID, newSessionID string) bool {
	if oldSessionID == "" || newSessionID == "" || oldSessionID == newSessionID {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	agent, ok := m.agents[oldSessionID]
	if !ok {
		return false
	}
	if _, collides := m.agents[newSessionID]; collides {
		return false
	}
	agent.setSessionID(newSessionID) // atomic; readers never tear
	m.agents[newSessionID] = agent
	delete(m.agents, oldSessionID)
	if agent.Config != nil {
		m.configIndex[agent.ConfigID] = newSessionID
	}
	// Move file locks owned by the old session to the new one so
	// the restart doesn't drop the agent's in-progress exclusivity
	// on files it was editing.
	for path, lock := range m.fileLocks {
		if lock.Owner == oldSessionID {
			lock.Owner = newSessionID
			m.fileLocks[path] = lock
		}
	}
	return true
}

// UnregisterAgent removes a team member.
func (m *Manager) UnregisterAgent(sessionID string) {
	m.mu.Lock()
	agent, ok := m.agents[sessionID]
	if ok {
		delete(m.agents, sessionID)
		delete(m.configIndex, agent.ConfigID)
	}
	// Reclaim the per-session recovery timestamp so it doesn't leak one stale
	// entry per dead/restarted session for the lifetime of the team.
	delete(m.lastRecoveryAt, sessionID)
	// Release any file locks owned by the departing agent.
	for path, lock := range m.fileLocks {
		if lock.Owner == sessionID {
			delete(m.fileLocks, path)
		}
	}
	m.mu.Unlock()

	// Drain the blocked-task count for any tasks still in TaskBlocked on the
	// departing agent. Done AFTER releasing m.mu so we can safely acquire
	// agent.mu inside CountBlockedTasks without lock-ordering concerns.
	// The agent is already removed from the map so no new task assignments
	// flow through the manager, but concurrent in-flight agent method calls
	// may still hold agent.mu — CountBlockedTasks is the safe API.
	if ok && agent != nil {
		if n := agent.CountBlockedTasks(); n > 0 {
			for i := 0; i < n; i++ {
				m.NotifyTaskUnblocked()
			}
		}
	}

	if ok {
		name := sessionID
		if agent.Config != nil {
			name = agent.Config.Name
			m.broadcast("office:memberUpdated", map[string]interface{}{
				"sessionId": sessionID, "configId": agent.ConfigID,
				"name": agent.Config.Name, "role": agent.Config.Role,
				"status": "stopped",
			})
		}
		m.auditLog("office:memberLeft", sessionID, name, nil)
		m.emitEvent(MemberLeftEvent{ConfigID: agent.ConfigID, SessionID: sessionID})
	}
}

// GetAgent returns agent state by session ID.
func (m *Manager) GetAgent(sessionID string) *AgentState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.agents[sessionID]
}

// GetAgentByConfig returns agent state by config ID.
func (m *Manager) GetAgentByConfig(configID string) *AgentState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sessionID, ok := m.configIndex[configID]
	if !ok {
		return nil
	}
	return m.agents[sessionID]
}

// GetRoster returns all registered agents.
func (m *Manager) GetRoster() []*AgentState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*AgentState, 0, len(m.agents))
	for _, a := range m.agents {
		result = append(result, a)
	}
	return result
}

// sessionIDOf returns an agent's current session ID. Now a thin nil-safe wrapper
// over the atomic AgentState.SessionID() accessor (kept because existing call
// sites reference it); no lock needed -- the read is atomic.
func (m *Manager) sessionIDOf(a *AgentState) string {
	if a == nil {
		return ""
	}
	return a.SessionID()
}

// truncRunes returns s clipped to at most maxBytes bytes, backing up to a rune
// boundary so a multibyte rune is never split (which json.Marshal would replace
// with U+FFFD). Used for short audit-log snippets.
func truncRunes(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	limit := maxBytes
	for limit > 0 && !utf8.RuneStart(s[limit]) {
		limit--
	}
	return s[:limit]
}

// GetAgentAndTouch returns the agent for sessionID and records that
// we just received an API call from them. Used by /api/office/agent/*
// handlers so the stuck-detector has a "heard from agent recently"
// signal even while a long tool-heavy turn hasn't emitted a
// turn-complete event that would otherwise update LastTurnAt.
func (m *Manager) GetAgentAndTouch(sessionID string) *AgentState {
	agent := m.GetAgent(sessionID)
	if agent != nil {
		agent.TouchAgentAPI()
	}
	return agent
}

// IsInActiveMeeting reports whether the given session is currently
// a participant in any active meeting (brainstorm etc). Used by the
// idle-check loops to avoid piling curiosity / water-cooler / other
// discretionary prompts on top of a meeting prompt the agent has
// yet to read.
//
// Scans the live meetings map under RLock. Active meetings are few
// (typically 0-2) and this is called at the idle-tick cadence, so a
// direct scan is cheap and — unlike a derived index — can never go
// stale relative to m.meetings.
func (m *Manager) IsInActiveMeeting(sessionID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.isInActiveMeetingLocked(sessionID)
}

// Sessions returns the session store for direct PTY writes.
func (m *Manager) Sessions() SessionStore {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions
}

// AgentCount returns the number of registered agents.
func (m *Manager) AgentCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.agents)
}

// IsTeamSpawned returns whether the team has been spawned.
func (m *Manager) IsTeamSpawned() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.teamSpawned
}

// TrySetTeamSpawning atomically claims the spawning slot. Returns true
// if this caller won the race (teamSpawned flipped false -> true),
// false if a team is already spawned/spawning.
//
// The plain IsTeamSpawned check in the spawn handler was racy: the
// actual mark happens ~200 lines of spawn work later, so two
// concurrent POSTs could both clear the check and both run the spawn
// loop, producing duplicate RegisterAgent calls that overwrite the
// configIndex map and orphan the first session's entry forever.
//
// Callers MUST pair this with SetTeamSpawned(false) on any rollback
// path (e.g., zero agents actually registered) so the claim is
// released instead of leaving the team in "spawned with 0 agents"
// limbo.
func (m *Manager) TrySetTeamSpawning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.teamSpawned {
		return false
	}
	m.teamSpawned = true
	m.teamSpawnedAt = time.Now()
	return true
}

// SetTeamSpawned marks the team as spawned and records the timestamp.
// When setting to false, it stops periodic Telegram updates and clears
// any pending leader query.
//
// It also starts/stops the idle-monitoring goroutine that drives stuck-agent
// detection, idle replenishment, auto-standups, and brainstorm recovery.
// Without this, agents that finish a turn with an error (e.g., Z.AI rate
// limit) sit idle forever because nothing nudges them back to work --
// production outage observed 2026-04-17.
func (m *Manager) SetTeamSpawned(v bool) {
	m.mu.Lock()
	m.teamSpawned = v
	if v {
		m.teamSpawnedAt = time.Now()
	} else {
		m.teamSpawnedAt = time.Time{}
		// Clear spawn context so a subsequent GetOfficeSettings doesn't
		// keep pointing the UI at the now-torn-down team.
		m.lastTemplateID = ""
		m.lastWorkingDir = ""
		// Clear triage tracking maps to prevent unbounded growth
		m.triageAttempts = make(map[string]int)
		m.lastTriageAt = make(map[string]time.Time)
		m.lastRecoveryAt = make(map[string]time.Time)
		// lastAskAt accumulates one entry per distinct (session, question) pair
		// and is otherwise never pruned; reset it on teardown like the siblings.
		m.lastAskAt = make(map[string]time.Time)
		// Clear the respawn snapshot + backoff history so a fresh team doesn't
		// inherit the previous team's dead-member records.
		m.spawnedMembers = make(map[string]*spawnedMemberRecord)
		m.respawnHistory = make(map[string][]time.Time)
		m.turnCapDeaths = make(map[string]bool)
		// Reset the perpetual-duration deadline + latch on teardown.
		m.perpetualDeadline = time.Time{}
		m.perpetualDurationWrappedUp = false
	}
	m.mu.Unlock()

	if v {
		m.StartIdleMonitoring()
	} else {
		m.StopIdleMonitoring()
		m.StopTelegramUpdates()
		// Team shut down — kill any orphaned dev server the lead started
		// so the next team doesn't race for ports against a zombie process.
		m.ClearTeamDevInstance(true)
		m.mu.Lock()
		m.pendingLeaderQuery = nil
		m.telegramEnabled = false
		m.mu.Unlock()
	}
}

// Events returns the event channel.
func (m *Manager) Events() <-chan OfficeEvent {
	return m.events
}

// PruneTriage drops any triage tracking entries for the given taskID. A helper
// for callers that hold a Manager reference and want to reclaim a single task's
// triage state eagerly; the routine reclamation path is SweepTriageMaps (the
// AgentState status transition can't reach the Manager to call this directly).
func (m *Manager) PruneTriage(taskID string) {
	if taskID == "" {
		return
	}
	m.mu.Lock()
	delete(m.triageAttempts, taskID)
	delete(m.lastTriageAt, taskID)
	m.mu.Unlock()
}

// BlockedTaskCount returns the current team-wide count of TaskBlocked tasks.
// The value is maintained by NotifyTaskBlocked/NotifyTaskUnblocked so this
// is O(1) — no roster or task-queue walk. Used by the statusline handler.
func (m *Manager) BlockedTaskCount() int64 {
	return m.blockedTaskCount.Load()
}

// NotifyTaskBlocked increments the team-wide blocked-task counter. Called
// from applyTaskStatusLocked (via AgentState) whenever a task transitions
// INTO TaskBlocked. The Manager pointer is passed in because AgentState
// does not hold a reference to the Manager; callers that don't have a
// manager reference (tests, standalone agents) pass nil and the call is a
// safe no-op.
func (m *Manager) NotifyTaskBlocked() {
	if m == nil {
		return
	}
	m.blockedTaskCount.Add(1)
}

// NotifyTaskUnblocked decrements the team-wide blocked-task counter. Called
// from applyTaskStatusLocked (via AgentState) whenever a task transitions
// OUT OF TaskBlocked, and from RemoveTask when a blocked task is removed.
// Clamps to zero to guard against any missed pairing.
func (m *Manager) NotifyTaskUnblocked() {
	if m == nil {
		return
	}
	for {
		cur := m.blockedTaskCount.Load()
		if cur <= 0 {
			return
		}
		if m.blockedTaskCount.CompareAndSwap(cur, cur-1) {
			return
		}
	}
}

// SweepTriageMaps drops triage-map entries whose taskIDs are no longer in the
// live set. Called periodically from the monitor so entries for agents or
// tasks that vanished (agent killed, task queue wiped) are reclaimed.
func (m *Manager) SweepTriageMaps(liveTaskIDs map[string]struct{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id := range m.triageAttempts {
		if _, alive := liveTaskIDs[id]; !alive {
			delete(m.triageAttempts, id)
			delete(m.lastTriageAt, id)
		}
	}
}

// SendMessage adds a message to the office message log and delivers it to the
// target session via WriteViaScreen so the agent actually sees it.
func (m *Manager) SendMessage(from, to, content string) {
	// Strip control chars and cap at the 8 KB message limit (not the
	// 2048 PTY flood guard). The OfficeAgentMessage handler already
	// rejects >8000; this cap is the backstop for internal/relay callers
	// so an accepted message is delivered in full instead of silently
	// truncated to 2048.
	sanitized := stripControlCapped(content, MaxMessageContentLen)
	msg := OfficeMessage{
		ID:        uuid.New().String(),
		From:      from,
		To:        to,
		Content:   sanitized,
		Timestamp: time.Now(),
	}

	// Look up agent names for fromName/toName + touch sender liveness.
	m.mu.RLock()
	var fromAgent *AgentState
	if a, ok := m.agents[from]; ok && a.Config != nil {
		msg.FromName = a.Config.Name
		fromAgent = a
	}
	if toAgent, ok := m.agents[to]; ok && toAgent.Config != nil {
		msg.ToName = toAgent.Config.Name
	}
	m.mu.RUnlock()
	if fromAgent != nil {
		fromAgent.TouchAgentAPI()
	}

	m.mu.Lock()
	m.messages = append(m.messages, msg)
	if len(m.messages) > m.config.MaxMessages {
		m.messages = m.messages[len(m.messages)-m.config.MaxMessages:]
	}
	sessions := m.sessions
	m.mu.Unlock()

	m.broadcast("office:message", msg)
	m.metrics.TotalMessages.Add(1)
	safego.Go("office.persistMessages", m.persistMessages)

	// Deliver the message to the target session so the agent sees it.
	if to != "" && sessions != nil {
		fromName := msg.FromName
		if fromName == "" {
			fromName = from
		}

		teamMsg := "[Team Message from " + fromName + "]: " + sanitized
		sessions.WriteViaScreen(to, teamMsg+"\r")
	}
}

// InjectRelayMessage delivers a cross-team message from another
// jarvis instance into the live team. Picks a strategic-tier lead as
// the recipient (falling back to the first available agent) so the
// peer team's broadcast reaches a single inbox rather than spamming
// every agent. Returns true if a live agent received the inject;
// false if no team is spawned (the relay layer will still have
// queued the message to disk, so the next spawn picks it up).
//
// The synthetic sender id "relay:<peerID>" lets the prompt builder
// recognise the source and render appropriate context to the agent.
func (m *Manager) InjectRelayMessage(fromPeerID, fromDisplayName, content string) (live bool) {
	if !m.IsTeamSpawned() {
		return false
	}
	roster := m.GetRoster()
	var target *AgentState
	for _, a := range roster {
		if a == nil || a.Config == nil {
			continue
		}
		if a.Config.Tier == "strategic" {
			target = a
			break
		}
	}
	if target == nil && len(roster) > 0 {
		target = roster[0]
	}
	if target == nil {
		return false
	}
	sanitized := sanitizeForTerminal(content)
	header := "📨 Relay from peer team \"" + fromDisplayName + "\""
	body := header + "\n\n" + sanitized
	syntheticFrom := "relay:" + fromPeerID
	// SendMessage handles the OfficeMessage append, SSE broadcast, and
	// WriteViaScreen delivery — reuse it so relay traffic flows through
	// the same audit log as regular team messages.
	// Read SessionID under m.mu (ReassignSessionID mutates it under m.mu) rather
	// than off the live roster pointer.
	m.SendMessage(syntheticFrom, m.sessionIDOf(target), body)
	return true
}

// GetMessages returns recent messages.
func (m *Manager) GetMessages() []OfficeMessage {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]OfficeMessage, len(m.messages))
	copy(result, m.messages)
	return result
}

// persistMessages writes the current messages to disk atomically.
// Errors are logged (not returned) because the call site is a
// fire-and-forget goroutine; silent failure would hide disk-full /
// permission / corrupted-state issues operators care about.
func (m *Manager) persistMessages() {
	m.mu.RLock()
	msgs := make([]OfficeMessage, len(m.messages))
	copy(msgs, m.messages)
	m.mu.RUnlock()

	homeDir, err := os.UserHomeDir()
	if err != nil {
		slog.Warn("Could not resolve home dir for office message persistence", "error", err)
		return
	}
	dir := filepath.Join(homeDir, ".jarvis", "office-memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Warn("Could not create office-memory dir", "dir", dir, "error", err)
		return
	}
	data, err := json.Marshal(msgs)
	if err != nil {
		slog.Warn("Could not marshal office messages", "count", len(msgs), "error", err)
		return
	}
	tmp := filepath.Join(dir, "messages.json.tmp")
	target := filepath.Join(dir, "messages.json")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		slog.Warn("Could not write office messages tmpfile", "path", tmp, "error", err)
		return
	}
	if err := os.Rename(tmp, target); err != nil {
		slog.Warn("Failed to persist office messages", "error", err)
		os.Remove(tmp)
	}
}

// loadPersistedMessages restores messages from disk on startup.
// R20 · parse errors quarantine the file + broadcast stateCorruption
// instead of silently zeroing the message history.
func (m *Manager) loadPersistedMessages() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}
	path := filepath.Join(homeDir, ".jarvis", "office-memory", "messages.json")
	var msgs []OfficeMessage
	present, loaded := loadJSONFile(path, &msgs, m.broadcast)
	if !present || !loaded {
		return
	}
	// R20 · Drop zero timestamps + clamp future ones so dashboard
	// sorting never shows bogus entries.
	msgs, dropped, clamped := sanitizeMessageTimestamps(msgs)
	if dropped > 0 || clamped > 0 {
		slog.Warn("sanitized persisted messages", "dropped", dropped, "clampedFuture", clamped)
	}
	m.mu.Lock()
	m.messages = msgs
	if len(m.messages) > m.config.MaxMessages {
		m.messages = m.messages[len(m.messages)-m.config.MaxMessages:]
	}
	m.mu.Unlock()
	slog.Info("Loaded persisted messages", "count", len(msgs))
}

func (m *Manager) emitEvent(event OfficeEvent) {
	select {
	case m.events <- event:
	default:
		// Consumer (server.go fans to SSE) is lagging. Silently dropping
		// used to leave the UI desynced with zero observability. Log so
		// operators can spot pathological bursts -- don't grow the
		// buffer, that just delays the symptom.
		slog.Warn("office events channel full, dropping event", "type", fmt.Sprintf("%T", event))
	}
}

func (m *Manager) auditLog(event, sessionID, agentName string, data interface{}) {
	m.auditWriter.Log(AuditEntry{
		Timestamp: time.Now(),
		Event:     event,
		SessionID: sessionID,
		AgentName: agentName,
		Data:      data,
	})
}

// GetMetricsSnapshot returns a point-in-time copy of all metrics, enriched with
// office-level cost/token totals aggregated across agents (the base Snapshot
// reports those as 0 because Metrics has no agent visibility) and the
// loop-engineering cost-per-accepted-change figure.
func (m *Manager) GetMetricsSnapshot() MetricsSnapshot {
	snap := m.metrics.Snapshot()

	var totalCost float64
	var totalIn, totalOut int64
	m.mu.RLock()
	for _, a := range m.agents {
		a.mu.Lock()
		totalCost += a.TotalCostUSD
		totalIn += a.TotalInputTokens
		totalOut += a.TotalOutputTokens
		a.mu.Unlock()
	}
	m.mu.RUnlock()

	snap.TotalCost = totalCost
	snap.TotalInputTokens = totalIn
	snap.TotalOutputTokens = totalOut
	if snap.TotalTasksCompleted > 0 {
		snap.CostPerAcceptedChange = totalCost / float64(snap.TotalTasksCompleted)
	}
	return snap
}

// GetAuditEntries returns all audit log entries.
func (m *Manager) GetAuditEntries() []AuditEntry {
	return m.auditWriter.Entries()
}

// GetConfig returns a COPY of the current office configuration, taken under the
// lock. A copy (not the live *m.config) so a caller reading fields can't race
// UpdateOfficeSettings, which mutates m.config's fields in place under m.mu.
func (m *Manager) GetConfig() *Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.config == nil {
		return &Config{}
	}
	cp := *m.config
	return &cp
}

// SetConfig replaces the office configuration.
func (m *Manager) SetConfig(cfg *Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config = cfg
}

// StartMeeting creates and stores a new meeting, returning it.
func (m *Manager) StartMeeting(meetingType string, participants []string) *Meeting {
	m.mu.Lock()
	defer m.mu.Unlock()
	meeting := &Meeting{
		ID:           uuid.New().String(),
		Type:         meetingType,
		Participants: participants,
		StartedAt:    time.Now(),
		Active:       true,
	}
	m.meetings[meeting.ID] = meeting
	return meeting
}

// GetBugTasks returns all bug tasks.
func (m *Manager) GetBugTasks() []*BugTask {
	m.mu.RLock()
	defer m.mu.RUnlock()
	// R26 concurrency review: deep copies so callers iterating /
	// serializing the result don't race with writers mutating
	// BugTask.Status / AssignedTo (UpdateBugTask, sweepOrphanedBugs).
	// Matches the R21 pattern applied to GetMeetings.
	result := make([]*BugTask, 0, len(m.bugTasks))
	for _, bt := range m.bugTasks {
		if bt == nil {
			continue
		}
		copied := *bt
		result = append(result, &copied)
	}
	return result
}

// AddBugTask adds a bug task to the office.
func (m *Manager) AddBugTask(task *BugTask) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bugTasks[task.ID] = task
	m.metrics.TotalBugs.Add(1)
	// TotalBugTasksCreated is the dashboard's dedicated "created" counter
	// (TotalBugs is the same event); increment it here so the field isn't
	// permanently zero.
	m.metrics.TotalBugTasksCreated.Add(1)
}

// UpdateBugTask updates a bug task by ID. Returns true if found.
func (m *Manager) UpdateBugTask(id string, status string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	bt, ok := m.bugTasks[id]
	if !ok {
		return false
	}
	bt.Status = status
	return true
}

// GetMeetings returns all meetings.
func (m *Manager) GetMeetings() []*Meeting {
	m.mu.RLock()
	defer m.mu.RUnlock()
	// R21 code review (third pass): return deep copies so callers
	// that iterate or serialize the result don't race with writers
	// that mutate Meeting.Active (sweepStaleMeetings,
	// abortMeetingsFacilitatedBy, endMeeting). Meetings are a small
	// map (bounded by SweepMeetings at 6h cleanup), so the copy cost
	// is negligible.
	result := make([]*Meeting, 0, len(m.meetings))
	for _, mtg := range m.meetings {
		if mtg == nil {
			continue
		}
		copied := *mtg
		// Participants slice is safe to share — it's written only
		// once at meeting creation and never mutated.
		result = append(result, &copied)
	}
	return result
}

// IsWrappingUp returns whether a wrap-up is in progress.
func (m *Manager) IsWrappingUp() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.wrappingUp
}

// SetWrappingUp sets the wrap-up state and records the timestamp.
func (m *Manager) SetWrappingUp(v bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.wrappingUp = v
	if v {
		m.wrapUpStartedAt = time.Now()
	} else {
		m.wrapUpStartedAt = time.Time{}
	}
}

// R25 · Pause / resume team-level automation. When paused, the
// recovery ticker short-circuits (no stuck-detection nudges, no
// idle-replenishment prompts, no auto-standup, no auto-brainstorm).
// In-flight agent turns still complete — pause is "no new input",
// not "suspend mid-turn". Returns the previous state so handlers
// can distinguish idempotent calls from state changes.
func (m *Manager) SetPaused(v bool, reason string) (wasPaused bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	wasPaused = m.paused
	m.paused = v
	if v {
		m.pausedAt = time.Now()
		m.pauseReason = reason
	} else {
		m.pausedAt = time.Time{}
		m.pauseReason = ""
	}
	return
}

// IsPaused returns whether the team is currently paused.
func (m *Manager) IsPaused() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.paused
}

// PauseSnapshot returns the current pause state + metadata for
// inclusion in the office-state payload.
func (m *Manager) PauseSnapshot() (paused bool, since time.Time, reason string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.paused, m.pausedAt, m.pauseReason
}

// SetPerpetualMode sets whether agents should continuously find new work.
func (m *Manager) SetPerpetualMode(v bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.perpetualMode = v
}

// IsPerpetualMode returns whether agents are in continuous improvement mode.
func (m *Manager) IsPerpetualMode() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.perpetualMode
}

// SetPerpetualDurationHours sets (or clears) the perpetual-mode wall-clock
// deadline. hours <= 0 means unlimited (perpetualDeadline is zeroed). A
// positive value anchors the deadline at teamSpawnedAt + hours, so the
// duration counts from when the team was first spawned, not from when this
// setter runs. Resets the wrapped-up latch so a re-arm (e.g. operator
// extending the run) can fire a fresh auto-wrap-up.
func (m *Manager) SetPerpetualDurationHours(hours int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if hours <= 0 {
		m.perpetualDeadline = time.Time{}
	} else {
		m.perpetualDeadline = m.teamSpawnedAt.Add(time.Duration(hours) * time.Hour)
	}
	m.perpetualDurationWrappedUp = false
}

// GetPerpetualDeadline returns the perpetual-mode auto-wrap-up deadline. A
// zero time means unlimited. Exposed for the status payload + tests.
func (m *Manager) GetPerpetualDeadline() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.perpetualDeadline
}

// spawnedMemberRecord captures everything needed to rebuild a dead member's
// session: the member config (identity, personality, skills) plus the
// team-level spawn context (workdir, provider, model, perpetual flag) that
// the original spawn used. Keyed by ConfigID in Manager.spawnedMembers.
type spawnedMemberRecord struct {
	Config     *TeamMemberConfig
	WorkingDir string
	Provider   string
	Model      string
	Perpetual  bool
}

// RecordSpawnedMembers snapshots the spawned roster so a member that later
// dies can be rebuilt by RespawnMemberByID / the peer-restart endpoint. Each
// record carries the member config plus the team-level spawn context. Called
// from FinalizeTeamSpawn once the roster is registered. Deep-copies the config
// so a later mutation of the live AgentState.Config can't tear the snapshot.
func (m *Manager) RecordSpawnedMembers(workingDir, provider, model string, perpetual bool) {
	roster := m.GetRoster()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.spawnedMembers == nil {
		m.spawnedMembers = make(map[string]*spawnedMemberRecord)
	}
	for _, a := range roster {
		if a == nil || a.Config == nil {
			continue
		}
		cfgCopy := *a.Config
		// Per-member provider/model wins over the team-level default when the
		// agent recorded its own (e.g. a Z.AI sonnet allocation or a template
		// pin). Read under the agent lock to avoid racing SetProvider.
		memberProvider, memberModel := provider, model
		a.mu.Lock()
		if a.Provider != "" {
			memberProvider = a.Provider
		}
		if a.Model != "" {
			memberModel = a.Model
		}
		a.mu.Unlock()
		m.spawnedMembers[a.ConfigID] = &spawnedMemberRecord{
			Config:     &cfgCopy,
			WorkingDir: workingDir,
			Provider:   memberProvider,
			Model:      memberModel,
			Perpetual:  perpetual,
		}
	}
}

// GetSpawnedMemberConfig returns a copy of the spawned config + spawn context
// for the given member ConfigID, or nil if the member was never recorded.
// Used by the dead-member respawn path to rebuild a session for a member whose
// live AgentState has been unregistered.
func (m *Manager) GetSpawnedMemberConfig(id string) *TeamMemberConfig {
	rec := m.getSpawnedMemberRecord(id)
	if rec == nil || rec.Config == nil {
		return nil
	}
	cp := *rec.Config
	return &cp
}

// getSpawnedMemberRecord returns a copy of the full spawned record (config +
// spawn context) for the given ConfigID under RLock, or nil if absent.
func (m *Manager) getSpawnedMemberRecord(id string) *spawnedMemberRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rec, ok := m.spawnedMembers[id]
	if !ok || rec == nil {
		return nil
	}
	cp := *rec
	if rec.Config != nil {
		cfgCopy := *rec.Config
		cp.Config = &cfgCopy
	}
	return &cp
}

// SetSevenStepMode toggles the 7-step engineering discipline overlay. It is
// additive: it combines with perpetual OR goal-focused mode and makes agents
// drive each feature through DEFINE->PLAN->BUILD->VERIFY->REVIEW->SIMPLIFY->SHIP
// using the vendored agent-skills commands + skills.
func (m *Manager) SetSevenStepMode(v bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sevenStepMode = v
}

// IsSevenStepMode returns whether the 7-step engineering overlay is active.
func (m *Manager) IsSevenStepMode() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sevenStepMode
}

// SetEccDisabled records whether the current team opted out of the ECC bundle.
func (m *Manager) SetEccDisabled(v bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.eccDisabled = v
}

// IsEccDisabled reports whether the current team opted out of the ECC bundle.
func (m *Manager) IsEccDisabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.eccDisabled
}

// GetStandup returns a snapshot of the current standup state.
// Returns a deep copy (new struct, copied slices/maps) to avoid races with the
// runStandupRoundRobin goroutine that mutates m.standup under the lock.
func (m *Manager) GetStandup() *StandupState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.standup == nil {
		return nil
	}
	participants := make([]string, len(m.standup.Participants))
	copy(participants, m.standup.Participants)
	responses := make(map[string]string, len(m.standup.Responses))
	for k, v := range m.standup.Responses {
		responses[k] = v
	}
	transcript := make([]StandupMessage, len(m.standup.Transcript))
	copy(transcript, m.standup.Transcript)
	return &StandupState{
		ID:           m.standup.ID,
		Active:       m.standup.Active,
		Participants: participants,
		Responses:    responses,
		Transcript:   transcript,
		StartedAt:    m.standup.StartedAt,
		CurrentIndex: m.standup.CurrentIndex,
	}
}

// SetStandup sets the standup state.
func (m *Manager) SetStandup(s *StandupState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.standup = s
}

// GetWhiteboard returns all whiteboard notes keyed by sessionID. The
// WhiteboardNote field is written under each agent's a.mu (SetWhiteboardNote),
// so it must be READ under a.mu too -- reading it under only m.mu (a different
// lock) is a data race. Snapshot the (sessionID, agent) pairs under m.mu, then
// read each note under its own a.mu and return a copy.
func (m *Manager) GetWhiteboard() map[string]*WhiteboardNote {
	type pair struct {
		sid string
		a   *AgentState
	}
	m.mu.RLock()
	pairs := make([]pair, 0, len(m.agents))
	for sid, a := range m.agents {
		pairs = append(pairs, pair{sid, a})
	}
	m.mu.RUnlock()

	result := make(map[string]*WhiteboardNote)
	for _, p := range pairs {
		p.a.mu.Lock()
		note := p.a.WhiteboardNote
		p.a.mu.Unlock()
		if note != nil {
			cp := *note
			result[p.sid] = &cp
		}
	}
	return result
}

// SetWhiteboardNote sets a whiteboard note for an agent.
func (m *Manager) SetWhiteboardNote(sessionID, content string) bool {
	m.mu.RLock()
	agent, ok := m.agents[sessionID]
	m.mu.RUnlock()
	if !ok {
		return false
	}
	agent.mu.Lock()
	agent.WhiteboardNote = &WhiteboardNote{
		Content:   content,
		UpdatedAt: time.Now(),
	}
	agent.mu.Unlock()
	return true
}

// ForceReleaseLock releases a file lock regardless of owner.
func (m *Manager) ForceReleaseLock(path string) bool {
	normalized := normalizePath(path)
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.fileLocks[normalized]
	if ok {
		delete(m.fileLocks, normalized)
	}
	return ok
}

// ---------------------------------------------------------------------------
// New getters/setters for Node.js parity
// ---------------------------------------------------------------------------

// IsAutoStandupEnabled returns whether auto-standup is enabled.
func (m *Manager) IsAutoStandupEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.autoStandupEnabled
}

// SetAutoStandupEnabled sets auto-standup enabled state.
func (m *Manager) SetAutoStandupEnabled(v bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.autoStandupEnabled = v
}

// IsCuriosityEnabled returns whether curiosity mode is enabled.
func (m *Manager) IsCuriosityEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.curiosityEnabled
}

// SetCuriosityEnabled sets curiosity enabled state.
func (m *Manager) SetCuriosityEnabled(v bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.curiosityEnabled = v
}

// GetTeamMission returns the current team mission.
func (m *Manager) GetTeamMission() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.teamMission
}

// SetSpawnContext records the template + working directory used to spawn
// the current team. The UI reads these back to show a "current team"
// banner while the team is active.
func (m *Manager) SetSpawnContext(templateID, workingDir string) {
	m.mu.Lock()
	m.lastTemplateID = templateID
	m.lastWorkingDir = workingDir
	m.mu.Unlock()
}

// GetSpawnContext returns the (template ID, working directory) used to
// spawn the current team. Either may be empty if unknown (e.g. pre-upgrade
// teams spawned before this field existed).
func (m *Manager) GetSpawnContext() (string, string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastTemplateID, m.lastWorkingDir
}

// VerifyCommand returns the configured objective-gate verify command (empty
// means auto-detect from the working directory). Exposed so the session-level
// Ralph trackers can attach the same gate the office loop uses.
func (m *Manager) VerifyCommand() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.VerifyCommand
}

// SetTeamPRNumber records the PR number the current team is working
// on (R3.6). Zero clears the linkage. Called from the spawn handler
// after a successful spawn that included a PR number in the payload.
func (m *Manager) SetTeamPRNumber(n int) {
	m.mu.Lock()
	m.teamPRNumber = n
	m.mu.Unlock()
}

// GetTeamPRNumber returns the linked PR number, or 0 when the current
// team isn't associated with a PR.
func (m *Manager) GetTeamPRNumber() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.teamPRNumber
}

// SetTeamMission sets the team mission, persists it to disk, and broadcasts.
func (m *Manager) SetTeamMission(s string) {
	trimmed := strings.TrimSpace(s)
	m.mu.Lock()
	m.teamMission = trimmed
	m.mu.Unlock()
	m.persistTeamMission(trimmed)
}

// persistTeamMission writes the team mission to disk atomically and
// broadcasts office:missionUpdated. R27 cleanup · Shared by
// SetTeamMission (explicit set) and AppendMissionInsight (discovered
// insight append) -- previously they each inlined the same write+rename
// logic, which drifted out of sync during earlier review rounds.
func (m *Manager) persistTeamMission(mission string) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		slog.Warn("failed to resolve home dir for mission persist", "err", err)
	} else {
		dir := filepath.Join(homeDir, ".jarvis", "office-memory")
		os.MkdirAll(dir, 0o755)
		tmp := filepath.Join(dir, "team-mission.txt.tmp")
		target := filepath.Join(dir, "team-mission.txt")
		if err := os.WriteFile(tmp, []byte(mission), 0o644); err != nil {
			slog.Warn("failed to persist team mission (write tmp)", "err", err)
		} else if err := os.Rename(tmp, target); err != nil {
			slog.Warn("failed to persist team mission (rename)", "err", err)
			_ = os.Remove(tmp)
		}
	}
	m.broadcast("office:missionUpdated", map[string]interface{}{"mission": mission})
}

// loadPersistedMission reads the team mission from disk if available.
// R20 · Invalid values broadcast stateCorruption + quarantine instead
// of silently falling back to zero (which operator would read as "no
// budget = unlimited").
func (m *Manager) loadPersistedMission() {
	homeDir, _ := os.UserHomeDir()
	missionPath := filepath.Join(homeDir, ".jarvis", "office-memory", "team-mission.txt")
	if mission, present, loaded := loadTextFile(missionPath, m.broadcast); present && loaded {
		m.teamMission = mission
	}
	// Dev instance slots — JSON files in ~/.jarvis/office-memory/ named
	// team-dev-<slot>.json. Also migrate legacy R13/R14 flat files into
	// the default "app" slot on first boot, then remove them.
	m.loadDevInstances()
}

// loadDevInstances reads every team-dev-*.json in office-memory and
// populates teamDevInstances. Also migrates the legacy R14 flat files
// (team-dev-url.txt, team-dev-pid.txt, team-dev-creds.txt) into the
// default "app" slot on first boot.
func (m *Manager) loadDevInstances() {
	homeDir, _ := os.UserHomeDir()
	dir := filepath.Join(homeDir, ".jarvis", "office-memory")
	if m.teamDevInstances == nil {
		m.teamDevInstances = map[string]*devInstance{}
	}

	// Step 1: load all existing JSON slot files.
	// R27 state review · Route through loadJSONFile so corrupt slots
	// are quarantined (renamed to .corrupt-<unix>) and an
	// office:stateCorruption event fires for the dashboard. Matches
	// the R20 pattern for messages / metrics / mission.
	entries, err := os.ReadDir(dir)
	if err == nil {
		for _, entry := range entries {
			name := entry.Name()
			if !strings.HasPrefix(name, "team-dev-") || !strings.HasSuffix(name, ".json") {
				continue
			}
			slotName := strings.TrimSuffix(strings.TrimPrefix(name, "team-dev-"), ".json")
			if !validDevSlotName(slotName) {
				continue
			}
			var inst devInstance
			present, loaded := loadJSONFile(filepath.Join(dir, name), &inst, m.broadcast)
			if !present || !loaded {
				continue
			}
			m.teamDevInstances[slotName] = &inst
		}
	}

	// Step 2: migrate legacy flat files into "app" if the JSON doesn't
	// already exist. JSON wins on conflict (covers the edge case of a
	// partial manual edit).
	if _, ok := m.teamDevInstances[defaultDevSlot]; !ok {
		legacy := devInstance{}
		migrated := false
		if data, lerr := os.ReadFile(filepath.Join(dir, "team-dev-url.txt")); lerr == nil {
			legacy.URL = strings.TrimSpace(string(data))
			migrated = migrated || legacy.URL != ""
		}
		if data, lerr := os.ReadFile(filepath.Join(dir, "team-dev-pid.txt")); lerr == nil {
			if pid, patErr := strconv.Atoi(strings.TrimSpace(string(data))); patErr == nil && pid > 0 {
				legacy.PID = pid
				migrated = true
			}
		}
		if data, lerr := os.ReadFile(filepath.Join(dir, "team-dev-creds.txt")); lerr == nil {
			legacy.Credentials = strings.TrimSpace(string(data))
			migrated = migrated || legacy.Credentials != ""
		}
		if migrated {
			m.teamDevInstances[defaultDevSlot] = &legacy
			m.persistDevInstance(defaultDevSlot, &legacy)
			slog.Info("migrated legacy dev instance flat files into app.json slot")
		}
	}

	// Step 3: clean up legacy flat files now that JSON has been written.
	// Idempotent — a later restart just finds nothing to remove.
	_ = os.Remove(filepath.Join(dir, "team-dev-url.txt"))
	_ = os.Remove(filepath.Join(dir, "team-dev-pid.txt"))
	_ = os.Remove(filepath.Join(dir, "team-dev-creds.txt"))
}

// devInstance is one dev-server slot for a team. Keyed by slot name in
// Manager.teamDevInstances ("app" is the default). Persisted to disk as
// team-dev-<slot>.json; creds go in the same file since the schema can
// grow without adding new files.
type devInstance struct {
	URL            string    `json:"url"`
	PID            int       `json:"pid"`
	Credentials    string    `json:"credentials,omitempty"`
	HealthFailures int       `json:"healthFailures,omitempty"`
	LastCheckAt    time.Time `json:"lastCheckAt,omitempty"`
	// NextCheckAt gates the next health check. Populated on failure so
	// repeated failures back off (tick×1, ×2, ×4). Zero = check next tick.
	NextCheckAt    time.Time `json:"nextCheckAt,omitempty"`
	ForceAnnounced bool      `json:"forceAnnounced,omitempty"`
}

// defaultDevSlot is the single-slot backward-compat key. R13/R14 callers
// that don't pass a name write to and read from this slot.
const defaultDevSlot = "app"

// devSlotNameRe validates slot names — simple to avoid filesystem footguns
// (slot name becomes part of the persisted filename).
var devSlotNameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,32}$`)

// validDevSlotName returns true for safe filesystem-safe slot names.
func validDevSlotName(name string) bool {
	return devSlotNameRe.MatchString(name)
}

// GetTeamDevURL returns the default "app" slot's URL (backward-compat).
func (m *Manager) GetTeamDevURL() string {
	inst, ok := m.GetDevInstance(defaultDevSlot)
	if !ok {
		return ""
	}
	return inst.URL
}

// GetTeamDevPID returns the default slot's PID (backward-compat).
func (m *Manager) GetTeamDevPID() int {
	inst, ok := m.GetDevInstance(defaultDevSlot)
	if !ok {
		return 0
	}
	return inst.PID
}

// GetTeamDevCredentials returns the default slot's credentials (backward-compat).
func (m *Manager) GetTeamDevCredentials() string {
	inst, ok := m.GetDevInstance(defaultDevSlot)
	if !ok {
		return ""
	}
	return inst.Credentials
}

// GetDevInstance returns a snapshot of the named slot (thread-safe). The
// second return is false when the slot is absent.
func (m *Manager) GetDevInstance(name string) (devInstance, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.teamDevInstances == nil {
		return devInstance{}, false
	}
	inst, ok := m.teamDevInstances[name]
	if !ok || inst == nil {
		return devInstance{}, false
	}
	// Return a value copy so callers can't mutate state via the pointer.
	return *inst, true
}

// ListDevInstances returns a snapshot of every slot keyed by name.
func (m *Manager) ListDevInstances() map[string]devInstance {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]devInstance, len(m.teamDevInstances))
	for k, v := range m.teamDevInstances {
		if v != nil {
			out[k] = *v
		}
	}
	return out
}

// HasHealthyDevInstance reports whether at least one slot has an announced
// URL with a clean health-check record (HealthFailures == 0). The wrap-up
// completion gate uses this to require a deployed, reachable live preview
// before a web-app mission is marked done.
func (m *Manager) HasHealthyDevInstance() bool {
	for _, inst := range m.ListDevInstances() {
		if inst.URL != "" && inst.HealthFailures == 0 {
			return true
		}
	}
	return false
}

// webAppMissionKeywords are conservative signals that a team's mission is a
// web app whose definition-of-done should include a working live preview.
// Kept deliberately narrow: a false negative (skipping the gate for a
// borderline team) is safer than blocking a non-web team's wrap-up.
var webAppMissionKeywords = []string{
	"site", "web", "app", "frontend", "front-end", "ui", "dashboard",
	"page", "html", "react", "next", "vite", "vue", "svelte",
}

// MissionLooksLikeWebApp returns true when the team's mission or working
// directory looks like a web-app build. It checks the working directory for
// package.json / index.html first (the strongest signal), then falls back to
// a keyword scan over the mission text.
func (m *Manager) MissionLooksLikeWebApp() bool {
	_, workingDir := m.GetSpawnContext()
	if workingDir != "" {
		for _, marker := range []string{"package.json", "index.html"} {
			if _, err := os.Stat(filepath.Join(workingDir, marker)); err == nil {
				return true
			}
		}
	}

	mission := strings.ToLower(m.GetTeamMission())
	if mission == "" {
		return false
	}
	for _, kw := range webAppMissionKeywords {
		if strings.Contains(mission, kw) {
			return true
		}
	}
	return false
}

// portFromURL extracts the host port as a string ("" if absent/invalid).
func portFromURL(raw string) string {
	u, err := urlPkg.Parse(raw)
	if err != nil || u == nil {
		return ""
	}
	return u.Port()
}

// DevInstancePortConflict returns the slot name that currently holds the
// given URL's port, or "" if free. Used by SetDevInstance to reject
// re-announces on an already-held port unless --force is passed.
func (m *Manager) DevInstancePortConflict(newName, newURL string) string {
	newPort := portFromURL(newURL)
	if newPort == "" {
		return ""
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for name, inst := range m.teamDevInstances {
		if name == newName || inst == nil || inst.URL == "" {
			continue
		}
		if portFromURL(inst.URL) == newPort {
			return name
		}
	}
	return ""
}

// SetDevInstance writes a slot. Pass force=true to skip the port-conflict
// guard. Callers should enforce localhost-reject + URL validation at the
// HTTP layer; this function only logs a warning.
func (m *Manager) SetDevInstance(name, url string, pid int, force bool) error {
	if name == "" {
		name = defaultDevSlot
	}
	if !validDevSlotName(name) {
		return fmt.Errorf("invalid slot name %q (allowed: a-z A-Z 0-9 _ - up to 32 chars)", name)
	}
	trimmed := strings.TrimSpace(url)
	if trimmed != "" && (strings.Contains(trimmed, "localhost") || strings.Contains(trimmed, "127.0.0.1")) {
		slog.Warn("dev URL appears to be localhost-only; agents browsing from headless-shell may not reach it",
			"slot", name, "url", trimmed, "hint", "use http://jarvis:<port> instead")
	}
	if pid < 0 {
		pid = 0
	}

	m.mu.Lock()
	if m.teamDevInstances == nil {
		m.teamDevInstances = map[string]*devInstance{}
	}
	// Port conflict guard — inlined inside the write lock so the check
	// and the insert are one atomic operation. If we released the lock
	// after `DevInstancePortConflict` (which takes RLock) and re-acquired
	// before the insert, two concurrent announces on the same port could
	// both pass. --force bypasses so leads can intentionally share a port
	// (e.g. a hot-reload proxy) without us second-guessing.
	if trimmed != "" && !force {
		newPort := portFromURL(trimmed)
		if newPort != "" {
			for existingName, other := range m.teamDevInstances {
				if existingName == name || other == nil || other.URL == "" {
					continue
				}
				if portFromURL(other.URL) == newPort {
					m.mu.Unlock()
					return fmt.Errorf("port %s is already held by slot %q; pick a different port or clear that slot first (or --force to override)",
						newPort, existingName)
				}
			}
		}
	}
	inst := &devInstance{
		URL:            trimmed,
		PID:            pid,
		ForceAnnounced: force && trimmed != "",
	}
	// Preserve existing credentials if this is an announce-only call.
	if prev, ok := m.teamDevInstances[name]; ok && prev != nil {
		inst.Credentials = prev.Credentials
	}
	m.teamDevInstances[name] = inst
	m.mu.Unlock()

	m.persistDevInstance(name, inst)
	m.broadcast("office:devInstanceChanged", map[string]interface{}{
		"name":           name,
		"url":            trimmed,
		"pid":            pid,
		"forceAnnounced": inst.ForceAnnounced,
	})
	if inst.ForceAnnounced {
		m.broadcast("office:devInstanceForced", map[string]interface{}{
			"name": name,
			"url":  trimmed,
		})
		slog.Warn("dev instance announced with --force", "slot", name, "url", trimmed)
	}
	return nil
}

// SetDevInstanceCredentials writes creds for the named slot. Empty clears.
func (m *Manager) SetDevInstanceCredentials(name, creds string) error {
	if name == "" {
		name = defaultDevSlot
	}
	if !validDevSlotName(name) {
		return fmt.Errorf("invalid slot name %q", name)
	}
	trimmed := strings.TrimSpace(creds)

	m.mu.Lock()
	if m.teamDevInstances == nil {
		m.teamDevInstances = map[string]*devInstance{}
	}
	inst, ok := m.teamDevInstances[name]
	if !ok || inst == nil {
		// Creating a creds-only slot (no URL yet) is useful for leads who
		// want to pre-seed credentials before starting the server.
		inst = &devInstance{}
		m.teamDevInstances[name] = inst
	}
	inst.Credentials = trimmed
	// If this slot has no URL and no PID and now no creds, drop it so
	// listings stay clean.
	empty := inst.URL == "" && inst.PID == 0 && inst.Credentials == ""
	if empty {
		delete(m.teamDevInstances, name)
	}
	m.mu.Unlock()

	if empty {
		m.removeDevInstanceFile(name)
	} else {
		m.persistDevInstance(name, inst)
	}
	m.broadcast("office:devCredentialsChanged", map[string]interface{}{
		"name": name,
		"set":  trimmed != "",
	})
	return nil
}

// SetTeamDevURL is the backward-compat wrapper for R13/R14 callers that
// target the default "app" slot. --force is off; callers who need it must
// call SetDevInstance directly (or go through the HTTP handler which
// already routes ?force=true).
func (m *Manager) SetTeamDevURL(url string, pid int) {
	_ = m.SetDevInstance(defaultDevSlot, url, pid, false)
}

// SetTeamDevCredentials is the backward-compat wrapper for the "app" slot.
func (m *Manager) SetTeamDevCredentials(creds string) {
	_ = m.SetDevInstanceCredentials(defaultDevSlot, creds)
}

// ClearDevInstance clears one slot. killProc=true SIGTERMs the tracked
// process tree (falling back to single-PID kill if pgrep isn't installed).
func (m *Manager) ClearDevInstance(name string, killProc bool) {
	if name == "" {
		name = defaultDevSlot
	}
	m.mu.RLock()
	var pid int
	if inst, ok := m.teamDevInstances[name]; ok && inst != nil {
		pid = inst.PID
	}
	m.mu.RUnlock()

	if killProc && pid >= 100 {
		killProcessTree(pid)
	}

	m.mu.Lock()
	delete(m.teamDevInstances, name)
	m.mu.Unlock()

	m.removeDevInstanceFile(name)
	m.broadcast("office:devInstanceCleared", map[string]interface{}{"name": name})
}

// ClearAllDevInstances wipes every slot. Used on team wrap-up.
func (m *Manager) ClearAllDevInstances(killProc bool) {
	m.mu.RLock()
	names := make([]string, 0, len(m.teamDevInstances))
	for n := range m.teamDevInstances {
		names = append(names, n)
	}
	m.mu.RUnlock()
	for _, n := range names {
		m.ClearDevInstance(n, killProc)
	}
}

// ClearTeamDevInstance is the backward-compat wrapper; clears every slot
// since R13/R14 callers assumed "the" dev instance. This is the right
// behavior on wrap-up / lead death: kill the whole tree.
func (m *Manager) ClearTeamDevInstance(killProc bool) {
	m.ClearAllDevInstances(killProc)
}

// persistDevInstance writes the slot JSON atomically. Credentials present
// → file mode 0600; otherwise 0644 so the URL/PID remain readable.
func (m *Manager) persistDevInstance(name string, inst *devInstance) {
	homeDir, _ := os.UserHomeDir()
	dir := filepath.Join(homeDir, ".jarvis", "office-memory")
	_ = os.MkdirAll(dir, 0o755)
	target := filepath.Join(dir, "team-dev-"+name+".json")
	tmp := target + ".tmp"
	data, err := json.MarshalIndent(inst, "", "  ")
	if err != nil {
		slog.Warn("failed to marshal dev instance", "slot", name, "err", err)
		return
	}
	mode := os.FileMode(0o644)
	if inst.Credentials != "" {
		mode = 0o600
	}
	if err := os.WriteFile(tmp, data, mode); err != nil {
		slog.Warn("failed to persist dev instance", "slot", name, "err", err)
		return
	}
	if err := os.Rename(tmp, target); err != nil {
		slog.Warn("failed to rename dev instance file", "slot", name, "err", err)
		return
	}
	_ = os.Chmod(target, mode)
}

func (m *Manager) removeDevInstanceFile(name string) {
	homeDir, _ := os.UserHomeDir()
	dir := filepath.Join(homeDir, ".jarvis", "office-memory")
	_ = os.Remove(filepath.Join(dir, "team-dev-"+name+".json"))
}

// killProcessTreeGraceTimeout / killProcessTreePollInterval govern how long
// killProcessTree waits for a SIGTERM'd tree to exit before escalating to
// SIGKILL. They are package vars (not consts) only so tests can shorten the
// wait; production behaviour is the original 3s grace / 150ms poll.
var (
	killProcessTreeGraceTimeout = 3 * time.Second
	killProcessTreePollInterval = 150 * time.Millisecond
)

// killProcessTree SIGTERMs the process and all descendants, escalating to
// SIGKILL after 3s. Falls back to single-PID signaling if pgrep isn't on
// $PATH (e.g. a minimal container image without procps).
func killProcessTree(pid int) {
	if pid < 100 {
		return
	}
	descendants := collectDescendants(pid)
	allPIDs := append([]int{}, descendants...)
	allPIDs = append(allPIDs, pid)

	// SIGTERM every process. Leaves first (descendants are collected
	// breadth-first, so reverse to signal grandchildren before parents).
	for i := len(allPIDs) - 1; i >= 0; i-- {
		_ = sysKill(allPIDs[i], syscall.SIGTERM)
	}
	slog.Info("dev server tree SIGTERM sent", "parentPID", pid, "descendants", len(descendants))

	// Wait up to 3s for the tree to exit.
	deadline := time.Now().Add(killProcessTreeGraceTimeout)
	for time.Now().Before(deadline) {
		anyAlive := false
		for _, p := range allPIDs {
			if sysKill(p, 0) == nil {
				anyAlive = true
				break
			}
		}
		if !anyAlive {
			return
		}
		time.Sleep(killProcessTreePollInterval)
	}

	// Escalate.
	survivors := 0
	for i := len(allPIDs) - 1; i >= 0; i-- {
		p := allPIDs[i]
		if sysKill(p, 0) == nil {
			if err := sysKill(p, syscall.SIGKILL); err == nil {
				survivors++
			}
		}
	}
	if survivors > 0 {
		slog.Info("dev server tree SIGKILL sent after SIGTERM timeout", "parentPID", pid, "survivors", survivors)
	}
}

// collectDescendants BFS-walks the process tree rooted at parent via
// `pgrep -P`. Returns descendants in BFS order (grandchildren last). If
// pgrep is unavailable, returns an empty slice; caller will still kill
// the parent PID on its own.
func collectDescendants(parent int) []int {
	out := []int{}
	queue := []int{parent}
	// Safety cap: don't walk more than 500 nodes; malformed trees can
	// produce cycles in pathological cases.
	visited := map[int]bool{parent: true}
	for len(queue) > 0 && len(out) < 500 {
		next := queue[0]
		queue = queue[1:]
		children, err := pgrepChildren(next)
		if err != nil {
			// pgrep missing or failed — caller falls back to single-PID kill.
			return out
		}
		for _, c := range children {
			if visited[c] {
				continue
			}
			visited[c] = true
			out = append(out, c)
			queue = append(queue, c)
		}
	}
	return out
}

// pgrepChildren runs `pgrep -P <parent>` and parses the PID list. Returns
// an error if pgrep isn't available so callers can fall back gracefully.
func pgrepChildren(parent int) ([]int, error) {
	cmd := execCommand("pgrep", "-P", strconv.Itoa(parent))
	out, err := cmd.Output()
	if err != nil {
		// Exit status 1 from pgrep = "no matches" (normal, not an error
		// for our purposes). Anything else means pgrep itself failed.
		if exitErr, ok := err.(*execExitError); ok && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		return nil, err
	}
	var pids []int
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		if p, perr := strconv.Atoi(strings.TrimSpace(line)); perr == nil && p > 0 {
			pids = append(pids, p)
		}
	}
	return pids, nil
}

// sharedDevHealthClient is reused across every slot's tick to avoid
// opening a new TCP handshake pool per tick. 5s timeout keeps the
// monitoring goroutine responsive even if a dev server hangs.
var sharedDevHealthClient = &http.Client{Timeout: 5 * time.Second}

// CheckDevInstanceHealth pings every announced slot, skipping those that
// are still in backoff (NextCheckAt > now). On 3 consecutive failures the
// URL is auto-cleared. 4xx responses (including 401/403) count as healthy
// because auth-gated dev instances legitimately return them.
func (m *Manager) CheckDevInstanceHealth() {
	m.mu.RLock()
	names := make([]string, 0, len(m.teamDevInstances))
	for n, inst := range m.teamDevInstances {
		if inst != nil && inst.URL != "" {
			names = append(names, n)
		}
	}
	m.mu.RUnlock()

	now := time.Now()
	for _, name := range names {
		m.checkOneDevInstance(name, now)
	}
}

// checkOneDevInstance runs one tick of the adaptive health-check for the
// named slot. On success the failure counter resets; on failure the
// counter increments and NextCheckAt is pushed out exponentially
// (interval×1, ×2, ×4 capped) so a rate-limited dev server doesn't get
// thrashed. Three strikes auto-clears the slot without SIGTERM (the
// process is likely dead; signaling the PID risks hitting a recycled one).
func (m *Manager) checkOneDevInstance(name string, now time.Time) {
	m.mu.RLock()
	inst, ok := m.teamDevInstances[name]
	if !ok || inst == nil || inst.URL == "" {
		m.mu.RUnlock()
		return
	}
	url := inst.URL
	nextCheckAt := inst.NextCheckAt
	m.mu.RUnlock()

	if !nextCheckAt.IsZero() && now.Before(nextCheckAt) {
		return
	}

	resp, err := sharedDevHealthClient.Get(url)
	healthy := err == nil && resp != nil && resp.StatusCode < 500
	if resp != nil {
		_ = resp.Body.Close()
	}

	m.mu.Lock()
	// Re-lookup in case the slot was cleared during the Get call.
	inst, ok = m.teamDevInstances[name]
	if !ok || inst == nil {
		m.mu.Unlock()
		return
	}
	inst.LastCheckAt = now
	if healthy {
		inst.HealthFailures = 0
		inst.NextCheckAt = time.Time{}
		m.mu.Unlock()
		return
	}
	inst.HealthFailures++
	failures := inst.HealthFailures
	// Exponential backoff: interval×1, ×2, ×4 (capped at ×4). Uses the
	// same base interval as the idle monitor so the schedule stays
	// predictable relative to other checks.
	base := m.config.IdleCheckInterval
	mult := time.Duration(1)
	if failures >= 3 {
		mult = 4
	} else if failures == 2 {
		mult = 2
	}
	inst.NextCheckAt = now.Add(base * mult)
	m.mu.Unlock()

	errMsg := "5xx"
	if err != nil {
		errMsg = err.Error()
	}
	if failures >= 3 {
		slog.Warn("dev URL unreachable after 3 consecutive checks; auto-clearing",
			"slot", name, "url", url, "lastError", errMsg)
		m.broadcast("office:devInstanceCleared", map[string]interface{}{
			"name":      name,
			"reason":    "health_check_failed",
			"lastError": errMsg,
			"url":       url,
		})
		// Don't kill — the process is almost certainly already dead, and
		// a stale PID could now belong to a recycled unrelated process.
		m.ClearDevInstance(name, false)
	}
}

// ActiveMeetingCount returns the number of active meetings.
func (m *Manager) ActiveMeetingCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, mtg := range m.meetings {
		if mtg.Active {
			count++
		}
	}
	return count
}

// ActiveLockCount returns the number of active (non-expired) file locks.
func (m *Manager) ActiveLockCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for path, lock := range m.fileLocks {
		if now.After(lock.ExpiresAt) {
			delete(m.fileLocks, path)
		}
	}
	return len(m.fileLocks)
}

// ---------------------------------------------------------------------------
// GetOfficeSettings - matches Node.js getOfficeSettings()
// ---------------------------------------------------------------------------

// GetOfficeSettings returns the full office settings matching the Node.js response shape.
func (m *Manager) GetOfficeSettings() map[string]interface{} {
	m.mu.RLock()
	// Snapshot all config fields while holding the lock to avoid torn reads
	// if another goroutine replaces m.config concurrently.
	autoStandupIntervalMs := m.config.AutoStandupInterval.Milliseconds()
	brainstormCooldownMs := m.config.BrainstormCooldown.Milliseconds()
	brainstormTimeoutMs := m.config.BrainstormTimeout.Milliseconds()
	idleThresholdMs := m.config.IdleThreshold.Milliseconds()
	idleCheckIntervalMs := m.config.IdleCheckInterval.Milliseconds()
	meetingInactivityTimeoutMs := m.config.MeetingInactivityTimeout.Milliseconds()
	standupResponseTimeoutMs := m.config.StandupResponseTimeout.Milliseconds()
	fileLockTTLMs := m.config.FileLockTTL.Milliseconds()
	curiosityCooldownMs := m.config.CuriosityCooldown.Milliseconds()
	reflectionTimeoutMs := m.config.ReflectionTimeout.Milliseconds()
	teamSpawned := m.teamSpawned
	var teamSpawnedAtMs int64
	if !m.teamSpawnedAt.IsZero() {
		teamSpawnedAtMs = m.teamSpawnedAt.UnixMilli()
	}
	autoStandupEnabled := m.autoStandupEnabled
	curiosityEnabled := m.curiosityEnabled
	teamMission := m.teamMission
	// Expose the default "app" slot URL for backward-compat consumers.
	// The full slot map is available via /api/office/agent/dev-instance/all.
	var teamDevURL string
	if inst, ok := m.teamDevInstances[defaultDevSlot]; ok && inst != nil {
		teamDevURL = inst.URL
	}
	lastTemplateID := m.lastTemplateID
	lastWorkingDir := m.lastWorkingDir
	var lastBrainstormAtMs int64
	if !m.lastBrainstormAt.IsZero() {
		lastBrainstormAtMs = m.lastBrainstormAt.UnixMilli()
	}
	standupActive := m.standup != nil && m.standup.Active
	memberCount := len(m.agents)
	activeMeetingCount := 0
	for _, mtg := range m.meetings {
		if mtg != nil && mtg.Active {
			activeMeetingCount++
		}
	}
	// Evict expired locks before counting for accuracy.
	activeLockCount := 0
	now := time.Now()
	for _, lock := range m.fileLocks {
		if now.Before(lock.ExpiresAt) {
			activeLockCount++
		}
	}
	m.mu.RUnlock()

	return map[string]interface{}{
		"autoStandup": map[string]interface{}{
			"enabled":    autoStandupEnabled,
			"intervalMs": autoStandupIntervalMs,
		},
		"brainstormCooldownMs":        brainstormCooldownMs,
		"brainstormResponseTimeoutMs": brainstormTimeoutMs,
		"idleThresholdMs":             idleThresholdMs,
		"idleCheckIntervalMs":         idleCheckIntervalMs,
		"meetingInactivityTimeoutMs":  meetingInactivityTimeoutMs,
		"standupResponseTimeoutMs":    standupResponseTimeoutMs,
		"fileLockExpiryMs":            fileLockTTLMs,
		"curiosityEnabled":            curiosityEnabled,
		"curiosityCooldownMs":         curiosityCooldownMs,
		"reflectionResponseTimeoutMs": reflectionTimeoutMs,
		"teamSpawned":                 teamSpawned,
		"teamSpawnedAt":               teamSpawnedAtMs,
		"teamTemplateId":              lastTemplateID,
		"teamWorkingDir":              lastWorkingDir,
		"memberCount":                 memberCount,
		"activeMeetingCount":          activeMeetingCount,
		"activeLockCount":             activeLockCount,
		"standupActive":               standupActive,
		"lastBrainstormAt":            lastBrainstormAtMs,
		"metrics":                     m.metrics.Snapshot(),
		"teamMission":                 teamMission,
		"teamDevURL":                  teamDevURL,
	}
}

// ---------------------------------------------------------------------------
// UpdateOfficeSettings - matches Node.js updateOfficeSettings()
// ---------------------------------------------------------------------------

// UpdateOfficeSettings applies validated setting changes from a JSON body.
func (m *Manager) UpdateOfficeSettings(settings map[string]interface{}) {
	m.mu.Lock()

	if v, ok := settings["brainstormCooldownMs"].(float64); ok && v >= 60000 {
		m.config.BrainstormCooldown = time.Duration(v) * time.Millisecond
	}
	if v, ok := settings["brainstormResponseTimeoutMs"].(float64); ok && v >= 30000 {
		m.config.BrainstormTimeout = time.Duration(v) * time.Millisecond
	}
	if v, ok := settings["idleThresholdMs"].(float64); ok && v >= 30000 {
		m.config.IdleThreshold = time.Duration(v) * time.Millisecond
	}
	if v, ok := settings["meetingInactivityTimeoutMs"].(float64); ok && v >= 10000 {
		m.config.MeetingInactivityTimeout = time.Duration(v) * time.Millisecond
	}
	if v, ok := settings["standupResponseTimeoutMs"].(float64); ok && v >= 30000 {
		m.config.StandupResponseTimeout = time.Duration(v) * time.Millisecond
	}
	if v, ok := settings["fileLockExpiryMs"].(float64); ok && v >= 60000 {
		m.config.FileLockTTL = time.Duration(v) * time.Millisecond
	}
	if v, ok := settings["curiosityEnabled"].(bool); ok {
		m.curiosityEnabled = v
	}
	// Opt-in Headroom failure-mining sweep. Off by default; writes
	// corrections into the project CLAUDE.md when enabled.
	if v, ok := settings["headroomLearnEnabled"].(bool); ok {
		m.headroomLearnEnabled = v
	}
	if v, ok := settings["curiosityCooldownMs"].(float64); ok && v >= 300000 {
		m.config.CuriosityCooldown = time.Duration(v) * time.Millisecond
	}
	if v, ok := settings["reflectionResponseTimeoutMs"].(float64); ok && v >= 15000 {
		m.config.ReflectionTimeout = time.Duration(v) * time.Millisecond
	}
	var missionUpdate string
	if v, ok := settings["teamMission"].(string); ok {
		missionUpdate = v
	}
	m.mu.Unlock()

	// SetTeamMission acquires its own lock, so call after releasing.
	if missionUpdate != "" {
		m.SetTeamMission(missionUpdate)
	}
}

// ---------------------------------------------------------------------------
// GetWrapUpStatus - matches Node.js getWrapUpStatus()
// ---------------------------------------------------------------------------

// WrapUpAgentStatus represents one agent's status during wrap-up.
type WrapUpAgentStatus struct {
	ConfigID      string  `json:"configId"`
	Name          string  `json:"name"`
	HasActiveTask bool    `json:"hasActiveTask"`
	TaskTitle     *string `json:"taskTitle"`
}

// WrapUpStatus is the response for wrap-up queries.
type WrapUpStatus struct {
	Active      bool                `json:"active"`
	StartedAt   int64               `json:"startedAt"`
	AgentStatus []WrapUpAgentStatus `json:"agentStatus"`
}

// wrapUpAgentSnap is an intermediate snapshot used by GetWrapUpStatus.
type wrapUpAgentSnap struct {
	agent    *AgentState
	configID string
	name     string
}

// GetWrapUpStatus returns the current wrap-up status with per-agent details.
// Snapshot pattern mirrors GetWhiteboard: take agent pointers + scalar fields
// under m.mu.RLock, release, THEN call agent.ActiveTask() (which takes a.mu)
// outside m.mu to avoid holding m.mu across N per-agent lock acquisitions.
func (m *Manager) GetWrapUpStatus() WrapUpStatus {
	m.mu.RLock()
	var startedAtMs int64
	if !m.wrapUpStartedAt.IsZero() {
		startedAtMs = m.wrapUpStartedAt.UnixMilli()
	}
	wrappingUp := m.wrappingUp
	snaps := make([]wrapUpAgentSnap, 0, len(m.agents))
	for _, agent := range m.agents {
		if agent.Config == nil {
			continue
		}
		snaps = append(snaps, wrapUpAgentSnap{
			agent:    agent,
			configID: agent.ConfigID,
			name:     agent.Config.Name,
		})
	}
	m.mu.RUnlock()

	agentStatus := make([]WrapUpAgentStatus, 0, len(snaps))
	for _, s := range snaps {
		active := s.agent.ActiveTask()
		status := WrapUpAgentStatus{
			ConfigID:      s.configID,
			Name:          s.name,
			HasActiveTask: active != nil,
		}
		if active != nil {
			title := active.Title
			status.TaskTitle = &title
		}
		agentStatus = append(agentStatus, status)
	}

	return WrapUpStatus{
		Active:      wrappingUp,
		StartedAt:   startedAtMs,
		AgentStatus: agentStatus,
	}
}

// ---------------------------------------------------------------------------
// BuildMemberPayload + GetOfficeState
// ---------------------------------------------------------------------------

// BuildMemberPayload returns a complete member map for SSE broadcasts and API responses.
// Always returns all fields the frontend needs -- never partial.
func (m *Manager) BuildMemberPayload(a *AgentState) map[string]interface{} {
	if a == nil || a.Config == nil {
		return nil
	}

	status := "idle"
	// Deep copy: activeTask is marshaled into the payload below and the live
	// *Task is mutated under a.mu (applyTaskStatusLocked / SetTaskProgress).
	activeTask := a.ActiveTaskCopy()
	if activeTask != nil {
		status = "working"
	} else if a.GetSDKWorking() {
		status = "working"
	} else {
		for _, t := range a.GetTasksCopy() {
			if t.Status == TaskInProgress {
				status = "working"
				break
			}
		}
	}

	emoji := ""
	avatarInitial := ""
	if len(a.Config.Name) > 0 {
		avatarInitial = string([]rune(a.Config.Name)[0:1])
		emoji = avatarInitial
	}

	learningData := LoadLearningData(a.ConfigID)
	learningCount := 0
	skillCount := 0
	if learningData != nil {
		learningCount = len(learningData.Learnings) + len(learningData.DistilledInsights)
		skillCount = len(learningData.Skills)
	}
	score := CalculateAgentScoreWithData(a, learningData)

	usage := a.Usage()
	// Snapshot provider / model / active-tool under the agent lock so we don't
	// race with SetProvider / SetActiveTool on concurrent SSE events.
	a.mu.Lock()
	provider := a.Provider
	model := a.Model
	activeTool := a.ActiveTool
	sessionMode := a.SessionMode
	voice := a.Config.Voice
	a.mu.Unlock()

	// lastActivityAt is the most recent of "last turn" or "right now" (if
	// we're in the middle of a turn). Emitted as Unix milliseconds so every
	// timestamp across the payload is the same type -- frontend used to hedge
	// between "number" and "RFC3339 string"; it no longer needs to.
	lastActivityMs := time.Now().UnixMilli()
	if !usage.LastTurnAt.IsZero() {
		lastActivityMs = usage.LastTurnAt.UnixMilli()
	}

	return map[string]interface{}{
		"sessionId":      m.sessionIDOf(a),
		"configId":       a.ConfigID,
		"name":           a.Config.Name,
		"role":           a.Config.Role,
		"tier":           a.Config.Tier,
		"color":          a.Config.Color,
		"emoji":          emoji,
		"status":         status,
		"atMeetingTable": false,
		"activeTask":     activeTask,
		"learningCount":  learningCount,
		"skillCount":     skillCount,
		"score":          score,
		"avatarColor":    a.Config.Color,
		"avatarInitial":  avatarInitial,
		"avatarEmoji":    emoji,
		// totalCost kept for frontend back-compat; usage carries the same
		// value in a structured shape alongside per-agent token counts.
		"totalCost":      usage.TotalCostUSD,
		"usage":          usage,
		"provider":       provider,
		"model":          model,
		"sessionMode":    sessionMode, // "sdk" | "hooks" | "legacy" | ""; drives the via-mode badge
		"voice":          voice,
		"lastActivityAt": lastActivityMs,
		"workingDir":     "",
		"activeTool":     activeTool,
	}
}

// GetOfficeState returns the full office state matching the Node.js response shape.
func (m *Manager) GetOfficeState() map[string]interface{} {
	m.mu.RLock()
	standupActive := m.standup != nil && m.standup.Active
	var standupTranscript interface{}
	if m.standup != nil && len(m.standup.Transcript) > 0 {
		// Return a COPY of the transcript slice so callers don't hold a
		// reference to the live slice (which could be reallocated by the
		// next append in runStandupRoundRobin after we release the lock).
		copied := make([]StandupMessage, len(m.standup.Transcript))
		copy(copied, m.standup.Transcript)
		standupTranscript = copied
	}
	teamSpawned := m.teamSpawned
	wrappingUp := m.wrappingUp
	perpetualMode := m.perpetualMode
	sevenStepMode := m.sevenStepMode
	// Template the current team was spawned from. Surfaced here (not just in
	// GetOfficeSettings) so the SPA's office-status hook can tell a one-click
	// "Demo Jarvis" team apart from a real one and offer a "Stop demo" control.
	teamTemplateID := m.lastTemplateID
	// R25 · Pause state so the dashboard can show a banner + gate
	// operator-affordances (e.g. Pause button flips to Resume).
	paused := m.paused
	pausedSince := m.pausedAt
	pauseReason := m.pauseReason
	prNumber := m.teamPRNumber
	m.mu.RUnlock()

	roster := m.GetRoster()
	members := make([]map[string]interface{}, 0, len(roster))
	for _, a := range roster {
		payload := m.BuildMemberPayload(a)
		if payload != nil {
			members = append(members, payload)
		}
	}

	messages := m.GetMessages()
	meetings := m.GetMeetings()
	locks := m.GetLocks()
	bugTasks := m.GetBugTasks()
	// Fix 3: copy only the last 50 entries directly, avoiding a full-ring copy.
	recentAudit := m.auditWriter.LastN(50)

	whiteboardMap := m.GetWhiteboard()
	whiteboardNotes := make([]interface{}, 0, len(whiteboardMap))
	for sessionID, note := range whiteboardMap {
		whiteboardNotes = append(whiteboardNotes, map[string]interface{}{
			"sessionId": sessionID,
			"content":   note.Content,
			"updatedAt": note.UpdatedAt,
		})
	}

	taskQueues := make([]interface{}, 0, len(roster))
	for _, a := range roster {
		taskQueues = append(taskQueues, map[string]interface{}{
			"sessionId": m.sessionIDOf(a),
			// Deep copies: this is marshaled by the handler outside any lock
			// while the live tasks are mutated under a.mu.
			"tasks": a.GetTasksCopy(),
		})
	}

	var wrapUp interface{}
	if wrappingUp {
		status := m.GetWrapUpStatus()
		wrapUp = status
	}

	var pauseBlock interface{}
	if paused {
		pauseBlock = map[string]interface{}{
			"since":  pausedSince.UnixMilli(),
			"reason": pauseReason,
		}
	}

	return map[string]interface{}{
		"members":           members,
		"standupActive":     standupActive,
		"standupTranscript": standupTranscript,
		"recentMessages":    messages,
		"meetings":          meetings,
		"fileLocks":         locks,
		"teamSpawned":       teamSpawned,
		"teamTemplateId":    teamTemplateID,
		"perpetualMode":     perpetualMode,
		"sevenStepMode":     sevenStepMode,
		"paused":            paused,
		"pause":             pauseBlock,
		"whiteboardNotes":   whiteboardNotes,
		"bugTasks":          bugTasks,
		"taskQueues":        taskQueues,
		"recentAudit":       recentAudit,
		"wrapUp":            wrapUp,
		"prNumber":          prNumber,
		"timestamp":         time.Now().UnixMilli(),
	}
}

// SubmitLeaderTasksResponse handles a leader's task assignment response.
// Delivers the response via the agent's LeaderTaskCh, logs the event, and
// broadcasts to SSE clients.
func (m *Manager) SubmitLeaderTasksResponse(sessionID, content string) bool {
	agent := m.GetAgent(sessionID)
	if agent == nil {
		return false
	}
	agent.TouchAgentAPI()

	agent.mu.Lock()
	slot := agent.LeaderTaskSlot
	agent.mu.Unlock()

	if slot != nil {
		if !slot.deliver(content) && slot.closed.Load() {
			recordLateResponse("leader-tasks", sessionID)
		}
	}

	agentName := sessionID
	if agent.Config != nil {
		agentName = agent.Config.Name
	}
	m.auditLog("office:leaderTasksResponse", sessionID, agentName, map[string]interface{}{
		"content": truncRunes(content, 200),
	})
	m.broadcast("office:leaderTasksResponse", map[string]interface{}{
		"sessionId": sessionID,
		"content":   content,
	})
	return true
}

// SubmitReflectionResponse handles an agent's reflection/learning response.
// Delivers the response via the agent's ReflectionCh, persists to the learning
// store, logs the event, and broadcasts to SSE clients.
// category, taskID, and taskTitle are optional metadata; pass "" for defaults.
func (m *Manager) SubmitReflectionResponse(sessionID, learning, category, taskID, taskTitle string) bool {
	agent := m.GetAgent(sessionID)
	if agent == nil {
		return false
	}
	agent.TouchAgentAPI()

	agent.mu.Lock()
	ch := agent.ReflectionCh
	agent.mu.Unlock()

	if ch != nil {
		select {
		case ch <- learning:
		default:
		}
	}

	agentName := sessionID
	configID := sessionID
	if agent.Config != nil {
		agentName = agent.Config.Name
	}
	if agent.ConfigID != "" {
		configID = agent.ConfigID
	}

	// Persist to the learning store.
	if category == "" {
		category = "general"
	}
	if err := AddLearningEntry(configID, category, learning, taskID, taskTitle); err != nil {
		slog.Warn("Failed to persist learning entry", "agent", agentName, "err", err)
	} else {
		m.metrics.TotalLearnings.Add(1)
		slog.Info("Learning entry persisted", "agent", agentName, "category", category)
	}

	// R17 · Also append the reflection to the agent's memory markdown so
	// kickstart memory-inject naturally surfaces recent lessons. Without
	// this, cm reflect → learnings.json only, and the agent's on-spawn
	// memory drifts out of sync with what they just learned.
	// Best-effort: a write failure here doesn't invalidate the learnings
	// entry above, which is the durable canonical store.
	m.mu.RLock()
	workingDir := m.lastWorkingDir
	m.mu.RUnlock()
	if workingDir != "" && configID != "" {
		ts := time.Now().UTC().Format("2006-01-02T15:04Z")
		line := fmt.Sprintf("- [%s] (%s) %s", ts, category, learning)
		if err := MemoryAppendSection(configID, workingDir, "learnings", line); err != nil {
			slog.Warn("Failed to append reflection to memory", "agent", agentName, "err", err)
		}
	}

	m.auditLog("office:reflectionResponse", sessionID, agentName, map[string]interface{}{
		"learning": truncRunes(learning, 200),
		"category": category,
	})
	m.broadcast("office:reflectionResponse", map[string]interface{}{
		"sessionId": sessionID,
		"learning":  learning,
		"category":  category,
	})

	// If the category is actionable (technique/pattern), suggest skill creation.
	m.mu.RLock()
	sessions := m.sessions
	m.mu.RUnlock()

	if sessions != nil && (category == "technique" || category == "pattern") {
		skillSuggestion := strings.Join([]string{
			"[SKILL SUGGESTION] Your reflection has been recorded.",
			"Since you discovered a reusable technique, consider creating a shared skill so other agents can benefit:",
			`  cm skill create "<descriptive-name>" "<detailed steps and when to use this>"`,
		}, "\n")
		// Use the sessionID parameter (caller-owned value), not the live
		// agent.SessionID field which ReassignSessionID mutates under m.mu.
		sessions.WriteInkPrompt(sessionID, skillSuggestion)
	}

	return true
}

// SubmitCuriosityDiscovery handles an agent's curiosity discovery.
// Delivers the response via the agent's CuriosityCh, logs the event, and
// broadcasts to SSE clients.
func (m *Manager) SubmitCuriosityDiscovery(sessionID, summary string) bool {
	agent := m.GetAgent(sessionID)
	if agent == nil {
		return false
	}
	agent.TouchAgentAPI()

	agent.mu.Lock()
	ch := agent.CuriosityCh
	agent.mu.Unlock()

	if ch != nil {
		select {
		case ch <- summary:
		default:
		}
	}

	agentName := sessionID
	if agent.Config != nil {
		agentName = agent.Config.Name
	}
	m.auditLog("office:curiosityDiscovery", sessionID, agentName, map[string]interface{}{
		"summary": truncRunes(summary, 200),
	})
	m.broadcast("office:curiosityDiscovery", map[string]interface{}{
		"sessionId": sessionID,
		"summary":   summary,
	})
	return true
}

// ---------------------------------------------------------------------------
// Per-agent project-scoped persistent memory
// ---------------------------------------------------------------------------

// ProjectHash returns a short hash of the working directory for scoping agent memory.
func ProjectHash(workingDir string) string {
	h := sha256.Sum256([]byte(workingDir))
	return fmt.Sprintf("%x", h[:4]) // 8 hex chars
}

// GetAgentMemoryPath returns the file path for an agent's project-scoped memory.
func GetAgentMemoryPath(configID, workingDir string) string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".jarvis", "agent-memory", ProjectHash(workingDir), configID+".md")
}

// LoadAgentMemory reads an agent's project-scoped memory file.
func LoadAgentMemory(configID, workingDir string) string {
	data, err := os.ReadFile(GetAgentMemoryPath(configID, workingDir))
	if err != nil {
		return ""
	}
	return string(data)
}

// SaveAgentMemory writes an agent's project-scoped memory file.
// Uses temp-file + rename so a crash or concurrent write can't leave
// the file half-written. Memory files can hold a full session of
// accumulated notes -- losing them silently to a truncation race
// isn't acceptable. Same pattern as AppendMissionInsight below.
func SaveAgentMemory(configID, workingDir, content string) error {
	path := GetAgentMemoryPath(configID, workingDir)
	os.MkdirAll(filepath.Dir(path), 0o755)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// agentMemoryOpenFile indirects the append-open syscall so tests can
// inject a writer whose WriteString / Close fail, exercising the
// disk-full / EIO branches in AppendAgentMemory that can't otherwise be
// reached through the filesystem (a real *os.File's WriteString/Close
// won't fail in-process). Mirrors experimentsOpenFile. Production opens
// the real memory file in append mode.
var agentMemoryOpenFile = func(path string) (io.WriteCloser, error) {
	return os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
}

// AppendAgentMemory appends to an agent's project-scoped memory file.
// R27 · Serializes with writeMemorySections via the shared
// memoryFileLock keyed by path. Without this, a concurrent section
// rewrite could overwrite an append (or vice versa) and leave the
// file internally inconsistent.
func AppendAgentMemory(configID, workingDir, entry string) error {
	path := GetAgentMemoryPath(configID, workingDir)
	lock := memoryFileLock(path)
	lock.Lock()
	defer lock.Unlock()
	os.MkdirAll(filepath.Dir(path), 0o755)
	f, err := agentMemoryOpenFile(path)
	if err != nil {
		return err
	}
	// Surface close errors: a buffered append that fails to flush at
	// Close() (disk full / EIO) would otherwise be lost behind defer.
	if _, err := f.Write([]byte(entry)); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close agent memory: %w", err)
	}
	return nil
}

// AppendMissionInsight appends a discovered insight to the team mission and
// persists it to disk. This creates a living mission document that improves
// across spawns (analogous to AutoAgent's program.md self-improvement).
func (m *Manager) AppendMissionInsight(insight string) {
	m.mu.Lock()
	if m.teamMission == "" || insight == "" {
		m.mu.Unlock()
		return
	}
	m.teamMission += "\n\n[Learned] " + insight
	updated := m.teamMission
	m.mu.Unlock()
	m.persistTeamMission(updated)
}

// GetTeamSpawnedAt returns the time the team was spawned, or a zero time if not spawned.
func (m *Manager) GetTeamSpawnedAt() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.teamSpawnedAt
}

// SetRespawnMemberFunc installs the dead-member respawn callback the auto-respawn
// watchdog (checkForExitedMembers) invokes when it detects a perpetual-mode
// member whose session has died. Wired once at server startup with the handler's
// respawnDeadMember method, which owns the spawnAgentSession + RegisterAgent path
// the office package can't reach directly. Lock-guarded so the watchdog's RLock
// read can't tear against this write.
func (m *Manager) SetRespawnMemberFunc(fn func(configID string) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.respawnMemberFn = fn
}

// lastRespawnAttempt returns the most recent recorded respawn timestamp for the
// given member ConfigID, or the zero time if none was ever recorded. Read under
// RLock. The watchdog uses this for its 90s per-member debounce; the authoritative
// backoff cap lives in PrepareMemberRespawn.
func (m *Manager) lastRespawnAttempt(configID string) time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var latest time.Time
	for _, t := range m.respawnHistory[configID] {
		if t.After(latest) {
			latest = t
		}
	}
	return latest
}

// SweepMeetings removes inactive meetings older than 6 hours so the meetings
// map doesn't grow unbounded across many brainstorms/standups. Returns swept count.
func (m *Manager) SweepMeetings() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	swept := 0
	for id, mtg := range m.meetings {
		if !mtg.Active && now.Sub(mtg.StartedAt) > 6*time.Hour {
			delete(m.meetings, id)
			swept++
		}
	}
	return swept
}

// staleMeetingTTL is the wall-clock ceiling for a meeting that still
// reports Active. Real brainstorms complete in 5–10 minutes; a 2-hour
// active meeting is almost certainly ghost state from a facilitator
// crash that didn't fire office:meetingAborted (e.g. before R19 landed
// or after a hard restart). Force-ending it unblocks participant UX.
const staleMeetingTTL = 2 * time.Hour

// sweepStaleMeetings force-ends meetings that have been Active longer
// than staleMeetingTTL. Broadcasts office:meetingForceEnded per meeting
// so the dashboard can toast + remove the ghost entry. Called from the
// idle-monitor tick alongside SweepMeetings.
//
// Returns the force-ended meeting IDs so tests can assert.
func (m *Manager) sweepStaleMeetings() []string {
	now := time.Now()
	m.mu.Lock()
	var stale []*Meeting
	for _, mtg := range m.meetings {
		if mtg != nil && mtg.Active && now.Sub(mtg.StartedAt) > staleMeetingTTL {
			mtg.Active = false
			stale = append(stale, mtg)
		}
	}
	m.mu.Unlock()

	ids := make([]string, 0, len(stale))
	for _, mtg := range stale {
		age := now.Sub(mtg.StartedAt)
		slog.Warn("force-ending stale meeting",
			"meetingId", mtg.ID, "type", mtg.Type,
			"age", age.Round(time.Minute).String())
		m.broadcast("office:meetingForceEnded", map[string]interface{}{
			"meetingId": mtg.ID,
			"type":      mtg.Type,
			"ageMs":     age.Milliseconds(),
			"reason":    "exceeded staleMeetingTTL",
		})
		ids = append(ids, mtg.ID)
	}
	return ids
}

// sweepOrphanedBugs clears BugTask.AssignedTo when the assignee's
// session is no longer in the agents map. Preserves the bug (operator
// may want to reassign) but removes the dangling reference so the
// dashboard doesn't show "assigned to: <dead session>". Returns the
// cleared bug IDs.
//
// Two-phase lock discipline (R21 code review fix): snapshot the list
// of orphaned IDs under m.mu, release, then broadcast. SSE Send is
// non-blocking so the prior pattern wasn't a real deadlock, but
// releasing the lock for fan-out keeps m.mu contention-free for
// unrelated reads happening on the same tick.
func (m *Manager) sweepOrphanedBugs() []string {
	m.mu.Lock()
	var cleared []string
	for id, task := range m.bugTasks {
		if task == nil || task.AssignedTo == "" {
			continue
		}
		if _, alive := m.agents[task.AssignedTo]; alive {
			continue
		}
		slog.Info("clearing orphaned bug assignment",
			"bugId", id, "deadSession", task.AssignedTo)
		task.AssignedTo = ""
		cleared = append(cleared, id)
	}
	m.mu.Unlock()

	for _, id := range cleared {
		m.broadcast("office:bugTaskOrphaned", map[string]interface{}{
			"bugId": id,
		})
	}
	return cleared
}

// SweepBugTasks removes resolved bugs older than 24h and caps at MaxBugTasks.
// Returns the number of swept entries.
func (m *Manager) SweepBugTasks() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	swept := 0

	// Remove resolved bugs older than 24h.
	for id, task := range m.bugTasks {
		if task.Status == "resolved" && now.Sub(task.CreatedAt) > 24*time.Hour {
			delete(m.bugTasks, id)
			swept++
		}
	}

	// Cap at MaxBugTasks by removing oldest resolved first, then oldest open.
	if len(m.bugTasks) > m.config.MaxBugTasks {
		type bugEntry struct {
			id   string
			task *BugTask
		}

		// Collect resolved tasks sorted by creation time (oldest first).
		var resolved []bugEntry
		var open []bugEntry
		for id, task := range m.bugTasks {
			if task.Status == "resolved" {
				resolved = append(resolved, bugEntry{id, task})
			} else {
				open = append(open, bugEntry{id, task})
			}
		}

		// Sort resolved by CreatedAt ascending.
		slices.SortFunc(resolved, func(a, b bugEntry) int {
			return a.task.CreatedAt.Compare(b.task.CreatedAt)
		})

		// Sort open by CreatedAt ascending.
		slices.SortFunc(open, func(a, b bugEntry) int {
			return a.task.CreatedAt.Compare(b.task.CreatedAt)
		})

		// Remove oldest resolved first, then oldest open until within limit.
		toRemove := len(m.bugTasks) - m.config.MaxBugTasks
		candidates := append(resolved, open...)
		for i := 0; i < toRemove && i < len(candidates); i++ {
			delete(m.bugTasks, candidates[i].id)
			swept++
		}
	}

	return swept
}

// PersistMetrics writes the current metrics snapshot to disk atomically.
func (m *Manager) PersistMetrics() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dir := filepath.Join(homeDir, ".jarvis", "office-memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}

	snap := m.metrics.Snapshot()
	data, err := json.Marshal(snap)
	if err != nil {
		return
	}
	tmpFile := filepath.Join(dir, "metrics.json.tmp")
	targetFile := filepath.Join(dir, "metrics.json")
	if err := os.WriteFile(tmpFile, data, 0o644); err != nil {
		slog.Warn("failed to persist metrics (write tmp)", "err", err)
		return
	}
	// R26 state review: log rename errors + clean up the temp file.
	if err := os.Rename(tmpFile, targetFile); err != nil {
		slog.Warn("failed to persist metrics (rename)", "err", err)
		_ = os.Remove(tmpFile)
	}
}

// LoadPersistedMetrics restores metrics counters from disk if available.
// R20 · parse error -> quarantine + broadcast, not silent reset-to-zero.
func (m *Manager) LoadPersistedMetrics() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}
	path := filepath.Join(homeDir, ".jarvis", "office-memory", "metrics.json")
	var snap MetricsSnapshot
	present, loaded := loadJSONFile(path, &snap, m.broadcast)
	if !present || !loaded {
		return
	}
	m.metrics.TotalStandups.Store(snap.TotalStandups)
	m.metrics.TotalStandupsComplete.Store(snap.TotalStandupsCompleted)
	m.metrics.TotalTasks.Store(snap.TotalTasks)
	m.metrics.TotalTasksCompleted.Store(snap.TotalTasksCompleted)
	m.metrics.TotalTasksRejected.Store(snap.TotalTasksRejected)
	m.metrics.TotalBugs.Store(snap.TotalBugs)
	m.metrics.TotalBugTasksCreated.Store(snap.TotalBugTasksCreated)
	m.metrics.TotalBugsResolved.Store(snap.TotalBugTasksResolved)
	m.metrics.TotalMessages.Store(snap.TotalMessages)
	m.metrics.TotalReflections.Store(snap.TotalReflections)
	m.metrics.TotalCuriosity.Store(snap.TotalCuriosity)
	m.metrics.TotalLearnings.Store(snap.TotalLearnings)
	m.metrics.TotalRecoveryAttempts.Store(snap.TotalRecoveryAttempts)
	m.metrics.TotalStuckAlerts.Store(snap.TotalStuckAlerts)
	m.metrics.TotalSkillsDiscovered.Store(snap.TotalSkillsDiscovered)
	slog.Info("Loaded persisted metrics from disk")
}

// MaxMessageContentLen is the 8 KB ceiling on inter-agent message,
// question, and answer text: large enough for a detailed prompt, small
// enough that a runaway can't flood the recipient's terminal/context.
// Single source of truth for the handler validation + delivery cap.
const MaxMessageContentLen = 8000

// stripControlCapped strips control characters (keeping printable runes
// plus \n and \t) using rune-safe iteration. maxRunes <= 0 means no cap.
func stripControlCapped(s string, maxRunes int) string {
	var b strings.Builder
	b.Grow(len(s))
	count := 0
	for _, r := range s {
		if maxRunes > 0 && count >= maxRunes {
			break
		}
		if r >= 32 || r == '\n' || r == '\t' {
			b.WriteRune(r)
			count++
		}
	}
	return b.String()
}

// stripControlChars strips control characters with NO length cap. Use for
// content already validated against MaxMessageContentLen so the cap isn't
// silently re-truncated to sanitizeForTerminal's flood guard.
func stripControlChars(s string) string { return stripControlCapped(s, 0) }

// sanitizeForTerminal strips control characters AND caps at 2048 runes --
// a flood guard for content written directly to a live PTY. Do not use it
// for stored/UI content with a larger documented limit; it silently
// truncates. Use stripControlChars + an explicit length check instead.
func sanitizeForTerminal(s string) string { return stripControlCapped(s, 2048) }
