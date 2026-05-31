package office

import (
	"testing"
	"time"
)

// TestAutoCompleteIdleAgentTasks_RecentAPISkips drives the recent-API skip
// branch: a stale in-progress task whose agent made an API call within the
// last 2 minutes is NOT auto-completed (the agent is clearly still engaged).
func TestAutoCompleteIdleAgentTasks_RecentAPISkips(t *testing.T) {
	m, _, _ := newCtrlManager(t, DefaultConfig())

	m.RegisterAgent("sess-recent", "recent", &TeamMemberConfig{ID: "recent", Name: "Recent", Tier: "worker"})
	agent := m.GetAgent("sess-recent")
	agent.AddTask(&Task{
		ID:        "stale-but-active",
		Title:     "old task",
		Status:    TaskInProgress,
		StartedAt: time.Now().Add(-30 * time.Minute), // well past threshold
	})
	agent.mu.Lock()
	agent.LastPromptSentAt = time.Now().Add(-30 * time.Minute)
	agent.LastTurnAt = time.Now().Add(-30 * time.Minute)
	// Recent API call -> skip via the "now.Sub(lastAPI) < 2m" branch.
	agent.LastAgentAPIAt = time.Now()
	agent.ProgressChecks["stale-but-active"] = time.Now().Add(-10 * time.Minute)
	agent.mu.Unlock()

	m.autoCompleteIdleAgentTasks()

	if got := agent.GetTasks()[0].Status; got != TaskInProgress {
		t.Errorf("expected task left in progress due to recent API activity, got %s", got)
	}
}

// TestCheckForIdleAgents_NilConfigSkipped verifies an agent with a nil Config
// is skipped in the idle-replenishment loop (the agent.Config == nil continue).
func TestCheckForIdleAgents_NilConfigSkipped(t *testing.T) {
	m, store, _ := newCtrlManager(t, idleTestConfig())
	m.SetPerpetualMode(true)

	m.RegisterAgent("sess-nilcfg", "nilcfg", &TeamMemberConfig{ID: "nilcfg", Name: "NilCfg", Role: "Backend", Tier: "worker"})
	agent := m.GetAgent("sess-nilcfg")
	agent.mu.Lock()
	agent.Config = nil
	agent.mu.Unlock()

	m.CheckForIdleAgents()

	if len(store.writesFor("sess-nilcfg")) != 0 {
		t.Error("expected no replenishment for a nil-config agent")
	}
}

// TestCheckForIdleAgents_ActiveTaskCountsTowardWork verifies the totalWork++
// branch: an agent with one active task plus one pending task has totalWork==2
// (> 1) and is therefore skipped (not replenished).
func TestCheckForIdleAgents_ActiveTaskCountsTowardWork(t *testing.T) {
	m, store, _ := newCtrlManager(t, idleTestConfig())
	m.SetPerpetualMode(true)

	m.RegisterAgent("sess-mix", "mix", &TeamMemberConfig{ID: "mix", Name: "Mix", Role: "Backend", Tier: "worker"})
	agent := m.GetAgent("sess-mix")
	agent.AddTask(&Task{ID: "active-1", Title: "in flight", Status: TaskInProgress})
	agent.AddTask(&Task{ID: "pending-1", Title: "queued", Status: TaskPending})

	m.CheckForIdleAgents()

	if len(store.writesFor("sess-mix")) != 0 {
		t.Error("expected no replenishment: active + pending makes totalWork > 1")
	}
}

// TestCheckForIdleAgents_InActiveMeetingSkipped verifies an idle agent already
// in an active meeting is not given a replenishment/brainstorm prompt.
func TestCheckForIdleAgents_InActiveMeetingSkipped(t *testing.T) {
	m, store, _ := newCtrlManager(t, idleTestConfig())
	m.SetPerpetualMode(true)

	m.RegisterAgent("sess-inmtg", "inmtg", &TeamMemberConfig{ID: "inmtg", Name: "InMtg", Role: "Backend", Tier: "worker"})
	m.mu.Lock()
	m.meetings["mtg-1"] = &Meeting{ID: "mtg-1", Type: "brainstorm", Active: true, Participants: []string{"sess-inmtg"}}
	m.mu.Unlock()

	m.CheckForIdleAgents()

	if len(store.writesFor("sess-inmtg")) != 0 {
		t.Error("expected no replenishment for an agent already in a meeting")
	}
}
