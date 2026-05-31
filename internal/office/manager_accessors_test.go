package office

import (
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// TrySetTeamSpawning / ReleaseTeamSpawnSlot atomic-claim discipline
// ---------------------------------------------------------------------------

// TestTrySetTeamSpawning_FirstWinsSecondLoses verifies the atomic spawn
// claim: the first caller flips the flag and wins, a concurrent second
// caller loses. SetTeamSpawned(true) starts the idle monitor, so we stop
// it to avoid leaking goroutines.
func TestTrySetTeamSpawning_FirstWinsSecondLoses(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m, _, _ := newCtrlManager(t, DefaultConfig())
	defer m.StopIdleMonitoring()

	if !m.TrySetTeamSpawning() {
		t.Fatal("expected first claim to win")
	}
	if !m.IsTeamSpawned() {
		t.Error("expected team spawned after winning claim")
	}
	if m.TrySetTeamSpawning() {
		t.Error("expected second concurrent claim to lose")
	}

	// Release rolls it back so a retry can win again.
	m.ReleaseTeamSpawnSlot()
	if m.IsTeamSpawned() {
		t.Error("expected slot released")
	}
	if !m.TrySetTeamSpawning() {
		t.Error("expected re-claim to win after release")
	}
}

// ---------------------------------------------------------------------------
// PruneTriage
// ---------------------------------------------------------------------------

// TestPruneTriage_DropsEntries verifies a single task's triage tracking is
// reclaimed, and that empty/unknown IDs are safe no-ops.
func TestPruneTriage_DropsEntries(t *testing.T) {
	m, _, _ := newCtrlManager(t, DefaultConfig())

	m.mu.Lock()
	m.triageAttempts["task-x"] = 2
	m.lastTriageAt["task-x"] = time.Now()
	m.mu.Unlock()

	// Empty ID is a no-op.
	m.PruneTriage("")
	m.mu.RLock()
	_, stillThere := m.triageAttempts["task-x"]
	m.mu.RUnlock()
	if !stillThere {
		t.Error("empty PruneTriage should not drop existing entries")
	}

	m.PruneTriage("task-x")
	m.mu.RLock()
	_, gone := m.triageAttempts["task-x"]
	_, goneAt := m.lastTriageAt["task-x"]
	m.mu.RUnlock()
	if gone || goneAt {
		t.Error("expected triage entries dropped for task-x")
	}

	// Unknown ID is a safe no-op.
	m.PruneTriage("never-existed")
}

// ---------------------------------------------------------------------------
// SetConfig / Events
// ---------------------------------------------------------------------------

// TestSetConfig_ReplacesConfig verifies SetConfig swaps the active config,
// observable through downstream behavior (a zeroed RecoveryMaxAttempts).
func TestSetConfig_ReplacesConfig(t *testing.T) {
	m, _, _ := newCtrlManager(t, DefaultConfig())
	newCfg := DefaultConfig()
	newCfg.RecoveryMaxAttempts = 99
	m.SetConfig(newCfg)

	m.mu.RLock()
	got := m.config.RecoveryMaxAttempts
	m.mu.RUnlock()
	if got != 99 {
		t.Errorf("expected RecoveryMaxAttempts=99 after SetConfig, got %d", got)
	}
}

// TestEvents_ReturnsChannel verifies Events returns a usable receive
// channel that carries emitted office events.
func TestEvents_ReturnsChannel(t *testing.T) {
	m, _, _ := newCtrlManager(t, DefaultConfig())
	ch := m.Events()
	if ch == nil {
		t.Fatal("expected non-nil events channel")
	}

	// Emit an event and confirm it arrives.
	m.emitEvent(CuriosityEvent{SessionID: "s", AgentName: "A"})
	select {
	case ev := <-ch:
		if ev.officeEventType() != "curiosityTriggered" {
			t.Errorf("unexpected event type %q", ev.officeEventType())
		}
	case <-time.After(time.Second):
		t.Fatal("expected an event on the channel")
	}
}

// ---------------------------------------------------------------------------
// Standup getter/setter
// ---------------------------------------------------------------------------

// TestGetSetStandup_RoundTrip verifies SetStandup stores state and
// GetStandup returns a deep copy (mutating the copy must not affect the
// stored state).
func TestGetSetStandup_RoundTrip(t *testing.T) {
	m, _, _ := newCtrlManager(t, DefaultConfig())

	// Nil standup -> GetStandup returns nil.
	if m.GetStandup() != nil {
		t.Error("expected nil standup initially")
	}

	m.SetStandup(&StandupState{
		ID:           "su-1",
		Active:       true,
		Participants: []string{"sess-a", "sess-b"},
		Responses:    map[string]string{"sess-a": "done"},
		CurrentIndex: 1,
	})

	got := m.GetStandup()
	if got == nil || got.ID != "su-1" || !got.Active || got.CurrentIndex != 1 {
		t.Fatalf("unexpected standup snapshot: %+v", got)
	}
	if len(got.Participants) != 2 || got.Responses["sess-a"] != "done" {
		t.Errorf("standup fields not copied through: %+v", got)
	}

	// Mutating the returned copy must not affect the stored state.
	got.Participants[0] = "MUTATED"
	got.Responses["sess-a"] = "MUTATED"
	fresh := m.GetStandup()
	if fresh.Participants[0] == "MUTATED" || fresh.Responses["sess-a"] == "MUTATED" {
		t.Error("GetStandup must return a deep copy, mutation leaked back")
	}
}

// ---------------------------------------------------------------------------
// Auto-standup / curiosity toggles
// ---------------------------------------------------------------------------

// TestAutoStandupAndCuriosityToggles verifies the enabled flags round-trip.
func TestAutoStandupAndCuriosityToggles(t *testing.T) {
	m, _, _ := newCtrlManager(t, DefaultConfig())

	// Defaults are true.
	if !m.IsAutoStandupEnabled() {
		t.Error("expected auto-standup enabled by default")
	}
	if !m.IsCuriosityEnabled() {
		t.Error("expected curiosity enabled by default")
	}

	m.SetAutoStandupEnabled(false)
	if m.IsAutoStandupEnabled() {
		t.Error("expected auto-standup disabled after setter")
	}
	m.SetCuriosityEnabled(false)
	if m.IsCuriosityEnabled() {
		t.Error("expected curiosity disabled after setter")
	}
}

// ---------------------------------------------------------------------------
// UpdateBugTask
// ---------------------------------------------------------------------------

// TestUpdateBugTask_FoundAndNotFound verifies UpdateBugTask mutates an
// existing bug's status and returns false for an unknown ID.
func TestUpdateBugTask_FoundAndNotFound(t *testing.T) {
	m, _, _ := newCtrlManager(t, DefaultConfig())
	m.AddBugTask(&BugTask{ID: "bug-1", Status: "open"})

	if !m.UpdateBugTask("bug-1", "fixed") {
		t.Fatal("expected UpdateBugTask to find bug-1")
	}
	tasks := m.GetBugTasks()
	var found *BugTask
	for _, bt := range tasks {
		if bt.ID == "bug-1" {
			found = bt
		}
	}
	if found == nil || found.Status != "fixed" {
		t.Errorf("expected bug-1 status=fixed, got %+v", found)
	}

	if m.UpdateBugTask("ghost", "fixed") {
		t.Error("expected UpdateBugTask to return false for unknown ID")
	}
}

// ---------------------------------------------------------------------------
// ActiveMeetingCount / ActiveLockCount
// ---------------------------------------------------------------------------

// TestActiveMeetingCount verifies only Active meetings are counted.
func TestActiveMeetingCount(t *testing.T) {
	m, _, _ := newCtrlManager(t, DefaultConfig())
	if m.ActiveMeetingCount() != 0 {
		t.Fatalf("expected 0 active meetings, got %d", m.ActiveMeetingCount())
	}
	mtg := m.StartMeeting("brainstorm", []string{"a", "b"})
	if m.ActiveMeetingCount() != 1 {
		t.Errorf("expected 1 active meeting, got %d", m.ActiveMeetingCount())
	}
	m.mu.Lock()
	m.meetings[mtg.ID].Active = false
	m.mu.Unlock()
	if m.ActiveMeetingCount() != 0 {
		t.Errorf("expected 0 active meetings after ending, got %d", m.ActiveMeetingCount())
	}
}

// TestActiveLockCount_ExpiresStale verifies expired locks are swept out and
// only live locks counted.
func TestActiveLockCount_ExpiresStale(t *testing.T) {
	m, _, _ := newCtrlManager(t, DefaultConfig())

	m.mu.Lock()
	m.fileLocks["/live"] = &FileLock{Path: "/live", ExpiresAt: time.Now().Add(time.Hour)}
	m.fileLocks["/stale"] = &FileLock{Path: "/stale", ExpiresAt: time.Now().Add(-time.Hour)}
	m.mu.Unlock()

	if got := m.ActiveLockCount(); got != 1 {
		t.Errorf("expected 1 active lock (stale swept), got %d", got)
	}
	// The stale lock should now be gone from the map.
	m.mu.RLock()
	_, staleGone := m.fileLocks["/stale"]
	m.mu.RUnlock()
	if staleGone {
		t.Error("expected stale lock removed by ActiveLockCount")
	}
}

// ---------------------------------------------------------------------------
// ForceReleaseLock
// ---------------------------------------------------------------------------

// TestForceReleaseLock verifies it releases regardless of owner and returns
// false for an unknown path.
func TestForceReleaseLock(t *testing.T) {
	m, _, _ := newCtrlManager(t, DefaultConfig())
	m.mu.Lock()
	m.fileLocks[normalizePath("/some/file.go")] = &FileLock{Path: "/some/file.go", ExpiresAt: time.Now().Add(time.Hour)}
	m.mu.Unlock()

	if !m.ForceReleaseLock("/some/file.go") {
		t.Error("expected ForceReleaseLock to release an existing lock")
	}
	if m.ForceReleaseLock("/some/file.go") {
		t.Error("expected false releasing an already-released lock")
	}
}

// ---------------------------------------------------------------------------
// InjectRelayMessage
// ---------------------------------------------------------------------------

// TestInjectRelayMessage_NoTeamSpawned verifies a relay inject returns false
// when no team is spawned.
func TestInjectRelayMessage_NoTeamSpawned(t *testing.T) {
	m, _, _ := newCtrlManager(t, DefaultConfig())
	if m.InjectRelayMessage("peer-1", "PeerTeam", "hello there") {
		t.Error("expected false when no team is spawned")
	}
}

// TestInjectRelayMessage_DeliversToLead verifies the relay lands on a
// strategic-tier lead and flows through the office message log.
func TestInjectRelayMessage_DeliversToLead(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// InjectRelayMessage -> SendMessage fires a fire-and-forget
	// persistMessages goroutine writing under HOME. Drain it before
	// t.TempDir's RemoveAll runs to avoid a flaky "directory not empty".
	t.Cleanup(func() { drainMessagePersist(home) })
	m, store, _ := newCtrlManager(t, DefaultConfig())
	defer m.StopIdleMonitoring()

	m.RegisterAgent("sess-w", "w", &TeamMemberConfig{ID: "w", Name: "Worker", Tier: "worker"})
	m.RegisterAgent("sess-lead", "lead", &TeamMemberConfig{ID: "lead", Name: "Lead", Tier: "strategic"})
	m.TrySetTeamSpawning() // marks team spawned (and starts monitor)

	live := m.InjectRelayMessage("peer-7", "OtherTeam", "deploy is green")
	if !live {
		t.Fatal("expected relay to be delivered to a live agent")
	}

	// The lead (strategic) should be the recipient via WriteViaScreen.
	found := false
	for _, w := range store.writesFor("sess-lead") {
		if containsAll(w.Input, "Relay from peer team", "OtherTeam", "deploy is green") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected relay message delivered to lead, got %v", store.writesFor("sess-lead"))
	}

	// And it should be in the office message log.
	msgs := m.GetMessages()
	logged := false
	for _, msg := range msgs {
		if msg.From == "relay:peer-7" {
			logged = true
		}
	}
	if !logged {
		t.Error("expected relay message recorded in the office message log")
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
