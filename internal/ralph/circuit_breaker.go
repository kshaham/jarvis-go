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

// autoResetDelay is the base delay before auto-resetting a tripped breaker.
const autoResetDelay = 5 * time.Minute

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

	completionIndicators int
	lastCompletionAt     time.Time

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
	cb.completionIndicators++
	cb.lastCompletionAt = time.Now()
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

	if cb.completionIndicators < 2 || !cb.exitSignalSeen {
		return false
	}

	now := time.Now()
	completionRecent := now.Sub(cb.lastCompletionAt) <= cb.window
	exitRecent := now.Sub(cb.lastExitSignalAt) <= cb.window
	if completionRecent && exitRecent {
		cb.tripped = true
		return true
	}
	return false
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
	cb.completionIndicators = 0
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
		CompletionIndicators: cb.completionIndicators,
		ExitSignalSeen:       cb.exitSignalSeen,
		Tripped:              cb.tripped,
		LastCompletionAt:     cb.lastCompletionAt,
		LastExitSignalAt:     cb.lastExitSignalAt,
		AutoResetCount:       cb.autoResetCount,
		Window:               cb.window.String(),
	}
}
