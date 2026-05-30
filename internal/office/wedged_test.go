package office

// R19 · checkForWedgedWorkingAgents should stop any session that
// reports itself as "working" but has been silent past the configured
// timeout, and it should emit office:agentIdleStopped. Non-working
// sessions are left alone (checkForStuckAgents handles those).

import (
	"sync"
	"testing"
	"time"
)

// workingMockStore reports sessions as working based on an explicit
// set, and records any StopSession calls so tests can assert that the
// manager actually asked for the stop.
type workingMockStore struct {
	recordingSessionStore
	mu      sync.Mutex
	working map[string]bool
	stopped []string
}

func (w *workingMockStore) IsSessionWorking(id string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.working[id]
}
func (w *workingMockStore) IsSessionAlive(id string) bool { return true }
func (w *workingMockStore) StopSession(id string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.stopped = append(w.stopped, id)
	return nil
}
func (w *workingMockStore) stoppedSessions() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]string, len(w.stopped))
	copy(out, w.stopped)
	return out
}

func newWedgedManager(t *testing.T, wedgedTimeout time.Duration) (*Manager, *workingMockStore, *[]string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Cleanup(func() { time.Sleep(80 * time.Millisecond) })

	store := &workingMockStore{working: map[string]bool{}}
	cfg := DefaultConfig()
	cfg.WedgedWorkingTimeout = wedgedTimeout
	cfg.StuckGracePeriod = 0 // disable grace so tests don't have to sleep
	var broadcasts []string
	var mu sync.Mutex
	broadcast := func(event string, _ interface{}) {
		mu.Lock()
		defer mu.Unlock()
		broadcasts = append(broadcasts, event)
	}
	m := NewManager(store, broadcast, cfg)
	return m, store, &broadcasts
}

func TestWedged_StopsWorkingAgentPastTimeout(t *testing.T) {
	m, store, broadcasts := newWedgedManager(t, 100*time.Millisecond)
	m.RegisterAgent("sess-a", "cfg-a", &TeamMemberConfig{ID: "cfg-a", Name: "Wedged", Tier: "worker"})
	store.mu.Lock()
	store.working["sess-a"] = true
	store.mu.Unlock()

	// Force last-activity backward so the agent is past the timeout.
	a := m.GetAgent("sess-a")
	a.mu.Lock()
	a.LastPromptSentAt = time.Now().Add(-500 * time.Millisecond)
	a.mu.Unlock()

	m.checkForWedgedWorkingAgents(m.GetRoster())

	stopped := store.stoppedSessions()
	if len(stopped) != 1 || stopped[0] != "sess-a" {
		t.Fatalf("expected sess-a stopped, got %v", stopped)
	}
	found := false
	for _, ev := range *broadcasts {
		if ev == "office:agentIdleStopped" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected office:agentIdleStopped, got %v", *broadcasts)
	}
}

func TestWedged_IgnoresNonWorkingAgent(t *testing.T) {
	m, store, _ := newWedgedManager(t, 100*time.Millisecond)
	m.RegisterAgent("sess-b", "cfg-b", &TeamMemberConfig{ID: "cfg-b", Name: "Idle", Tier: "worker"})
	// working left unset -> not working
	a := m.GetAgent("sess-b")
	a.mu.Lock()
	a.LastPromptSentAt = time.Now().Add(-500 * time.Millisecond)
	a.mu.Unlock()

	m.checkForWedgedWorkingAgents(m.GetRoster())

	if stopped := store.stoppedSessions(); len(stopped) != 0 {
		t.Fatalf("expected no stops for non-working agent, got %v", stopped)
	}
}

func TestWedged_IgnoresWithinTimeout(t *testing.T) {
	m, store, _ := newWedgedManager(t, 10*time.Second)
	m.RegisterAgent("sess-c", "cfg-c", &TeamMemberConfig{ID: "cfg-c", Name: "Busy", Tier: "worker"})
	store.mu.Lock()
	store.working["sess-c"] = true
	store.mu.Unlock()
	a := m.GetAgent("sess-c")
	a.mu.Lock()
	a.LastPromptSentAt = time.Now() // fresh
	a.mu.Unlock()

	m.checkForWedgedWorkingAgents(m.GetRoster())

	if stopped := store.stoppedSessions(); len(stopped) != 0 {
		t.Fatalf("expected no stop within timeout, got %v", stopped)
	}
}

func TestWedged_DisabledWhenTimeoutZero(t *testing.T) {
	m, store, _ := newWedgedManager(t, 0)
	m.RegisterAgent("sess-d", "cfg-d", &TeamMemberConfig{ID: "cfg-d", Name: "Never", Tier: "worker"})
	store.mu.Lock()
	store.working["sess-d"] = true
	store.mu.Unlock()
	a := m.GetAgent("sess-d")
	a.mu.Lock()
	a.LastPromptSentAt = time.Now().Add(-10 * time.Minute)
	a.mu.Unlock()

	m.checkForWedgedWorkingAgents(m.GetRoster())

	if stopped := store.stoppedSessions(); len(stopped) != 0 {
		t.Fatalf("expected no stop when timeout is 0, got %v", stopped)
	}
}
