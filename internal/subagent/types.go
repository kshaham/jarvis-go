package subagent

import "time"

// SubagentStatus represents the lifecycle state.
type SubagentStatus string

const (
	StatusActive    SubagentStatus = "active"
	StatusIdle      SubagentStatus = "idle"
	StatusCompleted SubagentStatus = "completed"
)

// SubagentInfo holds metadata about a tracked subagent.
type SubagentInfo struct {
	AgentID        string         `json:"agentId"`
	SessionID      string         `json:"sessionId"`
	Description    string         `json:"description"`
	Status         SubagentStatus `json:"status"`
	FilePath       string         `json:"filePath"`
	LastActivityAt time.Time      `json:"lastActivityAt"`
	StartedAt      time.Time      `json:"startedAt"`
	TokensUsed     int64          `json:"tokensUsed"`
	InputTokens    int64          `json:"inputTokens"`
	OutputTokens   int64          `json:"outputTokens"`
	ToolCallCount  int            `json:"toolCallCount"`
	EntryCount     int            `json:"entryCount"`
	FileSize       int64          `json:"fileSize"`
	Model          string         `json:"model,omitempty"`
	ModelShort     string         `json:"modelShort,omitempty"`
}

// SubagentEvent is emitted when a subagent state changes.
type SubagentEvent interface {
	subagentEventType() string
}

// SubagentDiscovered is emitted when a new subagent file is found.
type SubagentDiscovered struct{ Info SubagentInfo }

func (SubagentDiscovered) subagentEventType() string { return "discovered" }

// SubagentUpdated is emitted when agent metadata is updated.
type SubagentUpdated struct{ Info SubagentInfo }

func (SubagentUpdated) subagentEventType() string { return "updated" }

// SubagentCompleted is emitted when an agent finishes its work.
type SubagentCompleted struct {
	Info     SubagentInfo  `json:"info"`
	Duration time.Duration `json:"duration"`
}

func (SubagentCompleted) subagentEventType() string { return "completed" }

// SubagentTool is emitted when a tool invocation is parsed from the transcript.
type SubagentTool struct {
	AgentID   string `json:"agentId"`
	ToolName  string `json:"toolName"`
	ToolUseID string `json:"toolUseId,omitempty"`
	Status    string `json:"status"` // "started", "completed", "error"
	Input     string `json:"input,omitempty"`
}

func (SubagentTool) subagentEventType() string { return "tool" }

// SubagentProgress is emitted for general progress updates.
type SubagentProgress struct {
	AgentID string `json:"agentId"`
	Summary string `json:"summary"`
}

func (SubagentProgress) subagentEventType() string { return "progress" }

// SubagentMessage is emitted when a user or assistant message is parsed.
type SubagentMessage struct {
	AgentID string `json:"agentId"`
	Role    string `json:"role"` // "user" or "assistant"
	Content string `json:"content"`
}

func (SubagentMessage) subagentEventType() string { return "message" }
