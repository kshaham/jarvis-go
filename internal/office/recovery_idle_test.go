package office

import (
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// CheckForIdleAgents
// ---------------------------------------------------------------------------

// idleTestConfig returns a config with cooldowns relaxed so idle/replenish
// paths fire deterministically in a single tick.
func idleTestConfig() *Config {
	cfg := DefaultConfig()
	cfg.ReplenishCooldown = 0
	cfg.BrainstormCooldown = 0
	cfg.WaterCoolerCooldown = 0
	return cfg
}

// TestCheckForIdleAgents_GoalModeNoop verifies that in goal mode (non-
// perpetual) idle agents are NOT force-replenished.
func TestCheckForIdleAgents_GoalModeNoop(t *testing.T) {
	m, store, _ := newCtrlManager(t, idleTestConfig())
	m.SetPerpetualMode(false)

	m.RegisterAgent("sess-goal", "goal", &TeamMemberConfig{ID: "goal", Name: "Goalie", Role: "Backend", Tier: "worker"})

	m.CheckForIdleAgents()

	if len(store.writesFor("sess-goal")) != 0 {
		t.Error("expected no replenishment in goal (non-perpetual) mode")
	}
}

// TestCheckForIdleAgents_PausedNoop verifies a paused perpetual team gets
// no replenishment prompts.
func TestCheckForIdleAgents_PausedNoop(t *testing.T) {
	m, store, _ := newCtrlManager(t, idleTestConfig())
	m.SetPerpetualMode(true)
	m.SetPaused(true, "break")

	m.RegisterAgent("sess-pi", "pi", &TeamMemberConfig{ID: "pi", Name: "PausedIdle", Role: "Backend", Tier: "worker"})

	m.CheckForIdleAgents()
	if len(store.writesFor("sess-pi")) != 0 {
		t.Error("expected no replenishment while paused")
	}
}

// TestCheckForIdleAgents_WrappingUpNoop verifies wrap-up suppresses
// replenishment.
func TestCheckForIdleAgents_WrappingUpNoop(t *testing.T) {
	m, store, _ := newCtrlManager(t, idleTestConfig())
	m.SetPerpetualMode(true)
	m.SetWrappingUp(true)

	m.RegisterAgent("sess-wu", "wu", &TeamMemberConfig{ID: "wu", Name: "WrapIdle", Role: "Backend", Tier: "worker"})

	m.CheckForIdleAgents()
	if len(store.writesFor("sess-wu")) != 0 {
		t.Error("expected no replenishment during wrap-up")
	}
}

// TestCheckForIdleAgents_SelfReplenishWhenSolo verifies that an idle agent
// with no available brainstorm partner gets the self-replenishment prompt
// (Strategy 3).
func TestCheckForIdleAgents_SelfReplenishWhenSolo(t *testing.T) {
	m, store, rec := newCtrlManager(t, idleTestConfig())
	m.SetPerpetualMode(true)

	// Only one agent on the roster => no brainstorm partner => self-replenish.
	m.RegisterAgent("sess-solo", "solo", &TeamMemberConfig{ID: "solo", Name: "Solo", Role: "Backend", Tier: "worker"})

	m.CheckForIdleAgents()

	found := false
	for _, w := range store.writesFor("sess-solo") {
		if strings.Contains(w.Input, "[URGENT: FIND MORE WORK NOW]") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected self-replenishment prompt, got %v", store.writesFor("sess-solo"))
	}
	if !rec.has("office:agentReplenishment") {
		t.Error("expected office:agentReplenishment broadcast")
	}

	// LastReplenishmentAt must now be set so the next tick respects the cooldown.
	agent := m.GetAgent("sess-solo")
	agent.mu.Lock()
	lr := agent.LastReplenishmentAt
	agent.mu.Unlock()
	if lr.IsZero() {
		t.Error("expected LastReplenishmentAt to be set after replenishment")
	}
}

// TestCheckForIdleAgents_SkipsAgentWithEnoughWork verifies an agent that
// still has >1 unit of work queued is not replenished.
func TestCheckForIdleAgents_SkipsAgentWithEnoughWork(t *testing.T) {
	m, store, _ := newCtrlManager(t, idleTestConfig())
	m.SetPerpetualMode(true)

	m.RegisterAgent("sess-busy2", "busy2", &TeamMemberConfig{ID: "busy2", Name: "Busy2", Role: "Backend", Tier: "worker"})
	agent := m.GetAgent("sess-busy2")
	// 2 pending tasks => totalWork == 2 > 1 => skip.
	agent.AddTask(&Task{ID: "t1", Title: "one", Status: TaskPending})
	agent.AddTask(&Task{ID: "t2", Title: "two", Status: TaskPending})

	m.CheckForIdleAgents()
	if len(store.writesFor("sess-busy2")) != 0 {
		t.Error("expected no replenishment for an agent with queued work")
	}
}

// TestCheckForIdleAgents_RespectsCooldown verifies that an agent whose
// LastReplenishmentAt is within the cooldown is skipped.
func TestCheckForIdleAgents_RespectsCooldown(t *testing.T) {
	cfg := idleTestConfig()
	cfg.ReplenishCooldown = 10 * time.Minute
	m, store, _ := newCtrlManager(t, cfg)
	m.SetPerpetualMode(true)

	m.RegisterAgent("sess-cd", "cd", &TeamMemberConfig{ID: "cd", Name: "Cooldown", Role: "Backend", Tier: "worker"})
	agent := m.GetAgent("sess-cd")
	agent.mu.Lock()
	agent.LastReplenishmentAt = time.Now() // just replenished
	agent.mu.Unlock()

	m.CheckForIdleAgents()
	if len(store.writesFor("sess-cd")) != 0 {
		t.Error("expected no replenishment within cooldown")
	}
}

// TestCheckForIdleAgents_WorkingSkipped verifies a mid-turn agent is not
// replenished even if its queue is empty.
func TestCheckForIdleAgents_WorkingSkipped(t *testing.T) {
	m, store, _ := newCtrlManager(t, idleTestConfig())
	m.SetPerpetualMode(true)

	m.RegisterAgent("sess-w", "w", &TeamMemberConfig{ID: "w", Name: "Worker", Role: "Backend", Tier: "worker"})
	store.setWorking("sess-w", true)

	m.CheckForIdleAgents()
	if len(store.writesFor("sess-w")) != 0 {
		t.Error("expected no replenishment for a working agent")
	}
}

// ---------------------------------------------------------------------------
// findAvailableAgent
// ---------------------------------------------------------------------------

// TestFindAvailableAgent_PicksIdleTeammate verifies an idle non-asker
// teammate is returned.
func TestFindAvailableAgent_PicksIdleTeammate(t *testing.T) {
	m, _, _ := newCtrlManager(t, idleTestConfig())

	m.RegisterAgent("sess-asker", "asker", &TeamMemberConfig{ID: "asker", Name: "Asker", Role: "Frontend", Tier: "worker"})
	m.RegisterAgent("sess-peer", "peer", &TeamMemberConfig{ID: "peer", Name: "Peer", Role: "Backend", Tier: "worker"})

	got := m.findAvailableAgent("sess-asker")
	if got == nil {
		t.Fatal("expected an available teammate")
	}
	if got.SessionID() != "sess-peer" {
		t.Errorf("expected sess-peer, got %s", got.SessionID())
	}
}

// TestFindAvailableAgent_SkipsBusyAndSelf verifies the asker itself and any
// working teammate are excluded, returning nil when no one is eligible.
func TestFindAvailableAgent_SkipsBusyAndSelf(t *testing.T) {
	m, store, _ := newCtrlManager(t, idleTestConfig())

	m.RegisterAgent("sess-asker", "asker", &TeamMemberConfig{ID: "asker", Name: "Asker", Role: "Frontend", Tier: "worker"})
	m.RegisterAgent("sess-peer", "peer", &TeamMemberConfig{ID: "peer", Name: "Peer", Role: "Backend", Tier: "worker"})
	store.setWorking("sess-peer", true) // only peer, and it's busy

	if got := m.findAvailableAgent("sess-asker"); got != nil {
		t.Errorf("expected nil (only teammate is busy), got %s", got.SessionID())
	}
}

// TestFindAvailableAgent_SkipsAgentWithActiveTask verifies a teammate with
// an active task is not eligible.
func TestFindAvailableAgent_SkipsAgentWithActiveTask(t *testing.T) {
	m, _, _ := newCtrlManager(t, idleTestConfig())

	m.RegisterAgent("sess-asker", "asker", &TeamMemberConfig{ID: "asker", Name: "Asker", Role: "Frontend", Tier: "worker"})
	m.RegisterAgent("sess-peer", "peer", &TeamMemberConfig{ID: "peer", Name: "Peer", Role: "Backend", Tier: "worker"})
	peer := m.GetAgent("sess-peer")
	peer.AddTask(&Task{ID: "active-1", Title: "busy", Status: TaskInProgress})

	if got := m.findAvailableAgent("sess-asker"); got != nil {
		t.Errorf("expected nil (only teammate has active task), got %s", got.SessionID())
	}
}

// ---------------------------------------------------------------------------
// sendSelfReplenishmentPrompt
// ---------------------------------------------------------------------------

// TestSendSelfReplenishmentPrompt_IncludesMissionAndRole verifies the
// prompt threads the team mission and the agent's role, and updates
// LastPromptSentAt.
func TestSendSelfReplenishmentPrompt_IncludesMissionAndRole(t *testing.T) {
	m, store, rec := newCtrlManager(t, idleTestConfig())
	m.SetTeamMission("ship the dashboard")

	m.RegisterAgent("sess-sr", "sr", &TeamMemberConfig{ID: "sr", Name: "SelfRep", Role: "Platform Engineer", Tier: "worker"})
	agent := m.GetAgent("sess-sr")

	m.sendSelfReplenishmentPrompt(agent)

	writes := store.writesFor("sess-sr")
	if len(writes) == 0 {
		t.Fatal("expected a self-replenishment prompt")
	}
	prompt := writes[len(writes)-1].Input
	if !strings.Contains(prompt, "ship the dashboard") {
		t.Errorf("expected mission in prompt, got %q", prompt)
	}
	if !strings.Contains(prompt, "Platform Engineer") {
		t.Errorf("expected role in prompt, got %q", prompt)
	}
	if !rec.has("office:agentReplenishment") {
		t.Error("expected office:agentReplenishment broadcast")
	}
	agent.mu.Lock()
	lps := agent.LastPromptSentAt
	agent.mu.Unlock()
	if lps.IsZero() {
		t.Error("expected LastPromptSentAt set after self-replenish")
	}
}

// ---------------------------------------------------------------------------
// autoCompleteIdleAgentTasks
// ---------------------------------------------------------------------------

// TestAutoCompleteIdleAgentTasks_CompletesStaleAndStartsNext verifies a
// long-running in-progress task whose agent is fully idle gets
// auto-completed, and the next pending task is auto-started.
func TestAutoCompleteIdleAgentTasks_CompletesStaleAndStartsNext(t *testing.T) {
	m, store, rec := newCtrlManager(t, DefaultConfig())

	m.RegisterAgent("sess-ac", "ac", &TeamMemberConfig{ID: "ac", Name: "AutoComplete", Tier: "worker"})
	agent := m.GetAgent("sess-ac")

	// Active task started 10 min ago (past 5m threshold).
	agent.AddTask(&Task{
		ID:        "stale-active",
		Title:     "Long running",
		Status:    TaskInProgress,
		StartedAt: time.Now().Add(-10 * time.Minute),
	})
	// A pending task to auto-start next.
	agent.AddTask(&Task{ID: "next-pending", Title: "Up next", Status: TaskPending})

	// Make the agent look idle: old prompt, old turn, old API, and a stale
	// progress check (>2 min old).
	agent.mu.Lock()
	agent.LastPromptSentAt = time.Now().Add(-10 * time.Minute)
	agent.LastTurnAt = time.Now().Add(-10 * time.Minute)
	agent.LastAgentAPIAt = time.Now().Add(-10 * time.Minute)
	agent.ProgressChecks["stale-active"] = time.Now().Add(-5 * time.Minute)
	agent.mu.Unlock()

	m.autoCompleteIdleAgentTasks()

	// Auto-complete now routes through the objective gate asynchronously. With
	// no working directory configured the gate is Skipped, which preserves the
	// legacy "complete + auto-start next" behavior -- but it resolves on a
	// goroutine, so poll for it.
	completed := waitFor(2*time.Second, func() bool {
		for _, tk := range agent.GetTasks() {
			if tk.ID == "stale-active" && tk.Status == TaskCompleted {
				return true
			}
		}
		return false
	})
	if !completed {
		t.Fatalf("expected stale-active to be TaskCompleted (gate skipped), tasks=%+v", agent.GetTasks())
	}
	if !rec.has("office:taskUpdated") {
		t.Error("expected office:taskUpdated broadcast")
	}
	// The next pending task should have triggered an [AUTO-START] prompt.
	foundStart := waitFor(2*time.Second, func() bool {
		for _, w := range store.writesFor("sess-ac") {
			if strings.Contains(w.Input, "[AUTO-START]") && strings.Contains(w.Input, "next-pending") {
				return true
			}
		}
		return false
	})
	if !foundStart {
		t.Error("expected [AUTO-START] prompt for next pending task")
	}
}

// TestAutoCompleteIdleAgentTasks_SkipsWhenNoProgressCheck verifies the
// conservative branch: without a prior progress check, the task is NOT
// auto-completed even if it's old and the agent looks idle.
func TestAutoCompleteIdleAgentTasks_SkipsWhenNoProgressCheck(t *testing.T) {
	m, _, _ := newCtrlManager(t, DefaultConfig())

	m.RegisterAgent("sess-np", "np", &TeamMemberConfig{ID: "np", Name: "NoProgress", Tier: "worker"})
	agent := m.GetAgent("sess-np")
	agent.AddTask(&Task{
		ID:        "old-active",
		Title:     "old",
		Status:    TaskInProgress,
		StartedAt: time.Now().Add(-10 * time.Minute),
	})
	agent.mu.Lock()
	agent.LastPromptSentAt = time.Now().Add(-10 * time.Minute)
	agent.LastTurnAt = time.Now().Add(-10 * time.Minute)
	agent.LastAgentAPIAt = time.Now().Add(-10 * time.Minute)
	// No ProgressChecks entry => hasCheck == false => skip.
	agent.mu.Unlock()

	m.autoCompleteIdleAgentTasks()

	if got := agent.GetTasks()[0].Status; got != TaskInProgress {
		t.Errorf("expected task left in progress (no prior progress check), got %s", got)
	}
}

// TestAutoCompleteIdleAgentTasks_SkipsWorkingAgent verifies that a session
// the store reports as working is never auto-completed.
func TestAutoCompleteIdleAgentTasks_SkipsWorkingAgent(t *testing.T) {
	m, store, _ := newCtrlManager(t, DefaultConfig())

	m.RegisterAgent("sess-wk", "wk", &TeamMemberConfig{ID: "wk", Name: "Working", Tier: "worker"})
	agent := m.GetAgent("sess-wk")
	agent.AddTask(&Task{
		ID:        "active-wk",
		Title:     "in flight",
		Status:    TaskInProgress,
		StartedAt: time.Now().Add(-30 * time.Minute),
	})
	agent.mu.Lock()
	agent.ProgressChecks["active-wk"] = time.Now().Add(-10 * time.Minute)
	agent.mu.Unlock()
	store.setWorking("sess-wk", true)

	m.autoCompleteIdleAgentTasks()
	if got := agent.GetTasks()[0].Status; got != TaskInProgress {
		t.Errorf("expected working agent's task untouched, got %s", got)
	}
}

// ---------------------------------------------------------------------------
// checkStaleInProgressTasks
// ---------------------------------------------------------------------------

// TestCheckStaleInProgress_GentlePrompt verifies a 6-minute-old task with
// no prior check gets a [PROGRESS CHECK] gentle prompt and records the check.
func TestCheckStaleInProgress_GentlePrompt(t *testing.T) {
	m, store, _ := newCtrlManager(t, DefaultConfig())

	m.RegisterAgent("sess-gentle", "gentle", &TeamMemberConfig{ID: "gentle", Name: "Gentle", Tier: "worker"})
	agent := m.GetAgent("sess-gentle")
	agent.AddTask(&Task{
		ID:        "gentle-task",
		Title:     "slow work",
		Status:    TaskInProgress,
		StartedAt: time.Now().Add(-6 * time.Minute), // > 5m gentle, < 15m urgent
	})
	// Make sure recent-activity guards don't skip: no LastTurnAt/LastAPIAt.

	m.checkStaleInProgressTasks()

	found := false
	for _, w := range store.writesFor("sess-gentle") {
		if strings.Contains(w.Input, "[PROGRESS CHECK]") && strings.Contains(w.Input, "slow work") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected [PROGRESS CHECK] prompt, got %v", store.writesFor("sess-gentle"))
	}
	// ProgressChecks should now hold an entry for the task.
	agent.mu.Lock()
	_, has := agent.ProgressChecks["gentle-task"]
	agent.mu.Unlock()
	if !has {
		t.Error("expected ProgressChecks entry recorded after gentle prompt")
	}
}

// TestCheckStaleInProgress_UrgentPrompt verifies a >15m task whose last
// check was >5m ago escalates to an [URGENT] prompt.
func TestCheckStaleInProgress_UrgentPrompt(t *testing.T) {
	m, store, _ := newCtrlManager(t, DefaultConfig())

	m.RegisterAgent("sess-urg", "urg", &TeamMemberConfig{ID: "urg", Name: "Urgent", Tier: "worker"})
	agent := m.GetAgent("sess-urg")
	agent.AddTask(&Task{
		ID:        "urgent-task",
		Title:     "very slow",
		Status:    TaskInProgress,
		StartedAt: time.Now().Add(-20 * time.Minute), // > 15m urgent
	})
	agent.mu.Lock()
	agent.ProgressChecks["urgent-task"] = time.Now().Add(-6 * time.Minute) // last check >5m ago
	agent.mu.Unlock()

	m.checkStaleInProgressTasks()

	found := false
	for _, w := range store.writesFor("sess-urg") {
		if strings.Contains(w.Input, "[URGENT]") && strings.Contains(w.Input, "very slow") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected [URGENT] prompt, got %v", store.writesFor("sess-urg"))
	}
}

// TestCheckStaleInProgress_SkipsRecentApi verifies a recent API call from
// the agent suppresses the progress check (they're clearly still engaged).
func TestCheckStaleInProgress_SkipsRecentApi(t *testing.T) {
	m, store, _ := newCtrlManager(t, DefaultConfig())

	m.RegisterAgent("sess-eng", "eng", &TeamMemberConfig{ID: "eng", Name: "Engaged", Tier: "worker"})
	agent := m.GetAgent("sess-eng")
	agent.AddTask(&Task{
		ID:        "eng-task",
		Title:     "active work",
		Status:    TaskInProgress,
		StartedAt: time.Now().Add(-10 * time.Minute),
	})
	agent.mu.Lock()
	agent.LastAgentAPIAt = time.Now() // just made an API call
	agent.mu.Unlock()

	m.checkStaleInProgressTasks()
	for _, w := range store.writesFor("sess-eng") {
		if strings.Contains(w.Input, "PROGRESS CHECK") || strings.Contains(w.Input, "URGENT") {
			t.Errorf("expected no progress check for a recently-active agent, got %q", w.Input)
		}
	}
}

// ---------------------------------------------------------------------------
// checkForWedgedWorkingAgents
// ---------------------------------------------------------------------------

// TestCheckForWedgedWorkingAgents_StopsWedged verifies a session stuck in
// "working" beyond WedgedWorkingTimeout is stopped and broadcast.
func TestCheckForWedgedWorkingAgents_StopsWedged(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WedgedWorkingTimeout = 10 * time.Millisecond
	cfg.StuckGracePeriod = 0
	m, store, rec := newCtrlManager(t, cfg)

	m.RegisterAgent("sess-wedge", "wedge", &TeamMemberConfig{ID: "wedge", Name: "Wedged", Tier: "worker"})
	agent := m.GetAgent("sess-wedge")
	agent.mu.Lock()
	agent.LastPromptSentAt = time.Now().Add(-1 * time.Hour) // ancient activity
	agent.mu.Unlock()
	store.setWorking("sess-wedge", true)

	m.checkForWedgedWorkingAgents()

	stopped := store.stoppedSessions()
	foundStop := false
	for _, s := range stopped {
		if s == "sess-wedge" {
			foundStop = true
		}
	}
	if !foundStop {
		t.Errorf("expected wedged session stopped, stopped=%v", stopped)
	}
	if !rec.has("office:agentIdleStopped") {
		t.Error("expected office:agentIdleStopped broadcast")
	}
	if m.GetAgent("sess-wedge") != nil {
		t.Error("expected wedged agent unregistered")
	}
}

// TestCheckForWedgedWorkingAgents_DisabledWhenTimeoutZero verifies that a
// zero WedgedWorkingTimeout disables the sweep entirely.
func TestCheckForWedgedWorkingAgents_DisabledWhenTimeoutZero(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WedgedWorkingTimeout = 0
	m, store, _ := newCtrlManager(t, cfg)

	m.RegisterAgent("sess-nz", "nz", &TeamMemberConfig{ID: "nz", Name: "NoZero", Tier: "worker"})
	store.setWorking("sess-nz", true)

	m.checkForWedgedWorkingAgents()
	if len(store.stoppedSessions()) != 0 {
		t.Error("expected no stops when WedgedWorkingTimeout is 0")
	}
}

// TestCheckForWedgedWorkingAgents_NonWorkingSkipped verifies an agent that
// does NOT report working is left alone (covered by checkForStuckAgents).
func TestCheckForWedgedWorkingAgents_NonWorkingSkipped(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WedgedWorkingTimeout = 10 * time.Millisecond
	cfg.StuckGracePeriod = 0
	m, store, _ := newCtrlManager(t, cfg)

	m.RegisterAgent("sess-idle", "idle", &TeamMemberConfig{ID: "idle", Name: "Idle", Tier: "worker"})
	agent := m.GetAgent("sess-idle")
	agent.mu.Lock()
	agent.LastPromptSentAt = time.Now().Add(-1 * time.Hour)
	agent.mu.Unlock()
	// not working

	m.checkForWedgedWorkingAgents()
	if len(store.stoppedSessions()) != 0 {
		t.Error("expected non-working agent untouched by wedge sweep")
	}
}
