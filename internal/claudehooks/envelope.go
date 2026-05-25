package claudehooks

// JSON envelope types for the Claude Code hooks HTTP handler.
//
// We only model the fields Jarvis actually consumes; unknown fields are
// preserved through json.RawMessage where necessary. The full schema is
// at https://code.claude.com/docs/en/hooks. New events should add a
// dedicated struct here AND a handler in handlers.go.

import "encoding/json"

// BaseEvent is the common envelope every hook payload starts with.
// Embedded into each event-specific struct so handlers can access the
// shared fields without re-decoding.
type BaseEvent struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	CWD            string `json:"cwd"`
	HookEventName  string `json:"hook_event_name"`
}

// SessionStartEvent fires when a session starts/resumes/clears/compacts.
// Source distinguishes: "startup" | "resume" | "clear" | "compact".
type SessionStartEvent struct {
	BaseEvent
	Source string `json:"source"`
	Model  string `json:"model,omitempty"`
}

// UserPromptSubmitEvent fires immediately before Claude processes a
// user prompt. We use it to mark the agent as "working" and to flush
// the per-turn text buffer that drives Telegram lead-reply forwarding.
type UserPromptSubmitEvent struct {
	BaseEvent
	Prompt string `json:"prompt"`
}

// PreToolUseEvent fires before a tool call executes. tool_input is
// kept as RawMessage because it varies wildly per tool (Bash vs Read
// vs Edit) and we only surface the tool name to ActiveTool today.
type PreToolUseEvent struct {
	BaseEvent
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`
	ToolUseID string          `json:"tool_use_id"`
}

// PostToolUseEvent fires after a tool call completes. Drives the
// tool-loop detector (hash of tool_result) and clears ActiveTool.
type PostToolUseEvent struct {
	BaseEvent
	ToolName   string          `json:"tool_name"`
	ToolInput  json.RawMessage `json:"tool_input"`
	ToolUseID  string          `json:"tool_use_id"`
	ToolResult json.RawMessage `json:"tool_result"`
}

// PostToolUseFailureEvent mirrors PostToolUseEvent for failed tool
// calls. Same shape; separate event so we can count/telemeter tool
// failures distinctly in a future iteration.
type PostToolUseFailureEvent = PostToolUseEvent

// StopEvent fires when Claude finishes generating its response. This
// is the moment we (a) flip SDKWorking false, (b) tail the transcript
// to extract token usage for RecordTurnUsage, and (c) flush the
// per-turn text buffer.
type StopEvent struct {
	BaseEvent
}

// SessionEndEvent fires when the session terminates (any reason). We
// route this into HandleAgentDeath so the office's reassignment +
// dev-instance cleanup paths run, even when the underlying claude
// process exit detection hasn't fired yet.
type SessionEndEvent struct {
	BaseEvent
	Reason string `json:"reason,omitempty"`
}

// NotificationEvent fires when Claude Code shows the operator a
// notification (permission requests, "waiting for input", etc).
// Surfaced to the dashboard; no AgentState mutation.
type NotificationEvent struct {
	BaseEvent
	Message string `json:"message,omitempty"`
}

// PreCompactEvent / PostCompactEvent bracket the context-compaction
// process. We only broadcast these to the dashboard so operators see
// a deterministic ~70%-context marker rather than guessing from the
// output stream.
type CompactEvent struct {
	BaseEvent
	Trigger string `json:"trigger,omitempty"`
}

// PostToolBatchEvent fires after a batch of parallel tool calls
// resolves, before the next model call. Per-batch is a cleaner pivot
// for cost rollups than per-Stop because it correlates spend to the
// specific tool sequence that produced it.
type PostToolBatchEvent struct {
	BaseEvent
	ToolCalls []ToolCallSummary `json:"tool_calls"`
}

// ToolCallSummary is a single entry in PostToolBatchEvent.ToolCalls.
// ToolOutput is kept as RawMessage so each tool's idiosyncratic output
// shape passes through without us decoding it.
type ToolCallSummary struct {
	ToolName   string          `json:"tool_name"`
	ToolInput  json.RawMessage `json:"tool_input"`
	ToolOutput json.RawMessage `json:"tool_output"`
	ToolUseID  string          `json:"tool_use_id"`
}

// StopFailureEvent fires when a turn ends due to an API error rather
// than a normal Stop. ErrorType matches the documented matcher values
// (rate_limit / billing_error / oauth_org_not_allowed / model_not_found
// / server_error / max_output_tokens / invalid_request /
// authentication_failed / unknown).
type StopFailureEvent struct {
	BaseEvent
	ErrorType    string `json:"error_type"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// PermissionRequestEvent fires when Claude pops a permission dialog
// (default mode). For Jarvis's bypass-mode agents this is a tripwire:
// seeing it means bypass mode is unexpectedly off.
type PermissionRequestEvent struct {
	BaseEvent
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`
	ToolUseID string          `json:"tool_use_id"`
}

// PermissionDeniedEvent fires when the auto-mode classifier denies a
// tool call. Same tripwire: jarvis defaults to bypass, so a deny here
// is anomalous and worth surfacing.
type PermissionDeniedEvent struct {
	BaseEvent
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`
	ToolUseID string          `json:"tool_use_id"`
}

// TaskCreatedEvent fires when Claude uses the TaskCreate tool. These
// are Claude's internal subagent task lists -- not office tasks. We
// mirror them into the agent's progress log so operators see "what
// the agent is working through" without opening the transcript.
type TaskCreatedEvent struct {
	BaseEvent
	TaskName        string `json:"task_name"`
	TaskDescription string `json:"task_description,omitempty"`
}

// TaskCompletedEvent fires when Claude marks a TaskCreate-created
// task as complete.
type TaskCompletedEvent struct {
	BaseEvent
	TaskName string `json:"task_name"`
}

// SubagentStartEvent / SubagentStopEvent fire on Task-tool spawned
// subagents. More reliable than the 10-second filesystem poll in
// internal/subagent/watcher.go.
type SubagentStartEvent struct {
	BaseEvent
	AgentType string `json:"agent_type"`
	AgentID   string `json:"agent_id"`
}

type SubagentStopEvent struct {
	BaseEvent
	AgentType string `json:"agent_type"`
	AgentID   string `json:"agent_id"`
}

// CwdChangedEvent fires on `cd` from inside a Bash tool call. Used to
// update AgentState.WorkingDir live.
type CwdChangedEvent struct {
	BaseEvent
	PreviousCwd string `json:"previous_cwd"`
	NewCwd      string `json:"new_cwd"`
}

// InstructionsLoadedEvent fires when CLAUDE.md / .claude/rules/*.md
// are loaded into context. Useful for "why did the agent ignore X?"
// debugging in the dashboard.
type InstructionsLoadedEvent struct {
	BaseEvent
	FilePath   string `json:"file_path"`
	MemoryType string `json:"memory_type,omitempty"`
	LoadReason string `json:"load_reason,omitempty"`
}

// ConfigChangeEvent fires when a settings file changes mid-session.
type ConfigChangeEvent struct {
	BaseEvent
	ConfigSource string                 `json:"config_source"`
	Changes      map[string]interface{} `json:"changes,omitempty"`
}

// ElicitationEvent fires when an MCP server requests structured input
// from the user during a tool call. Jarvis can auto-respond when the
// requested fields map to office state (e.g. dev creds for an API key
// prompt).
type ElicitationEvent struct {
	BaseEvent
	ServerName    string          `json:"server_name"`
	ToolName      string          `json:"tool_name"`
	ElicitationID string          `json:"elicitation_id"`
	Request       json.RawMessage `json:"request"`
}

// ElicitationResultEvent fires after the user (or jarvis) responds to
// an ElicitationEvent. Audit-only.
type ElicitationResultEvent struct {
	BaseEvent
	ServerName    string          `json:"server_name"`
	ElicitationID string          `json:"elicitation_id"`
	Action        string          `json:"action"`
	Content       json.RawMessage `json:"content,omitempty"`
}

// SetupEvent fires for `claude --init-only` / `claude -p --init` /
// `claude -p --maintenance`. Used by the oneshot helper (R1.4) to
// distinguish initialisation runs.
type SetupEvent struct {
	BaseEvent
	Trigger string `json:"trigger"`
}

// WorktreeCreateEvent / WorktreeRemoveEvent fire when Claude itself
// manages a worktree via --worktree. Wired now so the R3.1 migration
// only needs handler logic, not envelope plumbing.
type WorktreeCreateEvent struct {
	BaseEvent
	BaseBranch     string `json:"base_branch,omitempty"`
	WorktreeBranch string `json:"worktree_branch,omitempty"`
	WorktreePath   string `json:"worktree_path"`
}

type WorktreeRemoveEvent struct {
	BaseEvent
	WorktreePath string `json:"worktree_path"`
	RemoveReason string `json:"remove_reason,omitempty"`
}

// HookResponse is the unified response shape. Empty/nil fields are
// omitted on the wire — most handlers return 204 No Content with no
// body at all, but a few influence Claude's flow:
//   - SessionStart can inject kickstart context via AdditionalContext.
//   - PreToolUse can DENY a dangerous tool call via
//     hookSpecificOutput.permissionDecision = "deny".
//   - PermissionRequest can auto-allow/deny without a user dialog.
//   - PermissionDenied can request a retry.
//   - Elicitation can auto-respond with content.
type HookResponse struct {
	HookSpecificOutput *HookSpecificOutput `json:"hookSpecificOutput,omitempty"`
}

// HookSpecificOutput is the documented payload for hooks that want to
// influence Claude's next step. Only the fields we actually emit are
// listed -- the spec has more (modifiedInput, updatedPermissionRule,
// etc.) we don't currently use.
type HookSpecificOutput struct {
	HookEventName            string                 `json:"hookEventName"`
	AdditionalContext        string                 `json:"additionalContext,omitempty"`
	PermissionDecision       string                 `json:"permissionDecision,omitempty"`
	PermissionDecisionReason string                 `json:"permissionDecisionReason,omitempty"`
	Retry                    bool                   `json:"retry,omitempty"`
	Action                   string                 `json:"action,omitempty"`
	Content                  map[string]interface{} `json:"content,omitempty"`
}
