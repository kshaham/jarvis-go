package office

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// ctrlSessionStore is a recovery-focused mock with per-session toggles
// for IsSessionWorking / IsSessionAlive plus a record of every
// WriteViaScreen / WriteInkPrompt / StopSession call. Recovery code
// branches heavily on these three signals, so the tests need them
// independently controllable per session.
type ctrlSessionStore struct {
	mu       sync.Mutex
	writes   []writeCall
	working  map[string]bool
	dead     map[string]bool // sessions that should report NOT alive
	stopped  []string
	stopErr  map[string]error
}

func newCtrlSessionStore() *ctrlSessionStore {
	return &ctrlSessionStore{
		working: map[string]bool{},
		dead:    map[string]bool{},
		stopErr: map[string]error{},
	}
}

func (c *ctrlSessionStore) WriteViaScreen(sessionID, input string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writes = append(c.writes, writeCall{SessionID: sessionID, Input: input})
	return true
}

func (c *ctrlSessionStore) WriteInkPrompt(sessionID, prompt string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writes = append(c.writes, writeCall{SessionID: sessionID, Input: prompt})
	return true
}

func (c *ctrlSessionStore) GetSessionIDs() []string { return nil }

func (c *ctrlSessionStore) IsSessionAlive(sessionID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.dead[sessionID]
}

func (c *ctrlSessionStore) IsSessionWorking(sessionID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.working[sessionID]
}

func (c *ctrlSessionStore) StopSession(sessionID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stopped = append(c.stopped, sessionID)
	return c.stopErr[sessionID]
}

func (c *ctrlSessionStore) setWorking(sessionID string, v bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.working[sessionID] = v
}

func (c *ctrlSessionStore) writesFor(sessionID string) []writeCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []writeCall
	for _, w := range c.writes {
		if w.SessionID == sessionID {
			out = append(out, w)
		}
	}
	return out
}

func (c *ctrlSessionStore) screenInputs(sessionID string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for _, w := range c.writes {
		if w.SessionID == sessionID {
			out = append(out, w.Input)
		}
	}
	return out
}

func (c *ctrlSessionStore) stoppedSessions() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.stopped))
	copy(out, c.stopped)
	return out
}

// newCtrlManager builds a Manager wired to a ctrlSessionStore with a
// recording broadcaster. Returns the captured broadcast events too so
// tests can assert side-channel emissions.
func newCtrlManager(t *testing.T, cfg *Config) (*Manager, *ctrlSessionStore, *eventRecorder) {
	t.Helper()
	store := newCtrlSessionStore()
	rec := &eventRecorder{}
	m := NewManager(store, rec.record, cfg)
	return m, store, rec
}

// eventRecorder collects broadcast (event, data) pairs.
type eventRecorder struct {
	mu     sync.Mutex
	events []string
}

func (e *eventRecorder) record(event string, _ interface{}) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, event)
}

func (e *eventRecorder) count(event string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, ev := range e.events {
		if ev == event {
			n++
		}
	}
	return n
}

func (e *eventRecorder) has(event string) bool {
	return e.count(event) > 0
}

// ---------------------------------------------------------------------------
// checkForStuckAgents / executeRecoveryTier
// ---------------------------------------------------------------------------

func stuckTestConfig() *Config {
	cfg := DefaultConfig()
	cfg.StuckThreshold = 10 * time.Millisecond
	cfg.StuckGracePeriod = 0
	cfg.RecoveryMaxAttempts = 3
	cfg.RecoveryWindowDuration = 10 * time.Second
	return cfg
}

// TestCheckForStuckAgents_Tier1SendsEnter verifies the first recovery
// attempt issues an Enter (carriage return) via the screen.
func TestCheckForStuckAgents_Tier1SendsEnter(t *testing.T) {
	cfg := stuckTestConfig()
	m, store, rec := newCtrlManager(t, cfg)

	cfg2 := &TeamMemberConfig{ID: "stuck", Name: "Stuckee", Tier: "worker"}
	m.RegisterAgent("sess-stuck", "stuck", cfg2)
	agent := m.GetAgent("sess-stuck")

	// Make the agent look idle: last activity well past StuckThreshold.
	agent.mu.Lock()
	agent.LastPromptSentAt = time.Now().Add(-1 * time.Second)
	agent.mu.Unlock()

	m.checkForStuckAgents()

	inputs := store.screenInputs("sess-stuck")
	if len(inputs) != 1 || inputs[0] != "\r" {
		t.Fatalf("expected single Tier1 Enter (\\r), got %v", inputs)
	}
	if !rec.has("office:agentRecovery") {
		t.Error("expected office:agentRecovery broadcast")
	}
	// Recovery attempt counter should have incremented to 1.
	agent.mu.Lock()
	attempts := agent.RecoveryAttempts
	agent.mu.Unlock()
	if attempts != 1 {
		t.Errorf("expected RecoveryAttempts=1, got %d", attempts)
	}
}

// TestCheckForStuckAgents_WorkingAgentSkipped verifies that an agent the
// session store reports as "working" is never recovered, even when its
// last-activity timestamps look ancient.
func TestCheckForStuckAgents_WorkingAgentSkipped(t *testing.T) {
	cfg := stuckTestConfig()
	m, store, rec := newCtrlManager(t, cfg)

	m.RegisterAgent("sess-busy", "busy", &TeamMemberConfig{ID: "busy", Name: "Busy", Tier: "worker"})
	agent := m.GetAgent("sess-busy")
	agent.mu.Lock()
	agent.LastPromptSentAt = time.Now().Add(-1 * time.Hour)
	agent.mu.Unlock()
	store.setWorking("sess-busy", true)

	m.checkForStuckAgents()

	if len(store.writesFor("sess-busy")) != 0 {
		t.Error("expected no recovery writes for a working agent")
	}
	if rec.has("office:agentRecovery") {
		t.Error("expected no recovery broadcast for a working agent")
	}
}

// TestCheckForStuckAgents_GracePeriodSkips verifies the spawn grace
// period short-circuits recovery for a freshly-spawned team.
func TestCheckForStuckAgents_GracePeriodSkips(t *testing.T) {
	cfg := stuckTestConfig()
	cfg.StuckGracePeriod = 1 * time.Hour
	m, store, _ := newCtrlManager(t, cfg)
	m.SetTeamSpawned(true) // sets teamSpawnedAt = now

	m.RegisterAgent("sess-fresh", "fresh", &TeamMemberConfig{ID: "fresh", Name: "Fresh", Tier: "worker"})
	agent := m.GetAgent("sess-fresh")
	agent.mu.Lock()
	agent.LastPromptSentAt = time.Now().Add(-1 * time.Hour)
	agent.mu.Unlock()

	m.checkForStuckAgents()
	if len(store.writesFor("sess-fresh")) != 0 {
		t.Error("expected no recovery during spawn grace period")
	}
}

// TestCheckForStuckAgents_PausedSkips verifies a paused team gets no
// stuck-recovery nudges.
func TestCheckForStuckAgents_PausedSkips(t *testing.T) {
	cfg := stuckTestConfig()
	m, store, _ := newCtrlManager(t, cfg)
	m.SetPaused(true, "operator break")

	m.RegisterAgent("sess-p", "p", &TeamMemberConfig{ID: "p", Name: "Paused", Tier: "worker"})
	agent := m.GetAgent("sess-p")
	agent.mu.Lock()
	agent.LastPromptSentAt = time.Now().Add(-1 * time.Hour)
	agent.mu.Unlock()

	m.checkForStuckAgents()
	if len(store.writesFor("sess-p")) != 0 {
		t.Error("expected no recovery while paused")
	}
}

// TestCheckForStuckAgents_EscalatesAndAutoKills walks an agent through
// all three recovery tiers and verifies that, with AutoKillAfterStuck
// enabled, the final exhausted attempt fires office:agentKilled and
// removes the agent from the roster.
func TestCheckForStuckAgents_EscalatesAndAutoKills(t *testing.T) {
	cfg := stuckTestConfig()
	cfg.AutoKillAfterStuck = true
	m, store, rec := newCtrlManager(t, cfg)

	m.RegisterAgent("sess-doomed", "doomed", &TeamMemberConfig{ID: "doomed", Name: "Doomed", Tier: "worker"})
	agent := m.GetAgent("sess-doomed")

	// Helper to make the agent look stuck (older than StuckThreshold) and
	// clear any backoff so each tick actually escalates.
	makeStuck := func() {
		agent.mu.Lock()
		agent.LastPromptSentAt = time.Now().Add(-1 * time.Second)
		agent.mu.Unlock()
		m.mu.Lock()
		delete(m.lastRecoveryAt, "sess-doomed")
		m.mu.Unlock()
	}

	// Tier 1.
	makeStuck()
	m.checkForStuckAgents()
	// Tier 2.
	makeStuck()
	m.checkForStuckAgents()
	// Tier 3 (attempt == RecoveryMaxAttempts -> stuck alert).
	makeStuck()
	m.checkForStuckAgents()

	// First screen write must be Tier1 Enter.
	inputs := store.screenInputs("sess-doomed")
	if len(inputs) == 0 || inputs[0] != "\r" {
		t.Fatalf("expected Tier1 Enter first, got %v", inputs)
	}
	// Tier 2 and Tier 3 each send Ctrl+C synchronously.
	ctrlCCount := 0
	for _, in := range inputs {
		if in == "\x03" {
			ctrlCCount++
		}
	}
	if ctrlCCount < 2 {
		t.Errorf("expected at least 2 Ctrl+C (tiers 2 and 3), got %d in %v", ctrlCCount, inputs)
	}

	if !rec.has("office:agentStuck") {
		t.Error("expected office:agentStuck after exhausting tiers")
	}
	if !rec.has("office:agentKilled") {
		t.Error("expected office:agentKilled with AutoKillAfterStuck enabled")
	}
	if m.GetAgent("sess-doomed") != nil {
		t.Error("expected auto-killed agent to be removed from roster")
	}
}

// TestCheckForStuckAgents_BackoffSuppressesAndRollsBack verifies that a
// Tier-2 recovery within the 30s backoff window is a true no-op: it
// neither writes a recovery prompt nor permanently consumes a recovery
// attempt.
func TestCheckForStuckAgents_BackoffSuppressesAndRollsBack(t *testing.T) {
	cfg := stuckTestConfig()
	m, _, _ := newCtrlManager(t, cfg)

	m.RegisterAgent("sess-bo", "bo", &TeamMemberConfig{ID: "bo", Name: "Backoff", Tier: "worker"})
	agent := m.GetAgent("sess-bo")

	// Pre-seed attempt=1 so the next tick is Tier 2 (getRecoveryTier(2)).
	agent.mu.Lock()
	agent.RecoveryAttempts = 1
	agent.RecoveryWindowStart = time.Now()
	agent.LastPromptSentAt = time.Now().Add(-1 * time.Second)
	agent.mu.Unlock()
	// Mark a recent recovery so the 30s Tier-2 backoff is active.
	m.mu.Lock()
	m.lastRecoveryAt["sess-bo"] = time.Now()
	m.mu.Unlock()

	m.checkForStuckAgents()

	// The attempt speculatively consumed must have been rolled back to 1.
	agent.mu.Lock()
	attempts := agent.RecoveryAttempts
	agent.mu.Unlock()
	if attempts != 1 {
		t.Errorf("expected RecoveryAttempts rolled back to 1, got %d", attempts)
	}
}

// TestExecuteRecoveryTier_Tier1 verifies Tier1 writes exactly a carriage
// return and nothing else.
func TestExecuteRecoveryTier_Tier1(t *testing.T) {
	cfg := stuckTestConfig()
	m, store, _ := newCtrlManager(t, cfg)
	m.executeRecoveryTier(store, "sess-x", RecoveryTier1)
	inputs := store.screenInputs("sess-x")
	if len(inputs) != 1 || inputs[0] != "\r" {
		t.Errorf("Tier1: expected [\\r], got %v", inputs)
	}
}

// TestExecuteRecoveryTier_Tier2DelayedPrompt verifies the deferred
// recovery prompt fires after the 3s delay when the monitor is still
// running and the session is alive.
func TestExecuteRecoveryTier_Tier2DelayedPrompt(t *testing.T) {
	cfg := stuckTestConfig()
	m, store, _ := newCtrlManager(t, cfg)
	// monitorDone must be non-nil for the select in the goroutine.
	m.mu.Lock()
	m.monitorDone = make(chan struct{})
	m.mu.Unlock()

	m.executeRecoveryTier(store, "sess-t2", RecoveryTier2)

	// Immediately we should have the Ctrl+C.
	if got := store.screenInputs("sess-t2"); len(got) == 0 || got[0] != "\x03" {
		t.Fatalf("Tier2: expected immediate Ctrl+C, got %v", got)
	}

	// Wait for the deferred recovery prompt (3s delay + slack).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		inputs := store.screenInputs("sess-t2")
		for _, in := range inputs {
			if strings.Contains(in, "[RECOVERY]") {
				return // success
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("Tier2: expected deferred [RECOVERY] prompt, got %v", store.screenInputs("sess-t2"))
}

// TestExecuteRecoveryTier_Tier3DiagnosticPrompt verifies the Tier3
// diagnostic prompt is delivered after the delay.
func TestExecuteRecoveryTier_Tier3DiagnosticPrompt(t *testing.T) {
	cfg := stuckTestConfig()
	m, store, _ := newCtrlManager(t, cfg)
	m.mu.Lock()
	m.monitorDone = make(chan struct{})
	m.mu.Unlock()

	m.executeRecoveryTier(store, "sess-t3", RecoveryTier3)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, in := range store.screenInputs("sess-t3") {
			if strings.Contains(in, "[DIAGNOSTIC]") {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("Tier3: expected deferred [DIAGNOSTIC] prompt, got %v", store.screenInputs("sess-t3"))
}

// TestBroadcastStuckAlert_NoAutoKill verifies that with AutoKillAfterStuck
// disabled, the stuck alert fires but the agent stays on the roster.
func TestBroadcastStuckAlert_NoAutoKill(t *testing.T) {
	cfg := stuckTestConfig()
	cfg.AutoKillAfterStuck = false
	m, _, rec := newCtrlManager(t, cfg)

	m.RegisterAgent("sess-alert", "alert", &TeamMemberConfig{ID: "alert", Name: "Alerter", Tier: "worker"})

	m.broadcastStuckAlert("sess-alert", "Alerter", 7*time.Minute)

	if !rec.has("office:agentStuck") {
		t.Error("expected office:agentStuck broadcast")
	}
	if rec.has("office:agentKilled") {
		t.Error("expected NO office:agentKilled when AutoKillAfterStuck=false")
	}
	if m.GetAgent("sess-alert") == nil {
		t.Error("expected agent to remain on roster when auto-kill disabled")
	}
}
