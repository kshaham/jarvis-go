package office

import (
	"strings"
	"testing"
	"time"
)

// TestRunBrainstormFlow_NilSessions drives the three sessions==nil branches:
// the isIdle helper, the facilitator-send guard, and the per-participant
// distribution guard. With nil sessions the flow returns at the facilitator
// guard after the isIdle helper has already short-circuited.
func TestRunBrainstormFlow_NilSessions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store := &configurableSessionStore{inkOK: true}
	cfg := DefaultConfig()
	cfg.BrainstormTimeout = 100 * time.Millisecond
	m := NewManager(store, func(string, interface{}) {}, cfg)
	m.RegisterAgent("sess-max", "max", &TeamMemberConfig{ID: "max", Name: "Max", Tier: "strategic"})
	m.RegisterAgent("sess-vex", "vex", &TeamMemberConfig{ID: "vex", Name: "Vex", Tier: "worker"})

	meeting := m.StartMeeting("brainstorm", []string{"sess-max", "sess-vex"})

	// Nil out sessions so the isIdle helper assumes idle and the facilitator
	// guard returns before any write.
	m.mu.Lock()
	m.sessions = nil
	m.mu.Unlock()

	m.runBrainstormFlow(meeting, []string{"sess-max", "sess-vex"})

	// No writes happened (sessions is nil), and the meeting was ended by the
	// deferred endMeeting.
	if store.countWrites("sess-max") != 0 || store.countWrites("sess-vex") != 0 {
		t.Error("expected no writes with nil sessions")
	}
	// The deferred endMeeting should have marked it inactive.
	for _, mtg := range m.GetMeetings() {
		if mtg.ID == meeting.ID && mtg.Active {
			t.Error("expected the meeting to be ended after the flow returns")
		}
	}
}

// TestRunBrainstormFlow_NilConfigParticipantNameFallsBackToID drives the
// names-loop else branch: a participant whose Config is nil is named by its
// session id instead of a config name.
func TestRunBrainstormFlow_NilConfigParticipantNameFallsBackToID(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store := &configurableSessionStore{inkOK: true}
	cfg := DefaultConfig()
	cfg.BrainstormTimeout = 80 * time.Millisecond
	m := NewManager(store, func(string, interface{}) {}, cfg)
	m.RegisterAgent("sess-lead", "lead", &TeamMemberConfig{ID: "lead", Name: "Lead", Tier: "strategic"})
	m.RegisterAgent("sess-nilcfg", "nilcfg", &TeamMemberConfig{ID: "nilcfg", Name: "Nil", Tier: "worker"})

	// Null the worker's config so the names loop's else arm runs.
	nilAgent := m.GetAgent("sess-nilcfg")
	nilAgent.mu.Lock()
	nilAgent.Config = nil
	nilAgent.mu.Unlock()

	meeting := m.StartMeeting("brainstorm", []string{"sess-lead", "sess-nilcfg"})
	m.runBrainstormFlow(meeting, []string{"sess-lead", "sess-nilcfg"})

	// The facilitator prompt should embed the nil-config participant's id.
	found := false
	store.mu.Lock()
	for _, w := range store.writes {
		if w.SessionID == "sess-lead" && strings.Contains(w.Input, "sess-nilcfg") {
			found = true
		}
	}
	store.mu.Unlock()
	if !found {
		t.Error("expected nil-config participant to be named by its session id in the prompt")
	}
}

// TestRunBrainstormFlow_MeetingRemovedMidFlow drives the !meetingExists return:
// the meeting is deleted while the facilitator's response is awaited, so the
// distribution phase is skipped.
func TestRunBrainstormFlow_MeetingRemovedMidFlow(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store := &configurableSessionStore{inkOK: true}
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

	// Let the facilitator prompt go out, then delete the meeting BEFORE
	// responding so the post-wait meetingExists check fails.
	time.Sleep(120 * time.Millisecond)
	m.mu.Lock()
	delete(m.meetings, meeting.ID)
	m.mu.Unlock()
	m.SubmitBrainstormResponse("sess-max", "1. task A")

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("brainstorm flow did not complete")
	}

	// vex must NOT have received a task distribution (flow returned at the
	// meeting-removed check).
	if store.countWrites("sess-vex") != 0 {
		t.Error("expected no task distribution after the meeting was removed mid-flow")
	}
}
