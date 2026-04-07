package office

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

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
	lastBrainstormAt   time.Time
	lastWaterCoolerAt  time.Time
	triageAttempts     map[string]int       // taskID -> triage attempt count
	lastTriageAt       map[string]time.Time // taskID -> last triage prompt time

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
		events:             make(chan OfficeEvent, 64),
	}
	mgr.loadPersistedMission()
	mgr.LoadPersistedMetrics()
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

	// Reassign pending/acknowledged tasks to a strategic-tier agent
	tasks := agent.GetTasks()
	for _, task := range tasks {
		if task.Status == TaskPending || task.Status == TaskAcknowledged {
			m.mu.RLock()
			for _, other := range m.agents {
				if other.SessionID != sessionID && other.Config != nil && other.Config.Tier == "strategic" {
					task.Source = "reassigned"
					other.AddTask(task)
					slog.Info("Reassigned task from dead agent", "task", task.Title, "to", other.Config.Name)
					break
				}
			}
			m.mu.RUnlock()
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

// SetTeamSpawned marks the team as spawned and records the timestamp.
// When setting to false, it stops periodic Telegram updates and clears
// any pending leader query.
func (m *Manager) SetTeamSpawned(v bool) {
	m.mu.Lock()
	m.teamSpawned = v
	if v {
		m.teamSpawnedAt = time.Now()
	} else {
		m.teamSpawnedAt = time.Time{}
	}
	m.mu.Unlock()

	if !v {
		m.StopTelegramUpdates()
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

	// Look up agent names for fromName/toName
	m.mu.RLock()
	if fromAgent, ok := m.agents[from]; ok && fromAgent.Config != nil {
		msg.FromName = fromAgent.Config.Name
	}
	if toAgent, ok := m.agents[to]; ok && toAgent.Config != nil {
		msg.ToName = toAgent.Config.Name
	}
	m.mu.RUnlock()

	m.mu.Lock()
	m.messages = append(m.messages, msg)
	if len(m.messages) > m.config.MaxMessages {
		m.messages = m.messages[len(m.messages)-m.config.MaxMessages:]
	}
	sessions := m.sessions
	m.mu.Unlock()

	m.broadcast("office:message", msg)
	m.metrics.TotalMessages.Add(1)

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

func (m *Manager) emitEvent(event OfficeEvent) {
	select {
	case m.events <- event:
	default:
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

// GetStandup returns the current standup state.
func (m *Manager) GetStandup() *StandupState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.standup
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
	cfg := m.config
	teamSpawned := m.teamSpawned
	var teamSpawnedAtMs int64
	if !m.teamSpawnedAt.IsZero() {
		teamSpawnedAtMs = m.teamSpawnedAt.UnixMilli()
	}
	autoStandupEnabled := m.autoStandupEnabled
	curiosityEnabled := m.curiosityEnabled
	teamMission := m.teamMission
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
	activeLockCount := len(m.fileLocks)
	m.mu.RUnlock()

	return map[string]interface{}{
		"autoStandup": map[string]interface{}{
			"enabled":    autoStandupEnabled,
			"intervalMs": cfg.AutoStandupInterval.Milliseconds(),
		},
		"brainstormCooldownMs":        cfg.BrainstormCooldown.Milliseconds(),
		"brainstormResponseTimeoutMs": cfg.BrainstormTimeout.Milliseconds(),
		"idleThresholdMs":             cfg.IdleThreshold.Milliseconds(),
		"idleCheckIntervalMs":         cfg.IdleCheckInterval.Milliseconds(),
		"meetingInactivityTimeoutMs":  cfg.MeetingInactivityTimeout.Milliseconds(),
		"standupResponseTimeoutMs":    cfg.StandupResponseTimeout.Milliseconds(),
		"fileLockExpiryMs":            cfg.FileLockTTL.Milliseconds(),
		"curiosityEnabled":            curiosityEnabled,
		"curiosityCooldownMs":         cfg.CuriosityCooldown.Milliseconds(),
		"reflectionResponseTimeoutMs": cfg.ReflectionTimeout.Milliseconds(),
		"teamSpawned":                 teamSpawned,
		"teamSpawnedAt":               teamSpawnedAtMs,
		"memberCount":                 memberCount,
		"activeMeetingCount":          activeMeetingCount,
		"activeLockCount":             activeLockCount,
		"standupActive":               standupActive,
		"lastBrainstormAt":            lastBrainstormAtMs,
		"metrics":                     m.metrics.Snapshot(),
		"teamMission":                 teamMission,
	}
}

// ---------------------------------------------------------------------------
// UpdateOfficeSettings - matches Node.js updateOfficeSettings()
// ---------------------------------------------------------------------------

// UpdateOfficeSettings applies validated setting changes from a JSON body.
func (m *Manager) UpdateOfficeSettings(settings map[string]interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()

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
	if v, ok := settings["teamMission"].(string); ok {
		m.teamMission = v
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
	} else if a.SDKWorking {
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
	learningCount := len(learningData.Learnings) + len(learningData.DistilledInsights)
	skillCount := len(learningData.Skills)
	score := CalculateAgentScore(a)

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
		"totalCost":      0,
		"lastActivityAt": time.Now().Format(time.RFC3339),
		"workingDir":     "",
		"activeTool":     nil,
	}
}

// GetOfficeState returns the full office state matching the Node.js response shape.
func (m *Manager) GetOfficeState() map[string]interface{} {
	m.mu.RLock()
	standupActive := m.standup != nil && m.standup.Active
	var standupTranscript interface{}
	if m.standup != nil && m.standup.Active {
		standupTranscript = m.standup.Responses
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
func SaveAgentMemory(configID, workingDir, content string) error {
	path := GetAgentMemoryPath(configID, workingDir)
	os.MkdirAll(filepath.Dir(path), 0o755)
	return os.WriteFile(path, []byte(content), 0o644)
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
	if m.teamMission != "" && insight != "" {
		m.teamMission += "\n\n[Learned] " + insight
	}
	updated := m.teamMission
	m.mu.Unlock()

	// Persist via SetTeamMission which handles disk write and broadcast.
	m.SetTeamMission(updated)
}

// GetTeamSpawnedAt returns the time the team was spawned, or a zero time if not spawned.
func (m *Manager) GetTeamSpawnedAt() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.teamSpawnedAt
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
		for i := 0; i < len(resolved); i++ {
			for j := i + 1; j < len(resolved); j++ {
				if resolved[j].task.CreatedAt.Before(resolved[i].task.CreatedAt) {
					resolved[i], resolved[j] = resolved[j], resolved[i]
				}
			}
		}

		// Sort open by CreatedAt ascending.
		for i := 0; i < len(open); i++ {
			for j := i + 1; j < len(open); j++ {
				if open[j].task.CreatedAt.Before(open[i].task.CreatedAt) {
					open[i], open[j] = open[j], open[i]
				}
			}
		}

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
	var result []rune
	for _, r := range s {
		if r >= 32 || r == '\n' || r == '\t' {
			result = append(result, r)
		}
	}
	if len(result) > 2048 { // rune count limit
		result = result[:2048]
	}
	return string(result)
}
