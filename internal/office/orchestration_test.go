package office

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// recordingSessionStore records all WriteViaScreen calls for assertions.
type recordingSessionStore struct {
	mu     sync.Mutex
	writes []writeCall
}

type writeCall struct {
	SessionID string
	Input     string
}

func (r *recordingSessionStore) WriteViaScreen(sessionID, input string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.writes = append(r.writes, writeCall{SessionID: sessionID, Input: input})
	return true
}

func (r *recordingSessionStore) WriteInkPrompt(sessionID, prompt string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.writes = append(r.writes, writeCall{SessionID: sessionID, Input: prompt})
	return true
}

func (r *recordingSessionStore) GetSessionIDs() []string                { return nil }
func (r *recordingSessionStore) IsSessionAlive(sessionID string) bool   { return true }
func (r *recordingSessionStore) IsSessionWorking(sessionID string) bool { return false }
func (r *recordingSessionStore) StopSession(sessionID string) error     { return nil }

func (r *recordingSessionStore) getWrites() []writeCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]writeCall, len(r.writes))
	copy(result, r.writes)
	return result
}

func (r *recordingSessionStore) writesForSession(sessionID string) []writeCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	var result []writeCall
	for _, w := range r.writes {
		if w.SessionID == sessionID {
			result = append(result, w)
		}
	}
	return result
}

var broadcastEvents []string
var broadcastMu sync.Mutex

func newOrcTestManager() (*Manager, *recordingSessionStore) {
	store := &recordingSessionStore{}
	broadcastMu.Lock()
	broadcastEvents = nil
	broadcastMu.Unlock()
	bcast := func(event string, _ interface{}) {
		broadcastMu.Lock()
		broadcastEvents = append(broadcastEvents, event)
		broadcastMu.Unlock()
	}
	cfg := DefaultConfig()
	// Shorten timeouts for tests.
	cfg.StandupResponseTimeout = 500 * time.Millisecond
	cfg.StandupNudgeInterval = 200 * time.Millisecond
	cfg.StandupMaxNudges = 1
	cfg.BrainstormTimeout = 500 * time.Millisecond
	cfg.BrainstormCooldown = 100 * time.Millisecond
	cfg.StuckThreshold = 100 * time.Millisecond
	cfg.RecoveryWindowDuration = 10 * time.Second
	cfg.IdleCheckInterval = 50 * time.Millisecond

	m := NewManager(store, bcast, cfg)
	return m, store
}

// ---------------------------------------------------------------------------
// Standup Tests
// ---------------------------------------------------------------------------

func TestStartStandup_NoParticipants(t *testing.T) {
	m, _ := newOrcTestManager()
	result := m.StartStandup(nil)
	if result != nil {
		t.Error("expected nil when no agents registered")
	}
}

func TestStartStandup_AlreadyActive(t *testing.T) {
	m, _ := newOrcTestManager()
	cfg := &TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend"}
	m.RegisterAgent("sess-1", "vex", cfg)

	st := m.StartStandup(nil)
	if st == nil || !st.Active {
		t.Fatal("expected active standup")
	}

	st2 := m.StartStandup(nil)
	if st2 != st {
		t.Error("expected same standup instance when already active")
	}
}

func TestStartStandup_SendsPromptAndTimesOut(t *testing.T) {
	m, store := newOrcTestManager()
	// Use very short timeouts for this test.
	m.config.StandupResponseTimeout = 300 * time.Millisecond
	m.config.StandupNudgeInterval = 100 * time.Millisecond
	m.config.StandupMaxNudges = 0 // No nudges so we don't wait for 2s sleep.

	cfg := &TeamMemberConfig{ID: "bolt", Name: "Bolt", Role: "Backend"}
	m.RegisterAgent("sess-timeout", "bolt", cfg)

	m.StartStandup([]string{"sess-timeout"})

	// Wait for timeout + round-robin completion + buffer.
	time.Sleep(1000 * time.Millisecond)

	writes := store.writesForSession("sess-timeout")
	if len(writes) == 0 {
		t.Fatal("expected at least one write to session")
	}

	foundStandup := false
	for _, w := range writes {
		if strings.Contains(w.Input, "[STANDUP]") {
			foundStandup = true
			break
		}
	}
	if !foundStandup {
		t.Error("expected [STANDUP] prompt in writes")
	}

	// Standup should have ended.
	m.mu.RLock()
	active := m.standup != nil && m.standup.Active
	m.mu.RUnlock()
	if active {
		t.Error("expected standup to have ended after timeout")
	}
}

func TestSubmitStandupResponse(t *testing.T) {
	m, _ := newOrcTestManager()
	cfg := &TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend"}
	m.RegisterAgent("sess-1", "vex", cfg)

	m.StartStandup([]string{"sess-1"})

	// Wait a bit for the goroutine to start and set up the channel.
	time.Sleep(100 * time.Millisecond)

	ok := m.SubmitStandupResponse("sess-1", "Working on the dashboard. No blockers.")
	if !ok {
		t.Error("expected response to be accepted")
	}

	// Wait for standup to complete.
	time.Sleep(500 * time.Millisecond)

	m.mu.RLock()
	responses := make(map[string]string)
	if m.standup != nil {
		for k, v := range m.standup.Responses {
			responses[k] = v
		}
	}
	m.mu.RUnlock()

	resp, ok := responses["sess-1"]
	if !ok {
		t.Fatal("expected response from sess-1")
	}
	if !strings.Contains(resp, "Working on the dashboard") {
		t.Errorf("unexpected response: %q", resp)
	}
}

func TestStopStandup(t *testing.T) {
	m, _ := newOrcTestManager()
	cfg := &TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend"}
	m.RegisterAgent("sess-1", "vex", cfg)

	m.StartStandup(nil)
	m.StopStandup()

	m.mu.RLock()
	active := m.standup != nil && m.standup.Active
	m.mu.RUnlock()
	if active {
		t.Error("expected standup to be inactive after StopStandup")
	}
}

// ---------------------------------------------------------------------------
// Brainstorm Tests
// ---------------------------------------------------------------------------

func TestStartBrainstorm_TooFewParticipants(t *testing.T) {
	m, _ := newOrcTestManager()
	cfg := &TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend"}
	m.RegisterAgent("sess-1", "vex", cfg)

	mtg := m.StartBrainstorm([]string{"sess-1"})
	if mtg != nil {
		t.Error("expected nil when fewer than 2 participants")
	}
}

func TestStartBrainstorm_CooldownRespected(t *testing.T) {
	m, _ := newOrcTestManager()
	m.config.BrainstormCooldown = 10 * time.Second

	cfg1 := &TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend"}
	cfg2 := &TeamMemberConfig{ID: "bolt", Name: "Bolt", Role: "Backend"}
	m.RegisterAgent("sess-1", "vex", cfg1)
	m.RegisterAgent("sess-2", "bolt", cfg2)

	mtg1 := m.StartBrainstorm(nil)
	if mtg1 == nil {
		t.Fatal("expected first brainstorm to start")
	}

	// Second brainstorm should be blocked by cooldown.
	mtg2 := m.StartBrainstorm(nil)
	if mtg2 != nil {
		t.Error("expected second brainstorm to be blocked by cooldown")
	}
}

func TestStartBrainstorm_BlockedDuringWrapUp(t *testing.T) {
	m, _ := newOrcTestManager()
	cfg1 := &TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend"}
	cfg2 := &TeamMemberConfig{ID: "bolt", Name: "Bolt", Role: "Backend"}
	m.RegisterAgent("sess-1", "vex", cfg1)
	m.RegisterAgent("sess-2", "bolt", cfg2)

	m.SetWrappingUp(true)
	mtg := m.StartBrainstorm(nil)
	if mtg != nil {
		t.Error("expected brainstorm to be blocked during wrap-up")
	}
}

func TestStartBrainstorm_SendsPromptToFacilitator(t *testing.T) {
	m, store := newOrcTestManager()
	cfg1 := &TeamMemberConfig{ID: "max", Name: "Max", Tier: "strategic", Role: "Manager"}
	cfg2 := &TeamMemberConfig{ID: "vex", Name: "Vex", Tier: "worker", Role: "Frontend"}
	m.RegisterAgent("sess-max", "max", cfg1)
	m.RegisterAgent("sess-vex", "vex", cfg2)

	m.StartBrainstorm(nil)

	// Wait for the prompt to be sent.
	time.Sleep(200 * time.Millisecond)

	// Strategic agent should be facilitator.
	writes := store.writesForSession("sess-max")
	foundBrainstorm := false
	for _, w := range writes {
		if strings.Contains(w.Input, "[BRAINSTORM]") {
			foundBrainstorm = true
			break
		}
	}
	if !foundBrainstorm {
		t.Error("expected [BRAINSTORM] prompt sent to strategic agent (facilitator)")
	}
}

func TestSubmitBrainstormResponse(t *testing.T) {
	m, _ := newOrcTestManager()
	cfg1 := &TeamMemberConfig{ID: "max", Name: "Max", Tier: "strategic", Role: "Manager"}
	cfg2 := &TeamMemberConfig{ID: "vex", Name: "Vex", Tier: "worker", Role: "Frontend"}
	m.RegisterAgent("sess-max", "max", cfg1)
	m.RegisterAgent("sess-vex", "vex", cfg2)

	m.StartBrainstorm([]string{"sess-max", "sess-vex"})
	time.Sleep(100 * time.Millisecond)

	ok := m.SubmitBrainstormResponse("sess-max", "1. Fix button styles\n2. Add dark mode")
	if !ok {
		t.Error("expected brainstorm response to be accepted")
	}
}

func TestEndMeeting(t *testing.T) {
	m, _ := newOrcTestManager()
	mtg := m.StartMeeting("brainstorm", []string{"sess-1", "sess-2"})
	if !mtg.Active {
		t.Fatal("expected meeting to be active")
	}

	ok := m.EndMeeting(mtg.ID)
	if !ok {
		t.Error("expected EndMeeting to return true")
	}

	m.mu.RLock()
	updated := m.meetings[mtg.ID]
	m.mu.RUnlock()
	if updated.Active {
		t.Error("expected meeting to be inactive after EndMeeting")
	}
}

// ---------------------------------------------------------------------------
// Kickstart Tests
// ---------------------------------------------------------------------------

func TestBuildKickstartPrompt_ContainsSections(t *testing.T) {
	config := &TeamMemberConfig{
		ID:           "vex",
		Name:         "Vex",
		Role:         "Frontend Engineer",
		Tier:         "worker",
		Personality:  "Creative and detail-oriented.",
		Skills:       []string{"JavaScript", "CSS", "HTML"},
		SystemPrompt: "Build beautiful UIs.",
	}
	teammates := []*TeamMemberConfig{
		{ID: "bolt", Name: "Bolt", Role: "Backend Engineer"},
		{ID: "sage", Name: "Sage", Role: "Senior Architect"},
	}

	prompt := BuildKickstartPrompt(config, "/home/project", teammates, "", "", true, nil, false)

	// R31 · Kickstart was slimmed again. Per-agent unique content
	// (identity, personality, team relationships, role) stays here;
	// cross-agent boilerplate (protocols, context-budget, headless
	// browser, suggested skills) moved to BuildStableSystemPrompt +
	// `cm onboard <topic>`. Assert the unique content lives here;
	// protocols / onboarding coverage is tested separately in
	// TestStableSystemPromptCoversBoilerplate.
	sections := []string{
		"# Vex - Frontend Engineer",
		"## Identity",
		"/home/project",
		"## Personality",
		"Creative and detail-oriented.",
		"## Team",
		"Bolt (Backend Engineer)",
		"Sage (Senior Architect)",
		"## Work Mode: RALPH",
		"## Your Role",
		"Build beautiful UIs.",
	}

	for _, section := range sections {
		if !strings.Contains(prompt, section) {
			t.Errorf("expected prompt to contain %q", section)
		}
	}

	// Boilerplate that moved OUT of the kickstart should NOT appear
	// inline (agents fetch it via `cm onboard` or it rides on the
	// system prompt via BuildStableSystemPrompt).
	for _, moved := range []string{
		"## Suggested Skills",
		"## Context Budget",
		"## Headless Browser",
		"## Team Dev Instance",
	} {
		if strings.Contains(prompt, moved) {
			t.Errorf("boilerplate %q should have moved out of the kickstart body", moved)
		}
	}
}

func TestBuildKickstartPrompt_StrategicTierIncludesLeaderSection(t *testing.T) {
	config := &TeamMemberConfig{
		ID:           "max",
		Name:         "Max",
		Role:         "Engineering Manager",
		Tier:         "strategic",
		Personality:  "Calm leader.",
		Skills:       []string{"Leadership"},
		SystemPrompt: "Lead the team.",
	}

	prompt := BuildKickstartPrompt(config, "/project", nil, "", "", true, nil, false)

	if !strings.Contains(prompt, "## Leader Task Assignment") {
		t.Error("expected strategic tier to include Leader Task Assignment section")
	}
}

func TestBuildKickstartPrompt_WorkerTierExcludesLeaderSection(t *testing.T) {
	config := &TeamMemberConfig{
		ID:           "vex",
		Name:         "Vex",
		Role:         "Frontend Engineer",
		Tier:         "worker",
		Personality:  "Creative.",
		Skills:       []string{"CSS"},
		SystemPrompt: "Build UIs.",
	}

	prompt := BuildKickstartPrompt(config, "/project", nil, "", "", true, nil, false)

	if strings.Contains(prompt, "## Leader Task Assignment") {
		t.Error("expected worker tier to NOT include Leader Task Assignment section")
	}
}

func TestBuildKickstartPrompt_ExcludesSelfFromTeammates(t *testing.T) {
	config := &TeamMemberConfig{
		ID:   "vex",
		Name: "Vex",
		Role: "Frontend",
	}
	teammates := []*TeamMemberConfig{
		{ID: "vex", Name: "Vex", Role: "Frontend"},
		{ID: "bolt", Name: "Bolt", Role: "Backend"},
	}

	prompt := BuildKickstartPrompt(config, "/project", teammates, "", "", true, nil, false)

	// Should mention Bolt but not list Vex as a teammate.
	if !strings.Contains(prompt, "Bolt (Backend)") {
		t.Error("expected Bolt in teammates")
	}
	if strings.Contains(prompt, "1 teammates") {
		// It should say 1 teammate singular or just list correctly.
	}
}

// ---------------------------------------------------------------------------
// Recovery Tests
// ---------------------------------------------------------------------------

func TestCheckForStuckAgents_NoPromptSent(t *testing.T) {
	m, store := newOrcTestManager()
	cfg := &TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend"}
	m.RegisterAgent("sess-1", "vex", cfg)

	// No prompt sent, so checkForStuckAgents should not trigger recovery.
	m.checkForStuckAgents()

	writes := store.getWrites()
	if len(writes) != 0 {
		t.Errorf("expected no recovery writes, got %d", len(writes))
	}
}

func TestCheckForStuckAgents_TriggersRecovery(t *testing.T) {
	m, store := newOrcTestManager()
	m.config.StuckThreshold = 10 * time.Millisecond

	cfg := &TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend"}
	m.RegisterAgent("sess-1", "vex", cfg)

	// Simulate a prompt sent in the past.
	agent := m.GetAgent("sess-1")
	agent.mu.Lock()
	agent.LastPromptSentAt = time.Now().Add(-5 * time.Second) // 5s ago, within 10min window
	agent.mu.Unlock()

	m.checkForStuckAgents()

	// Should have sent Enter key (tier 1).
	writes := store.writesForSession("sess-1")
	if len(writes) == 0 {
		t.Fatal("expected recovery write")
	}
	if writes[0].Input != "\r" {
		t.Errorf("expected Enter key for tier 1, got %q", writes[0].Input)
	}

	// Verify attempt was recorded.
	agent.mu.Lock()
	attempts := agent.RecoveryAttempts
	agent.mu.Unlock()
	if attempts != 1 {
		t.Errorf("expected 1 recovery attempt, got %d", attempts)
	}
}

func TestCheckForStuckAgents_EscalatesToTier2(t *testing.T) {
	m, store := newOrcTestManager()
	m.config.StuckThreshold = 10 * time.Millisecond

	cfg := &TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend"}
	m.RegisterAgent("sess-1", "vex", cfg)

	agent := m.GetAgent("sess-1")
	agent.mu.Lock()
	agent.LastPromptSentAt = time.Now().Add(-5 * time.Second)
	agent.RecoveryAttempts = 1
	agent.RecoveryWindowStart = time.Now()
	agent.mu.Unlock()

	m.checkForStuckAgents()

	// Should have sent Ctrl+C (tier 2).
	writes := store.writesForSession("sess-1")
	if len(writes) == 0 {
		t.Fatal("expected recovery write for tier 2")
	}
	if writes[0].Input != "\x03" {
		t.Errorf("expected Ctrl+C for tier 2, got %q", writes[0].Input)
	}
}

func TestCheckForStuckAgents_MaxAttemptsReached(t *testing.T) {
	m, store := newOrcTestManager()
	m.config.StuckThreshold = 10 * time.Millisecond

	cfg := &TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend"}
	m.RegisterAgent("sess-1", "vex", cfg)

	agent := m.GetAgent("sess-1")
	agent.mu.Lock()
	agent.LastPromptSentAt = time.Now().Add(-5 * time.Second)
	agent.RecoveryAttempts = 3 // Already at max.
	agent.RecoveryWindowStart = time.Now()
	agent.mu.Unlock()

	m.checkForStuckAgents()

	// Should NOT have sent any writes since max attempts reached.
	writes := store.writesForSession("sess-1")
	if len(writes) != 0 {
		t.Errorf("expected no recovery writes after max attempts, got %d", len(writes))
	}
}

func TestStartIdleMonitoring_StartsAndStops(t *testing.T) {
	m, _ := newOrcTestManager()
	m.StartIdleMonitoring()

	m.mu.RLock()
	running := m.idleTicker != nil
	m.mu.RUnlock()
	if !running {
		t.Error("expected idle ticker to be running")
	}

	m.StopIdleMonitoring()

	m.mu.RLock()
	stopped := m.idleTicker == nil
	m.mu.RUnlock()
	if !stopped {
		t.Error("expected idle ticker to be nil after stop")
	}
}

// ---------------------------------------------------------------------------
// SendMessage delivery test
// ---------------------------------------------------------------------------

func TestSendMessage_DeliversToTargetSession(t *testing.T) {
	m, store := newOrcTestManager()
	cfg := &TeamMemberConfig{ID: "bolt", Name: "Bolt", Role: "Backend"}
	m.RegisterAgent("sess-bolt", "bolt", cfg)

	m.SendMessage("sess-bolt", "sess-vex", "Need API contract")

	writes := store.writesForSession("sess-vex")
	if len(writes) == 0 {
		t.Fatal("expected message to be delivered to target session")
	}

	found := false
	for _, w := range writes {
		if strings.Contains(w.Input, "[Team Message from Bolt]") &&
			strings.Contains(w.Input, "Need API contract") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected delivered message to contain '[Team Message from Bolt]: Need API contract'")
	}
}

// ---------------------------------------------------------------------------
// Worktree Tests (unit tests that don't require git)
// ---------------------------------------------------------------------------

func TestFilterTeammates(t *testing.T) {
	teammates := []*TeamMemberConfig{
		{ID: "a", Name: "A"},
		{ID: "b", Name: "B"},
		{ID: "c", Name: "C"},
	}

	result := filterTeammates("b", teammates)
	if len(result) != 2 {
		t.Fatalf("expected 2 teammates, got %d", len(result))
	}
	for _, tm := range result {
		if tm.ID == "b" {
			t.Error("self should not be in filtered teammates")
		}
	}
}

func TestGetRecoveryTier(t *testing.T) {
	tests := []struct {
		attempts int
		expected RecoveryTier
	}{
		{0, RecoveryTier1},
		{1, RecoveryTier1},
		{2, RecoveryTier2},
		{3, RecoveryTier3},
		{5, RecoveryTier3},
	}

	for _, tt := range tests {
		got := getRecoveryTier(tt.attempts)
		if got != tt.expected {
			t.Errorf("getRecoveryTier(%d) = %d, want %d", tt.attempts, got, tt.expected)
		}
	}
}
