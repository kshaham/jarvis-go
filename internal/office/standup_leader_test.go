package office

import (
	"strings"
	"testing"
	"time"
)

// TestProcessLeaderTaskResponse_HappyPath drives the leader task processing
// with a pre-delivered slot so the select reads immediately. It must parse
// the response, create tasks for each named agent, inject their queues, and
// send a post-standup summary back to the leader.
func TestProcessLeaderTaskResponse_HappyPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m, store, rec := newCtrlManager(t, DefaultConfig())

	m.RegisterAgent("sess-lead", "lead", &TeamMemberConfig{ID: "lead", Name: "Lead", Tier: "strategic"})
	m.RegisterAgent("sess-a", "alpha", &TeamMemberConfig{ID: "alpha", Name: "Alpha", Tier: "worker", Role: "FE"})
	m.RegisterAgent("sess-b", "beta", &TeamMemberConfig{ID: "beta", Name: "Beta", Tier: "worker", Role: "BE"})
	leader := m.GetAgent("sess-lead")
	alpha := m.GetAgent("sess-a")
	beta := m.GetAgent("sess-b")

	allAgents := []*AgentState{leader, alpha, beta}

	slot := newWaitSlot()
	response := strings.Join([]string{
		"TASKS_FOR:alpha",
		"- [HIGH] Build login form | with validation",
		"- [LOW] Polish CSS",
		"TASKS_FOR:beta",
		"- [HIGH] Add auth endpoint",
		"TASKS_FOR:ghost", // unknown agent -> skipped without crashing
		"- [HIGH] nowhere",
	}, "\n")
	if !slot.deliver(response) {
		t.Fatal("pre-delivery to slot should succeed")
	}

	m.processLeaderTaskResponse(slot, leader, allAgents, store, "Ship v2")

	if got := alpha.PendingTaskCount(); got != 2 {
		t.Errorf("alpha should have 2 pending tasks, got %d", got)
	}
	if got := beta.PendingTaskCount(); got != 1 {
		t.Errorf("beta should have 1 pending task, got %d", got)
	}

	// Each agent got a task-queue prompt injected.
	if len(store.writesFor("sess-a")) == 0 {
		t.Error("expected a task queue injection for alpha")
	}
	if len(store.writesFor("sess-b")) == 0 {
		t.Error("expected a task queue injection for beta")
	}

	// The leader received a post-standup summary referencing both agents.
	// (Other writes -- e.g. an optimization suggestion from AnalyzeAndOptimize
	// -- may also land on the leader, so scan all writes for the summary.)
	leaderWrites := store.writesFor("sess-lead")
	if len(leaderWrites) == 0 {
		t.Fatal("expected a post-standup summary to the leader")
	}
	var summary string
	for _, w := range leaderWrites {
		if strings.Contains(w.Input, "Post-Standup Summary") {
			summary = w.Input
		}
	}
	if summary == "" {
		t.Fatalf("expected a Post-Standup Summary write, got %d writes", len(leaderWrites))
	}
	if !strings.Contains(summary, "Alpha") || !strings.Contains(summary, "Beta") {
		t.Errorf("expected both agent names in summary, got %q", summary)
	}

	if !rec.has("office:taskQueueGenerated") {
		t.Error("expected taskQueueGenerated broadcasts")
	}
}

// TestProcessLeaderTaskResponse_EmptyAssignments covers the early-return when
// the leader's reply parses to zero task blocks.
func TestProcessLeaderTaskResponse_EmptyAssignments(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m, store, _ := newCtrlManager(t, DefaultConfig())
	m.RegisterAgent("sess-lead", "lead", &TeamMemberConfig{ID: "lead", Name: "Lead", Tier: "strategic"})
	leader := m.GetAgent("sess-lead")

	slot := newWaitSlot()
	slot.deliver("no task markers in here at all")
	m.processLeaderTaskResponse(slot, leader, []*AgentState{leader}, store, "")

	if len(store.writesFor("sess-lead")) != 0 {
		t.Error("expected no summary when nothing parsed")
	}
}

// TestTriggerLeaderTaskAssignment_PromptsStrategicAgents verifies the standup
// leader-assignment trigger sends a prompt only to strategic-tier agents and
// installs a wait slot on them.
func TestTriggerLeaderTaskAssignment_PromptsStrategicAgents(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m, store, _ := newCtrlManager(t, DefaultConfig())
	m.RegisterAgent("sess-lead", "lead", &TeamMemberConfig{ID: "lead", Name: "Lead", Tier: "strategic", Role: "Lead"})
	m.RegisterAgent("sess-w", "worker", &TeamMemberConfig{ID: "worker", Name: "Worker", Tier: "worker", Role: "Dev"})
	lead := m.GetAgent("sess-lead")

	transcript := []StandupMessage{
		{Type: "status_report", SpeakerName: "Worker", Content: "shipped feature X"},
	}

	m.triggerLeaderTaskAssignment("standup-1", []string{"sess-lead", "sess-w"}, transcript)

	// Give the spawned processLeaderTaskResponse goroutine a moment to install
	// the slot (the prompt write is synchronous, the slot install happens
	// before the WriteInkPrompt).
	leadWrites := store.writesFor("sess-lead")
	if len(leadWrites) == 0 {
		t.Fatal("expected leader to receive a task-assignment prompt")
	}
	prompt := leadWrites[0].Input
	if !strings.Contains(prompt, "LEADER TASK ASSIGNMENT") {
		t.Errorf("expected leader-assignment prompt, got %q", prompt)
	}
	if !strings.Contains(prompt, "shipped feature X") {
		t.Errorf("expected transcript summary embedded, got %q", prompt)
	}
	// Worker (non-strategic) must NOT be prompted.
	if len(store.writesFor("sess-w")) != 0 {
		t.Error("non-strategic agent should not receive a leader-assignment prompt")
	}

	// Slot installed on the leader.
	lead.mu.Lock()
	slot := lead.LeaderTaskSlot
	lead.mu.Unlock()
	if slot == nil {
		t.Error("expected a LeaderTaskSlot installed on the leader")
	}
	// Finish the slot so the background goroutine's reader exits promptly
	// instead of waiting the full 5-minute timeout.
	if slot != nil {
		// Deliver an empty parse so the goroutine returns fast.
		slot.deliver("")
	}
	// Brief settle for the goroutine.
	time.Sleep(50 * time.Millisecond)
}
