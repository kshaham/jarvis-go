package office

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
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
	"syscall"
	"time"

	"github.com/google/uuid"
)

// execCommand + execExitError indirect the os/exec types so tests can
// swap them (not used today, but keeps the code testable).
var execCommand = exec.Command

type execExitError = exec.ExitError

// Manager is the central coordinator for the Virtual Office.
type Manager struct {
	mu sync.RWMutex

	agents      map[string]*AgentState // sessionId -> state
	configIndex map[string]string      // configId -> sessionId

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

	teamSpawned        bool
	wrappingUp         bool
	perpetualMode      bool
	teamSpawnedAt      time.Time
	wrapUpStartedAt    time.Time
	autoStandupEnabled bool
	curiosityEnabled   bool
	teamMission        string
	// Daily USD spend budget for the whole team. 0 = unset / no budget.
	// Purely a UI signal — used by the header budget pill and the
	// Insights progress bar. Persisted across restarts.
	teamDailyBudget    float64
	// Dev instances keyed by slot name (default slot is "app"; fullstack
	// teams can have e.g. "frontend" + "api"). One dev server per slot.
	// Persisted individually at ~/.jarvis/office-memory/team-dev-<slot>.json.
	// All legacy getters (GetTeamDevURL etc.) route through the "app" slot
	// so R13/R14 callers keep working without changes.
	teamDevInstances   map[string]*devInstance
	// Which template the current team was spawned from + its resolved
	// working directory. Exposed in the status payload so the UI can show
	// a "current team" banner while a team is running.
	lastTemplateID   string
	lastWorkingDir   string
	lastBrainstormAt   time.Time
	lastWaterCoolerAt  time.Time
	triageAttempts     map[string]int       // taskID -> triage attempt count
	lastTriageAt       map[string]time.Time // taskID -> last triage prompt time
	// Entries in the maps above are pruned when a task moves out of Blocked
	// state via pruneTriage (called from AgentState.UpdateTaskStatus through
	// the Manager), and swept against live-task IDs in
	// checkBlockedTasksForLeaderTriage so orphaned entries from deleted
	// agents don't linger forever.
	lastRecoveryAt     map[string]time.Time // sessionID -> last recovery prompt time (backoff)

	idleTicker   *time.Ticker
	monitorDone  chan struct{}
	standupTimer *time.Ticker
	standupDone  chan struct{}

	events chan OfficeEvent

	// Telegram integration
	telegramNotifier   TelegramNotifier
	telegramEnabled    bool
	updateInterval     time.Duration
	updateTicker       *time.Ticker
	updateStopCh       chan struct{}
	updateStopOnce     *sync.Once
	updateWg           sync.WaitGroup
	lastTelegramUpdate time.Time
	pendingLeaderQuery *PendingLeaderQuery
}

// OfficeMessage represents a team message.
type OfficeMessage struct {
	ID            string    `json:"id"`
	From          string    `json:"fromSessionId"`
	To            string    `json:"toSessionId,omitempty"`
	FromName      string    `json:"fromName,omitempty"`
	ToName        string    `json:"toName,omitempty"`
	Content       string    `json:"content"`
	Timestamp     time.Time `json:"timestamp"`
}

// Meeting represents a brainstorm or ad-hoc meeting.
type Meeting struct {
	ID           string    `json:"id"`
	Type         string    `json:"type"`
	Participants []string  `json:"participants"`
	StartedAt    time.Time `json:"startedAt"`
	Active       bool      `json:"active"`
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
		agents:      make(map[string]*AgentState),
		configIndex: make(map[string]string),
		fileLocks:   make(map[string]*FileLock),
		meetings:    make(map[string]*Meeting),
		bugTasks:    make(map[string]*BugTask),
		messages:    make([]OfficeMessage, 0),
		sessions:    sessions,
		broadcast:   broadcast,
		auditWriter: NewAuditWriter(config.AuditMaxEntries),
		metrics:            NewMetrics(),
		config:             config,
		autoStandupEnabled: true,
		curiosityEnabled:   true,
		triageAttempts:     make(map[string]int),
		lastTriageAt:       make(map[string]time.Time),
		lastRecoveryAt:     make(map[string]time.Time),
		events:             make(chan OfficeEvent, 64),
	}
	mgr.loadPersistedMission()
	mgr.LoadPersistedMetrics()
	mgr.loadPersistedMessages()
	return mgr
}

// RegisterAgent adds a team member to the office.
func (m *Manager) RegisterAgent(sessionID, configID string, config *TeamMemberConfig) {
	agent := NewAgentState(sessionID, configID, config)
	agent.LastPromptSentAt = time.Now() // Mark spawn time so stuck detection can track it

	m.mu.Lock()
	m.agents[sessionID] = agent
	m.configIndex[configID] = sessionID
	m.mu.Unlock()

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
				if other.SessionID != sessionID && other.Config != nil && other.Config.Tier == "strategic" {
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
			if other.SessionID != sessionID && other.Config != nil && other.Config.Tier == "strategic" {
				otherLeadAlive = true
				break
			}
		}
		m.mu.RUnlock()
		if !otherLeadAlive {
			go m.ClearTeamDevInstance(true)
		}
	}

	m.UnregisterAgent(sessionID)
	m.broadcast("office:agentDied", map[string]interface{}{
		"sessionId": sessionID, "name": agentName,
	})
}

// UnregisterAgent removes a team member.
func (m *Manager) UnregisterAgent(sessionID string) {
	m.mu.Lock()
	agent, ok := m.agents[sessionID]
	if ok {
		delete(m.agents, sessionID)
		delete(m.configIndex, agent.ConfigID)
	}
	// Release any file locks owned by the departing agent.
	for path, lock := range m.fileLocks {
		if lock.Owner == sessionID {
			delete(m.fileLocks, path)
		}
	}
	m.mu.Unlock()

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
func (m *Manager) IsInActiveMeeting(sessionID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, mtg := range m.meetings {
		if !mtg.Active {
			continue
		}
		for _, p := range mtg.Participants {
			if p == sessionID {
				return true
			}
		}
	}
	return false
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

// PruneTriage drops any triage tracking entries for the given taskID. Called
// from AgentState.UpdateTaskStatus whenever a task transitions out of Blocked
// (completion, failure, or just back to in-progress) so the triage maps don't
// grow without bound across long-running teams.
func (m *Manager) PruneTriage(taskID string) {
	if taskID == "" {
		return
	}
	m.mu.Lock()
	delete(m.triageAttempts, taskID)
	delete(m.lastTriageAt, taskID)
	m.mu.Unlock()
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
	sanitized := sanitizeForTerminal(content)
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
	go m.persistMessages()

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

// GetMessages returns recent messages.
func (m *Manager) GetMessages() []OfficeMessage {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]OfficeMessage, len(m.messages))
	copy(result, m.messages)
	return result
}

// persistMessages writes the current messages to disk atomically.
func (m *Manager) persistMessages() {
	m.mu.RLock()
	msgs := make([]OfficeMessage, len(m.messages))
	copy(msgs, m.messages)
	m.mu.RUnlock()

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dir := filepath.Join(homeDir, ".jarvis", "office-memory")
	os.MkdirAll(dir, 0o755)
	data, err := json.Marshal(msgs)
	if err != nil {
		return
	}
	tmp := filepath.Join(dir, "messages.json.tmp")
	target := filepath.Join(dir, "messages.json")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	if err := os.Rename(tmp, target); err != nil {
		slog.Warn("Failed to persist office messages", "error", err)
		os.Remove(tmp)
	}
}

// loadPersistedMessages restores messages from disk on startup.
func (m *Manager) loadPersistedMessages() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}
	data, err := os.ReadFile(filepath.Join(homeDir, ".jarvis", "office-memory", "messages.json"))
	if err != nil {
		return
	}
	var msgs []OfficeMessage
	if err := json.Unmarshal(data, &msgs); err != nil {
		return
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

// GetMetricsSnapshot returns a point-in-time copy of all metrics.
func (m *Manager) GetMetricsSnapshot() MetricsSnapshot {
	return m.metrics.Snapshot()
}

// GetAuditEntries returns all audit log entries.
func (m *Manager) GetAuditEntries() []AuditEntry {
	return m.auditWriter.Entries()
}

// GetConfig returns the current office configuration.
func (m *Manager) GetConfig() *Config {
	return m.config
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
	result := make([]*BugTask, 0, len(m.bugTasks))
	for _, bt := range m.bugTasks {
		result = append(result, bt)
	}
	return result
}

// AddBugTask adds a bug task to the office.
func (m *Manager) AddBugTask(task *BugTask) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bugTasks[task.ID] = task
	m.metrics.TotalBugs.Add(1)
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
	result := make([]*Meeting, 0, len(m.meetings))
	for _, mtg := range m.meetings {
		result = append(result, mtg)
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

// GetWhiteboard returns all whiteboard notes keyed by sessionID.
func (m *Manager) GetWhiteboard() map[string]*WhiteboardNote {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string]*WhiteboardNote)
	for _, a := range m.agents {
		if a.WhiteboardNote != nil {
			result[a.SessionID] = a.WhiteboardNote
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

// SetTeamMission sets the team mission, persists it to disk, and broadcasts.
func (m *Manager) SetTeamMission(s string) {
	trimmed := strings.TrimSpace(s)
	m.mu.Lock()
	m.teamMission = trimmed
	m.mu.Unlock()

	// Persist to disk atomically
	homeDir, _ := os.UserHomeDir()
	dir := filepath.Join(homeDir, ".jarvis", "office-memory")
	os.MkdirAll(dir, 0o755)
	tmpFile := filepath.Join(dir, "team-mission.txt.tmp")
	targetFile := filepath.Join(dir, "team-mission.txt")
	if err := os.WriteFile(tmpFile, []byte(trimmed), 0o644); err == nil {
		os.Rename(tmpFile, targetFile)
	}

	// Broadcast to frontend
	m.broadcast("office:missionUpdated", map[string]interface{}{"mission": trimmed})
}

// loadPersistedMission reads the team mission from disk if available.
func (m *Manager) loadPersistedMission() {
	homeDir, _ := os.UserHomeDir()
	data, err := os.ReadFile(filepath.Join(homeDir, ".jarvis", "office-memory", "team-mission.txt"))
	if err == nil {
		m.teamMission = strings.TrimSpace(string(data))
	}
	// Daily budget lives in its own tiny file so the mission file stays
	// human-readable plain text. Missing/invalid -> 0 (no budget).
	if bdata, berr := os.ReadFile(filepath.Join(homeDir, ".jarvis", "office-memory", "team-daily-budget.txt")); berr == nil {
		if v, perr := strconv.ParseFloat(strings.TrimSpace(string(bdata)), 64); perr == nil && v >= 0 {
			m.teamDailyBudget = v
		}
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
			data, readErr := os.ReadFile(filepath.Join(dir, name))
			if readErr != nil {
				continue
			}
			var inst devInstance
			if unmErr := json.Unmarshal(data, &inst); unmErr != nil {
				slog.Warn("malformed dev instance file; ignoring", "path", name, "err", unmErr)
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

// SetTeamDailyBudget updates the daily spend budget (USD) and persists it.
// 0 means "no budget" and hides the header pill on the frontend.
func (m *Manager) SetTeamDailyBudget(v float64) {
	if v < 0 {
		v = 0
	}
	m.mu.Lock()
	m.teamDailyBudget = v
	m.mu.Unlock()

	homeDir, _ := os.UserHomeDir()
	dir := filepath.Join(homeDir, ".jarvis", "office-memory")
	_ = os.MkdirAll(dir, 0o755)
	tmpFile := filepath.Join(dir, "team-daily-budget.txt.tmp")
	targetFile := filepath.Join(dir, "team-daily-budget.txt")
	if err := os.WriteFile(tmpFile, []byte(strconv.FormatFloat(v, 'f', -1, 64)), 0o644); err == nil {
		_ = os.Rename(tmpFile, targetFile)
	}
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
		_ = syscall.Kill(allPIDs[i], syscall.SIGTERM)
	}
	slog.Info("dev server tree SIGTERM sent", "parentPID", pid, "descendants", len(descendants))

	// Wait up to 3s for the tree to exit.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		anyAlive := false
		for _, p := range allPIDs {
			if syscall.Kill(p, 0) == nil {
				anyAlive = true
				break
			}
		}
		if !anyAlive {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}

	// Escalate.
	survivors := 0
	for i := len(allPIDs) - 1; i >= 0; i-- {
		p := allPIDs[i]
		if syscall.Kill(p, 0) == nil {
			if err := syscall.Kill(p, syscall.SIGKILL); err == nil {
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
	failures := inst.HealthFailures
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
	failures = inst.HealthFailures
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
	teamDailyBudget := m.teamDailyBudget
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
		if mtg.Active {
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
		"teamDailyBudget":             teamDailyBudget,
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
	// teamDailyBudget is applied after releasing the lock so the setter
	// can persist + lock internally without re-entrant locking.
	var budgetUpdate *float64
	if v, ok := settings["teamDailyBudget"].(float64); ok {
		clamped := v
		if clamped < 0 {
			clamped = 0
		}
		budgetUpdate = &clamped
	}
	m.mu.Unlock()

	// SetTeamMission acquires its own lock, so call after releasing.
	if missionUpdate != "" {
		m.SetTeamMission(missionUpdate)
	}
	if budgetUpdate != nil {
		m.SetTeamDailyBudget(*budgetUpdate)
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

// GetWrapUpStatus returns the current wrap-up status with per-agent details.
func (m *Manager) GetWrapUpStatus() WrapUpStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var startedAtMs int64
	if !m.wrapUpStartedAt.IsZero() {
		startedAtMs = m.wrapUpStartedAt.UnixMilli()
	}

	agentStatus := make([]WrapUpAgentStatus, 0, len(m.agents))
	for _, agent := range m.agents {
		if agent.Config == nil {
			continue
		}
		active := agent.ActiveTask()
		status := WrapUpAgentStatus{
			ConfigID:      agent.ConfigID,
			Name:          agent.Config.Name,
			HasActiveTask: active != nil,
		}
		if active != nil {
			title := active.Title
			status.TaskTitle = &title
		}
		agentStatus = append(agentStatus, status)
	}

	return WrapUpStatus{
		Active:      m.wrappingUp,
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
	activeTask := a.ActiveTask()
	if activeTask != nil {
		status = "working"
	} else if a.GetSDKWorking() {
		status = "working"
	} else {
		for _, t := range a.GetTasks() {
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
		"sessionId":      a.SessionID,
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
	auditEntries := m.GetAuditEntries()

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
			"sessionId": a.SessionID,
			"tasks":     a.GetTasks(),
		})
	}

	recentAudit := auditEntries
	if len(recentAudit) > 50 {
		recentAudit = recentAudit[len(recentAudit)-50:]
	}

	var wrapUp interface{}
	if wrappingUp {
		status := m.GetWrapUpStatus()
		wrapUp = status
	}

	return map[string]interface{}{
		"members":           members,
		"standupActive":     standupActive,
		"standupTranscript": standupTranscript,
		"recentMessages":    messages,
		"meetings":          meetings,
		"fileLocks":         locks,
		"teamSpawned":       teamSpawned,
		"perpetualMode":     perpetualMode,
		"whiteboardNotes":   whiteboardNotes,
		"bugTasks":          bugTasks,
		"taskQueues":        taskQueues,
		"recentAudit":       recentAudit,
		"wrapUp":            wrapUp,
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
	ch := agent.LeaderTaskCh
	agent.mu.Unlock()

	if ch != nil {
		select {
		case ch <- content:
		default:
		}
	}

	agentName := sessionID
	if agent.Config != nil {
		agentName = agent.Config.Name
	}
	m.auditLog("office:leaderTasksResponse", sessionID, agentName, map[string]interface{}{
		"content": content[:min(len(content), 200)],
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
	workingDir := m.lastWorkingDir
	if workingDir != "" && configID != "" {
		ts := time.Now().UTC().Format("2006-01-02T15:04Z")
		line := fmt.Sprintf("- [%s] (%s) %s", ts, category, learning)
		if err := MemoryAppendSection(configID, workingDir, "learnings", line); err != nil {
			slog.Warn("Failed to append reflection to memory", "agent", agentName, "err", err)
		}
	}

	m.auditLog("office:reflectionResponse", sessionID, agentName, map[string]interface{}{
		"learning": learning[:min(len(learning), 200)],
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
		sessions.WriteInkPrompt(agent.SessionID, skillSuggestion)
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
		"summary": summary[:min(len(summary), 200)],
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

// AppendAgentMemory appends to an agent's project-scoped memory file.
func AppendAgentMemory(configID, workingDir, entry string) error {
	path := GetAgentMemoryPath(configID, workingDir)
	os.MkdirAll(filepath.Dir(path), 0o755)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(entry)
	return err
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

	// Persist to disk and broadcast (same logic as SetTeamMission but avoids double-lock race).
	homeDir, err := os.UserHomeDir()
	if err == nil {
		dir := filepath.Join(homeDir, ".jarvis", "office-memory")
		os.MkdirAll(dir, 0o755)
		tmp := filepath.Join(dir, "team-mission.txt.tmp")
		target := filepath.Join(dir, "team-mission.txt")
		if err := os.WriteFile(tmp, []byte(updated), 0o644); err == nil {
			os.Rename(tmp, target)
		}
	}
	m.broadcast("office:missionUpdated", map[string]interface{}{"mission": updated})
}

// GetTeamSpawnedAt returns the time the team was spawned, or a zero time if not spawned.
func (m *Manager) GetTeamSpawnedAt() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.teamSpawnedAt
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
		return
	}
	os.Rename(tmpFile, targetFile)
}

// LoadPersistedMetrics restores metrics counters from disk if available.
func (m *Manager) LoadPersistedMetrics() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}
	data, err := os.ReadFile(filepath.Join(homeDir, ".jarvis", "office-memory", "metrics.json"))
	if err != nil {
		return
	}
	var snap MetricsSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return
	}
	m.metrics.TotalStandups.Store(snap.TotalStandups)
	m.metrics.TotalStandupsComplete.Store(snap.TotalStandupsCompleted)
	m.metrics.TotalTasks.Store(snap.TotalTasks)
	m.metrics.TotalTasksCompleted.Store(snap.TotalTasksCompleted)
	m.metrics.TotalBugs.Store(snap.TotalBugs)
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

// sanitizeForTerminal strips control characters from input using rune-safe iteration.
func sanitizeForTerminal(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	count := 0
	for _, r := range s {
		if count >= 2048 {
			break
		}
		if r >= 32 || r == '\n' || r == '\t' {
			b.WriteRune(r)
			count++
		}
	}
	return b.String()
}
