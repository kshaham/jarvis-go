package office

import (
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// ListPendingQuestionsFor
// ---------------------------------------------------------------------------

// TestListPendingQuestionsFor_ReturnsAddressedToSession verifies the list
// returns only questions whose current recipient is the given session and
// that are still pending.
func TestListPendingQuestionsFor_ReturnsAddressedToSession(t *testing.T) {
	asker := TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend", Tier: "worker"}
	lead := TeamMemberConfig{ID: "cap", Name: "Captain", Role: "Engineering Manager", Tier: "strategic"}
	m, store := newQuestionTestManager(t, asker, lead)

	resCh := make(chan struct{}, 1)
	go func() {
		_, _ = m.AskQuestion("sess-vex", "where is the router?", "", 2*time.Second)
		resCh <- struct{}{}
	}()

	// Wait for the lead to receive the question.
	deadline := time.Now().Add(1 * time.Second)
	for len(store.promptsFor("sess-cap")) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("lead never received the question prompt")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// The lead should see exactly one pending question.
	pending := m.ListPendingQuestionsFor("sess-cap")
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending question for lead, got %d", len(pending))
	}
	if pending[0].FromName != "Vex" {
		t.Errorf("expected question from Vex, got %q", pending[0].FromName)
	}
	if pending[0].Content != "where is the router?" {
		t.Errorf("unexpected content: %q", pending[0].Content)
	}

	// A different session sees nothing.
	if got := m.ListPendingQuestionsFor("sess-vex"); len(got) != 0 {
		t.Errorf("expected asker to have no pending questions addressed to it, got %d", len(got))
	}

	// Answer to release the blocked AskQuestion goroutine.
	if err := m.AnswerQuestion(pending[0].ID, "sess-cap", "internal/web/router.go"); err != nil {
		t.Fatalf("AnswerQuestion failed: %v", err)
	}
	<-resCh

	// After answering, nothing should be pending for the lead.
	if got := m.ListPendingQuestionsFor("sess-cap"); len(got) != 0 {
		t.Errorf("expected no pending questions after answer, got %d", len(got))
	}
}

// TestListPendingQuestionsFor_EmptyWhenNone verifies an empty (non-nil)
// slice is returned when there are no pending questions.
func TestListPendingQuestionsFor_EmptyWhenNone(t *testing.T) {
	asker := TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend", Tier: "worker"}
	m, _ := newQuestionTestManager(t, asker)

	got := m.ListPendingQuestionsFor("sess-vex")
	if got == nil {
		t.Fatal("expected non-nil empty slice")
	}
	if len(got) != 0 {
		t.Errorf("expected 0 pending questions, got %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// lookupTaskTitle (exercised via AskQuestion's AboutTitle)
// ---------------------------------------------------------------------------

// TestAskQuestion_AboutTitleResolved verifies that when --about names a
// real task on the asker, lookupTaskTitle resolves the human-readable
// title and it lands in both the Question and the recipient's prompt.
func TestAskQuestion_AboutTitleResolved(t *testing.T) {
	asker := TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend", Tier: "worker"}
	lead := TeamMemberConfig{ID: "cap", Name: "Captain", Role: "Engineering Manager", Tier: "strategic"}
	m, store := newQuestionTestManager(t, asker, lead)

	// Give the asker a task to reference.
	askerAgent := m.GetAgent("sess-vex")
	askerAgent.AddTask(&Task{ID: "task-99", Title: "Wire up the login form", Status: TaskInProgress})

	resCh := make(chan *Question, 1)
	go func() {
		q, _ := m.AskQuestion("sess-vex", "what validation library do we use?", "task-99", 2*time.Second)
		resCh <- q
	}()

	deadline := time.Now().Add(1 * time.Second)
	for len(store.promptsFor("sess-cap")) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("lead never received the question prompt")
		}
		time.Sleep(5 * time.Millisecond)
	}

	prompt := store.promptsFor("sess-cap")[0]
	if !strings.Contains(prompt, "Wire up the login form") {
		t.Errorf("expected resolved task title in prompt, got %q", prompt)
	}

	q := getOnlyPendingQuestion(t, m)
	if err := m.AnswerQuestion(q.ID, "sess-cap", "we use zod"); err != nil {
		t.Fatalf("AnswerQuestion failed: %v", err)
	}
	final := <-resCh
	if final.AboutTitle != "Wire up the login form" {
		t.Errorf("expected AboutTitle resolved, got %q", final.AboutTitle)
	}
}

// TestAskQuestion_AboutTitleUnknownTask verifies that referencing a task ID
// the asker does not own leaves AboutTitle empty (lookupTaskTitle returns "").
func TestAskQuestion_AboutTitleUnknownTask(t *testing.T) {
	asker := TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend", Tier: "worker"}
	m, _ := newQuestionTestManager(t, asker)

	// No recipient available, so this returns immediately with NoRecipient,
	// but the Question is still built with AboutTitle resolved (or blank).
	q, err := m.AskQuestion("sess-vex", "about a phantom task", "nonexistent-task", 1*time.Second)
	if err != nil {
		t.Fatalf("AskQuestion returned error: %v", err)
	}
	if q.AboutTitle != "" {
		t.Errorf("expected empty AboutTitle for unknown task, got %q", q.AboutTitle)
	}
	if q.AboutTaskID != "nonexistent-task" {
		t.Errorf("expected AboutTaskID preserved, got %q", q.AboutTaskID)
	}
}

// ---------------------------------------------------------------------------
// AskQuestion input validation
// ---------------------------------------------------------------------------

// TestAskQuestion_EmptyContentRejected verifies empty/whitespace content is
// rejected before any recipient lookup.
func TestAskQuestion_EmptyContentRejected(t *testing.T) {
	asker := TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend", Tier: "worker"}
	m, _ := newQuestionTestManager(t, asker)

	if _, err := m.AskQuestion("sess-vex", "   ", "", 1*time.Second); err == nil {
		t.Error("expected error for empty content")
	}
}

// TestAskQuestion_LongContentRejected verifies an oversized question is
// REJECTED at the 8 KB limit rather than silently truncated. Mid-range
// content (above the old 2048 sanitize cap, at/below 8000) is preserved
// intact -- the bug was that sanitizeForTerminal truncated to 2048 before
// the length check, silently cutting questions in the 2048-8000 range.
func TestAskQuestion_LongContentRejected(t *testing.T) {
	asker := TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend", Tier: "worker"}
	m, _ := newQuestionTestManager(t, asker)

	// Oversized -> rejected with a length error (no silent truncation).
	long := strings.Repeat("x", askMaxContentLen+1)
	if _, err := m.AskQuestion("sess-vex", long, "", 1*time.Second); err == nil || !strings.Contains(err.Error(), "too long") {
		t.Errorf("expected oversized question to be rejected with 'too long', got %v", err)
	}

	// Mid-range content (> old 2048 cap, <= 8000) is preserved, not truncated.
	// No recipient -> returns immediately with the built Question.
	mid := strings.Repeat("x", 5000)
	q, err := m.AskQuestion("sess-vex", mid, "", 1*time.Second)
	if err != nil {
		t.Fatalf("expected 5000-char question to be accepted, got err %v", err)
	}
	if len(q.Content) != 5000 {
		t.Errorf("expected content preserved at 5000 (no silent truncation to 2048), got %d", len(q.Content))
	}
}

// TestAskQuestion_UnregisteredAskerRejected verifies a session that is not a
// registered agent cannot ask.
func TestAskQuestion_UnregisteredAskerRejected(t *testing.T) {
	asker := TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend", Tier: "worker"}
	m, _ := newQuestionTestManager(t, asker)

	_, err := m.AskQuestion("sess-ghost", "hello?", "", 1*time.Second)
	if err == nil || !strings.Contains(err.Error(), "not a registered agent") {
		t.Errorf("expected unregistered-asker error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// AnswerQuestion validation
// ---------------------------------------------------------------------------

// TestAnswerQuestion_EmptyAnswerRejected verifies an empty answer is
// rejected.
func TestAnswerQuestion_EmptyAnswerRejected(t *testing.T) {
	asker := TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend", Tier: "worker"}
	m, _ := newQuestionTestManager(t, asker)

	if err := m.AnswerQuestion("q-anything", "sess-vex", "  "); err == nil {
		t.Error("expected error for empty answer")
	}
}

// TestAnswerQuestion_LongAnswerRejected verifies an oversized answer is
// REJECTED at the 8 KB limit. The "too long" error is now reachable;
// previously sanitizeForTerminal truncated the answer to 2048 before the
// length check, so oversized answers fell through to other paths.
func TestAnswerQuestion_LongAnswerRejected(t *testing.T) {
	asker := TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend", Tier: "worker"}
	m, _ := newQuestionTestManager(t, asker)

	long := strings.Repeat("y", askMaxContentLen+1)
	err := m.AnswerQuestion("q-anything", "sess-vex", long)
	if err == nil || !strings.Contains(err.Error(), "too long") {
		t.Errorf("expected oversized answer to be rejected with 'too long', got %v", err)
	}
}

// TestAnswerQuestion_UnknownQuestionRejected verifies answering a qid that
// isn't in the registry returns an "unknown question" error.
func TestAnswerQuestion_UnknownQuestionRejected(t *testing.T) {
	asker := TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend", Tier: "worker"}
	m, _ := newQuestionTestManager(t, asker)

	err := m.AnswerQuestion("q-missing", "sess-vex", "an answer")
	if err == nil || !strings.Contains(err.Error(), "unknown question") {
		t.Errorf("expected unknown-question error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// findAvailableAgentExcluding: role-affinity + active-meeting skip
// ---------------------------------------------------------------------------

// TestFindAvailableAgentExcluding_SkipsInActiveMeeting verifies a teammate
// currently in an active meeting is not pulled in as a fallback recipient.
func TestFindAvailableAgentExcluding_SkipsInActiveMeeting(t *testing.T) {
	asker := TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend", Tier: "worker"}
	peer := TeamMemberConfig{ID: "qa", Name: "Quill", Role: "QA", Tier: "worker"}
	m, _ := newQuestionTestManager(t, asker, peer)

	// Put the only candidate teammate into an active meeting.
	m.StartMeeting("brainstorm", []string{"sess-qa"})

	got := m.findAvailableAgentExcluding("sess-vex", nil)
	if got != nil {
		t.Errorf("expected nil (only teammate is in an active meeting), got %s", got.SessionID())
	}
}

// TestFindAvailableAgentExcluding_RespectsTriedList verifies a teammate in
// the already-tried list is excluded.
func TestFindAvailableAgentExcluding_RespectsTriedList(t *testing.T) {
	asker := TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend", Tier: "worker"}
	peer := TeamMemberConfig{ID: "qa", Name: "Quill", Role: "QA", Tier: "worker"}
	m, _ := newQuestionTestManager(t, asker, peer)

	// Already tried the only candidate.
	if got := m.findAvailableAgentExcluding("sess-vex", []string{"sess-qa"}); got != nil {
		t.Errorf("expected nil (only candidate already tried), got %s", got.SessionID())
	}
	// With no exclusions, the candidate is returned.
	if got := m.findAvailableAgentExcluding("sess-vex", nil); got == nil || got.SessionID() != "sess-qa" {
		t.Errorf("expected sess-qa returned, got %v", got)
	}
}

// TestFindAvailableAgentExcluding_PrefersRoleAffinity verifies the affinity
// pass picks a partner whose role matches the asker's preferred partners
// over an unrelated idle teammate.
func TestFindAvailableAgentExcluding_PrefersRoleAffinity(t *testing.T) {
	// Frontend's affinity partners include Backend (per RoleAffinityPartners).
	affinityRoles := RoleAffinityPartners("Frontend")
	if len(affinityRoles) == 0 {
		t.Skip("Frontend has no affinity partners in this build")
	}
	preferred := affinityRoles[0]

	asker := TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend", Tier: "worker"}
	// A teammate with the preferred role and an unrelated teammate.
	match := TeamMemberConfig{ID: "match", Name: "Match", Role: preferred, Tier: "worker"}
	other := TeamMemberConfig{ID: "other", Name: "Other", Role: "Designer", Tier: "worker"}
	m, _ := newQuestionTestManager(t, asker, match, other)

	got := m.findAvailableAgentExcluding("sess-vex", nil)
	if got == nil {
		t.Fatal("expected an available agent")
	}
	if got.Config.Role != preferred {
		t.Errorf("expected affinity match (%q), got %q", preferred, got.Config.Role)
	}
}

// ---------------------------------------------------------------------------
// isInActiveMeetingLocked (directly)
// ---------------------------------------------------------------------------

// TestIsInActiveMeetingLocked verifies the locked twin reports membership in
// active meetings and ignores ended ones.
func TestIsInActiveMeetingLocked(t *testing.T) {
	asker := TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend", Tier: "worker"}
	m, _ := newQuestionTestManager(t, asker)

	meeting := m.StartMeeting("brainstorm", []string{"sess-vex"})

	m.mu.RLock()
	inActive := m.isInActiveMeetingLocked("sess-vex")
	notIn := m.isInActiveMeetingLocked("sess-nobody")
	m.mu.RUnlock()

	if !inActive {
		t.Error("expected sess-vex to be reported in an active meeting")
	}
	if notIn {
		t.Error("expected sess-nobody to NOT be in any meeting")
	}

	// End the meeting; membership no longer counts.
	m.mu.Lock()
	m.meetings[meeting.ID].Active = false
	m.mu.Unlock()

	m.mu.RLock()
	stillIn := m.isInActiveMeetingLocked("sess-vex")
	m.mu.RUnlock()
	if stillIn {
		t.Error("expected no active-meeting membership after the meeting ended")
	}
}
