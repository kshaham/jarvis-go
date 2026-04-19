package office

import (
	"sync"
	"time"
)

// TeamMemberConfig defines a team member's role and personality.
type TeamMemberConfig struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Role         string   `json:"role"`
	Tier         string   `json:"tier"` // "strategic" or "worker"
	Color        string   `json:"color"`
	Personality  string   `json:"personality"`
	Skills       []string `json:"skills"`
	SystemPrompt string   `json:"systemPrompt"`
}

// TaskStatus represents the status of a task.
type TaskStatus string

const (
	TaskPending      TaskStatus = "pending"
	TaskAcknowledged TaskStatus = "acknowledged"
	TaskInProgress   TaskStatus = "in_progress"
	TaskCompleted    TaskStatus = "completed"
	TaskFailed       TaskStatus = "failed"
	TaskBlocked      TaskStatus = "blocked"
)

// TaskProgressEntry is a single timestamped progress update on a task.
// The ProgressLog ring-buffer on Task preserves a short history so the
// office UI can show "how we got here" instead of only the latest note.
type TaskProgressEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Note      string    `json:"note"`
}

// taskProgressLogCap bounds how many progress entries we keep per task.
// Older entries are dropped from the front when exceeded -- the latest
// is always preserved and mirrored onto Task.Description for back-compat.
const taskProgressLogCap = 20

// Task is a work item assigned to an agent.
type Task struct {
	ID                 string              `json:"id"`
	Title              string              `json:"title"`
	Description        string              `json:"description"`
	Status             TaskStatus          `json:"status"`
	Priority           string              `json:"priority"` // "high", "medium", "low"
	AssignedAt         time.Time           `json:"assignedAt"`
	StartedAt          time.Time           `json:"startedAt,omitempty"`
	CompletedAt        time.Time           `json:"completedAt,omitempty"`
	Source             string              `json:"source"` // "leader", "self", "bug"
	BlockedBy          []string            `json:"blockedBy,omitempty"`
	BlockerDescription string              `json:"blockerDescription,omitempty"`
	ProgressLog        []TaskProgressEntry `json:"progressLog,omitempty"`
}

// TaskQueue holds a list of tasks for an agent.
type TaskQueue struct {
	Tasks []*Task `json:"tasks"`
}

// AckNudgeState tracks nudge attempts for task acknowledgment.
type AckNudgeState struct {
	LastNudgeAt time.Time
	Count       int
}

// PersonalityOverride holds custom personality/prompt for an agent.
type PersonalityOverride struct {
	Personality  string `json:"personality,omitempty"`
	SystemPrompt string `json:"systemPrompt,omitempty"`
}

// WhiteboardNote is a freeform note from an agent.
type WhiteboardNote struct {
	Content   string    `json:"content"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// FileLock represents a distributed file lock.
type FileLock struct {
	Path       string    `json:"path"`
	Owner      string    `json:"owner"` // sessionID
	OwnerName  string    `json:"ownerName"`
	AcquiredAt time.Time `json:"acquiredAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

// AgentState consolidates all per-agent state.
type AgentState struct {
	mu sync.Mutex

	SessionID    string
	ConfigID     string
	Config       *TeamMemberConfig
	WorktreePath string

	// Provider + model the agent was spawned with. Set once by the spawn
	// handler and read back by BuildMemberPayload so the UI can show which
	// LLM each agent is running on (Active Now cards + current-team banner).
	Provider string
	Model    string

	// ActiveTool is the name of the Claude tool the agent is currently
	// invoking (e.g. "Bash", "Read", "Edit"), or "" when idle / thinking.
	// Written by the SDK event forwarder on every tool_use event.
	ActiveTool string

	// Response channels (replaces pending*Responses Maps). R19 wraps
	// standup / brainstorm / leader-task channels in waitSlot so a
	// late reply from round N cannot pollute round N+1 — the reader
	// marks the slot done via defer before returning, after which
	// Submit* calls hit the closed flag and drop with a counter.
	StandupSlot    *waitSlot
	BrainstormSlot *waitSlot
	LeaderTaskSlot *waitSlot
	// Reflection / curiosity are still raw channels — they're not
	// part of the round-pollution pattern (single reply, slot is torn
	// down on the reader side). Leaving them untouched keeps the
	// refactor surgical.
	ReflectionCh chan string
	CuriosityCh  chan string

	// Task queue (private -- use AddTask/GetTasks/UpdateTaskStatus)
	taskQueue *TaskQueue

	// SDK session working state (set by event forwarder, not task lifecycle)
	SDKWorking bool

	// Timing state
	LastPromptSentAt    time.Time
	LastCuriosityAt     time.Time
	LastReplenishmentAt time.Time
	LastLateStandupAt   time.Time
	LastReflectionAt    time.Time
	// LastAgentAPIAt is "we heard FROM this agent most recently" -- any
	// incoming API call from the agent (task-ack, task-start,
	// task-complete, task-progress, self-add, memory-write, etc.)
	// touches it. Used by checkForStuckAgents to avoid falsely flagging
	// an agent as stuck just because their SDK turn is still running
	// (multi-step tool-heavy turns can take 5+ minutes without emitting
	// an EventTokens that would update LastTurnAt).
	LastAgentAPIAt time.Time

	// Recovery tracking
	RecoveryAttempts    int
	RecoveryWindowStart time.Time

	// Ack nudge tracking
	AckNudges map[string]*AckNudgeState

	// Progress tracking
	ProgressChecks map[string]time.Time

	// Whiteboard
	WhiteboardNote *WhiteboardNote

	// Personality override
	PersonalityOverride *PersonalityOverride

	// Cost + token aggregation, fed by the SSE event forwarder on every
	// pty.EventTokens. Enables per-agent spend and token displays in the
	// office UI without a new endpoint -- exposed via BuildMemberPayload.
	TotalCostUSD      float64
	TotalInputTokens  int64
	TotalOutputTokens int64
	TurnCount         int64
	LastTurnAt        time.Time
	// Last ~20 per-turn cost values (newest last). Powers the per-card
	// sparkline and agent-detail cost chart. Bounded at recentTurnCostsCap.
	RecentTurnCosts []float64
	// R19 · Snapshot of the SDK session's dropped-event counter, synced
	// on every heartbeat (Idle/Working/Tokens). Non-zero means the
	// outbound events channel overflowed at some point and the
	// dashboard may have stale data. Surfaced on the member card.
	EventsDropped uint64
}

// recentTurnCostsCap bounds the per-agent recent-turn cost history.
const recentTurnCostsCap = 20

// NewAgentState creates a new agent state.
func NewAgentState(sessionID, configID string, config *TeamMemberConfig) *AgentState {
	return &AgentState{
		SessionID:      sessionID,
		ConfigID:       configID,
		Config:         config,
		taskQueue:      &TaskQueue{Tasks: make([]*Task, 0)},
		AckNudges:      make(map[string]*AckNudgeState),
		ProgressChecks: make(map[string]time.Time),
	}
}

// TouchAgentAPI records that we just received a call from this agent.
// Called by every /api/office/agent/* handler so the stuck-detector
// can tell a truly-frozen agent from one that's in a long tool-heavy
// turn and thus quiet on EventTokens.
func (a *AgentState) TouchAgentAPI() {
	a.mu.Lock()
	a.LastAgentAPIAt = time.Now()
	a.mu.Unlock()
}

// ActiveTask returns the first in_progress task, or nil.
func (a *AgentState) ActiveTask() *Task {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, t := range a.taskQueue.Tasks {
		if t.Status == TaskInProgress {
			return t
		}
	}
	return nil
}

// PendingTaskCount returns the number of pending/acknowledged tasks.
func (a *AgentState) PendingTaskCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	count := 0
	for _, t := range a.taskQueue.Tasks {
		if t.Status == TaskPending || t.Status == TaskAcknowledged {
			count++
		}
	}
	return count
}

// NextPendingTask returns the first pending/acknowledged task, or nil.
func (a *AgentState) NextPendingTask() *Task {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, t := range a.taskQueue.Tasks {
		if t.Status == TaskPending || t.Status == TaskAcknowledged {
			return t
		}
	}
	return nil
}

// AddTask appends a task to the agent's queue under the mutex.
func (a *AgentState) AddTask(task *Task) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.taskQueue.Tasks = append(a.taskQueue.Tasks, task)
}

// GetTasks returns a shallow copy of the task list under the mutex.
func (a *AgentState) GetTasks() []*Task {
	a.mu.Lock()
	defer a.mu.Unlock()
	result := make([]*Task, len(a.taskQueue.Tasks))
	copy(result, a.taskQueue.Tasks)
	return result
}

// UpdateTaskStatus updates the status of a task by ID. Returns true if found.
func (a *AgentState) UpdateTaskStatus(taskID string, status TaskStatus) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, t := range a.taskQueue.Tasks {
		if t.ID == taskID {
			t.Status = status
			now := time.Now()
			if status == TaskInProgress && t.StartedAt.IsZero() {
				t.StartedAt = now
			}
			if status == TaskCompleted || status == TaskFailed {
				t.CompletedAt = now
			}
			// Reclaim per-task side-channel state whenever the task leaves
			// TaskInProgress. Previously only the auto-complete path in
			// recovery.go cleared ProgressChecks; normal completions, fails,
			// and blocks left entries stranded -- a slow per-agent leak.
			if status != TaskInProgress {
				delete(a.ProgressChecks, taskID)
				delete(a.AckNudges, taskID)
			}
			return true
		}
	}
	return false
}

// SetTaskProgress appends a progress note to the task's ProgressLog and
// mirrors it onto Description for backward compat with readers that only
// look at the current state. Log is ring-buffered at taskProgressLogCap.
// Returns true if the task was found.
func (a *AgentState) SetTaskProgress(taskID string, progress string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, t := range a.taskQueue.Tasks {
		if t.ID == taskID {
			t.Description = progress
			t.ProgressLog = append(t.ProgressLog, TaskProgressEntry{
				Timestamp: time.Now(),
				Note:      progress,
			})
			if len(t.ProgressLog) > taskProgressLogCap {
				t.ProgressLog = t.ProgressLog[len(t.ProgressLog)-taskProgressLogCap:]
			}
			return true
		}
	}
	return false
}

// RecordTurnUsage accumulates token + cost from a single turn. Called from
// the SSE event forwarder on every pty.EventTokens. Thread-safe; appends to
// the bounded RecentTurnCosts ring so sparklines can render without a
// separate tracker.
func (a *AgentState) RecordTurnUsage(input, output int64, cost float64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.TotalInputTokens += input
	a.TotalOutputTokens += output
	a.TotalCostUSD += cost
	a.TurnCount++
	a.LastTurnAt = time.Now()
	a.RecentTurnCosts = append(a.RecentTurnCosts, cost)
	if len(a.RecentTurnCosts) > recentTurnCostsCap {
		a.RecentTurnCosts = a.RecentTurnCosts[len(a.RecentTurnCosts)-recentTurnCostsCap:]
	}
}

// UsageSnapshot returns a copy-safe view of aggregated usage for use in
// member payloads + dashboards.
type UsageSnapshot struct {
	TotalCostUSD      float64   `json:"totalCostUSD"`
	TotalInputTokens  int64     `json:"totalInputTokens"`
	TotalOutputTokens int64     `json:"totalOutputTokens"`
	TurnCount         int64     `json:"turnCount"`
	LastTurnAt        time.Time `json:"lastTurnAt,omitempty"`
	RecentTurnCosts   []float64 `json:"recentTurnCosts,omitempty"`
	EventsDropped     uint64    `json:"eventsDropped,omitempty"`
}

// Usage returns a snapshot of the agent's cumulative cost + token counts.
func (a *AgentState) Usage() UsageSnapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	recent := make([]float64, len(a.RecentTurnCosts))
	copy(recent, a.RecentTurnCosts)
	return UsageSnapshot{
		TotalCostUSD:      a.TotalCostUSD,
		TotalInputTokens:  a.TotalInputTokens,
		TotalOutputTokens: a.TotalOutputTokens,
		TurnCount:         a.TurnCount,
		LastTurnAt:        a.LastTurnAt,
		RecentTurnCosts:   recent,
		EventsDropped:     a.EventsDropped,
	}
}

// SetEventsDropped syncs the SDK-session-level drop counter into the
// agent so BuildMemberPayload can surface it without poking the pty
// package. Called from the SSE forwarder on each heartbeat event.
func (a *AgentState) SetEventsDropped(n uint64) {
	a.mu.Lock()
	a.EventsDropped = n
	a.mu.Unlock()
}

// SetSDKWorking sets the SDK working flag under the mutex.
func (a *AgentState) SetSDKWorking(working bool) {
	a.mu.Lock()
	a.SDKWorking = working
	a.mu.Unlock()
}

// SetProvider records the LLM provider + model string the agent was spawned
// with. Safe to call from the spawn handler; read back by BuildMemberPayload.
func (a *AgentState) SetProvider(provider, model string) {
	a.mu.Lock()
	a.Provider = provider
	a.Model = model
	a.mu.Unlock()
}

// SetActiveTool records the name of the tool the agent is currently invoking.
// Pass "" to clear. Used by the SDK event forwarder.
func (a *AgentState) SetActiveTool(name string) {
	a.mu.Lock()
	a.ActiveTool = name
	a.mu.Unlock()
}

// GetSDKWorking reads the SDK working flag under the mutex.
func (a *AgentState) GetSDKWorking() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.SDKWorking
}

// GetPersonalityOverride returns a snapshot of the override under the mutex.
func (a *AgentState) GetPersonalityOverride() *PersonalityOverride {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.PersonalityOverride == nil {
		return nil
	}
	copied := *a.PersonalityOverride
	return &copied
}

// SetPersonalityOverride sets the override under the mutex.
func (a *AgentState) SetPersonalityOverride(override *PersonalityOverride) {
	a.mu.Lock()
	a.PersonalityOverride = override
	a.mu.Unlock()
}

// RemoveTask removes a task by ID and returns it, or nil if not found.
func (a *AgentState) RemoveTask(taskID string) *Task {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i, t := range a.taskQueue.Tasks {
		if t.ID == taskID {
			a.taskQueue.Tasks = append(a.taskQueue.Tasks[:i], a.taskQueue.Tasks[i+1:]...)
			return t
		}
	}
	return nil
}
