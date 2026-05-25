package office

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// programmableSessionStore is a per-test mock that lets each case mark
// specific sessions as "working" (which the question router treats as
// busy and skips when picking a fallback recipient). Mirrors the
// minimal SessionStore surface needed by AskQuestion.
type programmableSessionStore struct {
	mu          sync.Mutex
	working     map[string]bool
	prompts     map[string][]string // sessionID -> prompts written via WriteInkPrompt
	screenWrite map[string][]string // sessionID -> data written via WriteViaScreen
}

func newProgrammableSessionStore() *programmableSessionStore {
	return &programmableSessionStore{
		working:     map[string]bool{},
		prompts:     map[string][]string{},
		screenWrite: map[string][]string{},
	}
}

func (p *programmableSessionStore) WriteViaScreen(sessionID, input string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.screenWrite[sessionID] = append(p.screenWrite[sessionID], input)
	return true
}

func (p *programmableSessionStore) WriteInkPrompt(sessionID, prompt string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.prompts[sessionID] = append(p.prompts[sessionID], prompt)
	return true
}

func (p *programmableSessionStore) GetSessionIDs() []string      { return nil }
func (p *programmableSessionStore) IsSessionAlive(s string) bool { return true }
func (p *programmableSessionStore) IsSessionWorking(s string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.working[s]
}
func (p *programmableSessionStore) StopSession(s string) error { return nil }

func (p *programmableSessionStore) setWorking(s string, v bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.working[s] = v
}

func (p *programmableSessionStore) promptsFor(sessionID string) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, len(p.prompts[sessionID]))
	copy(out, p.prompts[sessionID])
	return out
}

// newQuestionTestManager builds a manager wired up to a programmable
// session store and registers an asker (worker tier) plus a configurable
// set of additional teammates. Returns the store so tests can flip
// per-session "working" flags.
func newQuestionTestManager(t *testing.T, asker TeamMemberConfig, others ...TeamMemberConfig) (*Manager, *programmableSessionStore) {
	t.Helper()
	store := newProgrammableSessionStore()
	m := NewManager(store, func(event string, data interface{}) {}, nil)
	// Wipe any persisted on-disk state from prior runs so each test
	// starts clean — same trick as newTestManager().
	m.mu.Lock()
	m.messages = make([]OfficeMessage, 0)
	m.mu.Unlock()

	cfg := asker
	m.RegisterAgent("sess-"+cfg.ID, cfg.ID, &cfg)
	for _, o := range others {
		ocfg := o
		m.RegisterAgent("sess-"+ocfg.ID, ocfg.ID, &ocfg)
	}
	return m, store
}

func TestAskQuestion_LeadAvailable_DeliversAndAnswers(t *testing.T) {
	asker := TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend", Tier: "worker"}
	lead := TeamMemberConfig{ID: "cap", Name: "Captain", Role: "Engineering Manager", Tier: "strategic"}
	m, store := newQuestionTestManager(t, asker, lead)

	type result struct {
		q   *Question
		err error
	}
	resCh := make(chan result, 1)
	go func() {
		q, err := m.AskQuestion("sess-vex", "where is the auth middleware?", "", 2*time.Second)
		resCh <- result{q, err}
	}()

	// Wait for the prompt to land on the lead, then answer.
	deadline := time.Now().Add(1 * time.Second)
	for {
		if len(store.promptsFor("sess-cap")) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("lead never received the question prompt")
		}
		time.Sleep(5 * time.Millisecond)
	}
	prompt := store.promptsFor("sess-cap")[0]
	if !strings.Contains(prompt, "[QUESTION FROM Vex") {
		t.Errorf("expected QUESTION FROM Vex in prompt, got %q", prompt)
	}
	// Find the qid in the prompt.
	q := getOnlyPendingQuestion(t, m)
	if err := m.AnswerQuestion(q.ID, "sess-cap", "see internal/auth/middleware.go"); err != nil {
		t.Fatalf("AnswerQuestion failed: %v", err)
	}

	res := <-resCh
	if res.err != nil {
		t.Fatalf("AskQuestion failed: %v", res.err)
	}
	if res.q.Status != QuestionAnswered {
		t.Errorf("expected status %q, got %q", QuestionAnswered, res.q.Status)
	}
	if res.q.Answer != "see internal/auth/middleware.go" {
		t.Errorf("unexpected answer: %q", res.q.Answer)
	}
	if res.q.ToName != "Captain" {
		t.Errorf("expected lead Captain to be the responder, got %q", res.q.ToName)
	}
	// The asker should have received a [ANSWER FROM ...] prompt as well.
	askerPrompts := store.promptsFor("sess-vex")
	if len(askerPrompts) == 0 || !strings.Contains(askerPrompts[len(askerPrompts)-1], "[ANSWER FROM Captain]") {
		t.Errorf("expected [ANSWER FROM Captain] prompt to be written to asker, got %v", askerPrompts)
	}
}

func TestAskQuestion_LeadBusy_FallsBackToTeammate(t *testing.T) {
	asker := TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend", Tier: "worker"}
	lead := TeamMemberConfig{ID: "cap", Name: "Captain", Role: "Engineering Manager", Tier: "strategic"}
	peer := TeamMemberConfig{ID: "qa", Name: "Quill", Role: "QA", Tier: "worker"}
	m, store := newQuestionTestManager(t, asker, lead, peer)

	// Lead is mid-turn AND has an active task -- the question should land
	// on the lead first (lead is asked regardless of busy state, since the
	// office prefers them), but if no answer comes in time we rotate.
	store.setWorking("sess-cap", true)

	type result struct {
		q   *Question
		err error
	}
	resCh := make(chan result, 1)
	go func() {
		// Total wait short enough to keep the test fast but >2 * askMinWait
		// so the rotation has room to fire the fallback.
		q, err := m.AskQuestion("sess-vex", "what's our error wrapping convention?", "", 4*time.Second)
		resCh <- result{q, err}
	}()

	// Give the routine a moment to install the slot on the lead and start
	// the per-recipient wait. We don't answer as the lead -- we wait for
	// the fallback rotation to land the prompt on Quill.
	deadline := time.Now().Add(6 * time.Second)
	for {
		if len(store.promptsFor("sess-qa")) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("fallback teammate never received the question (got: cap=%d, qa=%d)",
				len(store.promptsFor("sess-cap")), len(store.promptsFor("sess-qa")))
		}
		time.Sleep(50 * time.Millisecond)
	}

	q := getOnlyPendingQuestion(t, m)
	if err := m.AnswerQuestion(q.ID, "sess-qa", "we use fmt.Errorf with %w"); err != nil {
		t.Fatalf("AnswerQuestion failed: %v", err)
	}

	res := <-resCh
	if res.err != nil {
		t.Fatalf("AskQuestion failed: %v", res.err)
	}
	if res.q.Status != QuestionAnswered {
		t.Fatalf("expected status %q, got %q", QuestionAnswered, res.q.Status)
	}
	if res.q.ToName != "Quill" {
		t.Errorf("expected fallback Quill to answer, got %q", res.q.ToName)
	}
	if len(res.q.Tried) < 2 || res.q.Tried[0] != "sess-cap" || res.q.Tried[1] != "sess-qa" {
		t.Errorf("expected Tried=[sess-cap, sess-qa], got %v", res.q.Tried)
	}
}

func TestAskQuestion_NobodyAnswers_ReturnsTimeout(t *testing.T) {
	asker := TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend", Tier: "worker"}
	lead := TeamMemberConfig{ID: "cap", Name: "Captain", Role: "Engineering Manager", Tier: "strategic"}
	m, _ := newQuestionTestManager(t, asker, lead)

	// askMinWait floor is 10s; total wait floors to 10s. Fast, but real.
	q, err := m.AskQuestion("sess-vex", "what is six times nine?", "", 1*time.Second)
	if err != nil {
		t.Fatalf("AskQuestion returned error: %v", err)
	}
	if q.Status != QuestionTimeout {
		t.Errorf("expected status %q, got %q", QuestionTimeout, q.Status)
	}
	// The lead should have been tried at least once.
	if len(q.Tried) == 0 || q.Tried[0] != "sess-cap" {
		t.Errorf("expected lead to be tried first, got Tried=%v", q.Tried)
	}
}

func TestAskQuestion_NoEligibleRecipients_ReturnsNoRecipient(t *testing.T) {
	asker := TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend", Tier: "worker"}
	// Only the asker on the team.
	m, _ := newQuestionTestManager(t, asker)

	q, err := m.AskQuestion("sess-vex", "anybody home?", "", 1*time.Second)
	if err != nil {
		t.Fatalf("AskQuestion returned error: %v", err)
	}
	if q.Status != QuestionNoRecipient {
		t.Errorf("expected status %q, got %q", QuestionNoRecipient, q.Status)
	}
	if len(q.Tried) != 0 {
		t.Errorf("expected no recipients tried, got %v", q.Tried)
	}
}

func TestAskQuestion_DuplicateWithinCooldown_Rejected(t *testing.T) {
	asker := TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend", Tier: "worker"}
	lead := TeamMemberConfig{ID: "cap", Name: "Captain", Role: "Engineering Manager", Tier: "strategic"}
	m, _ := newQuestionTestManager(t, asker, lead)

	// First ask: nobody answers, status=timeout. The duplicate-cooldown
	// fires off the FIRST AskQuestion regardless of outcome.
	if _, err := m.AskQuestion("sess-vex", "same question please", "", 1*time.Second); err != nil {
		t.Fatalf("first AskQuestion failed: %v", err)
	}
	// Second ask within cooldown should be rejected with an explanatory error.
	_, err := m.AskQuestion("sess-vex", "  Same   QUESTION  please ", "", 1*time.Second)
	if err == nil {
		t.Fatalf("expected duplicate-cooldown rejection, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate question") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAnswerQuestion_WrongSender_Rejected(t *testing.T) {
	asker := TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend", Tier: "worker"}
	lead := TeamMemberConfig{ID: "cap", Name: "Captain", Role: "Engineering Manager", Tier: "strategic"}
	intruder := TeamMemberConfig{ID: "snek", Name: "Snek", Role: "Backend", Tier: "worker"}
	m, store := newQuestionTestManager(t, asker, lead, intruder)

	resCh := make(chan struct{}, 1)
	go func() {
		_, _ = m.AskQuestion("sess-vex", "who can deploy?", "", 2*time.Second)
		resCh <- struct{}{}
	}()

	// Wait for the lead to receive the question.
	deadline := time.Now().Add(1 * time.Second)
	for {
		if len(store.promptsFor("sess-cap")) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("lead never received the question prompt")
		}
		time.Sleep(5 * time.Millisecond)
	}

	q := getOnlyPendingQuestion(t, m)
	if err := m.AnswerQuestion(q.ID, "sess-snek", "I'm not the lead but here goes"); err == nil {
		t.Fatalf("expected wrong-sender rejection, got nil")
	}
	// Now the real recipient answers cleanly.
	if err := m.AnswerQuestion(q.ID, "sess-cap", "any of us with prod access"); err != nil {
		t.Fatalf("AnswerQuestion from real recipient failed: %v", err)
	}
	<-resCh
}

func TestAskQuestion_LateAnswer_Dropped(t *testing.T) {
	// The slot-pollution invariant: an answer that arrives after the
	// asker has timed out is dropped at the deliver site, not delivered
	// to whoever happens to be the next asker.
	asker := TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend", Tier: "worker"}
	lead := TeamMemberConfig{ID: "cap", Name: "Captain", Role: "Engineering Manager", Tier: "strategic"}
	m, store := newQuestionTestManager(t, asker, lead)

	// First round: ask, time out, then try to answer late.
	q, err := m.AskQuestion("sess-vex", "race-window question", "", 1*time.Second)
	if err != nil {
		t.Fatalf("AskQuestion failed: %v", err)
	}
	if q.Status != QuestionTimeout {
		t.Fatalf("expected timeout, got %s", q.Status)
	}
	// Late answer attempt: the question is no longer in pendingQuestions
	// (cleanupQuestion deleted it on AskQuestion's defer), so AnswerQuestion
	// must reject with "unknown question".
	if err := m.AnswerQuestion(q.ID, "sess-cap", "late but here is your answer"); err == nil {
		t.Fatalf("expected late-answer rejection, got nil")
	}
	// Sanity: the prompt history on the lead still shows we delivered a
	// question (so the late-answer test is checking the right thing).
	if len(store.promptsFor("sess-cap")) == 0 {
		t.Fatalf("expected the lead to have received the prompt")
	}
}

// getOnlyPendingQuestion fishes out the single pending question from the
// manager's registry. Used by tests that need the qid to call
// AnswerQuestion without race-y string parsing of the prompt.
func getOnlyPendingQuestion(t *testing.T, m *Manager) *Question {
	t.Helper()
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.pendingQuestions) != 1 {
		t.Fatalf("expected exactly 1 pending question, got %d", len(m.pendingQuestions))
	}
	for _, q := range m.pendingQuestions {
		return q
	}
	return nil
}
