package office

// R20 · Staleness sweep tests. Each sweep has one invariant:
//   - sweepStaleMeetings only force-ends Active meetings older than
//     staleMeetingTTL
//   - sweepOrphanedBugs only clears AssignedTo when the assignee is
//     no longer in the agents map

import (
	"sync"
	"testing"
	"time"
)

func newSweepManager(t *testing.T) (*Manager, func() []string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Cleanup(func() { time.Sleep(80 * time.Millisecond) })
	store := &recordingSessionStore{}
	var mu sync.Mutex
	var events []string
	broadcast := func(event string, _ interface{}) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, event)
	}
	m := NewManager(store, broadcast, nil)
	return m, func() []string {
		mu.Lock()
		defer mu.Unlock()
		out := make([]string, len(events))
		copy(out, events)
		return out
	}
}

func TestSweepStaleMeetings_ForceEndsOldActive(t *testing.T) {
	m, events := newSweepManager(t)
	oldMtg := &Meeting{
		ID: "old", Type: "brainstorm", Active: true,
		StartedAt: time.Now().Add(-3 * time.Hour),
	}
	freshMtg := &Meeting{
		ID: "fresh", Type: "standup", Active: true,
		StartedAt: time.Now().Add(-5 * time.Minute),
	}
	m.mu.Lock()
	m.meetings[oldMtg.ID] = oldMtg
	m.meetings[freshMtg.ID] = freshMtg
	m.mu.Unlock()

	ids := m.sweepStaleMeetings()
	if len(ids) != 1 || ids[0] != "old" {
		t.Fatalf("expected ['old'], got %v", ids)
	}
	m.mu.RLock()
	if m.meetings["old"].Active {
		t.Fatal("expected old meeting force-ended")
	}
	if !m.meetings["fresh"].Active {
		t.Fatal("expected fresh meeting untouched")
	}
	m.mu.RUnlock()

	var saw bool
	for _, ev := range events() {
		if ev == "office:meetingForceEnded" {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("expected office:meetingForceEnded, got %v", events())
	}
}

func TestSweepStaleMeetings_IgnoresInactive(t *testing.T) {
	m, _ := newSweepManager(t)
	m.mu.Lock()
	m.meetings["closed"] = &Meeting{
		ID: "closed", Type: "brainstorm", Active: false,
		StartedAt: time.Now().Add(-72 * time.Hour),
	}
	m.mu.Unlock()

	if ids := m.sweepStaleMeetings(); len(ids) != 0 {
		t.Fatalf("inactive meetings should not be swept, got %v", ids)
	}
}

func TestSweepOrphanedBugs_ClearsDeadAssignee(t *testing.T) {
	m, events := newSweepManager(t)
	m.RegisterAgent("sess-alive", "cfg-alive", &TeamMemberConfig{ID: "cfg-alive", Name: "Alive"})

	m.mu.Lock()
	m.bugTasks["b1"] = &BugTask{ID: "b1", AssignedTo: "sess-dead", Status: "open"}
	m.bugTasks["b2"] = &BugTask{ID: "b2", AssignedTo: "sess-alive", Status: "open"}
	m.bugTasks["b3"] = &BugTask{ID: "b3", AssignedTo: "", Status: "open"} // already unassigned
	m.mu.Unlock()

	cleared := m.sweepOrphanedBugs()
	if len(cleared) != 1 || cleared[0] != "b1" {
		t.Fatalf("expected ['b1'] cleared, got %v", cleared)
	}

	m.mu.RLock()
	if m.bugTasks["b1"].AssignedTo != "" {
		t.Fatal("expected b1 assignee cleared")
	}
	if m.bugTasks["b2"].AssignedTo != "sess-alive" {
		t.Fatal("expected b2 untouched (assignee still alive)")
	}
	m.mu.RUnlock()

	var saw bool
	for _, ev := range events() {
		if ev == "office:bugTaskOrphaned" {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("expected office:bugTaskOrphaned, got %v", events())
	}
}
