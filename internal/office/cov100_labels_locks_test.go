package office

import (
	"testing"
	"time"
)

// TestAddLabel_TaskNotFound covers the not-found error.
func TestAddLabel_TaskNotFound(t *testing.T) {
	a := NewAgentState("s", "c", &TeamMemberConfig{ID: "c"})
	if err := a.AddLabel("ghost", "bug"); err == nil {
		t.Fatal("expected task-not-found error")
	}
}

// TestRemoveLabel_TaskNotFound covers the not-found error.
func TestRemoveLabel_TaskNotFound(t *testing.T) {
	a := NewAgentState("s", "c", &TeamMemberConfig{ID: "c"})
	if err := a.RemoveLabel("ghost", "bug"); err == nil {
		t.Fatal("expected task-not-found error")
	}
}

// TestRemoveLabel_MissingLabelNoOp covers the silent no-op when the label
// isn't present on a found task.
func TestRemoveLabel_MissingLabelNoOp(t *testing.T) {
	a := NewAgentState("s", "c", &TeamMemberConfig{ID: "c"})
	a.AddTask(&Task{ID: "t1"})
	if err := a.RemoveLabel("t1", "not-there"); err != nil {
		t.Fatalf("expected no-op success, got %v", err)
	}
}

// TestGetLabelRollups_SkipsUnlabeledAndTieBreaks drives the no-labels skip
// and the alphabetical tie-break (two labels with equal cost).
func TestGetLabelRollups_SkipsUnlabeledAndTieBreaks(t *testing.T) {
	m := newTestManager()
	m.RegisterAgent("s1", "c1", &TeamMemberConfig{ID: "c1", Name: "C1"})
	a := m.GetAgent("s1")
	// Unlabeled task → skipped.
	a.AddTask(&Task{ID: "t0", Status: TaskCompleted})
	// Two labeled, completed tasks with equal (zero) cost → alphabetical order.
	a.AddTask(&Task{ID: "tb", Labels: []string{"zeta"}, Status: TaskCompleted})
	a.AddTask(&Task{ID: "ta", Labels: []string{"alpha"}, Status: TaskCompleted})

	rollups := m.GetLabelRollups()
	if len(rollups) != 2 {
		t.Fatalf("expected 2 rollups, got %d", len(rollups))
	}
	// Equal cost → alphabetical: alpha before zeta.
	if rollups[0].Label != "alpha" || rollups[1].Label != "zeta" {
		t.Errorf("expected alphabetical tie-break, got %s,%s", rollups[0].Label, rollups[1].Label)
	}
}

// TestGetLocks_CleansExpired drives the expired-lock cleanup branch. (The
// nil-lock skip at locks.go:68 is unreachable in practice: AcquireLock only
// ever stores non-nil locks, and an injected nil would panic the earlier
// cleanup loop's lock.ExpiresAt deref first — so we don't fake it.)
func TestGetLocks_CleansExpired(t *testing.T) {
	m := newTestManager()
	// Acquire a live lock.
	if _, err := m.AcquireLock("/a/live.txt", "sess", "Owner"); err != nil {
		t.Fatal(err)
	}
	// Inject an expired lock directly.
	m.mu.Lock()
	m.fileLocks["/a/expired.txt"] = &FileLock{
		Path: "/a/expired.txt", Owner: "old", ExpiresAt: time.Now().Add(-time.Hour),
	}
	m.mu.Unlock()

	locks := m.GetLocks()
	for _, l := range locks {
		if l.Path == "/a/expired.txt" {
			t.Error("expected expired lock to be cleaned up")
		}
	}
	// The expired entry should be gone from the map too.
	m.mu.RLock()
	_, stillThere := m.fileLocks["/a/expired.txt"]
	m.mu.RUnlock()
	if stillThere {
		t.Error("expected expired lock removed from map")
	}
}
