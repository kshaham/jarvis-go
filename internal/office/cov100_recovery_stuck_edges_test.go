package office

import (
	"testing"
	"time"
)

// TestCheckForStuckAgents_LastTurnAndAPIAdvanceActivity exercises the
// lastActive update branches inside checkForStuckAgents: LastTurnAt advancing
// past LastPromptSentAt, then LastAgentAPIAt advancing past that. All three
// are still older than StuckThreshold so a Tier1 recovery still fires.
func TestCheckForStuckAgents_LastTurnAndAPIAdvanceActivity(t *testing.T) {
	cfg := stuckTestConfig()
	m, store, rec := newCtrlManager(t, cfg)

	m.RegisterAgent("sess-adv", "adv", &TeamMemberConfig{ID: "adv", Name: "Advancer", Tier: "worker"})
	agent := m.GetAgent("sess-adv")

	base := time.Now().Add(-1 * time.Second)
	agent.mu.Lock()
	agent.LastPromptSentAt = base
	agent.LastTurnAt = base.Add(100 * time.Millisecond) // newer than prompt
	agent.LastAgentAPIAt = base.Add(200 * time.Millisecond) // newest, still past threshold
	agent.mu.Unlock()

	m.checkForStuckAgents()

	if got := store.screenInputs("sess-adv"); len(got) != 1 || got[0] != "\r" {
		t.Fatalf("expected single Tier1 Enter after activity-advance branches, got %v", got)
	}
	if !rec.has("office:agentRecovery") {
		t.Error("expected recovery broadcast")
	}
}

// TestCheckForStuckAgents_ZeroActivitySkips verifies an agent with no activity
// timestamps at all (lastActive.IsZero()) is skipped -- we cannot judge it
// stuck without any baseline.
func TestCheckForStuckAgents_ZeroActivitySkips(t *testing.T) {
	cfg := stuckTestConfig()
	m, store, rec := newCtrlManager(t, cfg)

	m.RegisterAgent("sess-zero", "zero", &TeamMemberConfig{ID: "zero", Name: "Zero", Tier: "worker"})
	agent := m.GetAgent("sess-zero")
	// RegisterAgent stamps LastPromptSentAt=now; clear all three.
	agent.mu.Lock()
	agent.LastPromptSentAt = time.Time{}
	agent.LastTurnAt = time.Time{}
	agent.LastAgentAPIAt = time.Time{}
	agent.mu.Unlock()

	m.checkForStuckAgents()

	if got := store.writesFor("sess-zero"); len(got) != 0 {
		t.Fatalf("expected no recovery for zero-activity agent, got %v", got)
	}
	if rec.has("office:agentRecovery") {
		t.Error("expected no recovery broadcast for zero-activity agent")
	}
	// The attempt counter must not advance.
	agent.mu.Lock()
	attempts := agent.RecoveryAttempts
	agent.mu.Unlock()
	if attempts != 0 {
		t.Errorf("expected RecoveryAttempts=0 for skipped agent, got %d", attempts)
	}
}

// TestCheckForStuckAgents_RecoveryWindowResets verifies that when the recovery
// window has elapsed (RecoveryWindowStart older than RecoveryWindowDuration),
// the attempt counter resets and the window restarts -- so an agent that
// exhausted attempts long ago becomes eligible for recovery again.
func TestCheckForStuckAgents_RecoveryWindowResets(t *testing.T) {
	cfg := stuckTestConfig()
	cfg.RecoveryWindowDuration = 50 * time.Millisecond
	cfg.RecoveryMaxAttempts = 3
	m, store, rec := newCtrlManager(t, cfg)

	m.RegisterAgent("sess-win", "win", &TeamMemberConfig{ID: "win", Name: "Window", Tier: "worker"})
	agent := m.GetAgent("sess-win")

	agent.mu.Lock()
	agent.LastPromptSentAt = time.Now().Add(-1 * time.Second)
	// Exhausted attempts in a window that is now stale.
	agent.RecoveryAttempts = 3
	agent.RecoveryWindowStart = time.Now().Add(-1 * time.Hour)
	agent.mu.Unlock()

	m.checkForStuckAgents()

	// Window reset to 0, then immediately bumped to 1 by this tick -> Tier1.
	agent.mu.Lock()
	attempts := agent.RecoveryAttempts
	windowStart := agent.RecoveryWindowStart
	agent.mu.Unlock()
	if attempts != 1 {
		t.Errorf("expected RecoveryAttempts reset+bumped to 1, got %d", attempts)
	}
	if time.Since(windowStart) > time.Minute {
		t.Errorf("expected RecoveryWindowStart reset to ~now, got %v ago", time.Since(windowStart))
	}
	if got := store.screenInputs("sess-win"); len(got) != 1 || got[0] != "\r" {
		t.Errorf("expected Tier1 Enter after window reset, got %v", got)
	}
	if !rec.has("office:agentRecovery") {
		t.Error("expected recovery broadcast after window reset")
	}
}

// TestCheckForStuckAgents_MaxAttemptsExhaustedSkips verifies that once an
// agent has reached RecoveryMaxAttempts inside a still-live window, further
// ticks short-circuit without another recovery prompt.
func TestCheckForStuckAgents_MaxAttemptsExhaustedSkips(t *testing.T) {
	cfg := stuckTestConfig()
	cfg.RecoveryWindowDuration = 1 * time.Hour // window stays live
	cfg.RecoveryMaxAttempts = 2
	m, store, _ := newCtrlManager(t, cfg)

	m.RegisterAgent("sess-exh", "exh", &TeamMemberConfig{ID: "exh", Name: "Exhausted", Tier: "worker"})
	agent := m.GetAgent("sess-exh")
	agent.mu.Lock()
	agent.LastPromptSentAt = time.Now().Add(-1 * time.Second)
	agent.RecoveryAttempts = 2 // already at max
	agent.RecoveryWindowStart = time.Now()
	agent.mu.Unlock()

	m.checkForStuckAgents()

	if got := store.writesFor("sess-exh"); len(got) != 0 {
		t.Errorf("expected no recovery once attempts exhausted in a live window, got %v", got)
	}
	// Counter must stay at the max (not advance past it).
	agent.mu.Lock()
	attempts := agent.RecoveryAttempts
	agent.mu.Unlock()
	if attempts != 2 {
		t.Errorf("expected RecoveryAttempts to stay 2, got %d", attempts)
	}
}
