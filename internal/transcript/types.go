package transcript

import "time"

// HookEvent represents a Claude Code hook event from the transcript.
type HookEvent string

const (
	HookPermissionPrompt HookEvent = "permission_prompt"
	HookElicitation      HookEvent = "elicitation_dialog"
	HookIdlePrompt       HookEvent = "idle_prompt"
	HookStop             HookEvent = "stop"
)

// TranscriptEntry represents a line from Claude's transcript JSONL.
type TranscriptEntry struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp,omitempty"`
	Data      interface{} `json:"data,omitempty"`
}

// TranscriptEvent is emitted when a hook event is detected.
type TranscriptEvent interface {
	transcriptEventType() string
}

type HookDetectedEvent struct {
	Hook      HookEvent
	SessionID string
}
func (HookDetectedEvent) transcriptEventType() string { return "hook_detected" }
