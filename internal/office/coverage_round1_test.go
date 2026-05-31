package office

import (
	"strings"
	"testing"
	"time"
)

// TestOfficeEventTypeStrings verifies every OfficeEvent's officeEventType()
// returns its stable wire string. These are trivial but load-bearing: the
// SSE consumers in office.js switch on these exact strings.
func TestOfficeEventTypeStrings(t *testing.T) {
	cases := []struct {
		ev   OfficeEvent
		want string
	}{
		{MemberJoinedEvent{}, "memberJoined"},
		{MemberLeftEvent{}, "memberLeft"},
		{StandupStartedEvent{}, "standupStarted"},
		{StandupEndedEvent{}, "standupEnded"},
		{TaskAssignedEvent{}, "taskAssigned"},
		{BugTaskCreatedEvent{}, "bugTaskCreated"},
		{BrainstormResponseEvent{}, "brainstormResponse"},
		{MeetingStartedEvent{}, "meetingStarted"},
		{MeetingEndedEvent{}, "meetingEnded"},
		{AgentReplenishmentEvent{}, "agentReplenishment"},
		{AgentRecoveryEvent{}, "agentRecovery"},
		{AgentStuckEvent{}, "agentStuck"},
		{CuriosityEvent{}, "curiosityTriggered"},
		{WaterCoolerEvent{}, "waterCoolerChat"},
		{ReflectionEvent{}, "reflectionTriggered"},
	}
	for _, tc := range cases {
		if got := tc.ev.officeEventType(); got != tc.want {
			t.Errorf("officeEventType() = %q, want %q", got, tc.want)
		}
	}
}

// TestSubmitLeaderTasksResponse_DeliversAndBroadcasts verifies the leader
// task response is delivered to a waiting slot, audited, and broadcast.
func TestSubmitLeaderTasksResponse_DeliversAndBroadcasts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m, _, rec := newCtrlManager(t, DefaultConfig())

	// Unknown session -> false, no panic.
	if m.SubmitLeaderTasksResponse("ghost", "anything") {
		t.Fatal("expected false for unknown session")
	}

	m.RegisterAgent("sess-lead", "lead", &TeamMemberConfig{ID: "lead", Name: "Lead", Tier: "strategic"})
	agent := m.GetAgent("sess-lead")

	// Install a wait slot the way triggerLeaderTaskAssignment would, so the
	// deliver path is exercised.
	slot := newWaitSlot()
	agent.mu.Lock()
	agent.LeaderTaskSlot = slot
	agent.mu.Unlock()

	received := make(chan string, 1)
	go func() { received <- <-slot.ch() }()

	if !m.SubmitLeaderTasksResponse("sess-lead", "TASKS_FOR:lead\n- [HIGH] do thing") {
		t.Fatal("expected true for known session")
	}

	select {
	case got := <-received:
		if !strings.Contains(got, "TASKS_FOR:lead") {
			t.Errorf("slot received %q, expected the response content", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for slot delivery")
	}

	if !rec.has("office:leaderTasksResponse") {
		t.Error("expected office:leaderTasksResponse broadcast")
	}
}

// TestSubmitLeaderTasksResponse_NilSlot exercises the branch where no slot is
// installed -- delivery is skipped but the broadcast/audit still happen.
func TestSubmitLeaderTasksResponse_NilSlot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m, _, rec := newCtrlManager(t, DefaultConfig())
	m.RegisterAgent("sess-x", "x", &TeamMemberConfig{ID: "x", Name: "Ecks", Tier: "strategic"})

	if !m.SubmitLeaderTasksResponse("sess-x", "no slot here") {
		t.Fatal("expected true even without a slot")
	}
	if !rec.has("office:leaderTasksResponse") {
		t.Error("expected broadcast even without a slot")
	}
}

// TestSubmitCuriosityDiscovery covers the curiosity channel delivery, the
// nil-channel branch, the unknown-session branch, and the broadcast.
func TestSubmitCuriosityDiscovery(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m, _, rec := newCtrlManager(t, DefaultConfig())

	if m.SubmitCuriosityDiscovery("ghost", "x") {
		t.Fatal("expected false for unknown session")
	}

	m.RegisterAgent("sess-c", "c", &TeamMemberConfig{ID: "c", Name: "Curio", Tier: "worker"})
	agent := m.GetAgent("sess-c")

	ch := make(chan string, 1)
	agent.mu.Lock()
	agent.CuriosityCh = ch
	agent.mu.Unlock()

	if !m.SubmitCuriosityDiscovery("sess-c", "found a neat pattern") {
		t.Fatal("expected true for known session")
	}
	select {
	case got := <-ch:
		if got != "found a neat pattern" {
			t.Errorf("channel got %q", got)
		}
	default:
		t.Fatal("expected the summary on the curiosity channel")
	}
	if !rec.has("office:curiosityDiscovery") {
		t.Error("expected office:curiosityDiscovery broadcast")
	}

	// Second send with a full (non-drained) channel exercises the default
	// (drop) branch -- must not block or panic.
	full := make(chan string, 1)
	full <- "pre-filled"
	agent.mu.Lock()
	agent.CuriosityCh = full
	agent.mu.Unlock()
	if !m.SubmitCuriosityDiscovery("sess-c", "dropped") {
		t.Fatal("expected true even when channel is full")
	}
}

// TestInjectTaskQueue_WritesPromptAndBroadcasts verifies the rendered task
// queue prompt is written to the agent session and the SSE event fires.
func TestInjectTaskQueue_WritesPromptAndBroadcasts(t *testing.T) {
	m, store, rec := newCtrlManager(t, DefaultConfig())
	m.RegisterAgent("sess-q", "q", &TeamMemberConfig{ID: "q", Name: "Queue", Tier: "worker"})
	agent := m.GetAgent("sess-q")

	tasks := []*Task{
		{ID: "t1", Title: "Fix bug", Priority: "high", Description: "details here"},
		{ID: "t2", Title: "Write docs", Priority: "low"},
	}
	m.injectTaskQueue(agent, tasks, store, "Ship the v2 release")

	writes := store.writesFor("sess-q")
	if len(writes) == 0 {
		t.Fatal("expected a task queue prompt to be written")
	}
	body := writes[0].Input
	for _, want := range []string{"[Task Queue]", "[Team Mission] Ship the v2 release", "[HIGH] Fix bug", "details here", "[LOW] Write docs"} {
		if !strings.Contains(body, want) {
			t.Errorf("task queue prompt missing %q\n---\n%s", want, body)
		}
	}
	if !rec.has("office:taskQueueGenerated") {
		t.Error("expected office:taskQueueGenerated broadcast")
	}
}

// TestInjectTaskQueue_NilSessions covers the early-return when no session
// store is available -- still safe, no broadcast write.
func TestInjectTaskQueue_NilSessions(t *testing.T) {
	m, _, _ := newCtrlManager(t, DefaultConfig())
	m.RegisterAgent("sess-q2", "q2", &TeamMemberConfig{ID: "q2", Name: "Queue2", Tier: "worker"})
	agent := m.GetAgent("sess-q2")
	// Passing a nil SessionStore must not panic.
	m.injectTaskQueue(agent, []*Task{{ID: "t", Title: "x", Priority: "med"}}, nil, "")
}

// TestBuildOptimizationSuggestion_LearningAndSkillTips exercises the
// learning-tip and skill-recommendation branches that the existing
// gap-hints test leaves uncovered.
func TestBuildOptimizationSuggestion_LearningAndSkillTips(t *testing.T) {
	top := AgentScore{Name: "Ace", OverallScore: 0.9, CompletionRate: 0.9, CollaborationScore: 0.9, Grade: "A"}
	bottom := AgentScore{Name: "Slow", OverallScore: 0.3, CompletionRate: 0.85, CollaborationScore: 0.85, Grade: "D"}

	topL := &AgentLearningData{
		Learnings: []AgentLearning{
			{Content: "old tip"},
			{Content: "always run tests first"},
		},
		Skills: []AgentSkillEntry{
			{Name: "TableDriven", Description: "use table-driven tests"},
		},
	}
	// bottom lacks the skill -> recommendation should appear.
	bottomL := &AgentLearningData{Skills: []AgentSkillEntry{{Name: "OtherSkill"}}}

	got := buildOptimizationSuggestion(top, bottom, topL, bottomL)
	if !strings.Contains(got, "always run tests first") {
		t.Errorf("expected most-recent learning tip, got %q", got)
	}
	if !strings.Contains(got, "Consider trying the skill: TableDriven") {
		t.Errorf("expected skill recommendation, got %q", got)
	}
	// Completion and collaboration gaps are below threshold here, so the
	// gap hints must NOT appear.
	if strings.Contains(got, "Focus on completing tasks") {
		t.Errorf("did not expect completion hint with sub-threshold gap, got %q", got)
	}
}

// TestBuildOptimizationSuggestion_SkillAlreadyHeld covers the branch where
// the bottom performer already has the top performer's skill (no recommend).
func TestBuildOptimizationSuggestion_SkillAlreadyHeld(t *testing.T) {
	top := AgentScore{Name: "Ace", OverallScore: 0.9, Grade: "A"}
	bottom := AgentScore{Name: "Slow", OverallScore: 0.3, Grade: "D"}
	topL := &AgentLearningData{Skills: []AgentSkillEntry{{Name: "Shared", Description: "x"}}}
	bottomL := &AgentLearningData{Skills: []AgentSkillEntry{{Name: "shared"}}} // case-insensitive match

	got := buildOptimizationSuggestion(top, bottom, topL, bottomL)
	if strings.Contains(got, "Consider trying the skill") {
		t.Errorf("did not expect skill recommendation when already held, got %q", got)
	}
}
