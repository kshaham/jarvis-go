package runsummary

import (
	"sync"
	"time"
)

// Event categories for typed event tracking.
const (
	EventSessionStarted       = "session_started"
	EventIdleDetected         = "idle_detected"
	EventWorkingDetected      = "working_detected"
	EventRespawnCycleStarted  = "respawn_cycle_started"
	EventRespawnCycleComplete = "respawn_cycle_completed"
	EventRespawnStateChange   = "respawn_state_change"
	EventTokenMilestone       = "token_milestone"
	EventStuckDetected        = "stuck_detected"
	EventRecoveryAttempted    = "recovery_attempted"
	EventExit                 = "exit"
)

// Severity levels for events.
const (
	SeverityCritical = "critical"
	SeverityError    = "error"
	SeverityWarning  = "warning"
	SeverityInfo     = "info"
	SeveritySuccess  = "success"
)

// maxEvents is the maximum number of events retained per session.
const maxEvents = 1000

// trimToEvents is the number to keep when trimming.
const trimToEvents = 800

// tokenMilestoneInterval is the token count interval for milestones.
const tokenMilestoneInterval int64 = 50000

// Event represents a single tracked event in a session's run timeline.
type Event struct {
	Type      string      `json:"type"`
	Severity  string      `json:"severity"`
	Timestamp time.Time   `json:"timestamp"`
	Data      interface{} `json:"data,omitempty"`
}

// Stats holds aggregated statistics for a session's run.
type Stats struct {
	TotalRespawnCycles int            `json:"totalRespawnCycles"`
	TotalActiveTime    time.Duration  `json:"totalActiveTime"`
	TotalIdleTime      time.Duration  `json:"totalIdleTime"`
	EventCounts        map[string]int `json:"eventCounts"`
	LastTokenTotal     int64          `json:"lastTokenTotal"`
	StateTransitions   int            `json:"stateTransitions"`
}

// Tracker records significant events during a session's lifetime
// to provide a consolidated "what happened while I was away" summary.
type Tracker struct {
	mu        sync.Mutex
	sessionID string
	events    []Event
	startedAt time.Time

	// Aggregated stats
	totalRespawnCycles int
	stateTransitions   int
	eventCounts        map[string]int

	// Active/idle time tracking
	isCurrentlyActive  bool
	lastStatusChangeAt time.Time
	totalActiveTime    time.Duration
	totalIdleTime      time.Duration

	// Token milestone tracking
	lastTokenMilestone int64

	// Respawn cycle tracking
	inRespawnCycle bool
}

// New creates a new run summary tracker for the given session.
func New(sessionID string) *Tracker {
	now := time.Now()
	t := &Tracker{
		sessionID:          sessionID,
		events:             make([]Event, 0),
		startedAt:          now,
		eventCounts:        make(map[string]int),
		lastStatusChangeAt: now,
	}
	t.RecordWithSeverity(EventSessionStarted, SeverityInfo, nil)
	return t
}

// Record appends an event to the timeline with auto-detected severity.
func (t *Tracker) Record(eventType string, data interface{}) {
	severity := autoDetectSeverity(eventType)
	t.RecordWithSeverity(eventType, severity, data)
}

// RecordWithSeverity appends an event with an explicit severity level.
func (t *Tracker) RecordWithSeverity(eventType, severity string, data interface{}) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.events = append(t.events, Event{
		Type:      eventType,
		Severity:  severity,
		Timestamp: time.Now(),
		Data:      data,
	})
	t.eventCounts[eventType]++

	if len(t.events) > maxEvents {
		t.events = t.events[len(t.events)-trimToEvents:]
	}
}

// RecordIdle records an idle detection event and updates time tracking.
func (t *Tracker) RecordIdle() {
	t.mu.Lock()
	now := time.Now()
	if t.isCurrentlyActive {
		t.totalActiveTime += now.Sub(t.lastStatusChangeAt)
	}
	t.isCurrentlyActive = false
	t.lastStatusChangeAt = now
	t.mu.Unlock()

	t.Record(EventIdleDetected, nil)
}

// RecordWorking records a working detection event and updates time tracking.
func (t *Tracker) RecordWorking() {
	t.mu.Lock()
	now := time.Now()
	if !t.isCurrentlyActive {
		t.totalIdleTime += now.Sub(t.lastStatusChangeAt)
	}
	t.isCurrentlyActive = true
	t.lastStatusChangeAt = now
	t.mu.Unlock()

	t.Record(EventWorkingDetected, nil)
}

// RecordTokens checks for token milestones and emits events accordingly.
// Should be called whenever the total token count is updated.
func (t *Tracker) RecordTokens(total int64) {
	t.mu.Lock()
	lastMilestone := t.lastTokenMilestone
	t.mu.Unlock()

	// Check if we've crossed a new milestone
	currentMilestone := (total / tokenMilestoneInterval) * tokenMilestoneInterval
	if currentMilestone > lastMilestone && currentMilestone > 0 {
		t.mu.Lock()
		t.lastTokenMilestone = currentMilestone
		t.mu.Unlock()

		t.Record(EventTokenMilestone, map[string]interface{}{
			"totalTokens": total,
			"milestone":   currentMilestone,
		})
	}
}

// RecordStateChange records a respawn state transition and tracks cycles.
func (t *Tracker) RecordStateChange(fromState, toState string) {
	t.mu.Lock()
	t.stateTransitions++

	// Track respawn cycle start/completion
	if toState == "SENDING_UPDATE" && !t.inRespawnCycle {
		t.inRespawnCycle = true
		t.mu.Unlock()
		t.Record(EventRespawnCycleStarted, map[string]interface{}{
			"from": fromState,
			"to":   toState,
		})
		return
	}

	if toState == "WATCHING" && t.inRespawnCycle && fromState != "WATCHING" {
		t.inRespawnCycle = false
		t.totalRespawnCycles++
		cycles := t.totalRespawnCycles
		t.mu.Unlock()
		t.Record(EventRespawnCycleComplete, map[string]interface{}{
			"from":        fromState,
			"to":          toState,
			"totalCycles": cycles,
		})
		return
	}
	t.mu.Unlock()

	t.Record(EventRespawnStateChange, map[string]interface{}{
		"from": fromState,
		"to":   toState,
	})
}

// GetStats returns aggregated statistics for the session run.
func (t *Tracker) GetStats() Stats {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Finalize active/idle time up to now
	now := time.Now()
	activeTime := t.totalActiveTime
	idleTime := t.totalIdleTime
	if t.isCurrentlyActive {
		activeTime += now.Sub(t.lastStatusChangeAt)
	} else {
		idleTime += now.Sub(t.lastStatusChangeAt)
	}

	// Copy event counts
	counts := make(map[string]int, len(t.eventCounts))
	for k, v := range t.eventCounts {
		counts[k] = v
	}

	return Stats{
		TotalRespawnCycles: t.totalRespawnCycles,
		TotalActiveTime:    activeTime,
		TotalIdleTime:      idleTime,
		EventCounts:        counts,
		LastTokenTotal:     t.lastTokenMilestone,
		StateTransitions:   t.stateTransitions,
	}
}

// GetSummary returns an aggregated summary of the session's run.
func (t *Tracker) GetSummary() map[string]interface{} {
	t.mu.Lock()
	defer t.mu.Unlock()

	eventsCopy := make([]Event, len(t.events))
	copy(eventsCopy, t.events)

	// Finalize time tracking
	now := time.Now()
	activeTime := t.totalActiveTime
	idleTime := t.totalIdleTime
	if t.isCurrentlyActive {
		activeTime += now.Sub(t.lastStatusChangeAt)
	} else {
		idleTime += now.Sub(t.lastStatusChangeAt)
	}

	counts := make(map[string]int, len(t.eventCounts))
	for k, v := range t.eventCounts {
		counts[k] = v
	}

	return map[string]interface{}{
		"sessionId":          t.sessionID,
		"startedAt":          t.startedAt,
		"duration":           time.Since(t.startedAt).String(),
		"eventCount":         len(eventsCopy),
		"events":             eventsCopy,
		"totalRespawnCycles": t.totalRespawnCycles,
		"totalActiveTimeMs":  activeTime.Milliseconds(),
		"totalIdleTimeMs":    idleTime.Milliseconds(),
		"stateTransitions":   t.stateTransitions,
		"eventCounts":        counts,
	}
}

// autoDetectSeverity returns the appropriate severity for known event types.
func autoDetectSeverity(eventType string) string {
	switch eventType {
	case EventSessionStarted:
		return SeverityInfo
	case EventIdleDetected:
		return SeveritySuccess
	case EventWorkingDetected:
		return SeverityInfo
	case EventRespawnCycleStarted:
		return SeverityInfo
	case EventRespawnCycleComplete:
		return SeveritySuccess
	case EventRespawnStateChange:
		return SeverityInfo
	case EventTokenMilestone:
		return SeverityInfo
	case EventStuckDetected:
		return SeverityWarning
	case EventRecoveryAttempted:
		return SeverityWarning
	case EventExit:
		return SeverityInfo
	default:
		return SeverityInfo
	}
}
