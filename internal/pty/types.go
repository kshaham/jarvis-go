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

	// MaxCostUSD is an optional per-session spending cap. When > 0, the
	// session emits EventCostExceeded and stops once totalCostUSD >= cap.
	// Zero means unlimited.
	MaxCostUSD float64

	// MaxTurnsPerSession is an optional per-session turn-count cap. When
	// > 0, the session emits EventTurnCapReached and stops once the
	// cumulative NumTurns from the SDK crosses the ceiling. Zero means
	// unlimited. Defaults populated by the spawn flow so every session
	// gets a sane runaway guard. Observed production incident: an agent
	// hit 155 turns ($57.87) on one task window before operator noticed.
	MaxTurnsPerSession int

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
	// Cost is the USD cost for this turn. SDK sessions populate from the
	// provider's TotalCostUSD; PTY sessions leave 0 (unknown).
	Cost float64
	// Model identifies which model served the turn. Empty for PTY sessions.
	Model string
}
func (EventTokens) eventType() string { return "tokens" }

// EventCostExceeded fires when a session's cumulative cost hits its
// per-session MaxCostUSD cap. The session is stopped after this event.
type EventCostExceeded struct {
	Cost   float64
	CapUSD float64
}

func (EventCostExceeded) eventType() string { return "costExceeded" }

// EventTurnCapReached fires when a session's cumulative NumTurns hits
// its per-session MaxTurnsPerSession cap. The session is stopped after
// this event. Separate event (rather than reusing EventCostExceeded)
// so operators can distinguish "ran out of money" from "ran out of
// turns" in the activity log and billing analysis.
type EventTurnCapReached struct {
	Turns int
	Cap   int
	Cost  float64
}

func (EventTurnCapReached) eventType() string { return "turnCapReached" }

// EventTurnError fires when an SDK turn completes with IsError=true
// (e.g. provider rate-limit, 4xx/5xx, "Network error" code from a
// Z.AI-style gateway). Previously we only logged; this event lets
// the web layer surface the failure to the operator via SSE so the
// dashboard can flag the agent instead of silently falling idle.
type EventTurnError struct {
	Subtype    string
	StopReason string
	Result     string
	NumTurns   int
}

func (EventTurnError) eventType() string { return "turnError" }

type EventError struct{ Err error }
func (EventError) eventType() string { return "error" }

type ScreenManagerInterface interface {
	CreateScreen(sessionID, workingDir string, mode SessionMode, name string, niceConfig NiceConfig, providerEnv map[string]string) (*ScreenSession, error)
	KillScreen(sessionID string) (bool, error)
	SendInput(sessionID, input string) bool
	GetScreen(sessionID string) (*ScreenSession, bool)
}
