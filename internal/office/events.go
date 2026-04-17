package office

import "time"

// BroadcastFunc sends an event to all SSE clients.
type BroadcastFunc func(event string, data interface{})

// SessionStore provides access to session layer (interface for testability).
type SessionStore interface {
	WriteViaScreen(sessionID, input string) bool
	WriteInkPrompt(sessionID, prompt string) bool
	GetSessionIDs() []string
	IsSessionAlive(sessionID string) bool
	// IsSessionWorking reports whether the session is currently executing
	// a turn. Used to suppress ack-nudges while an agent is busy -- they
	// can't read the nudge until the turn ends, so the nudge counter
	// would run up to TaskAckMaxNudges and auto-fail every queued task
	// long before the agent gets a chance to acknowledge anything.
	IsSessionWorking(sessionID string) bool
}

// OfficeEvent is emitted by the manager for state changes.
type OfficeEvent interface {
	officeEventType() string
}

// MemberJoinedEvent is emitted when a team member joins.
type MemberJoinedEvent struct {
	ConfigID  string
	SessionID string
	Name      string
}

func (MemberJoinedEvent) officeEventType() string { return "memberJoined" }

// MemberLeftEvent is emitted when a team member leaves.
type MemberLeftEvent struct {
	ConfigID  string
	SessionID string
}

func (MemberLeftEvent) officeEventType() string { return "memberLeft" }

// StandupStartedEvent is emitted when a standup begins.
type StandupStartedEvent struct {
	Participants []string
}

func (StandupStartedEvent) officeEventType() string { return "standupStarted" }

// StandupEndedEvent is emitted when a standup concludes.
type StandupEndedEvent struct {
	Summary string
}

func (StandupEndedEvent) officeEventType() string { return "standupEnded" }

// TaskAssignedEvent is emitted when a task is assigned to an agent.
type TaskAssignedEvent struct {
	SessionID string
	TaskID    string
	Title     string
}

func (TaskAssignedEvent) officeEventType() string { return "taskAssigned" }

// BugTaskCreatedEvent is emitted when a bug task is created.
type BugTaskCreatedEvent struct {
	TaskID   string
	Title    string
	Priority string
}

func (BugTaskCreatedEvent) officeEventType() string { return "bugTaskCreated" }

// AuditEntry represents a logged office event.
type AuditEntry struct {
	Timestamp time.Time   `json:"timestamp"`
	Event     string      `json:"event"`
	SessionID string      `json:"sessionId,omitempty"`
	AgentName string      `json:"agentName,omitempty"`
	Data      interface{} `json:"data,omitempty"`
}
