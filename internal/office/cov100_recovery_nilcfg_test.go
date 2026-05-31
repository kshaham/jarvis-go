package office

import (
	"testing"
	"time"
)

// These tests drive the `agent.Config == nil` (and `agent == nil ||
// agent.Config == nil`) continue-guards inside the roster-iteration
// loops in recovery.go. Each loop takes a locked snapshot of the
// roster but reads agent.Config OUTSIDE the lock, so an agent whose
// Config was nulled after registration exercises the guard. We assert
// the guard fires by proving NO per-agent side effect (prompt write /
// task mutation / broadcast) happened for that session.

// nilCfgAgent registers an agent and then nulls its Config so the
// roster-loop guard fires. Returns the manager's agent pointer.
func nilCfgAgent(m *Manager, sid, configID string) *AgentState {
	m.RegisterAgent(sid, configID, &TeamMemberConfig{
		ID: configID, Name: "Temp", Role: "Backend", Tier: "worker",
	})
	a := m.GetAgent(sid)
	// Null the config under the agent lock so the read in the loop body
	// sees nil. The loops re-read agent.Config (a plain field) without
	// the manager lock, so this is the realistic mid-flight scenario.
	a.mu.Lock()
	a.Config = nil
	a.mu.Unlock()
	return a
}

func TestCheckUnacknowledgedTasks_NilConfigSkipped(t *testing.T) {
	m, store, rec := newCtrlManager(t, DefaultConfig())
	a := nilCfgAgent(m, "sess-unack", "unack")
	// Give it an overdue pending task so, absent the nil guard, it would
	// be nudged. The guard at the top of the loop must short-circuit first.
	a.AddTask(&Task{ID: "t1", Title: "x", Status: TaskPending, AssignedAt: time.Now().Add(-time.Hour)})

	m.checkUnacknowledgedTasks()

	if len(store.writesFor("sess-unack")) != 0 {
		t.Errorf("nil-config agent should be skipped, got writes: %v", store.writesFor("sess-unack"))
	}
	if rec.has("office:taskUpdated") {
		t.Error("nil-config agent should not have produced a taskUpdated broadcast")
	}
	// The task must remain pending (never auto-failed).
	tasks := a.GetTasks()
	if len(tasks) != 1 || tasks[0].Status != TaskPending {
		t.Errorf("task should remain pending, got %+v", tasks)
	}
}

func TestAutoCompleteIdleAgentTasks_NilConfigSkipped(t *testing.T) {
	m, store, rec := newCtrlManager(t, DefaultConfig())
	a := nilCfgAgent(m, "sess-auto", "auto")
	// In-progress task old enough to auto-complete absent the guard.
	a.AddTask(&Task{
		ID: "t1", Title: "x", Status: TaskInProgress,
		StartedAt: time.Now().Add(-time.Hour),
	})

	m.autoCompleteIdleAgentTasks()

	if len(store.writesFor("sess-auto")) != 0 {
		t.Errorf("nil-config agent should be skipped, got writes: %v", store.writesFor("sess-auto"))
	}
	if rec.has("office:taskUpdated") {
		t.Error("nil-config agent should not have produced a taskUpdated broadcast")
	}
	tasks := a.GetTasks()
	if len(tasks) != 1 || tasks[0].Status != TaskInProgress {
		t.Errorf("task should remain in_progress, got %+v", tasks)
	}
}

func TestCheckStaleInProgressTasks_NilConfigSkipped(t *testing.T) {
	m, store, _ := newCtrlManager(t, DefaultConfig())
	a := nilCfgAgent(m, "sess-stale", "stale")
	a.AddTask(&Task{
		ID: "t1", Title: "x", Status: TaskInProgress,
		StartedAt: time.Now().Add(-time.Hour),
	})

	m.checkStaleInProgressTasks()

	if len(store.writesFor("sess-stale")) != 0 {
		t.Errorf("nil-config agent should be skipped, got writes: %v", store.writesFor("sess-stale"))
	}
}

func TestCheckForStuckAgents_NilConfigSkipped(t *testing.T) {
	cfg := DefaultConfig()
	cfg.StuckGracePeriod = 0
	cfg.StuckThreshold = time.Millisecond
	m, store, _ := newCtrlManager(t, cfg)
	a := nilCfgAgent(m, "sess-stuck", "stuck")
	// Make it look long-inactive so, absent the guard, recovery would fire.
	a.mu.Lock()
	a.LastPromptSentAt = time.Now().Add(-time.Hour)
	a.mu.Unlock()

	m.checkForStuckAgents()

	if len(store.writesFor("sess-stuck")) != 0 {
		t.Errorf("nil-config agent should be skipped, got writes: %v", store.writesFor("sess-stuck"))
	}
	if store.IsSessionWorking("sess-stuck") {
		t.Error("sanity: session should not be working")
	}
}

func TestCheckForWedgedWorkingAgents_NilConfigSkipped(t *testing.T) {
	cfg := DefaultConfig()
	cfg.StuckGracePeriod = 0
	cfg.WedgedWorkingTimeout = time.Millisecond
	m, store, _ := newCtrlManager(t, cfg)
	a := nilCfgAgent(m, "sess-wedged", "wedged")
	// Report working + long-idle so the wedge path would fire absent guard.
	store.setWorking("sess-wedged", true)
	a.mu.Lock()
	a.LastTurnAt = time.Now().Add(-time.Hour)
	a.LastPromptSentAt = time.Now().Add(-time.Hour)
	a.mu.Unlock()

	m.checkForWedgedWorkingAgents()

	if len(store.stoppedSessions()) != 0 {
		t.Errorf("nil-config wedged agent should not be stopped, got %v", store.stoppedSessions())
	}
	// Agent must still be registered (not unregistered by the wedge path).
	if m.GetAgent("sess-wedged") == nil {
		t.Error("nil-config agent should not have been unregistered")
	}
}

func TestCheckForCuriousAgents_NilConfigSkipped(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CuriosityCooldown = 0
	m, store, rec := newCtrlManager(t, cfg)
	m.SetCuriosityEnabled(true)
	nilCfgAgent(m, "sess-curious", "curious")
	// No tasks => idle => would normally get a curiosity prompt.

	m.checkForCuriousAgents()

	if len(store.writesFor("sess-curious")) != 0 {
		t.Errorf("nil-config agent should be skipped, got writes: %v", store.writesFor("sess-curious"))
	}
	if rec.has("office:curiosityTriggered") {
		t.Error("nil-config agent should not trigger curiosity")
	}
}

func TestCheckForReflectionOpportunity_NilConfigSkipped(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ReflectionCooldown = 0
	m, store, rec := newCtrlManager(t, cfg)
	a := nilCfgAgent(m, "sess-reflect", "reflect")
	// A recently-completed task would normally trigger reflection.
	a.AddTask(&Task{
		ID: "t1", Title: "x", Status: TaskCompleted,
		CompletedAt: time.Now(),
	})

	m.checkForReflectionOpportunity()

	if len(store.writesFor("sess-reflect")) != 0 {
		t.Errorf("nil-config agent should be skipped, got writes: %v", store.writesFor("sess-reflect"))
	}
	if rec.has("office:reflectionTriggered") {
		t.Error("nil-config agent should not trigger reflection")
	}
}
