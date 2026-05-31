package office

import (
	"strings"
	"testing"
	"time"
)

// TestCheckForIdleAgents_BrainstormsWithLeader verifies Strategy 1: an idle
// worker with an idle strategic leader available triggers a brainstorm
// meeting (instead of a self-replenishment prompt).
func TestCheckForIdleAgents_BrainstormsWithLeader(t *testing.T) {
	cfg := idleTestConfig()
	m, _, _ := newCtrlManager(t, cfg)
	m.SetPerpetualMode(true)

	m.RegisterAgent("sess-worker", "worker", &TeamMemberConfig{ID: "worker", Name: "Worker", Role: "Backend", Tier: "worker"})
	m.RegisterAgent("sess-lead", "lead", &TeamMemberConfig{ID: "lead", Name: "Lead", Role: "Engineering Manager", Tier: "strategic"})

	m.CheckForIdleAgents()

	// A brainstorm meeting should now be active with both participants.
	if m.ActiveMeetingCount() == 0 {
		t.Fatal("expected a brainstorm meeting to start for the idle worker + leader")
	}
	// The worker should be in an active meeting.
	if !m.IsInActiveMeeting("sess-worker") {
		t.Error("expected the idle worker to be in the brainstorm meeting")
	}
	if !m.IsInActiveMeeting("sess-lead") {
		t.Error("expected the leader to be in the brainstorm meeting")
	}
}

// TestCheckForIdleAgents_BrainstormsWithPartner verifies Strategy 2: when no
// strategic leader is available but a non-busy peer is, the idle agent
// brainstorms with the peer.
func TestCheckForIdleAgents_BrainstormsWithPartner(t *testing.T) {
	cfg := idleTestConfig()
	m, _, _ := newCtrlManager(t, cfg)
	m.SetPerpetualMode(true)

	// No strategic agent; two idle workers.
	m.RegisterAgent("sess-w1", "w1", &TeamMemberConfig{ID: "w1", Name: "W1", Role: "Frontend", Tier: "worker"})
	m.RegisterAgent("sess-w2", "w2", &TeamMemberConfig{ID: "w2", Name: "W2", Role: "Backend", Tier: "worker"})

	m.CheckForIdleAgents()

	if m.ActiveMeetingCount() == 0 {
		t.Fatal("expected a brainstorm meeting between two idle workers")
	}
}

// TestCheckBlockedTasksForLeaderTriage_AutoFailsAfterMaxAttempts verifies the
// auto-fail branch: once triageAttempts reaches the max, the blocked task is
// moved to TaskFailed and the triage tracking entries are reclaimed.
func TestCheckBlockedTasksForLeaderTriage_AutoFailsAfterMaxAttempts(t *testing.T) {
	m, _, rec := newCtrlManager(t, DefaultConfig())
	m.mu.Lock()
	m.config.BlockedTriageMaxAttempts = 2
	m.mu.Unlock()

	m.RegisterAgent("sess-worker", "worker", &TeamMemberConfig{ID: "worker", Name: "Worker", Tier: "worker"})
	m.RegisterAgent("sess-lead", "lead", &TeamMemberConfig{ID: "lead", Name: "Lead", Tier: "strategic"})

	worker := m.GetAgent("sess-worker")
	worker.AddTask(&Task{
		ID:          "blocked-x",
		Title:       "Stuck task",
		Description: "needs a key",
		Status:      TaskBlocked,
		AssignedAt:  time.Now().Add(-30 * time.Minute),
	})

	// Pre-seed attempts at the max and an old lastTriageAt so the cooldown
	// gate doesn't suppress this pass.
	m.mu.Lock()
	m.triageAttempts["blocked-x"] = 2
	m.lastTriageAt["blocked-x"] = time.Now().Add(-10 * time.Minute)
	m.mu.Unlock()

	m.checkBlockedTasksForLeaderTriage()

	tasks := worker.GetTasks()
	if len(tasks) == 0 || tasks[0].Status != TaskFailed {
		t.Fatalf("expected blocked task auto-failed, got %+v", tasks)
	}
	if !rec.has("office:taskAutoFailed") {
		t.Error("expected office:taskAutoFailed broadcast")
	}
	// Triage tracking should have been reclaimed.
	m.mu.RLock()
	_, attemptsLeft := m.triageAttempts["blocked-x"]
	m.mu.RUnlock()
	if attemptsLeft {
		t.Error("expected triage tracking reclaimed after auto-fail")
	}
}

// TestCheckBlockedTasksForLeaderTriage_RespectsCooldown verifies a recent
// triage attempt (within 5 min) suppresses another triage prompt.
func TestCheckBlockedTasksForLeaderTriage_RespectsCooldown(t *testing.T) {
	m, store, _ := newCtrlManager(t, DefaultConfig())

	m.RegisterAgent("sess-w", "w", &TeamMemberConfig{ID: "w", Name: "W", Tier: "worker"})
	m.RegisterAgent("sess-l", "l", &TeamMemberConfig{ID: "l", Name: "L", Tier: "strategic"})
	worker := m.GetAgent("sess-w")
	worker.AddTask(&Task{ID: "blk", Title: "Blocked", Status: TaskBlocked, AssignedAt: time.Now()})

	// Recent triage attempt within the 5-min cooldown.
	m.mu.Lock()
	m.triageAttempts["blk"] = 1
	m.lastTriageAt["blk"] = time.Now()
	m.mu.Unlock()

	m.checkBlockedTasksForLeaderTriage()

	// No new triage prompt should have been written to the leader.
	for _, wc := range store.writesFor("sess-l") {
		if strings.Contains(wc.Input, "[BLOCKED TASK TRIAGE]") {
			t.Error("expected no triage prompt within cooldown")
		}
	}
}
