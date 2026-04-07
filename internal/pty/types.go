package pty

import "time"

type SessionMode string

const (
	ModeClaude    SessionMode = "claude"
	ModeClaudeSDK SessionMode = "claude-sdk"
	ModeShell     SessionMode = "shell"
)

type SessionStatus string

const (
	StatusIdle    SessionStatus = "idle"
	StatusBusy    SessionStatus = "busy"
	StatusStopped SessionStatus = "stopped"
	StatusError   SessionStatus = "error"
)

type SessionConfig struct {
	ID            string
	WorkingDir    string
	Mode          SessionMode
	Name          string
	UseScreen     bool
	ScreenManager ScreenManagerInterface
	NiceConfig    NiceConfig
	ProviderEnv   map[string]string

	// SDK mode fields (only used when Mode == ModeClaudeSDK)
	SystemPrompt string            // Initial system prompt (kickstart)
	McpServers   map[string]any    // MCP server configurations
}


type NiceConfig struct {
	Enabled  bool
	Priority int
}

func DefaultNiceConfig() NiceConfig {
	return NiceConfig{Enabled: false, Priority: 10}
}

type ClaudeMessage struct {
	Type      string  `json:"type"`
	Role      string  `json:"role,omitempty"`
	Content   string  `json:"content,omitempty"`
	SessionID string  `json:"session_id,omitempty"`
	CostUSD   float64 `json:"cost_usd,omitempty"`
}

type ScreenSession struct {
	SessionID  string      `json:"sessionId"`
	ScreenName string      `json:"screenName"`
	PID        int         `json:"pid"`
	CreatedAt  time.Time   `json:"createdAt"`
	WorkingDir string      `json:"workingDir"`
	Mode       SessionMode `json:"mode"`
	Attached   bool        `json:"attached"`
	Name       string      `json:"name,omitempty"`
}

// SessionEvent is the interface for all session events.
type SessionEvent interface {
	eventType() string
}

type EventTerminal struct{ Data string }
func (EventTerminal) eventType() string { return "terminal" }

type EventOutput struct{ Data string }
func (EventOutput) eventType() string { return "output" }

type EventIdle struct{}
func (EventIdle) eventType() string { return "idle" }

type EventWorking struct{}
func (EventWorking) eventType() string { return "working" }

type EventExit struct{ ExitCode int }
func (EventExit) eventType() string { return "exit" }

type EventTokens struct {
	Input  int64
	Output int64
	Total  int64
}
func (EventTokens) eventType() string { return "tokens" }

type EventError struct{ Err error }
func (EventError) eventType() string { return "error" }

type ScreenManagerInterface interface {
	CreateScreen(sessionID, workingDir string, mode SessionMode, name string, niceConfig NiceConfig, providerEnv map[string]string) (*ScreenSession, error)
	KillScreen(sessionID string) (bool, error)
	SendInput(sessionID, input string) bool
	GetScreen(sessionID string) (*ScreenSession, bool)
}
