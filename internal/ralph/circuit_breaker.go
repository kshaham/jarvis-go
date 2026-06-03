package ralph

import (
	"sync"
	"time"
)

// circuitBreakerDefaultWindow is the time window within which both conditions must
// be observed for the breaker to trip.
const circuitBreakerDefaultWindow = 30 * time.Second

// maxAutoResets is the maximum number of auto-reset attempts before the breaker
// stays permanently open.
const maxAutoResets = 5

// maxCompletionTimes bounds the sliding window of completion timestamps.
const maxCompletionTimes = 32

// CompletionIndicatorPattern is a regex string that matches natural-language
// phrases indicating all work is complete. Exported for use in tests.
var completionIndicatorPatterns = []string{
	`(?i)all\s+(?:tasks?|items?|work)\s+(?:are\s+)?(?:completed?|done|finished)`,
	`(?i)(?:completed?|finished)\s+all\s+(?:tasks?|items?|work)`,
	`(?i)nothing\s+(?:left|remaining)\s+to\s+do`,
	`(?i)no\s+more\s+(?:tasks?|items?|work)`,
	`(?i)everything\s+(?:is\s+)?(?:completed?|done)`,
	`(?i)project\s+(?:is\s+)?(?:completed?|done|finished)`,
}

// CircuitBreakerStatus exposes the circuit breaker state for serialisation.
type CircuitBreakerStatus struct {
	CompletionIndicators int       `json:"completionIndicators"`
	ExitSignalSeen       bool      `json:"exitSignalSeen"`
	Tripped              bool      `json:"tripped"`
	LastCompletionAt     time.Time `json:"lastCompletionAt,omitempty"`
	LastExitSignalAt     time.Time `json:"lastExitSignalAt,omitempty"`
	AutoResetCount       int       `json:"autoResetCount"`
	Window               string    `json:"window"`
}

// CircuitBreaker implements a dual-condition exit gate for the Ralph loop.
//
// The loop should only terminate when BOTH conditions are met within a
// configurable time window:
//  1. Completion indicators: natural-language phrases like "all tasks done"
//  2. Exit signal: an explicit EXIT_SIGNAL:true from a RALPH_STATUS block
//
// This prevents premature exit from a single false-positive signal.
type CircuitBreaker struct {
	mu sync.Mutex

	window time.Duration

	// completionTimes holds the timestamps of recent completion-indicator
	// detections (ascending). The window invariant requires >=2 of these to
	// fall within `window` of evaluation time -- a single cumulative counter
	// (the previous approach) would let a stale completion from an hour ago
	// combine with a fresh one to trip the breaker, the exact premature-exit
	// it exists to prevent.
	completionTimes  []time.Time
	lastCompletionAt time.Time

	exitSignalSeen   bool
	lastExitSignalAt time.Time

	tripped        bool
	autoResetCount int
}

// NewCircuitBreaker creates a circuit breaker with the default 30-second window.
func NewCircuitBreaker() *CircuitBreaker {
	return &CircuitBreaker{
		window: circuitBreakerDefaultWindow,
	}
}

// NewCircuitBreakerWithWindow creates a circuit breaker with a custom window.
func NewCircuitBreakerWithWindow(window time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		window: window,
	}
}

// RecordCompletion records a completion indicator detection.
func (cb *CircuitBreaker) RecordCompletion() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	now := time.Now()
	cb.lastCompletionAt = now
	cb.completionTimes = append(cb.completionTimes, now)
	cb.pruneCompletionsLocked(now)
}

// pruneCompletionsLocked drops completion timestamps older than the window and
// caps slice length. completionTimes is appended in time order, so the stale
// entries are always a prefix. Caller must hold cb.mu.
func (cb *CircuitBreaker) pruneCompletionsLocked(now time.Time) {
	cutoff := now.Add(-cb.window)
	i := 0
	for i < len(cb.completionTimes) && cb.completionTimes[i].Before(cutoff) {
		i++
	}
	if i > 0 {
		cb.completionTimes = cb.completionTimes[i:]
	}
	if len(cb.completionTimes) > maxCompletionTimes {
		cb.completionTimes = cb.completionTimes[len(cb.completionTimes)-maxCompletionTimes:]
	}
}

// recentCompletionsLocked returns the count of completion indicators within the
// window as of now. Caller must hold cb.mu.
func (cb *CircuitBreaker) recentCompletionsLocked(now time.Time) int {
	cutoff := now.Add(-cb.window)
	n := 0
	for _, ts := range cb.completionTimes {
		if !ts.Before(cutoff) {
			n++
		}
	}
	return n
}

// RecordExitSignal records an EXIT_SIGNAL:true from a RALPH_STATUS block.
func (cb *CircuitBreaker) RecordExitSignal() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.exitSignalSeen = true
	cb.lastExitSignalAt = time.Now()
}

// ShouldBreak returns true when the dual condition is met: at least two
// completion indicators AND an exit signal, both within the configured window.
// Once tripped, it stays tripped until Reset is called.
func (cb *CircuitBreaker) ShouldBreak() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.tripped {
		return true
	}

	if !cb.exitSignalSeen {
		return false
	}

	now := time.Now()
	cb.pruneCompletionsLocked(now)
	// Require at least two completion indicators WITHIN the window (not a
	// cumulative lifetime count) plus a recent exit signal.
	if len(cb.completionTimes) < 2 {
		return false
	}
	if now.Sub(cb.lastExitSignalAt) > cb.window {
		return false
	}
	cb.tripped = true
	return true
}

// Reset clears the breaker state. Returns false if auto-reset limit is
// reached, meaning the breaker should stay open permanently.
func (cb *CircuitBreaker) Reset() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.autoResetCount >= maxAutoResets {
		return false
	}
	cb.autoResetCount++
	cb.tripped = false
	cb.completionTimes = nil
	cb.exitSignalSeen = false
	cb.lastCompletionAt = time.Time{}
	cb.lastExitSignalAt = time.Time{}
	return true
}

// Status returns a snapshot of the circuit breaker state.
func (cb *CircuitBreaker) Status() CircuitBreakerStatus {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return CircuitBreakerStatus{
		CompletionIndicators: cb.recentCompletionsLocked(time.Now()),
		ExitSignalSeen:       cb.exitSignalSeen,
		Tripped:              cb.tripped,
		LastCompletionAt:     cb.lastCompletionAt,
		LastExitSignalAt:     cb.lastExitSignalAt,
		AutoResetCount:       cb.autoResetCount,
		Window:               cb.window.String(),
	}
}
