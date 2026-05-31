package office

import (
	"testing"
	"time"
)

// removeTaskHook returns a handoffRecheckHook that removes the task from
// sender "sess-vex"'s queue between the lock-free resolve and the locked
// re-check, deterministically driving AcceptHandoff's "handoff task no
// longer in sender queue" branch (handoff.go ~350-353).
func removeTaskHook(m *Manager) func(string) {
	fired := false
	return func(taskID string) {
		if fired {
			return
		}
		fired = true
		sender := m.GetAgent("sess-vex")
		if sender == nil {
			return
		}
		sender.mu.Lock()
		out := sender.taskQueue.Tasks[:0]
		for _, tk := range sender.taskQueue.Tasks {
			if tk.ID != taskID {
				out = append(out, tk)
			}
		}
		sender.taskQueue.Tasks = out
		sender.mu.Unlock()
	}
}

// TestAcceptHandoff_TaskVanishedFromQueue drives the idx==-1 branch in
// AcceptHandoff (handoff.go ~350-353): the live task is removed from the
// sender's queue by a concurrent resolver after the lock-free resolve,
// so the locked scan can't find it and the accept bails.
func TestAcceptHandoff_TaskVanishedFromQueue(t *testing.T) {
	m, _, _, task := setupTeamForHandoff(t)
	if _, err := m.InitiateHandoff("sess-vex", task.ID, "bolt", "ctx"); err != nil {
		t.Fatalf("InitiateHandoff: %v", err)
	}
	prev := handoffRecheckHook
	t.Cleanup(func() { handoffRecheckHook = prev })
	handoffRecheckHook = removeTaskHook(m)

	_, err := m.AcceptHandoff("sess-bolt", task.ID)
	if err == nil {
		t.Fatal("expected error when the task vanished from the sender queue")
	}
	if err.Error() != "handoff task no longer in sender queue" {
		t.Errorf("expected 'handoff task no longer in sender queue', got %v", err)
	}
}

// TestAcceptHandoff_ProgressLogCapped drives the ProgressLog cap branch
// in AcceptHandoff (handoff.go ~400-402). We seed the task with a
// progress log already at the cap so the accept's appended
// "[handoff accepted by ...]" entry pushes it over and the slice is
// trimmed back to taskProgressLogCap.
func TestAcceptHandoff_ProgressLogCapped(t *testing.T) {
	m, agentVex, _, task := setupTeamForHandoff(t)

	// Fill the task's progress log up to the cap before the handoff.
	agentVex.mu.Lock()
	for i := 0; i < taskProgressLogCap; i++ {
		task.ProgressLog = append(task.ProgressLog, TaskProgressEntry{
			Timestamp: time.Now(),
			Note:      "entry-" + itoa(i),
		})
	}
	agentVex.mu.Unlock()

	if _, err := m.InitiateHandoff("sess-vex", task.ID, "bolt", "ctx"); err != nil {
		t.Fatalf("InitiateHandoff: %v", err)
	}
	if _, err := m.AcceptHandoff("sess-bolt", task.ID); err != nil {
		t.Fatalf("AcceptHandoff: %v", err)
	}

	// The receiver's copy must have a capped progress log ending in the
	// handoff-accepted note.
	bolt := m.GetAgent("sess-bolt")
	tasks := bolt.GetTasksCopy()
	if len(tasks) != 1 {
		t.Fatalf("expected the moved task on the receiver, got %d", len(tasks))
	}
	pl := tasks[0].ProgressLog
	if len(pl) > taskProgressLogCap {
		t.Errorf("progress log should be capped at %d, got %d", taskProgressLogCap, len(pl))
	}
	if len(pl) == 0 || pl[len(pl)-1].Note == "" {
		t.Errorf("expected a non-empty final progress entry, got %+v", pl)
	}
}

// TestSweepStaleHandoffs_ReceiverAliveSkipped drives the "receiver still
// alive" continue in SweepStaleHandoffs (handoff.go ~670): an old
// handoff whose receiver is still on the roster is NOT swept.
func TestSweepStaleHandoffs_ReceiverAliveSkipped(t *testing.T) {
	m, agentVex, _, task := setupTeamForHandoff(t)
	if _, err := m.InitiateHandoff("sess-vex", task.ID, "bolt", "ctx"); err != nil {
		t.Fatalf("InitiateHandoff: %v", err)
	}
	// Backdate the handoff so it's well past the threshold.
	agentVex.mu.Lock()
	for _, tk := range agentVex.taskQueue.Tasks {
		if tk.ID == task.ID && tk.Handoff != nil {
			tk.Handoff.CreatedAt = time.Now().Add(-2 * time.Hour)
		}
	}
	agentVex.mu.Unlock()

	// Receiver sess-bolt is still registered, so the stale handoff must
	// NOT be swept.
	if swept := m.SweepStaleHandoffs(time.Minute); swept != 0 {
		t.Errorf("expected 0 swept (receiver still alive), got %d", swept)
	}
}
