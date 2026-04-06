package office

import (
	"strings"
	"testing"
	"time"
)

// healthMockSessionStore extends recordingSessionStore with a configurable
// IsSessionAlive implementation for testing checkSessionHealth.
type healthMockSessionStore struct {
	recordingSessionStore
	alive map[string]bool
}

func (h *healthMockSessionStore) IsSessionAlive(id string) bool {
	return h.alive[id]
}

func newHealthManager(aliveMap map[string]bool) (*Manager, *healthMockSessionStore) {
	store := &healthMockSessionStore{alive: aliveMap}
	cfg := DefaultConfig()
	cfg.TaskAckTimeout = 50 * time.Millisecond
	cfg.TaskAckMaxNudges = 2
	cfg.CuriosityCooldown = 0
	cfg.ReflectionCooldown = 0
	cfg.BlockedTriageMaxAttempts = 2
	m := NewManager(store, func(event string, data interface{}) {}, cfg)
	return m, store
}

// ---------------------------------------------------------------------------
// checkSessionHealth tests
// ---------------------------------------------------------------------------

// TestCheckSessionHealth_RemovesDead verifies that agents whose sessions are
// not alive are cleaned up via HandleAgentDeath.
func TestCheckSessionHealth_RemovesDead(t *testing.T) {
	aliveMap := map[string]bool{
		"sess-alive": true,
		// sess-dead intentionally absent (returns false from IsSessionAlive)
	}
	m, _ := newHealthManager(aliveMap)

	cfg1 := &TeamMemberConfig{ID: "alive-cfg", Name: "AliveAgent", Tier: "worker"}
	cfg2 := &TeamMemberConfig{ID: "dead-cfg", Name: "DeadAgent", Tier: "worker"}
	m.RegisterAgent("sess-alive", "alive-cfg", cfg1)
	m.RegisterAgent("sess-dead", "dead-cfg", cfg2)

	m.checkSessionHealth()

	if m.GetAgent("sess-dead") != nil {
		t.Error("expected dead agent to be removed from roster")
	}
	if m.GetAgent("sess-alive") == nil {
		t.Error("expected alive agent to remain on roster")
	}
}

// TestCheckSessionHealth_KeepsAlive verifies that agents with live sessions
// are not removed.
func TestCheckSessionHealth_KeepsAlive(t *testing.T) {
	aliveMap := map[string]bool{
		"sess-1": true,
		"sess-2": true,
	}
	m, _ := newHealthManager(aliveMap)

	cfg1 := &TeamMemberConfig{ID: "a1", Name: "Agent1", Tier: "worker"}
	cfg2 := &TeamMemberConfig{ID: "a2", Name: "Agent2", Tier: "worker"}
	m.RegisterAgent("sess-1", "a1", cfg1)
	m.RegisterAgent("sess-2", "a2", cfg2)

	m.checkSessionHealth()

	if m.AgentCount() != 2 {
		t.Errorf("expected 2 agents, got %d", m.AgentCount())
	}
}

// ---------------------------------------------------------------------------
// checkUnacknowledgedTasks tests
// ---------------------------------------------------------------------------

// TestCheckUnacknowledgedTasks_Nudges verifies that a pending task past the
// ack timeout receives a nudge prompt via WriteInkPrompt.
func TestCheckUnacknowledgedTasks_Nudges(t *testing.T) {
	m, store := newOrcTestManager()
	m.config.TaskAckTimeout = 10 * time.Millisecond
	m.config.TaskAckMaxNudges = 3

	cfg := &TeamMemberConfig{ID: "nudge-agent", Name: "NudgeAgent", Tier: "worker"}
	m.RegisterAgent("sess-nudge", "nudge-agent", cfg)

	agent := m.GetAgent("sess-nudge")
	agent.AddTask(&Task{
		ID:         "task-ack-1",
		Title:      "Pending task",
		Status:     TaskPending,
		AssignedAt: time.Now().Add(-1 * time.Second), // 1s ago, past timeout
	})

	m.checkUnacknowledgedTasks()

	writes := store.writesForSession("sess-nudge")
	if len(writes) == 0 {
		t.Fatal("expected nudge prompt to be sent")
	}
	found := false
	for _, w := range writes {
		if strings.Contains(w.Input, "[TASK REMINDER]") && strings.Contains(w.Input, "Pending task") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected [TASK REMINDER] nudge for overdue task")
	}
}

// TestCheckUnacknowledgedTasks_AutoFails verifies that after TaskAckMaxNudges
// nudges the task is auto-failed and no further nudge is sent.
func TestCheckUnacknowledgedTasks_AutoFails(t *testing.T) {
	m, _ := newOrcTestManager()
	m.config.TaskAckTimeout = 10 * time.Millisecond
	m.config.TaskAckMaxNudges = 2

	cfg := &TeamMemberConfig{ID: "fail-agent", Name: "FailAgent", Tier: "worker"}
	m.RegisterAgent("sess-fail", "fail-agent", cfg)

	agent := m.GetAgent("sess-fail")
	agent.AddTask(&Task{
		ID:         "task-fail-1",
		Title:      "Task to auto fail",
		Status:     TaskPending,
		AssignedAt: time.Now().Add(-1 * time.Second),
	})

	// Pre-fill nudge count to the max so next call auto-fails.
	agent.mu.Lock()
	agent.AckNudges["task-fail-1"] = &AckNudgeState{Count: m.config.TaskAckMaxNudges, LastNudgeAt: time.Now()}
	agent.mu.Unlock()

	m.checkUnacknowledgedTasks()

	tasks := agent.GetTasks()
	if len(tasks) == 0 {
		t.Fatal("expected task to still be in queue (just failed)")
	}
	if tasks[0].Status != TaskFailed {
		t.Errorf("expected task status Failed, got %q", tasks[0].Status)
	}
}

// ---------------------------------------------------------------------------
// checkForReflectionOpportunity tests
// ---------------------------------------------------------------------------

// TestCheckForReflectionOpportunity_Fires verifies that a reflection prompt
// is sent to an agent that recently completed all tasks and has not yet reflected.
func TestCheckForReflectionOpportunity_Fires(t *testing.T) {
	m, store := newOrcTestManager()
	m.config.ReflectionCooldown = 0

	cfg := &TeamMemberConfig{ID: "reflect-agent", Name: "ReflectAgent", Tier: "worker"}
	m.RegisterAgent("sess-reflect", "reflect-agent", cfg)

	agent := m.GetAgent("sess-reflect")
	// Add a recently completed task (within 2 min).
	agent.AddTask(&Task{
		ID:          "task-done-1",
		Title:       "Finished task",
		Status:      TaskCompleted,
		AssignedAt:  time.Now().Add(-5 * time.Minute),
		CompletedAt: time.Now().Add(-30 * time.Second),
	})
	// Ensure no active or pending tasks.

	m.checkForReflectionOpportunity()

	writes := store.writesForSession("sess-reflect")
	found := false
	for _, w := range writes {
		if strings.Contains(w.Input, "[REFLECTION]") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected [REFLECTION] prompt to be sent")
	}

	// LastReflectionAt should be set.
	agent.mu.Lock()
	lastRefl := agent.LastReflectionAt
	agent.mu.Unlock()
	if lastRefl.IsZero() {
		t.Error("expected LastReflectionAt to be set after reflection prompt")
	}
}

// TestCheckForReflectionOpportunity_Cooldown verifies that an agent that
// recently reflected is skipped.
func TestCheckForReflectionOpportunity_Cooldown(t *testing.T) {
	m, store := newOrcTestManager()
	m.config.ReflectionCooldown = 10 * time.Minute

	cfg := &TeamMemberConfig{ID: "cooldown-reflect", Name: "CooldownReflect", Tier: "worker"}
	m.RegisterAgent("sess-cooldown-r", "cooldown-reflect", cfg)

	agent := m.GetAgent("sess-cooldown-r")
	agent.AddTask(&Task{
		ID:          "task-done-2",
		Title:       "Recently reflected task",
		Status:      TaskCompleted,
		AssignedAt:  time.Now().Add(-5 * time.Minute),
		CompletedAt: time.Now().Add(-30 * time.Second),
	})

	// Set LastReflectionAt to now (within cooldown).
	agent.mu.Lock()
	agent.LastReflectionAt = time.Now()
	agent.mu.Unlock()

	m.checkForReflectionOpportunity()

	writes := store.writesForSession("sess-cooldown-r")
	for _, w := range writes {
		if strings.Contains(w.Input, "[REFLECTION]") {
			t.Error("expected NO reflection prompt during cooldown")
		}
	}
}

// ---------------------------------------------------------------------------
// checkForCuriousAgents tests
// ---------------------------------------------------------------------------

// TestCheckForCuriousAgents_Sends verifies that an idle agent with no tasks
// and no recent curiosity receives a curiosity prompt.
func TestCheckForCuriousAgents_Sends(t *testing.T) {
	m, store := newOrcTestManager()
	m.config.CuriosityCooldown = 0
	m.curiosityEnabled = true

	cfg := &TeamMemberConfig{ID: "curious-agent", Name: "CuriousAgent", Tier: "worker"}
	m.RegisterAgent("sess-curious", "curious-agent", cfg)

	// Agent has no tasks and no LastCuriosityAt.
	m.checkForCuriousAgents()

	writes := store.writesForSession("sess-curious")
	found := false
	for _, w := range writes {
		if strings.Contains(w.Input, "[CURIOSITY]") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected [CURIOSITY] prompt to be sent to idle agent")
	}
}

// TestCheckForCuriousAgents_RespectsDisabled verifies no curiosity prompt is
// sent when curiosity mode is disabled.
func TestCheckForCuriousAgents_RespectsDisabled(t *testing.T) {
	m, store := newOrcTestManager()
	m.config.CuriosityCooldown = 0
	m.curiosityEnabled = false

	cfg := &TeamMemberConfig{ID: "no-curiosity", Name: "NoCuriosity", Tier: "worker"}
	m.RegisterAgent("sess-nocuriosity", "no-curiosity", cfg)

	m.checkForCuriousAgents()

	writes := store.writesForSession("sess-nocuriosity")
	for _, w := range writes {
		if strings.Contains(w.Input, "[CURIOSITY]") {
			t.Error("expected no [CURIOSITY] prompt when curiosity is disabled")
		}
	}
}

// ---------------------------------------------------------------------------
// checkBlockedTasksForLeaderTriage tests
// ---------------------------------------------------------------------------

// TestCheckBlockedTasksForLeaderTriage_Prompts verifies that a blocked task
// triggers a triage prompt to the strategic-tier leader.
func TestCheckBlockedTasksForLeaderTriage_Prompts(t *testing.T) {
	m, store := newOrcTestManager()
	m.config.BlockedTriageMaxAttempts = 3

	workerCfg := &TeamMemberConfig{ID: "worker-triage", Name: "WorkerAgent", Tier: "worker"}
	leaderCfg := &TeamMemberConfig{ID: "leader-triage", Name: "LeaderAgent", Tier: "strategic"}
	m.RegisterAgent("sess-worker-triage", "worker-triage", workerCfg)
	m.RegisterAgent("sess-leader-triage", "leader-triage", leaderCfg)

	worker := m.GetAgent("sess-worker-triage")
	worker.AddTask(&Task{
		ID:          "blocked-task-1",
		Title:       "Blocked Feature",
		Description: "Cannot proceed without API key",
		Status:      TaskBlocked,
		AssignedAt:  time.Now().Add(-10 * time.Minute),
	})

	m.checkBlockedTasksForLeaderTriage()

	writes := store.writesForSession("sess-leader-triage")
	found := false
	for _, w := range writes {
		if strings.Contains(w.Input, "[BLOCKED TASK TRIAGE]") && strings.Contains(w.Input, "Blocked Feature") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected [BLOCKED TASK TRIAGE] prompt sent to leader")
	}
}

// ---------------------------------------------------------------------------
// HandleAgentDeath tests
// ---------------------------------------------------------------------------

// TestHandleAgentDeath_ReassignsTasks verifies that pending tasks from a dead
// agent are assigned to the strategic-tier agent.
func TestHandleAgentDeath_ReassignsTasks(t *testing.T) {
	m, _ := newOrcTestManager()

	workerCfg := &TeamMemberConfig{ID: "dying-worker", Name: "DyingWorker", Tier: "worker"}
	leaderCfg := &TeamMemberConfig{ID: "leader-recv", Name: "LeaderRecv", Tier: "strategic"}
	m.RegisterAgent("sess-dying", "dying-worker", workerCfg)
	m.RegisterAgent("sess-leader", "leader-recv", leaderCfg)

	dying := m.GetAgent("sess-dying")
	dying.AddTask(&Task{
		ID:     "pending-task-1",
		Title:  "Urgent pending task",
		Status: TaskPending,
	})
	dying.AddTask(&Task{
		ID:     "inprogress-task-1",
		Title:  "Active task (should not be reassigned)",
		Status: TaskInProgress,
	})

	m.HandleAgentDeath("sess-dying")

	// Dying agent should be removed.
	if m.GetAgent("sess-dying") != nil {
		t.Error("expected dying agent to be removed from roster")
	}

	// Leader should have received the pending task.
	leader := m.GetAgent("sess-leader")
	if leader == nil {
		t.Fatal("expected leader to still be registered")
	}
	leaderTasks := leader.GetTasks()
	found := false
	for _, task := range leaderTasks {
		if task.ID == "pending-task-1" {
			found = true
			if task.Source != "reassigned" {
				t.Errorf("expected task source 'reassigned', got %q", task.Source)
			}
			break
		}
	}
	if !found {
		t.Error("expected pending task to be reassigned to leader")
	}
}

// TestHandleAgentDeath_Unregisters verifies that HandleAgentDeath removes the
// agent from the roster even when there are no pending tasks.
func TestHandleAgentDeath_Unregisters(t *testing.T) {
	m, _ := newOrcTestManager()

	cfg := &TeamMemberConfig{ID: "gone-agent", Name: "GoneAgent", Tier: "worker"}
	m.RegisterAgent("sess-gone", "gone-agent", cfg)

	if m.AgentCount() != 1 {
		t.Fatalf("expected 1 agent before death, got %d", m.AgentCount())
	}

	m.HandleAgentDeath("sess-gone")

	if m.AgentCount() != 0 {
		t.Errorf("expected 0 agents after death, got %d", m.AgentCount())
	}
	if m.GetAgentByConfig("gone-agent") != nil {
		t.Error("expected agent config index to be cleared after death")
	}
}
