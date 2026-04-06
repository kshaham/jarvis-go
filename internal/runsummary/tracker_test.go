package runsummary

import (
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	tracker := New("session-1")
	if tracker == nil {
		t.Fatal("expected non-nil tracker")
	}
	if tracker.sessionID != "session-1" {
		t.Errorf("expected session-1, got %s", tracker.sessionID)
	}

	// Should have recorded a session_started event
	summary := tracker.GetSummary()
	count := summary["eventCount"].(int)
	if count != 1 {
		t.Errorf("expected 1 initial event, got %d", count)
	}
	events := summary["events"].([]Event)
	if events[0].Type != EventSessionStarted {
		t.Errorf("expected session_started event, got %s", events[0].Type)
	}
	if events[0].Severity != SeverityInfo {
		t.Errorf("expected info severity, got %s", events[0].Severity)
	}
}

func TestTypedEvents(t *testing.T) {
	tracker := New("session-2")

	tracker.Record(EventIdleDetected, nil)
	tracker.Record(EventWorkingDetected, nil)
	tracker.Record(EventStuckDetected, map[string]string{"state": "SENDING_UPDATE"})
	tracker.Record(EventExit, map[string]interface{}{"exitCode": 0})

	summary := tracker.GetSummary()
	count := summary["eventCount"].(int)
	// 1 session_started + 4 recorded = 5
	if count != 5 {
		t.Errorf("expected 5 events, got %d", count)
	}

	events := summary["events"].([]Event)
	expectedTypes := []string{
		EventSessionStarted,
		EventIdleDetected,
		EventWorkingDetected,
		EventStuckDetected,
		EventExit,
	}
	for i, expected := range expectedTypes {
		if events[i].Type != expected {
			t.Errorf("event %d: expected type %s, got %s", i, expected, events[i].Type)
		}
	}
}

func TestAutoDetectSeverity(t *testing.T) {
	tests := []struct {
		eventType string
		expected  string
	}{
		{EventSessionStarted, SeverityInfo},
		{EventIdleDetected, SeveritySuccess},
		{EventWorkingDetected, SeverityInfo},
		{EventRespawnCycleStarted, SeverityInfo},
		{EventRespawnCycleComplete, SeveritySuccess},
		{EventStuckDetected, SeverityWarning},
		{EventRecoveryAttempted, SeverityWarning},
		{EventExit, SeverityInfo},
		{"unknown_event", SeverityInfo},
	}

	for _, tt := range tests {
		got := autoDetectSeverity(tt.eventType)
		if got != tt.expected {
			t.Errorf("autoDetectSeverity(%s): expected %s, got %s", tt.eventType, tt.expected, got)
		}
	}
}

func TestRecordWithSeverity(t *testing.T) {
	tracker := New("session-3")
	tracker.RecordWithSeverity(EventStuckDetected, SeverityCritical, nil)

	summary := tracker.GetSummary()
	events := summary["events"].([]Event)
	last := events[len(events)-1]
	if last.Severity != SeverityCritical {
		t.Errorf("expected critical severity, got %s", last.Severity)
	}
}

func TestTokenMilestones(t *testing.T) {
	tracker := New("session-4")

	// First call at 30k -- no milestone
	tracker.RecordTokens(30000)
	stats := tracker.GetStats()
	if stats.EventCounts[EventTokenMilestone] != 0 {
		t.Errorf("expected 0 token milestones, got %d", stats.EventCounts[EventTokenMilestone])
	}

	// Cross 50k milestone
	tracker.RecordTokens(55000)
	stats = tracker.GetStats()
	if stats.EventCounts[EventTokenMilestone] != 1 {
		t.Errorf("expected 1 token milestone, got %d", stats.EventCounts[EventTokenMilestone])
	}

	// Stay within same milestone range (no new event)
	tracker.RecordTokens(60000)
	stats = tracker.GetStats()
	if stats.EventCounts[EventTokenMilestone] != 1 {
		t.Errorf("expected still 1 token milestone, got %d", stats.EventCounts[EventTokenMilestone])
	}

	// Cross 100k milestone
	tracker.RecordTokens(105000)
	stats = tracker.GetStats()
	if stats.EventCounts[EventTokenMilestone] != 2 {
		t.Errorf("expected 2 token milestones, got %d", stats.EventCounts[EventTokenMilestone])
	}
}

func TestActiveIdleTimeTracking(t *testing.T) {
	tracker := New("session-5")

	// Start idle, then become active
	time.Sleep(10 * time.Millisecond)
	tracker.RecordWorking()

	time.Sleep(10 * time.Millisecond)
	tracker.RecordIdle()

	stats := tracker.GetStats()
	if stats.TotalActiveTime <= 0 {
		t.Error("expected positive active time")
	}
	if stats.TotalIdleTime <= 0 {
		t.Error("expected positive idle time")
	}
}

func TestRecordStateChange(t *testing.T) {
	tracker := New("session-6")

	// Start a respawn cycle
	tracker.RecordStateChange("WATCHING", "SENDING_UPDATE")
	stats := tracker.GetStats()
	if stats.EventCounts[EventRespawnCycleStarted] != 1 {
		t.Errorf("expected 1 cycle started event, got %d", stats.EventCounts[EventRespawnCycleStarted])
	}

	// Complete the cycle
	tracker.RecordStateChange("WAITING_KICKSTART", "WATCHING")
	stats = tracker.GetStats()
	if stats.TotalRespawnCycles != 1 {
		t.Errorf("expected 1 completed cycle, got %d", stats.TotalRespawnCycles)
	}
	if stats.EventCounts[EventRespawnCycleComplete] != 1 {
		t.Errorf("expected 1 cycle complete event, got %d", stats.EventCounts[EventRespawnCycleComplete])
	}
}

func TestGetStatsCopy(t *testing.T) {
	tracker := New("session-7")
	tracker.Record(EventIdleDetected, nil)
	tracker.Record(EventWorkingDetected, nil)

	stats := tracker.GetStats()
	// Modifying the returned map should not affect the tracker
	stats.EventCounts["fake"] = 999

	stats2 := tracker.GetStats()
	if _, ok := stats2.EventCounts["fake"]; ok {
		t.Error("expected stats to be a copy, but mutation was reflected")
	}
}

func TestEventTrimming(t *testing.T) {
	tracker := New("session-8")

	// Record more than maxEvents
	for i := 0; i < maxEvents+50; i++ {
		tracker.Record(EventWorkingDetected, nil)
	}

	summary := tracker.GetSummary()
	count := summary["eventCount"].(int)
	// After trimming, count should be at most trimToEvents (the trim keeps the last trimToEvents).
	// Since we add events one at a time and trim when exceeding maxEvents,
	// the count should never exceed maxEvents.
	if count > maxEvents {
		t.Errorf("expected events to be at most %d, got %d", maxEvents, count)
	}
	// Should be trimmed to approximately trimToEvents
	if count < trimToEvents {
		t.Errorf("expected at least %d events after trimming, got %d", trimToEvents, count)
	}
}
