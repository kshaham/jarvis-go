package office

import (
	"testing"
	"time"
)

// TestRunAutoStandupTick_Disabled verifies the tick body returns false (no
// standup started) when auto-standup is disabled.
func TestRunAutoStandupTick_Disabled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := DefaultConfig()
	m, _, _ := newCtrlManager(t, cfg)
	m.RegisterAgent("sess-a", "alpha", &TeamMemberConfig{ID: "alpha", Name: "Alpha", Tier: "worker"})

	m.SetAutoStandupEnabled(false)

	if m.runAutoStandupTick() {
		t.Error("expected tick to be a no-op when auto-standup disabled")
	}
	if st := m.GetStandup(); st != nil && st.Active {
		t.Error("expected no active standup when disabled")
	}
}

// TestRunAutoStandupTick_StandupAlreadyActive verifies the tick body bails
// when a standup is already in progress (the !enabled || standupActive gate).
func TestRunAutoStandupTick_StandupAlreadyActive(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := DefaultConfig()
	m, _, _ := newCtrlManager(t, cfg)

	// Inject an active standup directly.
	m.mu.Lock()
	m.standup = &StandupState{ID: "live", Active: true, Participants: []string{"sess-a"}}
	m.mu.Unlock()

	if m.runAutoStandupTick() {
		t.Error("expected tick to be a no-op when a standup is already active")
	}
}

// TestRunAutoStandupTick_PausedSkips verifies the paused gate short-circuits
// the tick body even when enabled and no standup is active.
func TestRunAutoStandupTick_PausedSkips(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := DefaultConfig()
	m, _, _ := newCtrlManager(t, cfg)
	m.RegisterAgent("sess-a", "alpha", &TeamMemberConfig{ID: "alpha", Name: "Alpha", Tier: "worker"})

	m.SetPaused(true, "operator break")

	if m.runAutoStandupTick() {
		t.Error("expected tick to be a no-op while paused")
	}
	if st := m.GetStandup(); st != nil && st.Active {
		t.Error("expected no active standup while paused")
	}
}

// TestRunAutoStandupTick_BrainstormInProgressSkips verifies an active
// brainstorm meeting suppresses the auto-standup (the hasBrainstorm gate and
// the meeting-scan loop both get exercised).
func TestRunAutoStandupTick_BrainstormInProgressSkips(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := DefaultConfig()
	m, _, _ := newCtrlManager(t, cfg)
	m.RegisterAgent("sess-a", "alpha", &TeamMemberConfig{ID: "alpha", Name: "Alpha", Tier: "worker"})

	// Inject an active brainstorm meeting.
	m.mu.Lock()
	m.meetings["mtg-1"] = &Meeting{ID: "mtg-1", Type: "brainstorm", Active: true, Participants: []string{"sess-a"}}
	m.mu.Unlock()

	if m.runAutoStandupTick() {
		t.Error("expected tick to be a no-op while a brainstorm meeting is active")
	}
	if st := m.GetStandup(); st != nil && st.Active {
		t.Error("expected no standup started while brainstorm active")
	}
}

// TestRunAutoStandupTick_NonBrainstormMeetingIgnored verifies that a
// non-brainstorm (or inactive) meeting does NOT suppress the standup -- the
// loop must keep scanning rather than treat any meeting as a brainstorm.
func TestRunAutoStandupTick_NonBrainstormMeetingIgnored(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := DefaultConfig()
	m, _, _ := newCtrlManager(t, cfg)
	m.RegisterAgent("sess-a", "alpha", &TeamMemberConfig{ID: "alpha", Name: "Alpha", Tier: "worker"})

	m.mu.Lock()
	// An inactive brainstorm and an active non-brainstorm: neither counts.
	m.meetings["mtg-inactive"] = &Meeting{ID: "mtg-inactive", Type: "brainstorm", Active: false}
	m.meetings["mtg-other"] = &Meeting{ID: "mtg-other", Type: "review", Active: true}
	m.mu.Unlock()

	if !m.runAutoStandupTick() {
		t.Error("expected tick to start a standup (no active brainstorm present)")
	}
	st := m.GetStandup()
	if st == nil || !st.Active {
		t.Fatal("expected an active standup to be started")
	}
}

// TestRunAutoStandupTick_StartsStandup verifies the happy path: enabled, not
// paused, no standup, no brainstorm -> StartStandup runs and the tick returns
// true.
func TestRunAutoStandupTick_StartsStandup(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := DefaultConfig()
	cfg.StandupResponseTimeout = 50 * time.Millisecond
	m, _, rec := newCtrlManager(t, cfg)
	m.RegisterAgent("sess-a", "alpha", &TeamMemberConfig{ID: "alpha", Name: "Alpha", Tier: "worker"})

	if !m.runAutoStandupTick() {
		t.Fatal("expected tick to start a standup on the happy path")
	}
	if !rec.has("office:standupStarted") {
		t.Error("expected office:standupStarted broadcast")
	}
}
