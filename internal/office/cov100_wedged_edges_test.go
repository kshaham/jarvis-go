package office

import (
	"errors"
	"testing"
	"time"
)

// TestWedged_LastTurnAndAPIAdvanceActivity exercises the lastActivity update
// branches: LastTurnAt advancing past LastPromptSentAt, then LastAgentAPIAt
// advancing past that. With all signals old, the agent is still stopped.
func TestWedged_LastTurnAndAPIAdvanceActivity(t *testing.T) {
	m, store, broadcasts := newWedgedManager(t, 100*time.Millisecond)
	m.RegisterAgent("sess-turn", "cfg-turn", &TeamMemberConfig{ID: "cfg-turn", Name: "Turner", Tier: "worker"})
	store.mu.Lock()
	store.working["sess-turn"] = true
	store.mu.Unlock()

	a := m.GetAgent("sess-turn")
	base := time.Now().Add(-1 * time.Second)
	a.mu.Lock()
	// All three timestamps set, in increasing order so both After-branches run.
	a.LastPromptSentAt = base
	a.LastTurnAt = base.Add(100 * time.Millisecond)
	a.LastAgentAPIAt = base.Add(200 * time.Millisecond) // most recent but still past timeout
	a.mu.Unlock()

	m.checkForWedgedWorkingAgents()

	if stopped := store.stoppedSessions(); len(stopped) != 1 || stopped[0] != "sess-turn" {
		t.Fatalf("expected sess-turn stopped, got %v", stopped)
	}
	found := false
	for _, ev := range *broadcasts {
		if ev == "office:agentIdleStopped" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected office:agentIdleStopped, got %v", *broadcasts)
	}
}

// TestWedged_RecentAPISkips verifies that a recent LastAgentAPIAt (the most
// recent activity) keeps the agent within timeout and prevents a stop.
func TestWedged_RecentAPISkips(t *testing.T) {
	m, store, _ := newWedgedManager(t, 500*time.Millisecond)
	m.RegisterAgent("sess-recentapi", "cfg-ra", &TeamMemberConfig{ID: "cfg-ra", Name: "RA", Tier: "worker"})
	store.mu.Lock()
	store.working["sess-recentapi"] = true
	store.mu.Unlock()

	a := m.GetAgent("sess-recentapi")
	a.mu.Lock()
	a.LastPromptSentAt = time.Now().Add(-1 * time.Hour) // ancient
	a.LastAgentAPIAt = time.Now()                       // fresh -> within timeout
	a.mu.Unlock()

	m.checkForWedgedWorkingAgents()

	if stopped := store.stoppedSessions(); len(stopped) != 0 {
		t.Fatalf("expected no stop when LastAgentAPIAt is fresh, got %v", stopped)
	}
}

// TestWedged_ZeroActivityFallsBackToSpawnTime verifies that when an agent has
// no prompt/turn/API timestamps, the wedge detector falls back to the team
// spawn time. With teamSpawnedAt old enough, the agent is stopped.
func TestWedged_ZeroActivityFallsBackToSpawnTime(t *testing.T) {
	m, store, _ := newWedgedManager(t, 100*time.Millisecond)
	// Set teamSpawnedAt to well in the past so the fallback is past timeout.
	m.mu.Lock()
	m.teamSpawnedAt = time.Now().Add(-1 * time.Second)
	m.mu.Unlock()

	m.RegisterAgent("sess-bornwedged", "cfg-bw", &TeamMemberConfig{ID: "cfg-bw", Name: "BornWedged", Tier: "worker"})
	store.mu.Lock()
	store.working["sess-bornwedged"] = true
	store.mu.Unlock()
	// RegisterAgent stamps LastPromptSentAt=now; clear all timestamps so the
	// wedge detector must fall back to teamSpawnedAt.
	a := m.GetAgent("sess-bornwedged")
	a.mu.Lock()
	a.LastPromptSentAt = time.Time{}
	a.LastTurnAt = time.Time{}
	a.LastAgentAPIAt = time.Time{}
	a.mu.Unlock()

	m.checkForWedgedWorkingAgents()

	if stopped := store.stoppedSessions(); len(stopped) != 1 || stopped[0] != "sess-bornwedged" {
		t.Fatalf("expected born-wedged agent stopped via spawn-time fallback, got %v", stopped)
	}
}

// TestWedged_ZeroActivityAndZeroSpawnContinues verifies that when both the
// agent timestamps AND teamSpawnedAt are zero, the agent is forgiven (the
// "lastActivity still zero -> continue" branch).
func TestWedged_ZeroActivityAndZeroSpawnContinues(t *testing.T) {
	m, store, _ := newWedgedManager(t, 100*time.Millisecond)
	// teamSpawnedAt stays zero (SetTeamSpawned never called).
	m.RegisterAgent("sess-noinfo", "cfg-ni", &TeamMemberConfig{ID: "cfg-ni", Name: "NoInfo", Tier: "worker"})
	store.mu.Lock()
	store.working["sess-noinfo"] = true
	store.mu.Unlock()
	// Clear RegisterAgent's LastPromptSentAt so there is genuinely zero
	// activity info and teamSpawnedAt is also zero.
	a := m.GetAgent("sess-noinfo")
	a.mu.Lock()
	a.LastPromptSentAt = time.Time{}
	a.mu.Unlock()

	m.checkForWedgedWorkingAgents()

	if stopped := store.stoppedSessions(); len(stopped) != 0 {
		t.Fatalf("expected no stop when there is no activity info at all, got %v", stopped)
	}
}

// wedgedStopErrStore wraps recordingSessionStore reporting a session as
// working and failing StopSession so we can exercise the stop-failure branch.
type wedgedStopErrStore struct {
	recordingSessionStore
	stopped []string
}

func (w *wedgedStopErrStore) IsSessionWorking(id string) bool { return true }
func (w *wedgedStopErrStore) IsSessionAlive(id string) bool   { return true }
func (w *wedgedStopErrStore) StopSession(id string) error {
	w.stopped = append(w.stopped, id)
	return errors.New("stop failed: pty already gone")
}

// TestWedged_StopSessionErrorStillBroadcasts verifies that when StopSession
// returns an error, the manager logs+falls through and still broadcasts the
// idle-stopped event and unregisters the agent.
func TestWedged_StopSessionErrorStillBroadcasts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Cleanup(func() { time.Sleep(60 * time.Millisecond) })

	store := &wedgedStopErrStore{}
	cfg := DefaultConfig()
	cfg.WedgedWorkingTimeout = 100 * time.Millisecond
	cfg.StuckGracePeriod = 0
	rec := &eventRecorder{}
	m := NewManager(store, rec.record, cfg)

	m.RegisterAgent("sess-stoperr", "cfg-se", &TeamMemberConfig{ID: "cfg-se", Name: "StopErr", Tier: "worker"})
	a := m.GetAgent("sess-stoperr")
	a.mu.Lock()
	a.LastPromptSentAt = time.Now().Add(-1 * time.Second)
	a.mu.Unlock()

	m.checkForWedgedWorkingAgents()

	if len(store.stopped) != 1 {
		t.Fatalf("expected StopSession to be attempted once, got %v", store.stopped)
	}
	if !rec.has("office:agentIdleStopped") {
		t.Error("expected office:agentIdleStopped even when StopSession errors")
	}
	if m.GetAgent("sess-stoperr") != nil {
		t.Error("expected agent to be unregistered after wedge stop attempt")
	}
}
