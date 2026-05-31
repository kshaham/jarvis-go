package office

import (
	"testing"
)

// flipHandoffStatusHook returns a handoffRecheckHook that, on first fire,
// flips the live handoff's status on sender "sess-vex"'s task to a
// non-pending value under the sender's lock — exactly the state a
// concurrent resolver would leave behind. This deterministically drives
// the "handoff no longer pending" re-check guards in AcceptHandoff,
// CancelHandoff, and finishReject that otherwise fire only under a real
// goroutine race.
func flipHandoffStatusHook(m *Manager, newStatus HandoffStatus) func(string) {
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
		for _, tk := range sender.taskQueue.Tasks {
			if tk.ID == taskID && tk.Handoff != nil {
				tk.Handoff.Status = newStatus
			}
		}
		sender.mu.Unlock()
	}
}

// TestAcceptHandoff_RecheckGuardFires drives the locked
// "handoff no longer pending" guard in AcceptHandoff (handoff.go ~355).
// The hook flips the live handoff to HandoffAccepted between the
// lock-free resolve and the locked re-check, so the re-check observes a
// non-pending status and the accept bails.
func TestAcceptHandoff_RecheckGuardFires(t *testing.T) {
	m, _, _, task := setupTeamForHandoff(t)
	if _, err := m.InitiateHandoff("sess-vex", task.ID, "bolt", "ctx"); err != nil {
		t.Fatalf("InitiateHandoff: %v", err)
	}
	prev := handoffRecheckHook
	t.Cleanup(func() { handoffRecheckHook = prev })
	handoffRecheckHook = flipHandoffStatusHook(m, HandoffAccepted)

	_, err := m.AcceptHandoff("sess-bolt", task.ID)
	if err == nil {
		t.Fatal("expected 'handoff no longer pending' on a concurrent-resolve race")
	}
	if err.Error() != "handoff no longer pending" {
		t.Errorf("expected 'handoff no longer pending', got %v", err)
	}
}

// TestCancelHandoff_RecheckGuardFires drives the locked guard in
// CancelHandoff (handoff.go ~435).
func TestCancelHandoff_RecheckGuardFires(t *testing.T) {
	m, _, _, task := setupTeamForHandoff(t)
	if _, err := m.InitiateHandoff("sess-vex", task.ID, "bolt", "ctx"); err != nil {
		t.Fatalf("InitiateHandoff: %v", err)
	}
	prev := handoffRecheckHook
	t.Cleanup(func() { handoffRecheckHook = prev })
	handoffRecheckHook = flipHandoffStatusHook(m, HandoffAccepted)

	_, err := m.CancelHandoff("sess-vex", task.ID)
	if err == nil {
		t.Fatal("expected 'handoff no longer pending' on a concurrent-resolve race")
	}
	if err.Error() != "handoff no longer pending" {
		t.Errorf("expected 'handoff no longer pending', got %v", err)
	}
}

// TestFinishReject_RecheckGuardFires drives the locked guard in
// finishReject (handoff.go ~501) via the receiver-facing RejectHandoff
// entry point.
func TestFinishReject_RecheckGuardFires(t *testing.T) {
	m, _, _, task := setupTeamForHandoff(t)
	if _, err := m.InitiateHandoff("sess-vex", task.ID, "bolt", "ctx"); err != nil {
		t.Fatalf("InitiateHandoff: %v", err)
	}
	prev := handoffRecheckHook
	t.Cleanup(func() { handoffRecheckHook = prev })
	handoffRecheckHook = flipHandoffStatusHook(m, HandoffAccepted)

	_, err := m.RejectHandoff("sess-bolt", task.ID, "too busy")
	if err == nil {
		t.Fatal("expected 'handoff no longer pending' on a concurrent-resolve race")
	}
	if err.Error() != "handoff no longer pending" {
		t.Errorf("expected 'handoff no longer pending', got %v", err)
	}
}
