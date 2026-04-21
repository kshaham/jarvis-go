package office

import (
	"strings"
	"testing"
	"time"
)

func setupTeamForHandoff(t *testing.T) (*Manager, *AgentState, *AgentState, *Task) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	vex := &TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend", Tier: "worker"}
	bolt := &TeamMemberConfig{ID: "bolt", Name: "Bolt", Role: "Backend", Tier: "worker"}
	m.RegisterAgent("sess-vex", "vex", vex)
	m.RegisterAgent("sess-bolt", "bolt", bolt)

	task := &Task{
		ID:         "t-1",
		Title:      "Refactor auth",
		Status:     TaskInProgress,
		Priority:   "medium",
		AssignedAt: time.Now(),
		StartedAt:  time.Now(),
	}
	agentVex := m.GetAgent("sess-vex")
	agentBolt := m.GetAgent("sess-bolt")
	agentVex.AddTask(task)
	return m, agentVex, agentBolt, task
}

func TestHandoff_HappyAccept(t *testing.T) {
	m, vex, bolt, task := setupTeamForHandoff(t)

	h, err := m.InitiateHandoff(vex.SessionID, task.ID, bolt.ConfigID, "API contract questions")
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}
	if h.Status != HandoffPending {
		t.Fatalf("expected pending, got %s", h.Status)
	}

	// Task is still in sender's queue but status is handoff_pending.
	if task.Status != TaskHandoffPending {
		t.Errorf("expected handoff_pending, got %s", task.Status)
	}
	if len(vex.GetTasks()) != 1 || len(bolt.GetTasks()) != 0 {
		t.Error("task should still be in sender's queue until accepted")
	}

	// Receiver accepts.
	if _, err := m.AcceptHandoff(bolt.SessionID, task.ID); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if len(vex.GetTasks()) != 0 || len(bolt.GetTasks()) != 1 {
		t.Errorf("task should have moved to receiver. vex=%d bolt=%d",
			len(vex.GetTasks()), len(bolt.GetTasks()))
	}
	moved := bolt.GetTasks()[0]
	if moved.Status != TaskInProgress {
		t.Errorf("accepted task should be in_progress, got %s", moved.Status)
	}
	if moved.Source != HandoffSource {
		t.Errorf("accepted task source=handoff, got %s", moved.Source)
	}
	if moved.Handoff != nil {
		t.Error("handoff pointer should be cleared after accept")
	}
}

func TestHandoff_Reject(t *testing.T) {
	m, vex, bolt, task := setupTeamForHandoff(t)

	_, err := m.InitiateHandoff(vex.SessionID, task.ID, bolt.ConfigID, "API contract questions")
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}
	if _, err := m.RejectHandoff(bolt.SessionID, task.ID, "already swamped"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if task.Status != TaskInProgress {
		t.Errorf("rejected task should revert to in_progress, got %s", task.Status)
	}
	if task.Handoff != nil {
		t.Error("handoff pointer should be cleared after reject")
	}
	if len(vex.GetTasks()) != 1 {
		t.Error("task should still belong to sender")
	}
	// Sender should have received a targeted chat message.
	msgs := m.GetMessages()
	found := false
	for _, msg := range msgs {
		if msg.To == vex.SessionID && strings.Contains(msg.Content, "declined") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected rejection message to sender")
	}
}

func TestHandoff_SenderNotOwner(t *testing.T) {
	m, _, bolt, task := setupTeamForHandoff(t)

	// Wrong sender tries to hand off vex's task.
	_, err := m.InitiateHandoff("sess-stranger", task.ID, bolt.ConfigID, "x")
	if err == nil {
		t.Fatal("expected error when non-owner initiates handoff")
	}
}

func TestHandoff_ReceiverNotInRoster(t *testing.T) {
	m, vex, _, task := setupTeamForHandoff(t)

	_, err := m.InitiateHandoff(vex.SessionID, task.ID, "nonexistent-config", "ctx")
	if err == nil || !strings.Contains(err.Error(), "not in roster") {
		t.Fatalf("expected 'not in roster' error, got %v", err)
	}
}

func TestHandoff_SelfHandoff(t *testing.T) {
	m, vex, _, task := setupTeamForHandoff(t)

	_, err := m.InitiateHandoff(vex.SessionID, task.ID, vex.ConfigID, "ctx")
	if err == nil || !strings.Contains(err.Error(), "self") {
		t.Fatalf("expected self-handoff error, got %v", err)
	}
}

func TestHandoff_DoubleInitiate(t *testing.T) {
	m, vex, bolt, task := setupTeamForHandoff(t)

	if _, err := m.InitiateHandoff(vex.SessionID, task.ID, bolt.ConfigID, "ctx1"); err != nil {
		t.Fatalf("first initiate: %v", err)
	}
	_, err := m.InitiateHandoff(vex.SessionID, task.ID, bolt.ConfigID, "ctx2")
	if err == nil || !strings.Contains(err.Error(), "already has a pending") {
		t.Fatalf("expected double-initiate error, got %v", err)
	}
}

func TestHandoff_ContextTooLarge(t *testing.T) {
	m, vex, bolt, task := setupTeamForHandoff(t)
	big := strings.Repeat("x", HandoffContextLimit+1)
	_, err := m.InitiateHandoff(vex.SessionID, task.ID, bolt.ConfigID, big)
	if err == nil || !strings.Contains(err.Error(), "context too large") {
		t.Fatalf("expected size-limit error, got %v", err)
	}
}

func TestHandoff_NotInProgress(t *testing.T) {
	m, vex, bolt, task := setupTeamForHandoff(t)
	vex.UpdateTaskStatus(task.ID, TaskCompleted)
	_, err := m.InitiateHandoff(vex.SessionID, task.ID, bolt.ConfigID, "ctx")
	if err == nil || !strings.Contains(err.Error(), "in_progress") {
		t.Fatalf("expected in_progress error, got %v", err)
	}
}

func TestHandoff_PendingLimit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	vex := &TeamMemberConfig{ID: "vex", Name: "Vex", Tier: "worker"}
	bolt := &TeamMemberConfig{ID: "bolt", Name: "Bolt", Tier: "worker"}
	m.RegisterAgent("sess-vex", "vex", vex)
	m.RegisterAgent("sess-bolt", "bolt", bolt)
	agentVex := m.GetAgent("sess-vex")

	// Queue HandoffMaxPendingPerSender in_progress tasks and initiate on each.
	for i := 0; i < HandoffMaxPendingPerSender; i++ {
		task := &Task{
			ID:     "t-" + string(rune('a'+i)),
			Title:  "task",
			Status: TaskInProgress,
		}
		agentVex.AddTask(task)
		if _, err := m.InitiateHandoff("sess-vex", task.ID, "bolt", "ctx"); err != nil {
			t.Fatalf("initiate %d: %v", i, err)
		}
	}
	// One more task, initiate should now be refused.
	extra := &Task{ID: "t-extra", Title: "extra", Status: TaskInProgress}
	agentVex.AddTask(extra)
	_, err := m.InitiateHandoff("sess-vex", extra.ID, "bolt", "ctx")
	if err == nil || !strings.Contains(err.Error(), "too many pending") {
		t.Fatalf("expected pending-limit error, got %v", err)
	}
}

func TestHandoff_StaleSweepReceiverGone(t *testing.T) {
	m, vex, bolt, task := setupTeamForHandoff(t)
	if _, err := m.InitiateHandoff(vex.SessionID, task.ID, bolt.ConfigID, "ctx"); err != nil {
		t.Fatalf("initiate: %v", err)
	}
	// Unregister the receiver -- simulates a despawn / crash.
	m.UnregisterAgent(bolt.SessionID)

	// Backdate the handoff so it's past the threshold.
	vex.mu.Lock()
	task.Handoff.CreatedAt = time.Now().Add(-30 * time.Minute)
	vex.mu.Unlock()

	swept := m.SweepStaleHandoffs(15 * time.Minute)
	if swept != 1 {
		t.Fatalf("expected 1 swept, got %d", swept)
	}
	if task.Status != TaskInProgress {
		t.Errorf("swept task should revert to in_progress, got %s", task.Status)
	}
	if task.Handoff != nil {
		t.Error("handoff pointer should be cleared after sweep")
	}
}

func TestHandoff_StaleSweepReceiverAlive(t *testing.T) {
	m, vex, bolt, task := setupTeamForHandoff(t)
	if _, err := m.InitiateHandoff(vex.SessionID, task.ID, bolt.ConfigID, "ctx"); err != nil {
		t.Fatalf("initiate: %v", err)
	}
	// Receiver is still registered; sweep should not touch the handoff.
	vex.mu.Lock()
	task.Handoff.CreatedAt = time.Now().Add(-30 * time.Minute)
	vex.mu.Unlock()

	if swept := m.SweepStaleHandoffs(15 * time.Minute); swept != 0 {
		t.Errorf("expected 0 swept while receiver alive, got %d", swept)
	}
	if task.Status != TaskHandoffPending {
		t.Errorf("task should still be handoff_pending, got %s", task.Status)
	}
}

// R32 · sender-initiated cancel. Task returns to sender's queue in
// in_progress state; audit entry records HandoffCancelled.
func TestHandoff_SenderCancels(t *testing.T) {
	m, vex, bolt, task := setupTeamForHandoff(t)

	if _, err := m.InitiateHandoff(vex.SessionID, task.ID, bolt.ConfigID, "nevermind"); err != nil {
		t.Fatalf("initiate: %v", err)
	}
	if task.Status != TaskHandoffPending || task.Handoff == nil {
		t.Fatal("precondition: task should be handoff_pending with non-nil Handoff")
	}

	h, err := m.CancelHandoff(vex.SessionID, task.ID)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if h.Status != HandoffCancelled {
		t.Errorf("expected status=cancelled, got %s", h.Status)
	}
	if task.Status != TaskInProgress {
		t.Errorf("expected task back to in_progress, got %s", task.Status)
	}
	if task.Handoff != nil {
		t.Error("handoff pointer should be cleared")
	}
	// Audit store should contain the cancelled entry.
	history := m.GetHandoffHistory()
	found := false
	for _, e := range history {
		if e.TaskID == task.ID && e.Status == HandoffCancelled {
			found = true
			break
		}
	}
	if !found {
		t.Error("audit store missing the cancelled entry")
	}
	// Task stays with sender.
	if len(vex.GetTasks()) != 1 || len(bolt.GetTasks()) != 0 {
		t.Errorf("task should still be on sender; vex=%d bolt=%d",
			len(vex.GetTasks()), len(bolt.GetTasks()))
	}
}

func TestHandoff_CancelByNonSender_Forbidden(t *testing.T) {
	m, vex, bolt, task := setupTeamForHandoff(t)
	if _, err := m.InitiateHandoff(vex.SessionID, task.ID, bolt.ConfigID, "x"); err != nil {
		t.Fatalf("initiate: %v", err)
	}
	// Bolt (the receiver, not the sender) tries to cancel.
	_, err := m.CancelHandoff(bolt.SessionID, task.ID)
	if err == nil {
		t.Fatal("expected error when non-sender cancels")
	}
	if !strings.Contains(err.Error(), "not your handoff") {
		t.Errorf("expected 'not your handoff', got %v", err)
	}
	// Sanity: task remains handoff_pending.
	if task.Status != TaskHandoffPending {
		t.Errorf("task should still be handoff_pending; got %s", task.Status)
	}
}

func TestHandoff_IncomingHandoffsFor(t *testing.T) {
	m, vex, bolt, task := setupTeamForHandoff(t)
	if _, err := m.InitiateHandoff(vex.SessionID, task.ID, bolt.ConfigID, "ctx"); err != nil {
		t.Fatalf("initiate: %v", err)
	}
	incoming := m.IncomingHandoffsFor(bolt.SessionID)
	if len(incoming) != 1 || incoming[0].TaskID != task.ID {
		t.Errorf("expected 1 incoming, got %v", incoming)
	}
	if other := m.IncomingHandoffsFor(vex.SessionID); len(other) != 0 {
		t.Errorf("sender should have 0 incoming, got %d", len(other))
	}
}
