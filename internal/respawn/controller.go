package respawn

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"
)

// Controller manages the respawn state machine for a single session.
type Controller struct {
	mu sync.Mutex

	// State
	state          RespawnState
	stateEnteredAt time.Time
	config         RespawnConfig
	sessionID      string
	enabled        bool
	cycleCount     int
	cycleStartedAt time.Time

	// Recovery
	stuckRecoveryCount    int
	lastStuckRecoveryAt   time.Time
	stuckStateWarned      bool
	stuckRecoveryNotified bool
	blockedRetryCount     int

	// Components
	timers     *TimerPool
	signals    *SignalAggregator
	events     chan RespawnEvent
	cancelFunc context.CancelFunc
	writer     SessionWriter
}

// NewController creates a new respawn controller for a session.
func NewController(sessionID string, config RespawnConfig) *Controller {
	return &Controller{
		state:     StateStopped,
		config:    config,
		sessionID: sessionID,
		timers:    NewTimerPool(),
		signals:   NewSignalAggregator(config),
		events:    make(chan RespawnEvent, 64),
	}
}

// Start begins the respawn controller. Transitions from Stopped to Watching.
func (rc *Controller) Start(ctx context.Context) error {
	childCtx, cancel := context.WithCancel(ctx)

	rc.mu.Lock()
	if rc.state != StateStopped {
		rc.mu.Unlock()
		cancel()
		return fmt.Errorf("controller already running (state: %s)", rc.state)
	}
	rc.enabled = true
	rc.cancelFunc = cancel
	rc.mu.Unlock()

	if err := rc.setState(StateWatching); err != nil {
		cancel()
		return err
	}

	// Start stuck detection
	go rc.stuckDetector(childCtx)

	return nil
}

// Stop halts the controller and cancels all timers.
func (rc *Controller) Stop() {
	rc.mu.Lock()
	rc.enabled = false
	cancelFn := rc.cancelFunc
	rc.cancelFunc = nil
	rc.state = StateStopped
	rc.mu.Unlock()

	rc.timers.CancelAll()
	if cancelFn != nil {
		cancelFn()
	}
}

// State returns the current state.
func (rc *Controller) State() RespawnState {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return rc.state
}

// Config returns the current config.
func (rc *Controller) Config() RespawnConfig {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return rc.config
}

// UpdateConfig updates the configuration.
func (rc *Controller) UpdateConfig(cfg RespawnConfig) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.config = cfg
}

// SessionID returns the session ID this controller manages.
func (rc *Controller) SessionID() string {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return rc.sessionID
}

// Events returns the event channel.
func (rc *Controller) Events() <-chan RespawnEvent {
	return rc.events
}

// CycleCount returns the number of completed cycles.
func (rc *Controller) CycleCount() int {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return rc.cycleCount
}

// GetStatus returns the controller's current status for SSE init data.
func (rc *Controller) GetStatus() map[string]interface{} {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return map[string]interface{}{
		"state":      rc.state.String(),
		"enabled":    rc.enabled,
		"cycleCount": rc.cycleCount,
	}
}

// ActiveTimers returns timer info for UI display.
func (rc *Controller) ActiveTimers() []ActiveTimerInfo {
	return rc.timers.ActiveTimers()
}

// Confidence returns the current idle confidence score.
func (rc *Controller) Confidence() int {
	return rc.signals.ComputeConfidence()
}

// --- Signal Receivers (called from session/hook watchers) ---

// OnStopHook is called when a stop hook is received.
func (rc *Controller) OnStopHook() {
	rc.signals.AddSignal(DetectionSignal{
		Layer:      LayerStopHook,
		Confidence: 100,
		Timestamp:  time.Now(),
		Reason:     "stop hook received",
	})
	rc.checkConfidence()
}

// OnIdlePrompt is called when an idle prompt hook fires.
func (rc *Controller) OnIdlePrompt() {
	rc.signals.AddSignal(DetectionSignal{
		Layer:      LayerIdlePrompt,
		Confidence: 100,
		Timestamp:  time.Now(),
		Reason:     "idle prompt received",
	})
	rc.checkConfidence()
}

// OnOutputSilence is called when no output for a threshold period.
func (rc *Controller) OnOutputSilence(elapsed time.Duration) {
	rc.signals.AddSignal(DetectionSignal{
		Layer:      LayerOutputSilence,
		Confidence: 100,
		Timestamp:  time.Now(),
		Reason:     fmt.Sprintf("silent for %s", elapsed),
	})
	rc.checkConfidence()
}

// OnTokenStability is called when token count hasn't changed.
func (rc *Controller) OnTokenStability() {
	rc.signals.AddSignal(DetectionSignal{
		Layer:      LayerTokenStability,
		Confidence: 100,
		Timestamp:  time.Now(),
		Reason:     "tokens stable",
	})
	rc.checkConfidence()
}

// OnWorkingDetected resets all idle signals (agent is working).
func (rc *Controller) OnWorkingDetected(reason string) {
	rc.signals.Reset()

	rc.mu.Lock()
	state := rc.state
	rc.mu.Unlock()

	// Return to watching if we were confirming idle, and cancel pending timers
	if state == StateConfirmingIdle {
		rc.timers.Cancel("confirm-idle")
		_ = rc.setState(StateWatching)
	}
}

// OnCompletionMessage is called when "Worked for Xm Ys" is detected.
func (rc *Controller) OnCompletionMessage(text string) {
	rc.signals.AddSignal(DetectionSignal{
		Layer:      LayerCompletionMessage,
		Confidence: 100,
		Timestamp:  time.Now(),
		Reason:     text,
	})
	rc.checkConfidence()
}

// --- State Machine ---

func (rc *Controller) setState(newState RespawnState) error {
	rc.mu.Lock()

	if newState == rc.state {
		rc.mu.Unlock()
		return nil
	}

	allowed, ok := validTransitions[rc.state]
	if !ok {
		rc.mu.Unlock()
		return fmt.Errorf("no transitions from state %s", rc.state)
	}
	if !slices.Contains(allowed, newState) {
		currentState := rc.state
		rc.mu.Unlock()
		return fmt.Errorf("invalid transition: %s -> %s", currentState, newState)
	}

	prevState := rc.state
	rc.state = newState
	rc.stateEnteredAt = time.Now()
	rc.stuckStateWarned = false

	// Collect events to emit after releasing the lock
	var eventsToEmit []RespawnEvent

	// Track cycle completion: any transition to Watching from an in-cycle state
	// (any state after SendingUpdate) counts as completing a cycle.
	if newState == StateWatching && !rc.cycleStartedAt.IsZero() {
		rc.cycleCount++
		duration := time.Since(rc.cycleStartedAt)
		eventsToEmit = append(eventsToEmit, CycleCompleteEvent{
			DurationMs: duration.Milliseconds(),
			Cycle:      rc.cycleCount,
		})
	}

	// Reset recovery and cycle tracking on return to watching.
	// This clears cycleStartedAt on ALL transitions to Watching (not just from
	// WaitingKickstart), which fixes broken cycle tracking on error paths.
	if newState == StateWatching && prevState != StateStopped {
		rc.stuckRecoveryCount = 0
		rc.lastStuckRecoveryAt = time.Time{}
		rc.stuckRecoveryNotified = false
		rc.blockedRetryCount = 0
		rc.cycleStartedAt = time.Time{}
	}

	if newState == StateSendingUpdate && rc.cycleStartedAt.IsZero() {
		rc.cycleStartedAt = time.Now()
	}

	eventsToEmit = append(eventsToEmit, StateChangedEvent{From: prevState, To: newState})

	sessionID := rc.sessionID
	rc.mu.Unlock()

	// Emit events outside the lock to prevent deadlock
	for _, event := range eventsToEmit {
		rc.emitEvent(event)
	}

	slog.Info("respawn state change",
		"session", sessionID,
		"from", prevState.String(),
		"to", newState.String(),
	)

	// Dispatch entry actions for the new state (outside the lock)
	rc.onEnterState(newState)

	return nil
}

// checkConfidence evaluates accumulated idle signals and transitions to
// ConfirmingIdle when the threshold is met.
//
// Note: There is an intentional TOCTOU gap between reading state under the lock
// and calling setState (which re-acquires the lock). This is safe because
// setState validates the transition -- if the state changed between the read and
// the setState call, the transition will be rejected as invalid and return an
// error, which we intentionally discard.
func (rc *Controller) checkConfidence() {
	rc.mu.Lock()
	state := rc.state
	threshold := rc.config.ConfidenceThreshold
	rc.mu.Unlock()

	if state != StateWatching {
		return
	}

	confidence := rc.signals.ComputeConfidence()
	if confidence >= threshold {
		rc.emitEvent(IdleDetectedEvent{
			Confidence: confidence,
			Layers:     rc.signals.ActiveLayers(),
		})
		_ = rc.setState(StateConfirmingIdle)
	}
}

func (rc *Controller) stuckDetector(ctx context.Context) {
	// Poll interval is 1/4 of the stuck detection threshold, minimum 1 second.
	// This ensures we react promptly even with short thresholds (e.g., in tests).
	rc.mu.Lock()
	interval := time.Duration(rc.config.StuckDetectionMs/4) * time.Millisecond
	rc.mu.Unlock()
	if interval < 50*time.Millisecond {
		interval = 50 * time.Millisecond
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rc.mu.Lock()
			if rc.state == StateStopped || rc.state == StateWatching {
				rc.mu.Unlock()
				continue
			}
			elapsed := time.Since(rc.stateEnteredAt)
			stuckWarningMs := rc.config.StuckDetectionMs
			stuckRecoveryMs := rc.config.StuckRecoveryMs
			maxRecoveries := rc.config.MaxStuckRecoveries
			state := rc.state
			warned := rc.stuckStateWarned
			recoveryCount := rc.stuckRecoveryCount
			rc.mu.Unlock()

			warningThreshold := time.Duration(stuckWarningMs) * time.Millisecond
			recoveryThreshold := time.Duration(stuckRecoveryMs) * time.Millisecond

			// Warning: emit stuck warning event
			if elapsed > warningThreshold && !warned {
				slog.Warn("respawn stuck in state",
					"session", rc.sessionID,
					"state", state.String(),
					"elapsed", elapsed.String(),
				)
				rc.emitEvent(StuckWarningEvent{
					State:      state,
					DurationMs: elapsed.Milliseconds(),
				})
				rc.mu.Lock()
				rc.stuckStateWarned = true
				rc.mu.Unlock()
			}

			// Recovery: if stuck beyond recovery threshold, attempt to unstick
			if elapsed > recoveryThreshold && recoveryCount < maxRecoveries {
				rc.mu.Lock()
				rc.stuckRecoveryCount++
				attempt := rc.stuckRecoveryCount
				rc.mu.Unlock()

				slog.Warn("respawn stuck recovery attempt",
					"session", rc.sessionID,
					"state", state.String(),
					"attempt", attempt,
					"maxRecoveries", maxRecoveries,
				)
				rc.emitEvent(StuckRecoveryEvent{
					State:      state,
					DurationMs: elapsed.Milliseconds(),
					Attempt:    attempt,
				})

				// Recovery action: return to watching to retry the cycle
				rc.timers.CancelAll()
				_ = rc.setState(StateWatching)
			}
		}
	}
}

func (rc *Controller) emitEvent(event RespawnEvent) {
	select {
	case rc.events <- event:
	default:
		// Drop on backpressure
	}
}
