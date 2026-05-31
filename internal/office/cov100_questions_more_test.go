package office

import (
	"strings"
	"testing"
	"time"
)

// TestAskQuestion_ContentTooLong drives the askMaxContentLen guard
// (questions.go:96-98). sanitizeForTerminal caps at 2048 runes, so an
// over-length ASCII string can't trip the byte-length guard; a 4-byte
// emoji × 2048 = 8192 bytes > askMaxContentLen (8000) does.
func TestAskQuestion_ContentTooLong(t *testing.T) {
	m, _ := newQuestionTestManager(t, TeamMemberConfig{ID: "asker", Name: "Asker", Tier: "worker"})
	huge := strings.Repeat("\U0001F600", 2048)
	_, err := m.AskQuestion("sess-asker", huge, "", time.Second)
	if err == nil {
		t.Fatal("expected too-long error")
	}
	if !strings.Contains(err.Error(), "too long") {
		t.Errorf("expected 'too long' error, got %v", err)
	}
}

// TestFindAvailableAgentExcluding_PrefersRoleAffinityMatch drives the
// first-pass affinity loop (questions.go:478-482): a teammate whose role
// is an affinity partner of the asker's role is selected over a
// non-affinity teammate that appears earlier in the map iteration.
func TestFindAvailableAgentExcluding_PrefersRoleAffinityMatch(t *testing.T) {
	m, _ := newQuestionTestManager(t,
		TeamMemberConfig{ID: "fe", Name: "FE", Role: "Frontend Engineer", Tier: "worker"},
		// "UI/UX Designer" is the first affinity partner of "Frontend Engineer".
		TeamMemberConfig{ID: "ux", Name: "UX", Role: "UI/UX Designer", Tier: "worker"},
		// A non-affinity teammate that must NOT win the affinity pass.
		TeamMemberConfig{ID: "ops", Name: "Ops", Role: "DevOps Engineer", Tier: "worker"})

	got := m.findAvailableAgentExcluding("sess-fe", nil)
	if got == nil {
		t.Fatal("expected an available teammate")
	}
	if got.SessionID() != "sess-ux" {
		t.Errorf("expected affinity partner sess-ux, got %q", got.SessionID())
	}
}

// TestAskOnce_NonPositiveMaxWaitReturnsFalse drives the maxWait<=0 early
// return (questions.go:220-222): a deadline already in the past clamps
// maxWait below zero, so askOnce returns immediately without installing
// a slot.
func TestAskOnce_NonPositiveMaxWaitReturnsFalse(t *testing.T) {
	m, _ := newQuestionTestManager(t,
		TeamMemberConfig{ID: "asker", Name: "Asker", Tier: "worker"},
		TeamMemberConfig{ID: "peer", Name: "Peer", Tier: "worker"})
	peer := m.GetAgent("sess-peer")

	q := &Question{ID: "q1", FromSessionID: "sess-asker"}
	// Deadline already elapsed → remaining < maxWait → maxWait becomes
	// negative → the <=0 guard fires.
	pastDeadline := time.Now().Add(-time.Second)
	answer, ok := m.askOnce(q, peer, time.Minute, pastDeadline)
	if ok {
		t.Errorf("expected ok=false for non-positive maxWait, got answer=%q", answer)
	}
	// No slot should have been installed (we returned before that step).
	peer.mu.Lock()
	slot := peer.QuestionSlot
	peer.mu.Unlock()
	if slot != nil {
		t.Error("askOnce should not install a slot when maxWait<=0")
	}
}

// TestAskOnce_DropsStrandedPriorSlot drives the recipient.QuestionSlot!=nil
// finish() branch (questions.go:232-234): a recipient who still holds a
// slot from a previous rotation has it finished before the new slot is
// installed.
func TestAskOnce_DropsStrandedPriorSlot(t *testing.T) {
	m, _ := newQuestionTestManager(t,
		TeamMemberConfig{ID: "asker", Name: "Asker", Tier: "worker"},
		TeamMemberConfig{ID: "peer", Name: "Peer", Tier: "worker"})
	peer := m.GetAgent("sess-peer")

	// Pre-install a stranded slot from a "previous rotation".
	stranded := newWaitSlot()
	peer.mu.Lock()
	peer.QuestionSlot = stranded
	peer.QuestionSlotQID = "q-old"
	peer.mu.Unlock()

	q := &Question{ID: "q-new", FromSessionID: "sess-asker"}
	// Give a tiny positive window so askOnce installs the new slot then
	// times out quickly.
	deadline := time.Now().Add(30 * time.Millisecond)
	answer, ok := m.askOnce(q, peer, 30*time.Millisecond, deadline)
	if ok {
		t.Errorf("expected timeout (ok=false), got answer=%q", answer)
	}

	// The branch under test: the stranded slot from the prior rotation
	// must have been finished (closed) before the new slot was installed.
	if !stranded.closed.Load() {
		t.Error("stranded prior slot should have been finished")
	}
	// q-new's slot was installed and then cleared by askOnce's own defer on
	// timeout (it nils QuestionSlot/QID when the slot is still its own), so
	// the post-call QID is empty — that is the documented timeout cleanup.
	peer.mu.Lock()
	gotSlot := peer.QuestionSlot
	peer.mu.Unlock()
	if gotSlot == stranded {
		t.Error("stranded slot should have been replaced, not still installed")
	}
}
