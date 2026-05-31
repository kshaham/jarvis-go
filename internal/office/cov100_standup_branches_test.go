package office

import (
	"strings"
	"testing"
	"time"
)

// TestStartStandup_WrappingUpReturnsNil verifies StartStandup short-circuits
// to nil while the team is wrapping up.
func TestStartStandup_WrappingUpReturnsNil(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.RegisterAgent("sess-a", "a", &TeamMemberConfig{ID: "a", Name: "A", Tier: "worker"})
	m.SetWrappingUp(true)

	if st := m.StartStandup(nil); st != nil {
		t.Error("expected nil standup while wrapping up")
	}
}

// TestRunStandupRoundRobin_NilStandupReturns verifies the goroutine body bails
// immediately if the captured standup pointer is nil (no live standup).
func TestRunStandupRoundRobin_NilStandupReturns(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m, store, _ := newCtrlManager(t, DefaultConfig())
	m.RegisterAgent("sess-a", "a", &TeamMemberConfig{ID: "a", Name: "A", Tier: "worker"})

	// m.standup is nil here, so the captured st is nil -> immediate return.
	m.runStandupRoundRobin("ghost-id", []string{"sess-a"}, store, m.config)

	// No standup messages should have been broadcast and no prompts written.
	if len(store.writesFor("sess-a")) != 0 {
		t.Error("expected no prompts when there is no live standup")
	}
}

// failingPromptStore is a recordingSessionStore whose WriteInkPrompt reports
// failure (returns false) so the standup "delivery failed" branch runs.
type failingPromptStore struct {
	recordingSessionStore
}

func (f *failingPromptStore) WriteInkPrompt(sessionID, prompt string) bool {
	f.recordingSessionStore.WriteInkPrompt(sessionID, prompt) // still record it
	return false                                              // report failure
}

// TestRunStandupRoundRobin_PromptDeliveryFails drives the `!sent` branch where
// WriteInkPrompt returns false. We arm a live standup and a store that always
// reports a failed delivery.
func TestRunStandupRoundRobin_PromptDeliveryFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store := &failingPromptStore{}
	cfg := DefaultConfig()
	cfg.StandupResponseTimeout = 30 * time.Millisecond
	cfg.StandupNudgeInterval = 20 * time.Millisecond
	cfg.StandupMaxNudges = 0
	m := NewManager(store, func(string, interface{}) {}, cfg)

	m.RegisterAgent("sess-fp", "fp", &TeamMemberConfig{ID: "fp", Name: "FP", Tier: "worker"})

	// Arm a live standup that runStandupRoundRobin will own.
	m.mu.Lock()
	m.standup = &StandupState{
		ID:           "live-standup",
		Active:       true,
		Participants: []string{"sess-fp"},
		Responses:    map[string]string{},
		Transcript:   []StandupMessage{},
		StartedAt:    time.Now(),
	}
	m.mu.Unlock()

	m.runStandupRoundRobin("live-standup", []string{"sess-fp"}, store, cfg)

	// The prompt was still attempted (recorded) even though delivery "failed".
	found := false
	for _, w := range store.writesForSession("sess-fp") {
		if strings.Contains(w.Input, "[STANDUP]") {
			found = true
		}
	}
	if !found {
		t.Error("expected a [STANDUP] prompt write attempt despite delivery failure")
	}
}

// TestRunStandupRoundRobin_NilSessions drives the `sessions == nil` skip-prompt
// branch: the round-robin still runs but logs that the prompt was skipped.
func TestRunStandupRoundRobin_NilSessions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := DefaultConfig()
	cfg.StandupResponseTimeout = 30 * time.Millisecond
	cfg.StandupNudgeInterval = 20 * time.Millisecond
	cfg.StandupMaxNudges = 0
	m, _, _ := newCtrlManager(t, cfg)
	m.RegisterAgent("sess-ns", "ns", &TeamMemberConfig{ID: "ns", Name: "NS", Tier: "worker"})

	m.mu.Lock()
	m.standup = &StandupState{
		ID:           "ns-standup",
		Active:       true,
		Participants: []string{"sess-ns"},
		Responses:    map[string]string{},
		Transcript:   []StandupMessage{},
		StartedAt:    time.Now(),
	}
	m.mu.Unlock()

	// Pass nil sessions explicitly so the sessions==nil branch runs.
	m.runStandupRoundRobin("ns-standup", []string{"sess-ns"}, nil, cfg)

	// The standup should still have recorded a (timed-out) response.
	m.mu.RLock()
	resp, ok := m.standup.Responses["sess-ns"]
	m.mu.RUnlock()
	if !ok {
		t.Error("expected a recorded response even with nil sessions")
	}
	_ = resp
}

// TestRunStandupRoundRobin_CrossContextIncluded seeds a teammate's learnings so
// BuildCrossAgentContext returns non-empty, exercising the crossContext != ""
// branch that threads peer context into the standup prompt.
func TestRunStandupRoundRobin_CrossContextIncluded(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_, cleanup := withTempLearningsDir(t)
	defer cleanup()

	cfg := DefaultConfig()
	cfg.StandupResponseTimeout = 30 * time.Millisecond
	cfg.StandupNudgeInterval = 20 * time.Millisecond
	cfg.StandupMaxNudges = 0
	store := &recordingSessionStore{}
	m := NewManager(store, func(string, interface{}) {}, cfg)

	m.RegisterAgent("sess-cc", "cc", &TeamMemberConfig{ID: "cc", Name: "CC", Tier: "worker"})

	// Seed a DIFFERENT peer's learning so BuildCrossAgentContext("cc")
	// returns that peer's context (excludes "cc" itself).
	if err := AddLearningEntry("peer", "technique", "always use idempotent writes when persisting", "t1", "Persist"); err != nil {
		t.Fatalf("seed learning: %v", err)
	}

	// Sanity: cross-context for cc should be non-empty.
	if BuildCrossAgentContext("cc") == "" {
		t.Skip("cross-agent context unexpectedly empty; learnings dir not wired")
	}

	m.mu.Lock()
	m.standup = &StandupState{
		ID:           "cc-standup",
		Active:       true,
		Participants: []string{"sess-cc"},
		Responses:    map[string]string{},
		Transcript:   []StandupMessage{},
		StartedAt:    time.Now(),
	}
	m.mu.Unlock()

	m.runStandupRoundRobin("cc-standup", []string{"sess-cc"}, store, cfg)

	// The standup prompt should embed the peer's learning context.
	found := false
	for _, w := range store.writesForSession("sess-cc") {
		if strings.Contains(w.Input, "[STANDUP]") && strings.Contains(w.Input, "idempotent writes") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected cross-agent context in standup prompt, writes=%v", store.writesForSession("sess-cc"))
	}
}

// standupCompleteNotifier is a TelegramNotifier that records the
// NotifyStandupComplete payload.
type standupCompleteNotifier struct {
	connected bool
	summaries []string
}

func (s *standupCompleteNotifier) SendOfficeUpdate(string)  {}
func (s *standupCompleteNotifier) IsConnected() bool        { return s.connected }
func (s *standupCompleteNotifier) NotifyStandupComplete(summary string) {
	s.summaries = append(s.summaries, summary)
}

// TestRunStandupRoundRobin_NotifiesTelegramOnComplete drives the connected-
// notifier branch at standup completion: a single-participant standup runs to
// completion and pushes a Telegram summary.
func TestRunStandupRoundRobin_NotifiesTelegramOnComplete(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := DefaultConfig()
	cfg.StandupResponseTimeout = 30 * time.Millisecond
	cfg.StandupNudgeInterval = 20 * time.Millisecond
	cfg.StandupMaxNudges = 0
	store := &recordingSessionStore{}
	m := NewManager(store, func(string, interface{}) {}, cfg)

	notifier := &standupCompleteNotifier{connected: true}
	m.SetTelegramNotifier(notifier)

	m.RegisterAgent("sess-tg", "tg", &TeamMemberConfig{ID: "tg", Name: "TG", Tier: "worker"})

	m.mu.Lock()
	m.standup = &StandupState{
		ID:           "tg-standup",
		Active:       true,
		Participants: []string{"sess-tg"},
		Responses:    map[string]string{},
		Transcript:   []StandupMessage{},
		StartedAt:    time.Now(),
	}
	m.mu.Unlock()

	m.runStandupRoundRobin("tg-standup", []string{"sess-tg"}, store, cfg)

	if len(notifier.summaries) == 0 {
		t.Error("expected NotifyStandupComplete to be called on a connected notifier")
	}
}
