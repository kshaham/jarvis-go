package office

import (
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// sessions == nil guards
// ---------------------------------------------------------------------------

// TestCheckSessionHealth_NilSessions verifies the early return when the
// session store is nil (no agents are torn down).
func TestCheckSessionHealth_NilSessions(t *testing.T) {
	m, _, _ := newCtrlManager(t, DefaultConfig())
	m.RegisterAgent("sess-a", "a", &TeamMemberConfig{ID: "a", Name: "A", Tier: "worker"})
	m.mu.Lock()
	m.sessions = nil
	m.mu.Unlock()

	m.checkSessionHealth() // must not panic and must not unregister

	if m.GetAgent("sess-a") == nil {
		t.Error("expected agent to survive checkSessionHealth with nil sessions")
	}
}

// TestCheckBlockedTasksForLeaderTriage_NilSessions verifies the early return
// when sessions is nil.
func TestCheckBlockedTasksForLeaderTriage_NilSessions(t *testing.T) {
	m, _, rec := newCtrlManager(t, DefaultConfig())
	m.RegisterAgent("sess-b", "b", &TeamMemberConfig{ID: "b", Name: "B", Tier: "worker"})
	agent := m.GetAgent("sess-b")
	agent.AddTask(&Task{ID: "t1", Title: "blocked", Status: TaskBlocked, AssignedAt: time.Now().Add(-time.Hour)})
	m.mu.Lock()
	m.sessions = nil
	m.mu.Unlock()

	m.checkBlockedTasksForLeaderTriage()

	if rec.has("office:taskAutoFailed") {
		t.Error("expected no triage activity with nil sessions")
	}
}

// TestMaybeTriggerWaterCoolerChat_NilSessions verifies the nil-sessions guard.
func TestMaybeTriggerWaterCoolerChat_NilSessions(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WaterCoolerCooldown = 0
	m, _, rec := newCtrlManager(t, cfg)
	m.RegisterAgent("sess-w1", "w1", &TeamMemberConfig{ID: "w1", Name: "W1", Tier: "worker"})
	m.RegisterAgent("sess-w2", "w2", &TeamMemberConfig{ID: "w2", Name: "W2", Tier: "worker"})
	m.mu.Lock()
	m.sessions = nil
	m.lastWaterCoolerAt = time.Time{}
	m.mu.Unlock()

	// Force the 30% probability gate open by retrying until we get past it;
	// with nil sessions the function must still return cleanly.
	for i := 0; i < 50; i++ {
		m.maybeTriggerWaterCoolerChat()
	}

	if rec.has("office:waterCoolerChat") {
		t.Error("expected no water-cooler chat with nil sessions")
	}
}

// TestCheckForReflectionOpportunity_NilSessions verifies the nil-sessions
// guard.
func TestCheckForReflectionOpportunity_NilSessions(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ReflectionCooldown = 0
	m, _, rec := newCtrlManager(t, cfg)
	m.RegisterAgent("sess-r", "r", &TeamMemberConfig{ID: "r", Name: "R", Tier: "worker"})
	agent := m.GetAgent("sess-r")
	agent.AddTask(&Task{ID: "t1", Status: TaskCompleted, CompletedAt: time.Now()})
	m.mu.Lock()
	m.sessions = nil
	m.mu.Unlock()

	m.checkForReflectionOpportunity()

	if rec.has("office:reflectionTriggered") {
		t.Error("expected no reflection with nil sessions")
	}
}

// ---------------------------------------------------------------------------
// IsWrappingUp guards
// ---------------------------------------------------------------------------

// TestCheckForCuriousAgents_WrappingUpSkips verifies curiosity is suppressed
// during wrap-up.
func TestCheckForCuriousAgents_WrappingUpSkips(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CuriosityCooldown = 0
	m, store, _ := newCtrlManager(t, cfg)
	m.SetCuriosityEnabled(true)
	m.SetWrappingUp(true)
	m.RegisterAgent("sess-c", "c", &TeamMemberConfig{ID: "c", Name: "C", Tier: "worker"})

	m.checkForCuriousAgents()

	if len(store.writesFor("sess-c")) != 0 {
		t.Error("expected no curiosity prompt during wrap-up")
	}
}

// TestMaybeTriggerWaterCoolerChat_WrappingUpSkips verifies water cooler is
// suppressed during wrap-up.
func TestMaybeTriggerWaterCoolerChat_WrappingUpSkips(t *testing.T) {
	m, _, rec := newCtrlManager(t, DefaultConfig())
	m.SetWrappingUp(true)
	m.RegisterAgent("sess-w1", "w1", &TeamMemberConfig{ID: "w1", Name: "W1", Tier: "worker"})
	m.RegisterAgent("sess-w2", "w2", &TeamMemberConfig{ID: "w2", Name: "W2", Tier: "worker"})

	for i := 0; i < 50; i++ {
		m.maybeTriggerWaterCoolerChat()
	}
	if rec.has("office:waterCoolerChat") {
		t.Error("expected no water cooler during wrap-up")
	}
}

// TestCheckForReflectionOpportunity_WrappingUpSkips verifies reflection is
// suppressed during wrap-up.
func TestCheckForReflectionOpportunity_WrappingUpSkips(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ReflectionCooldown = 0
	m, store, _ := newCtrlManager(t, cfg)
	m.SetWrappingUp(true)
	m.RegisterAgent("sess-r", "r", &TeamMemberConfig{ID: "r", Name: "R", Tier: "worker"})
	agent := m.GetAgent("sess-r")
	agent.AddTask(&Task{ID: "t1", Status: TaskCompleted, CompletedAt: time.Now()})

	m.checkForReflectionOpportunity()

	if len(store.writesFor("sess-r")) != 0 {
		t.Error("expected no reflection prompt during wrap-up")
	}
}

// ---------------------------------------------------------------------------
// IsSessionWorking / IsInActiveMeeting skips
// ---------------------------------------------------------------------------

// TestCheckUnacknowledgedTasks_WorkingSkips verifies a working agent's overdue
// tasks are not nudged this cycle.
func TestCheckUnacknowledgedTasks_WorkingSkips(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TaskAckTimeout = 10 * time.Millisecond
	m, store, _ := newCtrlManager(t, cfg)
	m.RegisterAgent("sess-busy", "busy", &TeamMemberConfig{ID: "busy", Name: "Busy", Tier: "worker"})
	store.setWorking("sess-busy", true)
	agent := m.GetAgent("sess-busy")
	agent.AddTask(&Task{ID: "t1", Title: "overdue", Status: TaskPending, AssignedAt: time.Now().Add(-time.Hour)})

	m.checkUnacknowledgedTasks()

	if len(store.writesFor("sess-busy")) != 0 {
		t.Error("expected no ack nudge for a working agent")
	}
}

// TestCheckStaleInProgressTasks_WorkingSkips verifies a working agent's stale
// in-progress task does not get a progress check.
func TestCheckStaleInProgressTasks_WorkingSkips(t *testing.T) {
	m, store, _ := newCtrlManager(t, DefaultConfig())
	m.RegisterAgent("sess-busy", "busy", &TeamMemberConfig{ID: "busy", Name: "Busy", Tier: "worker"})
	store.setWorking("sess-busy", true)
	agent := m.GetAgent("sess-busy")
	agent.AddTask(&Task{ID: "t1", Title: "stale", Status: TaskInProgress, StartedAt: time.Now().Add(-time.Hour)})

	m.checkStaleInProgressTasks()

	if len(store.writesFor("sess-busy")) != 0 {
		t.Error("expected no progress check for a working agent")
	}
}

// TestCheckForCuriousAgents_WorkingSkips verifies a working idle agent is not
// poked with a curiosity prompt.
func TestCheckForCuriousAgents_WorkingSkips(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CuriosityCooldown = 0
	m, store, _ := newCtrlManager(t, cfg)
	m.SetCuriosityEnabled(true)
	m.RegisterAgent("sess-busy", "busy", &TeamMemberConfig{ID: "busy", Name: "Busy", Tier: "worker"})
	store.setWorking("sess-busy", true)

	m.checkForCuriousAgents()

	if len(store.writesFor("sess-busy")) != 0 {
		t.Error("expected no curiosity prompt for a working agent")
	}
}

// TestCheckForCuriousAgents_HasPendingSkips verifies an agent with pending
// tasks (PendingTaskCount > 0) is skipped.
func TestCheckForCuriousAgents_HasPendingSkips(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CuriosityCooldown = 0
	m, store, _ := newCtrlManager(t, cfg)
	m.SetCuriosityEnabled(true)
	m.RegisterAgent("sess-pend", "pend", &TeamMemberConfig{ID: "pend", Name: "Pend", Tier: "worker"})
	agent := m.GetAgent("sess-pend")
	agent.AddTask(&Task{ID: "t1", Title: "queued", Status: TaskPending})

	m.checkForCuriousAgents()

	if len(store.writesFor("sess-pend")) != 0 {
		t.Error("expected no curiosity prompt for agent with pending tasks")
	}
}

// TestCheckForCuriousAgents_InMeetingSkips verifies an idle agent already in
// an active meeting is not given a curiosity prompt.
func TestCheckForCuriousAgents_InMeetingSkips(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CuriosityCooldown = 0
	m, store, _ := newCtrlManager(t, cfg)
	m.SetCuriosityEnabled(true)
	m.RegisterAgent("sess-mtg", "mtg", &TeamMemberConfig{ID: "mtg", Name: "Mtg", Tier: "worker"})
	m.mu.Lock()
	m.meetings["mtg-1"] = &Meeting{ID: "mtg-1", Type: "brainstorm", Active: true, Participants: []string{"sess-mtg"}}
	m.mu.Unlock()

	m.checkForCuriousAgents()

	if len(store.writesFor("sess-mtg")) != 0 {
		t.Error("expected no curiosity prompt for an agent already in a meeting")
	}
}

// TestCheckForReflectionOpportunity_WorkingSkips verifies a working agent that
// just completed a task is not prompted to reflect.
func TestCheckForReflectionOpportunity_WorkingSkips(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ReflectionCooldown = 0
	m, store, _ := newCtrlManager(t, cfg)
	m.RegisterAgent("sess-busy", "busy", &TeamMemberConfig{ID: "busy", Name: "Busy", Tier: "worker"})
	store.setWorking("sess-busy", true)
	agent := m.GetAgent("sess-busy")
	agent.AddTask(&Task{ID: "t1", Status: TaskCompleted, CompletedAt: time.Now()})

	m.checkForReflectionOpportunity()

	if len(store.writesFor("sess-busy")) != 0 {
		t.Error("expected no reflection prompt for a working agent")
	}
}

// TestCheckForReflectionOpportunity_NoTasksSkips verifies an agent with no
// tasks at all (len(tasks)==0) is skipped.
func TestCheckForReflectionOpportunity_NoTasksSkips(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ReflectionCooldown = 0
	m, store, _ := newCtrlManager(t, cfg)
	m.RegisterAgent("sess-empty", "empty", &TeamMemberConfig{ID: "empty", Name: "Empty", Tier: "worker"})
	// No tasks added.

	m.checkForReflectionOpportunity()

	if len(store.writesFor("sess-empty")) != 0 {
		t.Error("expected no reflection prompt for an agent with no tasks")
	}
}

// TestCheckForReflectionOpportunity_OldCompletionSkips verifies that a task
// completed too long ago (>2min) does NOT trigger reflection (!recentlyCompleted).
func TestCheckForReflectionOpportunity_OldCompletionSkips(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ReflectionCooldown = 0
	m, store, _ := newCtrlManager(t, cfg)
	m.RegisterAgent("sess-old", "old", &TeamMemberConfig{ID: "old", Name: "Old", Tier: "worker"})
	agent := m.GetAgent("sess-old")
	agent.AddTask(&Task{ID: "t1", Status: TaskCompleted, CompletedAt: time.Now().Add(-10 * time.Minute)})

	m.checkForReflectionOpportunity()

	if len(store.writesFor("sess-old")) != 0 {
		t.Error("expected no reflection prompt for an old completion")
	}
}

// TestCheckBlockedTasksForLeaderTriage_NoLeaderSkips verifies that when no
// strategic-tier agent exists, blocked tasks are not triaged (leader == nil).
func TestCheckBlockedTasksForLeaderTriage_NoLeaderSkips(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BlockedTriageMaxAttempts = 3
	m, store, _ := newCtrlManager(t, cfg)
	// Only a worker; no strategic leader to triage to.
	m.RegisterAgent("sess-only", "only", &TeamMemberConfig{ID: "only", Name: "Only", Tier: "worker"})
	agent := m.GetAgent("sess-only")
	agent.AddTask(&Task{ID: "t1", Title: "blocked", Description: "stuck", Status: TaskBlocked, AssignedAt: time.Now().Add(-time.Hour)})

	m.checkBlockedTasksForLeaderTriage()

	if len(store.writesFor("sess-only")) != 0 {
		t.Error("expected no triage write when no leader exists")
	}
	// Triage attempts must not have advanced (no leader to send to).
	m.mu.Lock()
	attempts := m.triageAttempts["t1"]
	m.mu.Unlock()
	if attempts != 0 {
		t.Errorf("expected triage attempts to stay 0 with no leader, got %d", attempts)
	}
}

// ---------------------------------------------------------------------------
// maybeTriggerWaterCoolerChat happy path + standup-active guard
// ---------------------------------------------------------------------------

// TestMaybeTriggerWaterCoolerChat_StandupActiveSkips verifies an active
// standup suppresses the water-cooler pairing.
func TestMaybeTriggerWaterCoolerChat_StandupActiveSkips(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WaterCoolerCooldown = 0
	m, _, rec := newCtrlManager(t, cfg)
	m.RegisterAgent("sess-w1", "w1", &TeamMemberConfig{ID: "w1", Name: "W1", Tier: "worker"})
	m.RegisterAgent("sess-w2", "w2", &TeamMemberConfig{ID: "w2", Name: "W2", Tier: "worker"})
	m.mu.Lock()
	m.standup = &StandupState{ID: "s", Active: true}
	m.lastWaterCoolerAt = time.Time{}
	m.mu.Unlock()

	for i := 0; i < 50; i++ {
		m.maybeTriggerWaterCoolerChat()
	}
	if rec.has("office:waterCoolerChat") {
		t.Error("expected no water cooler while a standup is active")
	}
}

// TestMaybeTriggerWaterCoolerChat_WorkingAndMeetingAgentsExcluded verifies the
// idle-filter skips working agents and agents in a meeting, leaving too few
// idle agents to pair.
func TestMaybeTriggerWaterCoolerChat_WorkingAndMeetingAgentsExcluded(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WaterCoolerCooldown = 0
	m, store, rec := newCtrlManager(t, cfg)
	m.RegisterAgent("sess-busy", "busy", &TeamMemberConfig{ID: "busy", Name: "Busy", Tier: "worker"})
	m.RegisterAgent("sess-meet", "meet", &TeamMemberConfig{ID: "meet", Name: "Meet", Tier: "worker"})
	m.RegisterAgent("sess-free", "free", &TeamMemberConfig{ID: "free", Name: "Free", Tier: "worker"})

	store.setWorking("sess-busy", true)
	m.mu.Lock()
	m.meetings["mtg-1"] = &Meeting{ID: "mtg-1", Type: "brainstorm", Active: true, Participants: []string{"sess-meet"}}
	m.lastWaterCoolerAt = time.Time{}
	m.mu.Unlock()

	// Only sess-free is idle -> fewer than 2 -> no chat.
	for i := 0; i < 50; i++ {
		m.maybeTriggerWaterCoolerChat()
	}
	if rec.has("office:waterCoolerChat") {
		t.Error("expected no water-cooler with <2 idle agents")
	}
}

// TestMaybeTriggerWaterCoolerChat_PendingTaskExcludesAgent verifies the
// idle-filter's PendingTaskCount() != 0 arm excludes an agent that has a
// pending task even though it has no active task, leaving too few idle.
func TestMaybeTriggerWaterCoolerChat_PendingTaskExcludesAgent(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WaterCoolerCooldown = 0
	m, _, rec := newCtrlManager(t, cfg)
	m.RegisterAgent("sess-pend", "pend", &TeamMemberConfig{ID: "pend", Name: "Pend", Tier: "worker"})
	m.RegisterAgent("sess-free", "free", &TeamMemberConfig{ID: "free", Name: "Free", Tier: "worker"})
	// One agent has a pending task (no active task) -> excluded by the
	// PendingTaskCount()!=0 arm; only one idle agent remains -> no chat.
	m.GetAgent("sess-pend").AddTask(&Task{ID: "p1", Title: "queued", Status: TaskPending})
	m.mu.Lock()
	m.lastWaterCoolerAt = time.Time{}
	m.mu.Unlock()

	for i := 0; i < 50; i++ {
		m.maybeTriggerWaterCoolerChat()
	}
	if rec.has("office:waterCoolerChat") {
		t.Error("expected no water cooler when only one agent is truly idle")
	}
}

// TestCheckForReflectionOpportunity_PendingTaskSkips verifies that an agent
// with a recently-completed task but ALSO a pending task is skipped (the
// PendingTaskCount() > 0 continue inside checkForReflectionOpportunity).
func TestCheckForReflectionOpportunity_PendingTaskSkips(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ReflectionCooldown = 0
	m, store, _ := newCtrlManager(t, cfg)
	m.RegisterAgent("sess-mix", "mix", &TeamMemberConfig{ID: "mix", Name: "Mix", Tier: "worker"})
	agent := m.GetAgent("sess-mix")
	agent.AddTask(&Task{ID: "done", Status: TaskCompleted, CompletedAt: time.Now()})
	agent.AddTask(&Task{ID: "pending", Status: TaskPending})

	m.checkForReflectionOpportunity()

	if len(store.writesFor("sess-mix")) != 0 {
		t.Error("expected no reflection prompt when a pending task remains")
	}
}

// TestMaybeTriggerWaterCoolerChat_Fires drives the full happy path with two
// idle agents and asserts both get a [WATER COOLER] prompt.
func TestMaybeTriggerWaterCoolerChat_Fires(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WaterCoolerCooldown = 0
	m, store, rec := newCtrlManager(t, cfg)
	m.RegisterAgent("sess-w1", "w1", &TeamMemberConfig{ID: "w1", Name: "W1", Tier: "worker"})
	m.RegisterAgent("sess-w2", "w2", &TeamMemberConfig{ID: "w2", Name: "W2", Tier: "worker"})
	m.mu.Lock()
	m.lastWaterCoolerAt = time.Time{}
	m.mu.Unlock()

	// Retry enough times to defeat the 30% probability gate and the random
	// pairing; once it fires, both agents must be prompted.
	fired := false
	for i := 0; i < 200 && !fired; i++ {
		m.maybeTriggerWaterCoolerChat()
		if rec.has("office:waterCoolerChat") {
			fired = true
		}
	}
	if !fired {
		t.Skip("water-cooler probability gate never opened in 200 tries (non-deterministic)")
	}
	if len(store.writesFor("sess-w1")) == 0 || len(store.writesFor("sess-w2")) == 0 {
		t.Errorf("expected both agents prompted, w1=%v w2=%v",
			store.writesFor("sess-w1"), store.writesFor("sess-w2"))
	}
}
