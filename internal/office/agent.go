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

// Task is a work item assigned to an agent.
type Task struct {
	ID                 string     `json:"id"`
	Title              string     `json:"title"`
	Description        string     `json:"description"`
	Status             TaskStatus `json:"status"`
	Priority           string     `json:"priority"` // "high", "medium", "low"
	AssignedAt         time.Time  `json:"assignedAt"`
	StartedAt          time.Time  `json:"startedAt,omitempty"`
	CompletedAt        time.Time  `json:"completedAt,omitempty"`
	Source             string     `json:"source"` // "leader", "self", "bug"
	BlockedBy          []string   `json:"blockedBy,omitempty"`
	BlockerDescription string     `json:"blockerDescription,omitempty"`
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

	// Response channels (replaces pending*Responses Maps)
	StandupCh    chan string
	BrainstormCh chan string
	ReflectionCh chan string
	CuriosityCh  chan string
	LeaderTaskCh chan string

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
}

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
			return true
		}
	}
	return false
}

// SetTaskProgress updates the description of a task to reflect progress.
// Returns true if found.
func (a *AgentState) SetTaskProgress(taskID string, progress string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, t := range a.taskQueue.Tasks {
		if t.ID == taskID {
			t.Description = progress
			return true
		}
	}
	return false
}

// SetSDKWorking sets the SDK working flag under the mutex.
func (a *AgentState) SetSDKWorking(working bool) {
	a.mu.Lock()
	a.SDKWorking = working
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
