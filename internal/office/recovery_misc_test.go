package office

import (
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// maybeTriggerWaterCoolerChat
// ---------------------------------------------------------------------------

// TestMaybeTriggerWaterCoolerChat_WrappingUpNoop verifies wrap-up
// short-circuits the water-cooler chat regardless of the dice roll.
func TestMaybeTriggerWaterCoolerChat_WrappingUpNoop(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WaterCoolerCooldown = 0
	m, store, _ := newCtrlManager(t, cfg)
	m.SetWrappingUp(true)

	m.RegisterAgent("sess-a", "a", &TeamMemberConfig{ID: "a", Name: "A", Tier: "worker"})
	m.RegisterAgent("sess-b", "b", &TeamMemberConfig{ID: "b", Name: "B", Tier: "worker"})

	// Many attempts: every one must be suppressed by wrap-up.
	for i := 0; i < 50; i++ {
		m.maybeTriggerWaterCoolerChat()
	}
	if len(store.writesFor("sess-a"))+len(store.writesFor("sess-b")) != 0 {
		t.Error("expected no water-cooler prompts during wrap-up")
	}
}

// TestMaybeTriggerWaterCoolerChat_NeedsTwoIdle verifies that with only one
// idle agent the chat never fires, even across many dice rolls.
func TestMaybeTriggerWaterCoolerChat_NeedsTwoIdle(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WaterCoolerCooldown = 0
	m, store, _ := newCtrlManager(t, cfg)

	// Single idle agent only.
	m.RegisterAgent("sess-lonely", "lonely", &TeamMemberConfig{ID: "lonely", Name: "Lonely", Tier: "worker"})

	for i := 0; i < 100; i++ {
		m.maybeTriggerWaterCoolerChat()
	}
	if len(store.writesFor("sess-lonely")) != 0 {
		t.Error("expected no water-cooler prompt with fewer than 2 idle agents")
	}
}

// TestMaybeTriggerWaterCoolerChat_FiresWithTwoIdle verifies the positive
// path: with two idle agents and a zero cooldown, repeated calls
// eventually pair them and write the [WATER COOLER] prompt to both.
// The 30% probability means we loop until it fires (or fail after a
// generous bound).
func TestMaybeTriggerWaterCoolerChat_FiresWithTwoIdle(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WaterCoolerCooldown = 0
	m, store, rec := newCtrlManager(t, cfg)

	m.RegisterAgent("sess-x", "x", &TeamMemberConfig{ID: "x", Name: "Xavier", Tier: "worker"})
	m.RegisterAgent("sess-y", "y", &TeamMemberConfig{ID: "y", Name: "Yara", Tier: "worker"})

	fired := false
	for i := 0; i < 500 && !fired; i++ {
		m.maybeTriggerWaterCoolerChat()
		if len(store.writesFor("sess-x")) > 0 || len(store.writesFor("sess-y")) > 0 {
			fired = true
		}
		// Reset cooldown so the next call isn't gated.
		m.mu.Lock()
		m.lastWaterCoolerAt = time.Time{}
		m.mu.Unlock()
	}
	if !fired {
		t.Fatal("expected water-cooler chat to fire across 500 attempts with two idle agents")
	}
	// Both agents must have received a [WATER COOLER] prompt.
	for _, sid := range []string{"sess-x", "sess-y"} {
		got := false
		for _, w := range store.writesFor(sid) {
			if strings.Contains(w.Input, "[WATER COOLER]") {
				got = true
			}
		}
		if !got {
			t.Errorf("expected [WATER COOLER] prompt for %s", sid)
		}
	}
	if !rec.has("office:waterCoolerChat") {
		t.Error("expected office:waterCoolerChat broadcast")
	}
}

// TestMaybeTriggerWaterCoolerChat_CooldownBlocks verifies that a recent
// chat (within cooldown) blocks a new one even when the dice would allow it.
func TestMaybeTriggerWaterCoolerChat_CooldownBlocks(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WaterCoolerCooldown = 1 * time.Hour
	m, store, _ := newCtrlManager(t, cfg)

	m.RegisterAgent("sess-c1", "c1", &TeamMemberConfig{ID: "c1", Name: "C1", Tier: "worker"})
	m.RegisterAgent("sess-c2", "c2", &TeamMemberConfig{ID: "c2", Name: "C2", Tier: "worker"})

	// Seed a recent chat so the cooldown gate trips.
	m.mu.Lock()
	m.lastWaterCoolerAt = time.Now()
	m.mu.Unlock()

	for i := 0; i < 100; i++ {
		m.maybeTriggerWaterCoolerChat()
	}
	if len(store.writesFor("sess-c1"))+len(store.writesFor("sess-c2")) != 0 {
		t.Error("expected no water-cooler prompt within cooldown")
	}
}

// ---------------------------------------------------------------------------
// StartIdleMonitoring / StopIdleMonitoring / startAutoStandup lifecycle
// ---------------------------------------------------------------------------

// TestStartStopIdleMonitoring_Lifecycle verifies the monitoring goroutine
// starts (idleTicker + monitorDone set), is idempotent on a second start,
// and shuts down cleanly on stop. Run under -race this also exercises the
// goroutine's tick body at least once without panicking.
func TestStartStopIdleMonitoring_Lifecycle(t *testing.T) {
	// Isolate on-disk persistence (PersistMetrics etc. run on each tick) so
	// the monitor goroutine doesn't race other tests' ~/.jarvis writes.
	t.Setenv("HOME", t.TempDir())
	cfg := DefaultConfig()
	cfg.IdleCheckInterval = 5 * time.Millisecond
	// Long auto-standup interval so the monitor exercises its tick body
	// without churning StartStandup (which is orthogonal to this test).
	cfg.AutoStandupInterval = 1 * time.Hour
	m, _, _ := newCtrlManager(t, cfg)
	m.SetAutoStandupEnabled(false)

	m.RegisterAgent("sess-mon", "mon", &TeamMemberConfig{ID: "mon", Name: "Mon", Tier: "worker"})

	m.StartIdleMonitoring()

	m.mu.RLock()
	tick := m.idleTicker
	done := m.monitorDone
	m.mu.RUnlock()
	if tick == nil || done == nil {
		t.Fatal("expected idleTicker and monitorDone to be set after start")
	}

	// Second start must be a no-op (idempotent).
	m.StartIdleMonitoring()

	// Let several ticks fire so the monitoring body runs under -race.
	time.Sleep(40 * time.Millisecond)

	m.StopIdleMonitoring()

	m.mu.RLock()
	tickAfter := m.idleTicker
	doneAfter := m.monitorDone
	m.mu.RUnlock()
	if tickAfter != nil || doneAfter != nil {
		t.Error("expected idleTicker and monitorDone cleared after stop")
	}

	// A second stop must not panic.
	m.StopIdleMonitoring()
}

// TestStartAutoStandup_DisabledSkips verifies that when auto-standup is
// disabled the timer is never created.
func TestStartAutoStandup_DisabledSkips(t *testing.T) {
	cfg := DefaultConfig()
	m, _, _ := newCtrlManager(t, cfg)
	m.SetAutoStandupEnabled(false)

	m.startAutoStandup()

	m.mu.RLock()
	timer := m.standupTimer
	m.mu.RUnlock()
	if timer != nil {
		t.Error("expected no standup timer when auto-standup disabled")
	}
}

// TestStartAutoStandup_Idempotent verifies a second startAutoStandup with
// the timer already running is a no-op.
func TestStartAutoStandup_Idempotent(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AutoStandupInterval = 1 * time.Hour
	m, _, _ := newCtrlManager(t, cfg)

	m.startAutoStandup()
	m.mu.RLock()
	first := m.standupTimer
	m.mu.RUnlock()
	if first == nil {
		t.Fatal("expected standup timer after first start")
	}

	m.startAutoStandup() // idempotent
	m.mu.RLock()
	second := m.standupTimer
	m.mu.RUnlock()
	if second != first {
		t.Error("expected the same standup timer after idempotent second start")
	}

	m.stopAutoStandup()
	m.mu.RLock()
	after := m.standupTimer
	m.mu.RUnlock()
	if after != nil {
		t.Error("expected standup timer cleared after stop")
	}
}
