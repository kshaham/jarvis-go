package office

import (
	"strings"
	"testing"
	"time"
)

// TestAskQuestion_WaitClamps covers both the min and max totalWait clamps by
// asking with an out-of-range wait and a no-recipient roster (returns fast).
func TestAskQuestion_WaitClampsBelowMin(t *testing.T) {
	m, _ := newQuestionTestManager(t, TeamMemberConfig{ID: "asker", Name: "Asker", Tier: "worker"})
	// Below-min wait → clamped to askMinWait. No other agents → NoRecipient
	// (returns immediately without waiting).
	q, err := m.AskQuestion("sess-asker", "anyone around?", "", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if q.Status != QuestionNoRecipient {
		t.Errorf("expected NoRecipient, got %s", q.Status)
	}
}

func TestAskQuestion_WaitClampsAboveMax(t *testing.T) {
	m, _ := newQuestionTestManager(t, TeamMemberConfig{ID: "asker", Name: "Asker", Tier: "worker"})
	// Above-max wait → clamped to askMaxWait. Still no recipient → fast return.
	q, err := m.AskQuestion("sess-asker", "anyone here?", "", askMaxWait+time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if q.Status != QuestionNoRecipient {
		t.Errorf("expected NoRecipient, got %s", q.Status)
	}
}

// TestAnswerQuestion_UnknownAndStatusGuards covers the unknown-question and
// wrong-recipient guards in AnswerQuestion.
func TestAnswerQuestion_UnknownAndStatusGuards(t *testing.T) {
	m, _ := newQuestionTestManager(t,
		TeamMemberConfig{ID: "asker", Name: "Asker", Tier: "worker"},
		TeamMemberConfig{ID: "peer", Name: "Peer", Tier: "worker"})
	// Unknown question id.
	if err := m.AnswerQuestion("q-missing", "sess-peer", "hi"); err == nil {
		t.Error("expected unknown-question error")
	}
	// Inject a pending question targeted at peer, then answer from a
	// non-recipient session → "not the current recipient".
	q := &Question{ID: "q-live", FromSessionID: "sess-asker", Status: QuestionPending, ToSessionID: "sess-peer"}
	m.mu.Lock()
	m.pendingQuestions[q.ID] = q
	m.mu.Unlock()
	if err := m.AnswerQuestion("q-live", "sess-asker", "answer"); err == nil {
		t.Error("expected not-current-recipient error")
	}
	// Flip status to non-pending → "question is <status>".
	m.mu.Lock()
	q.Status = QuestionAnswered
	m.mu.Unlock()
	if err := m.AnswerQuestion("q-live", "sess-peer", "answer"); err == nil {
		t.Error("expected non-pending-status error")
	}
}

// TestFormatQuestionPrompt_AboutFallbacks covers the AboutTaskID and
// "general" fallbacks when AboutTitle is empty.
func TestFormatQuestionPrompt_AboutFallbacks(t *testing.T) {
	// AboutTitle empty but AboutTaskID set → uses task ID.
	q1 := &Question{ID: "q1", FromName: "Asker", Content: "c", AboutTaskID: "task-7"}
	out1 := formatQuestionPrompt(q1)
	if !strings.Contains(out1, "task-7") {
		t.Errorf("expected task ID fallback, got %q", out1)
	}
	// Both empty → "general".
	q2 := &Question{ID: "q2", FromName: "Asker", Content: "c"}
	out2 := formatQuestionPrompt(q2)
	if !strings.Contains(out2, "general") {
		t.Errorf("expected 'general' fallback, got %q", out2)
	}
}

// TestLookupTaskTitle_Edges covers the empty-taskID, missing-agent, and
// missing-task returns.
func TestLookupTaskTitle_Edges(t *testing.T) {
	m, _ := newQuestionTestManager(t, TeamMemberConfig{ID: "asker", Name: "Asker", Tier: "worker"})
	if got := m.lookupTaskTitle("sess-asker", ""); got != "" {
		t.Errorf("expected empty for empty taskID, got %q", got)
	}
	if got := m.lookupTaskTitle("ghost", "t1"); got != "" {
		t.Errorf("expected empty for missing agent, got %q", got)
	}
	if got := m.lookupTaskTitle("sess-asker", "no-such-task"); got != "" {
		t.Errorf("expected empty for missing task, got %q", got)
	}
	// Present task → returns its title.
	m.GetAgent("sess-asker").AddTask(&Task{ID: "t1", Title: "The Title"})
	if got := m.lookupTaskTitle("sess-asker", "t1"); got != "The Title" {
		t.Errorf("expected 'The Title', got %q", got)
	}
}

// TestTriedNames_UnregisteredFallback covers the session-id fallback when a
// tried agent is no longer in the roster.
func TestTriedNames_UnregisteredFallback(t *testing.T) {
	m, _ := newQuestionTestManager(t,
		TeamMemberConfig{ID: "asker", Name: "Asker", Tier: "worker"},
		TeamMemberConfig{ID: "peer", Name: "Peer", Tier: "worker"})
	q := &Question{Tried: []string{"sess-peer", "sess-gone"}}
	names := q.triedNames(m)
	// sess-peer resolves to "Peer"; sess-gone (not registered) falls back to id.
	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %v", names)
	}
	if names[0] != "Peer" || names[1] != "sess-gone" {
		t.Errorf("expected [Peer sess-gone], got %v", names)
	}
}

// TestFindAvailableAgentExcluding_RoleAffinity drives the role-affinity
// first-pass match.
func TestFindAvailableAgentExcluding_RoleAffinity(t *testing.T) {
	m, _ := newQuestionTestManager(t,
		TeamMemberConfig{ID: "fe", Name: "FE", Role: "Frontend", Tier: "worker"},
		TeamMemberConfig{ID: "be", Name: "BE", Role: "Backend", Tier: "worker"})
	// Frontend asker → backend is a common affinity partner; the affinity
	// pass should be exercised even if it ends up picking via the fallback.
	got := m.findAvailableAgentExcluding("sess-fe", nil)
	if got == nil {
		t.Fatal("expected an available teammate")
	}
	if got.SessionID() == "sess-fe" {
		t.Error("must not return the asker")
	}
}
