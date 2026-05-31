package office

import (
	"strings"
	"testing"
)

// TestAnswerQuestion_AnswerTooLong drives the askMaxContentLen guard:
// an over-length answer is rejected before any roster lookup.
func TestAnswerQuestion_AnswerTooLong(t *testing.T) {
	m, _ := newQuestionTestManager(t,
		TeamMemberConfig{ID: "asker", Name: "Asker", Tier: "worker"},
		TeamMemberConfig{ID: "peer", Name: "Peer", Tier: "worker"})

	q := &Question{ID: "q-long", FromSessionID: "sess-asker", Status: QuestionPending, ToSessionID: "sess-peer"}
	m.mu.Lock()
	m.pendingQuestions[q.ID] = q
	m.mu.Unlock()

	// sanitizeForTerminal caps at 2048 RUNES, so an ASCII string can
	// never exceed askMaxContentLen (8000 bytes) after sanitisation.
	// Use a 4-byte emoji: 2048 runes × 4 bytes = 8192 bytes > 8000, so
	// the post-sanitise byte length still trips the guard.
	huge := strings.Repeat("\U0001F600", 2048) // 😀 = 4 bytes each
	err := m.AnswerQuestion("q-long", "sess-peer", huge)
	if err == nil {
		t.Fatal("expected error for over-length answer")
	}
	if !strings.Contains(err.Error(), "too long") {
		t.Errorf("expected 'too long' error, got %v", err)
	}
}

// TestAnswerQuestion_ResponderNotRegistered drives the responder==nil
// branch: the question's recipient session is in pendingQuestions but
// the agent was deregistered, so GetAgent returns nil.
func TestAnswerQuestion_ResponderNotRegistered(t *testing.T) {
	m, _ := newQuestionTestManager(t,
		TeamMemberConfig{ID: "asker", Name: "Asker", Tier: "worker"})

	// Recipient session "sess-ghost" is NOT a registered agent.
	q := &Question{ID: "q-ghost", FromSessionID: "sess-asker", Status: QuestionPending, ToSessionID: "sess-ghost"}
	m.mu.Lock()
	m.pendingQuestions[q.ID] = q
	m.mu.Unlock()

	err := m.AnswerQuestion("q-ghost", "sess-ghost", "here is the answer")
	if err == nil {
		t.Fatal("expected error when responder session is unregistered")
	}
	if !strings.Contains(err.Error(), "not a registered agent") {
		t.Errorf("expected 'not a registered agent' error, got %v", err)
	}
}

// TestAnswerQuestion_AskerStoppedWaiting drives the deliver-false branch:
// the recipient's QuestionSlot is missing (asker already gave up), so
// AnswerQuestion records a late response and returns the drop error.
func TestAnswerQuestion_AskerStoppedWaiting(t *testing.T) {
	m, _ := newQuestionTestManager(t,
		TeamMemberConfig{ID: "asker", Name: "Asker", Tier: "worker"},
		TeamMemberConfig{ID: "peer", Name: "Peer", Tier: "worker"})

	q := &Question{ID: "q-drop", FromSessionID: "sess-asker", Status: QuestionPending, ToSessionID: "sess-peer"}
	m.mu.Lock()
	m.pendingQuestions[q.ID] = q
	m.mu.Unlock()

	// Responder agent exists but has no QuestionSlot installed → slot==nil
	// → deliver path short-circuits to the late-response drop.
	peer := m.GetAgent("sess-peer")
	peer.mu.Lock()
	peer.QuestionSlot = nil
	peer.QuestionSlotQID = ""
	peer.mu.Unlock()

	err := m.AnswerQuestion("q-drop", "sess-peer", "late answer")
	if err == nil {
		t.Fatal("expected drop error when asker stopped waiting")
	}
	if !strings.Contains(err.Error(), "stopped waiting") {
		t.Errorf("expected 'stopped waiting' error, got %v", err)
	}
}

// TestAnswerQuestion_StaleSlotQIDDrops drives the slotQID != qID arm of
// the same condition: the recipient has a slot, but it serves a NEWER
// question, so answering the older one must not deliver.
func TestAnswerQuestion_StaleSlotQIDDrops(t *testing.T) {
	m, _ := newQuestionTestManager(t,
		TeamMemberConfig{ID: "asker", Name: "Asker", Tier: "worker"},
		TeamMemberConfig{ID: "peer", Name: "Peer", Tier: "worker"})

	q := &Question{ID: "q-old", FromSessionID: "sess-asker", Status: QuestionPending, ToSessionID: "sess-peer"}
	m.mu.Lock()
	m.pendingQuestions[q.ID] = q
	m.mu.Unlock()

	peer := m.GetAgent("sess-peer")
	slot := newWaitSlot()
	peer.mu.Lock()
	peer.QuestionSlot = slot
	peer.QuestionSlotQID = "q-NEWER" // slot serves a different (newer) question
	peer.mu.Unlock()

	err := m.AnswerQuestion("q-old", "sess-peer", "answer to the old one")
	if err == nil {
		t.Fatal("expected drop error when slot serves a different question")
	}
	if !strings.Contains(err.Error(), "stopped waiting") {
		t.Errorf("expected 'stopped waiting' error, got %v", err)
	}
}
