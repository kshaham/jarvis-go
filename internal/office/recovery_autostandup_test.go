package office

import (
	"testing"
	"time"
)

// TestStartAutoStandup_TickTriggersStandup drives the auto-standup goroutine
// with a very short interval and asserts the tick body actually fires a
// standup (the "Auto-standup triggered" -> StartStandup path) and emits the
// scheduling broadcast.
func TestStartAutoStandup_TickTriggersStandup(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := DefaultConfig()
	cfg.AutoStandupInterval = 10 * time.Millisecond
	m, _, rec := newCtrlManager(t, cfg)

	// At least one registered agent so StartStandup has a participant.
	m.RegisterAgent("sess-a", "alpha", &TeamMemberConfig{ID: "alpha", Name: "Alpha", Tier: "worker"})

	m.startAutoStandup()
	defer m.stopAutoStandup()

	if !rec.has("office:autoStandupScheduled") {
		t.Error("expected office:autoStandupScheduled broadcast on start")
	}

	// Wait for at least one tick to fire a standup.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		st := m.GetStandup()
		if st != nil && st.Active {
			return // tick body reached StartStandup -- success.
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("expected the auto-standup tick to start a standup within the deadline")
}

// TestStartAutoStandup_PausedSkipsTick verifies the tick body honours the
// paused gate: with the manager paused, no standup is started even though
// ticks keep firing.
func TestStartAutoStandup_PausedSkipsTick(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := DefaultConfig()
	cfg.AutoStandupInterval = 10 * time.Millisecond
	m, _, _ := newCtrlManager(t, cfg)
	m.RegisterAgent("sess-a", "alpha", &TeamMemberConfig{ID: "alpha", Name: "Alpha", Tier: "worker"})

	m.SetPaused(true, "test pause")
	m.startAutoStandup()
	defer m.stopAutoStandup()

	// Let several ticks fire while paused.
	time.Sleep(80 * time.Millisecond)

	if st := m.GetStandup(); st != nil && st.Active {
		t.Error("expected no standup to start while paused")
	}
}
