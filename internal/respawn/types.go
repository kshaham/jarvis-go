package respawn

import "time"

// RespawnState represents the state machine state.
type RespawnState int

const (
	StateStopped RespawnState = iota
	StateWatching
	StateConfirmingIdle
	StateAiChecking
	StateSendingUpdate
	StateWaitingUpdate
	StateSendingClear
	StateWaitingClear
	StateSendingInit
	StateWaitingInit
	StateMonitoringInit
	StateSendingKickstart
	StateWaitingKickstart
)

// String returns the state name.
func (s RespawnState) String() string {
	names := [...]string{
		"STOPPED", "WATCHING", "CONFIRMING_IDLE", "AI_CHECKING",
		"SENDING_UPDATE", "WAITING_UPDATE", "SENDING_CLEAR", "WAITING_CLEAR",
		"SENDING_INIT", "WAITING_INIT", "MONITORING_INIT",
		"SENDING_KICKSTART", "WAITING_KICKSTART",
	}
	if int(s) < len(names) {
		return names[s]
	}
	return "UNKNOWN"
}

// Valid state transitions
var validTransitions = map[RespawnState][]RespawnState{
	StateStopped:          {StateWatching},
	StateWatching:         {StateConfirmingIdle, StateAiChecking, StateSendingUpdate, StateStopped},
	StateConfirmingIdle:   {StateWatching, StateAiChecking, StateSendingUpdate, StateStopped},
	StateAiChecking:       {StateWatching, StateSendingUpdate, StateStopped},
	StateSendingUpdate:    {StateWaitingUpdate, StateWatching, StateStopped},
	StateWaitingUpdate:    {StateSendingClear, StateSendingInit, StateSendingKickstart, StateWatching, StateStopped},
	StateSendingClear:     {StateWaitingClear, StateWatching, StateStopped},
	StateWaitingClear:     {StateSendingInit, StateSendingKickstart, StateWatching, StateStopped},
	StateSendingInit:      {StateWaitingInit, StateWatching, StateStopped},
	StateWaitingInit:      {StateMonitoringInit, StateWatching, StateStopped},
	StateMonitoringInit:   {StateSendingKickstart, StateWatching, StateStopped},
	StateSendingKickstart: {StateWaitingKickstart, StateWatching, StateStopped},
	StateWaitingKickstart: {StateWatching, StateStopped},
}

// DetectionLayer identifies which idle detection signal fired.
type DetectionLayer int

const (
	LayerStopHook DetectionLayer = iota
	LayerIdlePrompt
	LayerCompletionMessage
	LayerOutputSilence
	LayerTokenStability
	LayerWorkingAbsence
	LayerAiCheck
)

// String returns the layer name.
func (l DetectionLayer) String() string {
	names := [...]string{
		"stop_hook", "idle_prompt", "completion_message",
		"output_silence", "token_stability", "working_absence", "ai_check",
	}
	if int(l) < len(names) {
		return names[l]
	}
	return "unknown"
}

// DetectionSignal represents a signal from one idle detection layer.
type DetectionSignal struct {
	Layer      DetectionLayer
	Confidence int
	Timestamp  time.Time
	Reason     string
}

// ResetSignal clears all accumulated idle signals (working detected).
type ResetSignal struct {
	Reason    string
	Timestamp time.Time
}

// AiVerdict is the result of an AI idle check.
type AiVerdict string

const (
	VerdictIdle    AiVerdict = "IDLE"
	VerdictWorking AiVerdict = "WORKING"
	VerdictError   AiVerdict = "ERROR"
)

// AiCheckResult holds the outcome of an AI check.
type AiCheckResult struct {
	Verdict    AiVerdict
	Reasoning  string
	DurationMs int64
}

// AiCheckerStatus tracks the AI checker state.
type AiCheckerStatus string

const (
	CheckerReady    AiCheckerStatus = "ready"
	CheckerChecking AiCheckerStatus = "checking"
	CheckerCooldown AiCheckerStatus = "cooldown"
	CheckerDisabled AiCheckerStatus = "disabled"
)

// RespawnConfig holds configurable parameters.
type RespawnConfig struct {
	Enabled                    bool
	IdleTimeoutMs              int64
	ConfirmationDelayMs        int64
	SilenceThresholdMs         int64
	StepDelayMs                int64
	AiCheckEnabled             bool
	AiCheckTimeoutMs           int64
	AiCheckCooldownMs          int64
	StuckDetectionMs           int64
	StuckRecoveryMs            int64
	MaxStuckRecoveries         int
	ConfidenceThreshold        int
	ConfidenceWeightCompletion int
	ConfidenceWeightSilence    int
	ConfidenceWeightTokens     int
	ConfidenceWeightNoWorking  int

	// Prompt configuration
	UpdatePrompt    string
	SendClear       bool
	SendInit        bool
	KickstartPrompt string
}

// DefaultRespawnConfig returns sensible defaults.
func DefaultRespawnConfig() RespawnConfig {
	return RespawnConfig{
		Enabled:                    false,
		IdleTimeoutMs:              60000,
		ConfirmationDelayMs:        5000,
		SilenceThresholdMs:         5000,
		StepDelayMs:                3000,
		AiCheckEnabled:             true,
		AiCheckTimeoutMs:           90000,
		AiCheckCooldownMs:          30000,
		StuckDetectionMs:           120000,
		StuckRecoveryMs:            300000,
		MaxStuckRecoveries:         3,
		ConfidenceThreshold:        70,
		ConfidenceWeightCompletion: 40,
		ConfidenceWeightSilence:    25,
		ConfidenceWeightTokens:     20,
		ConfidenceWeightNoWorking:  15,
		UpdatePrompt:               DefaultUpdatePrompt,
		SendClear:                  true,
		SendInit:                   true,
		KickstartPrompt:            "",
	}
}

// RespawnEvent is the interface for controller events.
type RespawnEvent interface {
	respawnEventType() string
}

// StateChangedEvent is emitted on state transitions.
type StateChangedEvent struct {
	From RespawnState
	To   RespawnState
}

func (StateChangedEvent) respawnEventType() string { return "state_changed" }

// IdleDetectedEvent signals idle was confirmed.
type IdleDetectedEvent struct {
	Confidence int
	Layers     []DetectionLayer
}

func (IdleDetectedEvent) respawnEventType() string { return "idle_detected" }

// CycleCompleteEvent signals a full respawn cycle completed.
type CycleCompleteEvent struct {
	DurationMs int64
	Cycle      int
}

func (CycleCompleteEvent) respawnEventType() string { return "cycle_complete" }

// StepSentEvent is emitted when a command is sent to the session.
type StepSentEvent struct {
	Step  string
	Input string
}

func (StepSentEvent) respawnEventType() string { return "step_sent" }

// StepCompletedEvent is emitted when a step finishes.
type StepCompletedEvent struct {
	Step string
}

func (StepCompletedEvent) respawnEventType() string { return "step_completed" }

// StuckWarningEvent is emitted when the controller appears stuck.
type StuckWarningEvent struct {
	State      RespawnState
	DurationMs int64
}

func (StuckWarningEvent) respawnEventType() string { return "stuck_warning" }

// StuckRecoveryEvent is emitted when stuck recovery is attempted.
type StuckRecoveryEvent struct {
	State      RespawnState
	DurationMs int64
	Attempt    int
}

func (StuckRecoveryEvent) respawnEventType() string { return "stuck_recovery" }

// ActionLogEvent carries a log entry for UI display.
type ActionLogEvent struct {
	Type   string
	Detail string
}

func (ActionLogEvent) respawnEventType() string { return "action_log" }

// ErrorEvent carries an error.
type ErrorEvent struct {
	Err error
}

func (ErrorEvent) respawnEventType() string { return "error" }
