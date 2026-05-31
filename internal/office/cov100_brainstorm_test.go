package office

import (
	"sync"
	"testing"
	"time"
)

// configurableSessionStore lets a test control WriteInkPrompt success and
// IsSessionWorking so the brainstorm/standup flows can be driven into
// their delivery-failure and all-working facilitator branches.
type configurableSessionStore struct {
	mu          sync.Mutex
	writes      []writeCall
	inkOK       bool
	working     bool
	failForSess map[string]bool // sessions whose WriteInkPrompt fails
}

func (c *configurableSessionStore) WriteViaScreen(sessionID, input string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writes = append(c.writes, writeCall{SessionID: sessionID, Input: input})
	return true
}

func (c *configurableSessionStore) WriteInkPrompt(sessionID, prompt string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writes = append(c.writes, writeCall{SessionID: sessionID, Input: prompt})
	if c.failForSess != nil && c.failForSess[sessionID] {
		return false
	}
	return c.inkOK
}

func (c *configurableSessionStore) GetSessionIDs() []string              { return nil }
func (c *configurableSessionStore) IsSessionAlive(sessionID string) bool { return true }
func (c *configurableSessionStore) IsSessionWorking(sessionID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.working
}
func (c *configurableSessionStore) StopSession(sessionID string) error { return nil }

func (c *configurableSessionStore) countWrites(sessionID string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, w := range c.writes {
		if w.SessionID == sessionID {
			n++
		}
	}
	return n
}

// TestRunBrainstormFlow_FacilitatorDeliveryFails drives the facilitator
// WriteInkPrompt==false early return.
func TestRunBrainstormFlow_FacilitatorDeliveryFails(t *testing.T) {
	store := &configurableSessionStore{inkOK: true, failForSess: map[string]bool{"sess-max": true}}
	cfg := DefaultConfig()
	cfg.BrainstormTimeout = 100 * time.Millisecond
	m := NewManager(store, func(string, interface{}) {}, cfg)
	m.RegisterAgent("sess-max", "max", &TeamMemberConfig{ID: "max", Name: "Max", Tier: "strategic"})
	m.RegisterAgent("sess-vex", "vex", &TeamMemberConfig{ID: "vex", Name: "Vex", Tier: "worker"})

	meeting := m.StartMeeting("brainstorm", []string{"sess-max", "sess-vex"})
	// Run synchronously for determinism.
	m.runBrainstormFlow(meeting, []string{"sess-max", "sess-vex"})

	// Facilitator (sess-max, strategic+idle) got the prompt attempt; because
	// delivery failed, the flow returned before distributing to vex.
	if store.countWrites("sess-vex") != 0 {
		t.Error("expected no task distribution after facilitator delivery failure")
	}
}

// TestRunBrainstormFlow_AllWorkingPicksStrategic forces every agent to
// report working so the first two facilitator-preference loops fail and
// the third (first strategic, even if working) is taken.
func TestRunBrainstormFlow_AllWorkingPicksStrategic(t *testing.T) {
	store := &configurableSessionStore{inkOK: true, working: true}
	cfg := DefaultConfig()
	cfg.BrainstormTimeout = 100 * time.Millisecond
	m := NewManager(store, func(string, interface{}) {}, cfg)
	m.RegisterAgent("sess-w1", "w1", &TeamMemberConfig{ID: "w1", Name: "W1", Tier: "worker"})
	m.RegisterAgent("sess-lead", "lead", &TeamMemberConfig{ID: "lead", Name: "Lead", Tier: "strategic"})

	meeting := m.StartMeeting("brainstorm", []string{"sess-w1", "sess-lead"})
	m.runBrainstormFlow(meeting, []string{"sess-w1", "sess-lead"})

	// The strategic lead should have been chosen as facilitator and received
	// the [BRAINSTORM] prompt despite being "working".
	if store.countWrites("sess-lead") == 0 {
		t.Error("expected the strategic agent to be chosen as facilitator")
	}
}

// TestRunBrainstormFlow_TaskDeliveryFails drives the per-participant
// WriteInkPrompt==false branch (facilitator delivery succeeds, then a
// teammate's delivery fails after the facilitator responds).
func TestRunBrainstormFlow_TaskDeliveryFails(t *testing.T) {
	store := &configurableSessionStore{inkOK: true, failForSess: map[string]bool{"sess-vex": true}}
	cfg := DefaultConfig()
	cfg.BrainstormTimeout = 2 * time.Second
	m := NewManager(store, func(string, interface{}) {}, cfg)
	m.RegisterAgent("sess-max", "max", &TeamMemberConfig{ID: "max", Name: "Max", Tier: "strategic"})
	m.RegisterAgent("sess-vex", "vex", &TeamMemberConfig{ID: "vex", Name: "Vex", Tier: "worker"})

	meeting := m.StartMeeting("brainstorm", []string{"sess-max", "sess-vex"})

	done := make(chan struct{})
	go func() {
		m.runBrainstormFlow(meeting, []string{"sess-max", "sess-vex"})
		close(done)
	}()
	// Let the facilitator prompt go out, then respond so the flow proceeds to
	// task distribution where vex's delivery fails.
	time.Sleep(100 * time.Millisecond)
	m.SubmitBrainstormResponse("sess-max", "1. task A\n2. task B")

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("brainstorm flow did not complete")
	}
	// Both sessions saw a write attempt (facilitator prompt + failed task).
	if store.countWrites("sess-vex") == 0 {
		t.Error("expected a task-delivery attempt to vex")
	}
}
