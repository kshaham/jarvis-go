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

	// MaxTurnsPerSession is an optional per-session turn-count cap. When
	// > 0, the session emits EventTurnCapReached and stops once the
	// cumulative NumTurns from the SDK crosses the ceiling. Zero means
	// unlimited. Defaults populated by the spawn flow so every session
	// gets a sane runaway guard. Observed production incident: an agent
	// hit 155 turns ($57.87) on one task window before operator noticed.
	MaxTurnsPerSession int

	// SDK mode fields (only used when Mode == ModeClaudeSDK)
	SystemPrompt string         // Initial system prompt (kickstart)
	McpServers   map[string]any // MCP server configurations
	// Model is the specific Claude model ID (e.g. "claude-opus-4-7").
	// Empty string = let Claude CLI / ANTHROPIC_DEFAULT_*_MODEL env pick
	// the default. Honored by ModeClaudeSDK via claude.WithModel.
	Model string
	// AppendSystemPrompt is stable cross-agent boilerplate appended to
	// Claude's system prompt via claude.WithAppendSystemPrompt. Content
	// must be byte-for-byte identical across calls that should share
	// Anthropic's ephemeral cache (5-min TTL, 90% discount on hits).
	// R31 uses this for the onboarding router + critical rules that
	// every teammate sees.
	AppendSystemPrompt string

	// SystemPromptFile / AppendSystemPromptFile (R1.3) point at a
	// file containing the same content as SystemPrompt /
	// AppendSystemPrompt. Forwarded via Claude's --system-prompt-file
	// / --append-system-prompt-file CLI flags (SDK forwards via
	// WithExtraArgs). File-backed prompts cache more reliably than
	// inline strings because the prompt-cache key is the file path
	// rather than the in-memory bytes, so multiple sessions that
	// share the same file path see cache hits even when the SDK
	// rebuilds the option list per session. When both inline +
	// file are set the file wins (matches Claude's documented
	// precedence).
	SystemPromptFile       string
	AppendSystemPromptFile string

	// Effort is the per-session reasoning-effort tier passed via
	// Claude's --effort flag (SDK: claude.WithEffort). Accepts
	// "low" | "medium" | "high" | "max". Empty defers to the SDK
	// default (which respects ANTHROPIC_*_EFFORT env vars). R1.3
	// defaults: medium for workers, high for strategic-tier.
	Effort string

	// FallbackModel is the model the SDK switches to when the
	// primary is rate-limited or unavailable. Empty disables
	// fallback. R1.3 default: one tier down from the primary
	// (opus -> sonnet, sonnet -> haiku).
	FallbackModel string

	// ExcludeDynamicSystemPromptSections (R1.3) excludes per-machine
	// sections (cwd, env, memory paths, git-repo flag) from the
	// system prompt so the prompt-cache hash is portable across
	// hosts running the same team template. Wired via Claude's
	// --exclude-dynamic-system-prompt-sections CLI flag (SDK forwards
	// via WithExtraArgs).
	ExcludeDynamicSystemPromptSections bool

	// StrictMcpConfig (R1.3) locks MCP discovery to whatever jarvis
	// passes via --mcp-config / WithMcpServers, ignoring user-level
	// or project-level MCP registrations. Defends against an
	// operator accidentally polluting the office MCP surface from
	// their personal ~/.claude.json.
	StrictMcpConfig bool

	// ResumeSessionID (R3.5) instructs the SDK to spawn this session
	// against an existing Claude session ID via --resume <id>. Used
	// by the snapshot-restore "fork" path: a snapshot captures the
	// per-member ClaudeSessionID, and on restore each new SDK session
	// resumes its predecessor's transcript. Empty = fresh session
	// (current behaviour).
	ResumeSessionID string

	// ForkSession (R3.5) pairs with ResumeSessionID to ask Claude to
	// branch the resumed session into a NEW session ID rather than
	// reusing it. Lets jarvis snapshot-restore in parallel to the
	// original team without colliding on session state. Ignored
	// when ResumeSessionID is empty.
	ForkSession bool

	// FromPRNumber (R3.6) forwards --from-pr <n> so Claude loads the
	// PR's diff + description into its system prompt context. Zero =
	// no PR linkage. Pushed through claude.WithExtraArgs because the
	// SDK doesn't expose a typed option (CLI-only flag at time of
	// writing).
	FromPRNumber int

	// ChromeEnabled (R4.1) forwards --chrome so the agent gets
	// Claude's built-in browser-automation tools (navigate, click,
	// screenshot, evaluate). Off by default because it widens the
	// blast radius significantly (any reachable URL becomes a tool
	// target). Per-team opt-in via the spawn modal.
	ChromeEnabled bool

	// Channels (R4.3) is an opt-in list of channel subscriptions
	// passed via --channels <spec>. Each entry is forwarded as its
	// own --channels arg so the SDK / CLI sees the full multi-value
	// shape. Empty slice = no subscriptions (default). Entries look
	// like "plugin:office-events@local" — exact grammar is owned by
	// Claude Code's channels research preview and may change.
	Channels []string
	// OmitSkipPermissions drops the --dangerously-skip-permissions
	// flag from the claude CLI spawn (ModeClaude only). Default zero
	// value = false = keep the flag (existing behaviour). Auth
	// sessions set this true because the Claude CLI refuses the flag
	// when running as root ("cannot be used with root/sudo privileges
	// for security reasons") and exits immediately, breaking /login.
	OmitSkipPermissions bool

	// HooksEnabled (ModeClaude only) flags that this session should
	// receive Claude Code HTTP hooks. The office spawn handler also
	// writes a .claude/settings.local.json into WorkingDir before the
	// session starts, pointing the hooks at the daemon's
	// /api/claude/hooks/{sessionID}/{event} endpoint. Lets ModeClaude
	// match ModeClaudeSDK on signal fidelity (ActiveTool, SDKWorking,
	// RecordTurnUsage, etc) while keeping interactive subscription
	// billing.
	HooksEnabled bool

	// ClaudeBinaryPath overrides the `claude` binary lookup. Empty
	// means resolve via PATH (production behaviour). Tests point this
	// at cmd/fakeclaude so integration tests can run without a real
	// Claude install. ModeClaude only.
	ClaudeBinaryPath string
}

type NiceConfig struct {
	Enabled  bool
	Priority int
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

// EventToolInvocation fires once per completed tool_use → tool_result
// pair inside an SDK turn. R21 · Powers tool-level loop detection:
// identical (Name, ResultHash) tuples repeated across recent calls
// inside the same turn are strong evidence the agent is stuck in a
// retry loop. Emitted from sdk_session.go's eventConsumer by parsing
// content blocks out of event.Raw (the Go SDK's ContentBlock struct
// drops the tool_use/tool_result fields).
//
// InputHash is SHA-256 of the canonical JSON of the tool input;
// ResultHash is SHA-256 of the tool_result content string. Both are
// hex-encoded. ToolUseID is the pairing key the model assigns.
type EventToolInvocation struct {
	Name       string
	ToolUseID  string
	InputHash  string
	ResultHash string
	IsError    bool
}

func (EventToolInvocation) eventType() string { return "toolInvocation" }

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
