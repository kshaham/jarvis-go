package respawn

import (
	"sync"
	"sync/atomic"
	"time"
)

// TimerEvent is emitted when a timer fires, starts, or is cancelled.
type TimerEvent struct {
	Name string
	Type string // "fired", "cancelled", "started"
}

// ManagedTimer wraps a time.Timer with metadata.
type ManagedTimer struct {
	Name       string
	Timer      *time.Timer
	Duration   time.Duration
	StartedAt  time.Time
	Reason     string
	generation int64
}

// ActiveTimerInfo is a snapshot of a running timer for UI display.
type ActiveTimerInfo struct {
	Name        string `json:"name"`
	RemainingMs int64  `json:"remainingMs"`
	TotalMs     int64  `json:"totalMs"`
	Reason      string `json:"reason"`
}

// TimerPool manages named timers with cancellation support.
type TimerPool struct {
	mu      sync.Mutex
	timers  map[string]*ManagedTimer
	events  chan TimerEvent
	nextGen int64
}

// NewTimerPool creates a new timer pool.
func NewTimerPool() *TimerPool {
	return &TimerPool{
		timers: make(map[string]*ManagedTimer),
		events: make(chan TimerEvent, 64),
	}
}

// Start creates or resets a named timer. Cancels any existing timer with the same name.
func (tp *TimerPool) Start(name string, duration time.Duration, reason string, fn func()) {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	if existing, ok := tp.timers[name]; ok {
		existing.Timer.Stop()
	}

	gen := atomic.AddInt64(&tp.nextGen, 1)

	timer := time.AfterFunc(duration, func() {
		tp.mu.Lock()
		existing, ok := tp.timers[name]
		if !ok || existing.generation != gen {
			tp.mu.Unlock()
			return // Timer was replaced or cancelled
		}
		delete(tp.timers, name)
		tp.mu.Unlock()

		select {
		case tp.events <- TimerEvent{Name: name, Type: "fired"}:
		default:
		}

		fn()
	})

	tp.timers[name] = &ManagedTimer{
		Name:       name,
		Timer:      timer,
		Duration:   duration,
		StartedAt:  time.Now(),
		Reason:     reason,
		generation: gen,
	}

	select {
	case tp.events <- TimerEvent{Name: name, Type: "started"}:
	default:
	}
}

// Cancel stops a named timer.
func (tp *TimerPool) Cancel(name string) {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	if t, ok := tp.timers[name]; ok {
		t.Timer.Stop()
		delete(tp.timers, name)
		select {
		case tp.events <- TimerEvent{Name: name, Type: "cancelled"}:
		default:
		}
	}
}

// CancelAll stops all timers.
func (tp *TimerPool) CancelAll() {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	for name, t := range tp.timers {
		t.Timer.Stop()
		delete(tp.timers, name)
	}
}

// ActiveTimers returns a snapshot of all running timers.
func (tp *TimerPool) ActiveTimers() []ActiveTimerInfo {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	result := make([]ActiveTimerInfo, 0, len(tp.timers))
	now := time.Now()
	for _, t := range tp.timers {
		remaining := t.Duration - now.Sub(t.StartedAt)
		if remaining < 0 {
			remaining = 0
		}
		result = append(result, ActiveTimerInfo{
			Name:        t.Name,
			RemainingMs: remaining.Milliseconds(),
			TotalMs:     t.Duration.Milliseconds(),
			Reason:      t.Reason,
		})
	}
	return result
}

// Count returns the number of active timers.
func (tp *TimerPool) Count() int {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	return len(tp.timers)
}

// Events returns the timer event channel.
func (tp *TimerPool) Events() <-chan TimerEvent {
	return tp.events
}
