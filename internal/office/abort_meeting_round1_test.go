package office

import (
	"testing"
	"time"
)

// TestAbortMeetingsFacilitatedBy_DefaultTypePokesBothSlots covers the default
// (non-brainstorm) meeting-type branch: it best-effort delivers the abort
// sentinel to BOTH the brainstorm and standup slots of every participant.
func TestAbortMeetingsFacilitatedBy_DefaultTypePokesBothSlots(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()

	m.RegisterAgent("sess-fac", "fac", &TeamMemberConfig{ID: "fac", Name: "Fac", Tier: "strategic"})
	m.RegisterAgent("sess-p", "p", &TeamMemberConfig{ID: "p", Name: "Part", Tier: "worker"})
	part := m.GetAgent("sess-p")

	// A future / unknown meeting type takes the default branch.
	meeting := &Meeting{
		ID:                   "mtg-review",
		Type:                 "review", // not "brainstorm" -> default branch
		Participants:         []string{"sess-fac", "sess-p"},
		StartedAt:            time.Now(),
		Active:               true,
		FacilitatorSessionID: "sess-fac",
	}
	m.mu.Lock()
	m.meetings[meeting.ID] = meeting
	m.mu.Unlock()

	// Arm both slots on the participant; the default branch pokes both.
	bslot := newWaitSlot()
	sslot := newWaitSlot()
	part.mu.Lock()
	part.BrainstormSlot = bslot
	part.StandupSlot = sslot
	part.mu.Unlock()

	m.abortMeetingsFacilitatedBy("sess-fac", "Fac", "facilitator died")

	// Both slots should have received the abort sentinel.
	select {
	case got := <-bslot.ch():
		if got != meetingAbortedSentinel {
			t.Errorf("brainstorm slot got %q, want sentinel", got)
		}
	default:
		t.Error("expected brainstorm slot poked in default branch")
	}
	select {
	case got := <-sslot.ch():
		if got != meetingAbortedSentinel {
			t.Errorf("standup slot got %q, want sentinel", got)
		}
	default:
		t.Error("expected standup slot poked in default branch")
	}

	// Meeting marked inactive.
	m.mu.RLock()
	live := m.meetings["mtg-review"]
	m.mu.RUnlock()
	if live == nil || live.Active {
		t.Error("expected meeting marked inactive")
	}
}

// TestAbortMeetingsFacilitatedBy_NilParticipantAgentSkipped verifies a
// participant whose agent has been unregistered is skipped without panic.
func TestAbortMeetingsFacilitatedBy_NilParticipantAgentSkipped(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.RegisterAgent("sess-fac", "fac", &TeamMemberConfig{ID: "fac", Name: "Fac"})

	meeting := &Meeting{
		ID:                   "mtg-x",
		Type:                 "brainstorm",
		Participants:         []string{"sess-fac", "sess-gone"}, // sess-gone never registered
		StartedAt:            time.Now(),
		Active:               true,
		FacilitatorSessionID: "sess-fac",
	}
	m.mu.Lock()
	m.meetings[meeting.ID] = meeting
	m.mu.Unlock()

	// Must not panic despite the missing participant agent.
	m.abortMeetingsFacilitatedBy("sess-fac", "Fac", "died")

	m.mu.RLock()
	live := m.meetings["mtg-x"]
	m.mu.RUnlock()
	if live == nil || live.Active {
		t.Error("expected meeting marked inactive even with a missing participant")
	}
}
