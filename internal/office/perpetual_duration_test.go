package office

import (
	"testing"
	"time"
)

// newPerpetualTestManager builds a manager in perpetual mode with the team
// marked spawned, so the duration-deadline check is exercisable without a live
// session store. teamSpawnedAt is set directly (under lock) so SetPerpetual
// DurationHours anchors a finite deadline relative to it.
func newPerpetualTestManager(t *testing.T) *Manager {
	t.Helper()
	m := NewManager(nil, func(string, interface{}) {}, DefaultConfig())
	m.mu.Lock()
	m.teamSpawned = true
	m.teamSpawnedAt = time.Now().Add(-2 * time.Hour) // spawned 2h ago
	m.mu.Unlock()
	m.SetPerpetualMode(true)
	return m
}

func TestSetPerpetualDurationHours(t *testing.T) {
	m := newPerpetualTestManager(t)

	// hours <= 0 -> unlimited (zero deadline).
	m.SetPerpetualDurationHours(0)
	if !m.GetPerpetualDeadline().IsZero() {
		t.Fatalf("hours=0 should yield zero (unlimited) deadline, got %v", m.GetPerpetualDeadline())
	}
	m.SetPerpetualDurationHours(-5)
	if !m.GetPerpetualDeadline().IsZero() {
		t.Fatalf("negative hours should yield zero deadline, got %v", m.GetPerpetualDeadline())
	}

	// Positive hours anchor the deadline at teamSpawnedAt + hours.
	m.SetPerpetualDurationHours(6)
	spawnedAt := m.GetTeamSpawnedAt()
	want := spawnedAt.Add(6 * time.Hour)
	if got := m.GetPerpetualDeadline(); !got.Equal(want) {
		t.Fatalf("deadline = %v, want %v (spawnedAt + 6h)", got, want)
	}

	// Re-arming resets the wrapped-up latch.
	m.mu.Lock()
	m.perpetualDurationWrappedUp = true
	m.mu.Unlock()
	m.SetPerpetualDurationHours(3)
	m.mu.RLock()
	wrapped := m.perpetualDurationWrappedUp
	m.mu.RUnlock()
	if wrapped {
		t.Fatal("SetPerpetualDurationHours should reset the wrapped-up latch")
	}
}

func TestCheckPerpetualDurationDeadline_FiresOncePastDeadline(t *testing.T) {
	m := newPerpetualTestManager(t)

	// Deadline in the past: team spawned 2h ago with a 1h budget.
	m.SetPerpetualDurationHours(1)
	if m.IsWrappingUp() {
		t.Fatal("precondition: should not be wrapping up before the check runs")
	}

	m.checkPerpetualDurationDeadline()

	if !m.IsWrappingUp() {
		t.Fatal("checkPerpetualDurationDeadline should initiate wrap-up once the deadline passes")
	}
	m.mu.RLock()
	latched := m.perpetualDurationWrappedUp
	m.mu.RUnlock()
	if !latched {
		t.Fatal("the wrapped-up latch should be set after the first fire")
	}

	// Second call is a no-op: the latch prevents a re-fire. Flip wrappingUp
	// off so the IsWrappingUp early-return doesn't mask the latch check.
	m.SetWrappingUp(false)
	m.checkPerpetualDurationDeadline()
	if m.IsWrappingUp() {
		t.Fatal("second call should be a no-op (latched); wrap-up must not re-fire")
	}
}

func TestCheckPerpetualDurationDeadline_NoopWhenUnlimited(t *testing.T) {
	m := newPerpetualTestManager(t)
	m.SetPerpetualDurationHours(0) // unlimited

	m.checkPerpetualDurationDeadline()
	if m.IsWrappingUp() {
		t.Fatal("unlimited duration (zero deadline) must never auto-wrap-up")
	}
}

func TestCheckPerpetualDurationDeadline_NoopBeforeDeadline(t *testing.T) {
	m := NewManager(nil, func(string, interface{}) {}, DefaultConfig())
	m.mu.Lock()
	m.teamSpawned = true
	m.teamSpawnedAt = time.Now() // just spawned
	m.mu.Unlock()
	m.SetPerpetualMode(true)
	m.SetPerpetualDurationHours(6) // 6h budget, deadline well in the future

	m.checkPerpetualDurationDeadline()
	if m.IsWrappingUp() {
		t.Fatal("a future deadline must not trigger wrap-up yet")
	}
}

func TestCheckPerpetualDurationDeadline_NoopWhenNotPerpetual(t *testing.T) {
	m := NewManager(nil, func(string, interface{}) {}, DefaultConfig())
	m.mu.Lock()
	m.teamSpawned = true
	m.teamSpawnedAt = time.Now().Add(-2 * time.Hour)
	m.mu.Unlock()
	// Not perpetual. Even with a past deadline set directly, the check bails.
	m.SetPerpetualDurationHours(1)
	m.SetPerpetualMode(false)

	m.checkPerpetualDurationDeadline()
	if m.IsWrappingUp() {
		t.Fatal("non-perpetual team must not auto-wrap-up on a duration deadline")
	}
}
