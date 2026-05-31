package office

import (
	"testing"
	"time"
)

// makeHandoffStale rewinds the live handoff's CreatedAt on the given sender's
// task so SweepStaleHandoffs treats it as expired.
func makeHandoffStale(t *testing.T, sender *AgentState, taskID string, age time.Duration) {
	t.Helper()
	sender.mu.Lock()
	defer sender.mu.Unlock()
	for _, tk := range sender.taskQueue.Tasks {
		if tk.ID == taskID && tk.Handoff != nil {
			tk.Handoff.CreatedAt = time.Now().Add(-age)
			return
		}
	}
	t.Fatalf("no pending handoff found on task %s to age", taskID)
}

// TestSweepStaleHandoffs_SenderGoneSkips drives the sender==nil continue:
// a stale handoff whose receiver AND sender are both off the roster is left
// alone (we can't rewrite a task we no longer own).
func TestSweepStaleHandoffs_SenderGoneSkips(t *testing.T) {
	m, vex, _, task := setupTeamForHandoff(t)
	if _, err := m.InitiateHandoff("sess-vex", task.ID, "bolt", "ctx"); err != nil {
		t.Fatalf("InitiateHandoff: %v", err)
	}
	makeHandoffStale(t, vex, task.ID, time.Hour)

	// Remove BOTH receiver (bolt) and sender (vex) from the roster.
	m.UnregisterAgent("sess-bolt")
	m.UnregisterAgent("sess-vex")

	swept := m.SweepStaleHandoffs(time.Minute)
	if swept != 0 {
		t.Errorf("expected 0 sweeps when the sender is gone, got %d", swept)
	}
}

// TestSweepStaleHandoffs_NoMatchingPendingTaskSkips drives the target==nil
// continue: receiver is gone and sender is present, but the sender's task no
// longer carries a pending handoff (it was resolved), so nothing is swept.
func TestSweepStaleHandoffs_NoMatchingPendingTaskSkips(t *testing.T) {
	m, vex, _, task := setupTeamForHandoff(t)
	if _, err := m.InitiateHandoff("sess-vex", task.ID, "bolt", "ctx"); err != nil {
		t.Fatalf("InitiateHandoff: %v", err)
	}
	makeHandoffStale(t, vex, task.ID, time.Hour)

	// Receiver leaves so the handoff looks abandoned...
	m.UnregisterAgent("sess-bolt")

	// ...but flip the live handoff to a non-pending status so the inner
	// "find the live pending-handoff task" loop finds no target.
	vex.mu.Lock()
	for _, tk := range vex.taskQueue.Tasks {
		if tk.ID == task.ID && tk.Handoff != nil {
			tk.Handoff.Status = HandoffAccepted
		}
	}
	vex.mu.Unlock()

	swept := m.SweepStaleHandoffs(time.Minute)
	if swept != 0 {
		t.Errorf("expected 0 sweeps when no matching pending task remains, got %d", swept)
	}
}

// TestSweepStaleHandoffs_ExpiresAbandoned drives the happy path: a stale
// handoff whose receiver left the roster (but sender remains, with the live
// pending task) is expired -- the task's handoff pointer is cleared and the
// task returns to in_progress.
func TestSweepStaleHandoffs_ExpiresAbandoned(t *testing.T) {
	m, vex, _, task := setupTeamForHandoff(t)
	if _, err := m.InitiateHandoff("sess-vex", task.ID, "bolt", "ctx"); err != nil {
		t.Fatalf("InitiateHandoff: %v", err)
	}
	makeHandoffStale(t, vex, task.ID, time.Hour)
	m.UnregisterAgent("sess-bolt") // receiver leaves roster

	swept := m.SweepStaleHandoffs(time.Minute)
	if swept != 1 {
		t.Fatalf("expected 1 stale handoff swept, got %d", swept)
	}
	// The task should be back to in_progress with its handoff cleared.
	tasks := vex.GetTasks()
	var got *Task
	for _, tk := range tasks {
		if tk.ID == task.ID {
			got = tk
		}
	}
	if got == nil {
		t.Fatal("task vanished after sweep")
	}
	if got.Handoff != nil {
		t.Error("expected handoff pointer cleared after expiry")
	}
	if got.Status != TaskInProgress {
		t.Errorf("expected task restored to in_progress, got %s", got.Status)
	}
}
