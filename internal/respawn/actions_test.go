package respawn

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockSessionWriter records all writes for assertion.
type mockSessionWriter struct {
	mu     sync.Mutex
	writes []writeRecord
	fail   bool
}

type writeRecord struct {
	SessionID string
	Text      string
}

func (m *mockSessionWriter) WriteToSession(sessionID, text string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return false
	}
	m.writes = append(m.writes, writeRecord{SessionID: sessionID, Text: text})
	return true
}

func (m *mockSessionWriter) getWrites() []writeRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]writeRecord, len(m.writes))
	copy(result, m.writes)
	return result
}

func (m *mockSessionWriter) setFail(fail bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fail = fail
}

// drainEvents reads all available events from the channel without blocking.
func drainEvents(ch <-chan RespawnEvent, timeout time.Duration) []RespawnEvent {
	var events []RespawnEvent
	deadline := time.After(timeout)
	for {
		select {
		case event := <-ch:
			events = append(events, event)
		case <-deadline:
			return events
		}
	}
}

// waitForState polls the controller until it reaches the desired state or times out.
func waitForState(rc *Controller, desired RespawnState, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if rc.State() == desired {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// fastConfig returns a config with very short delays for testing.
func fastConfig() RespawnConfig {
	cfg := DefaultRespawnConfig()
	cfg.ConfidenceThreshold = 50
	cfg.ConfirmationDelayMs = 50
	cfg.StepDelayMs = 50
	cfg.StuckDetectionMs = 500
	cfg.StuckRecoveryMs = 1000
	cfg.MaxStuckRecoveries = 2
	cfg.SendClear = true
	cfg.SendInit = true
	cfg.KickstartPrompt = ""
	return cfg
}

func TestFullCycle_UpdateClearInitWatching(t *testing.T) {
	cfg := fastConfig()
	rc := NewController("test-session", cfg)
	writer := &mockSessionWriter{}
	rc.SetSessionWriter(writer)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := rc.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer rc.Stop()

	// Trigger idle detection via stop hook (confidence=100 > threshold=50)
	rc.OnStopHook()

	// Should transition: Watching -> ConfirmingIdle -> SendingUpdate -> WaitingUpdate
	// -> SendingClear -> WaitingClear -> SendingInit -> WaitingInit -> Watching
	if !waitForState(rc, StateWatching, 5*time.Second) {
		t.Fatalf("did not return to Watching; stuck in %s", rc.State())
	}

	// Verify writes happened
	writes := writer.getWrites()
	if len(writes) < 3 {
		t.Fatalf("expected at least 3 writes (update, clear, init), got %d", len(writes))
	}

	// First write should be the update prompt
	if writes[0].Text != cfg.UpdatePrompt+"\r" {
		t.Errorf("first write = %q, want update prompt", writes[0].Text)
	}
	// Second write should be /clear
	if writes[1].Text != ClearCommand+"\r" {
		t.Errorf("second write = %q, want /clear", writes[1].Text)
	}
	// Third write should be /init
	if writes[2].Text != InitCommand+"\r" {
		t.Errorf("third write = %q, want /init", writes[2].Text)
	}

	// Cycle count should be 1
	if rc.CycleCount() != 1 {
		t.Errorf("expected 1 cycle, got %d", rc.CycleCount())
	}
}

func TestFullCycle_SkipClear(t *testing.T) {
	cfg := fastConfig()
	cfg.SendClear = false
	cfg.SendInit = true
	rc := NewController("test-session", cfg)
	writer := &mockSessionWriter{}
	rc.SetSessionWriter(writer)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := rc.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer rc.Stop()

	rc.OnStopHook()

	if !waitForState(rc, StateWatching, 5*time.Second) {
		t.Fatalf("did not return to Watching; stuck in %s", rc.State())
	}

	writes := writer.getWrites()
	if len(writes) < 2 {
		t.Fatalf("expected at least 2 writes (update, init), got %d", len(writes))
	}

	// Should skip /clear
	for _, w := range writes {
		if w.Text == ClearCommand+"\r" {
			t.Error("should not have sent /clear when SendClear=false")
		}
	}
}

func TestFullCycle_SkipClearAndInit(t *testing.T) {
	cfg := fastConfig()
	cfg.SendClear = false
	cfg.SendInit = false
	rc := NewController("test-session", cfg)
	writer := &mockSessionWriter{}
	rc.SetSessionWriter(writer)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := rc.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer rc.Stop()

	rc.OnStopHook()

	if !waitForState(rc, StateWatching, 5*time.Second) {
		t.Fatalf("did not return to Watching; stuck in %s", rc.State())
	}

	writes := writer.getWrites()
	if len(writes) != 1 {
		t.Fatalf("expected 1 write (update only), got %d", len(writes))
	}

	if writes[0].Text != cfg.UpdatePrompt+"\r" {
		t.Errorf("write = %q, want update prompt", writes[0].Text)
	}
}

func TestFullCycle_WithKickstart(t *testing.T) {
	cfg := fastConfig()
	cfg.KickstartPrompt = "continue working on the task"
	cfg.StuckDetectionMs = 10000 // high to avoid interfering with 3s monitoring
	cfg.StuckRecoveryMs = 20000
	rc := NewController("test-session", cfg)
	writer := &mockSessionWriter{}
	rc.SetSessionWriter(writer)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := rc.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer rc.Stop()

	rc.OnStopHook()

	if !waitForState(rc, StateWatching, 5*time.Second) {
		t.Fatalf("did not return to Watching; stuck in %s", rc.State())
	}

	writes := writer.getWrites()
	if len(writes) < 4 {
		t.Fatalf("expected at least 4 writes (update, clear, init, kickstart), got %d", len(writes))
	}

	// Last write should be the kickstart prompt
	lastWrite := writes[len(writes)-1]
	if lastWrite.Text != cfg.KickstartPrompt+"\r" {
		t.Errorf("last write = %q, want kickstart prompt", lastWrite.Text)
	}
}

func TestSendFailure_ReturnsToWatching(t *testing.T) {
	cfg := fastConfig()
	rc := NewController("test-session", cfg)
	writer := &mockSessionWriter{fail: true}
	rc.SetSessionWriter(writer)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := rc.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer rc.Stop()

	rc.OnStopHook()

	// Should end up back in Watching because send fails
	if !waitForState(rc, StateWatching, 5*time.Second) {
		t.Fatalf("did not return to Watching after send failure; stuck in %s", rc.State())
	}
}

func TestNoWriter_EmitsError(t *testing.T) {
	cfg := fastConfig()
	rc := NewController("test-session", cfg)
	// Do NOT set a session writer

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := rc.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer rc.Stop()

	rc.OnStopHook()

	// Should end up back in Watching because send fails (no writer)
	if !waitForState(rc, StateWatching, 5*time.Second) {
		t.Fatalf("did not return to Watching without writer; stuck in %s", rc.State())
	}

	// Check that we got error events
	events := drainEvents(rc.Events(), 200*time.Millisecond)
	hasError := false
	for _, event := range events {
		if _, ok := event.(ErrorEvent); ok {
			hasError = true
			break
		}
	}
	if !hasError {
		t.Error("expected ErrorEvent when no session writer configured")
	}
}

func TestWorkingDetected_CancelsConfirmation(t *testing.T) {
	cfg := fastConfig()
	cfg.ConfirmationDelayMs = 500 // longer so we can cancel it
	rc := NewController("test-session", cfg)
	writer := &mockSessionWriter{}
	rc.SetSessionWriter(writer)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := rc.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer rc.Stop()

	// Trigger idle detection
	rc.OnStopHook()

	// Wait briefly for confirming idle
	time.Sleep(50 * time.Millisecond)
	if rc.State() != StateConfirmingIdle {
		t.Fatalf("expected ConfirmingIdle, got %s", rc.State())
	}

	// Working detected should cancel and return to watching
	rc.OnWorkingDetected("spinner detected")

	if rc.State() != StateWatching {
		t.Errorf("expected Watching after working detected, got %s", rc.State())
	}

	// No writes should have happened
	writes := writer.getWrites()
	if len(writes) != 0 {
		t.Errorf("expected 0 writes, got %d", len(writes))
	}
}

func TestMultipleCycles(t *testing.T) {
	cfg := fastConfig()
	cfg.SendClear = false
	cfg.SendInit = false
	rc := NewController("test-session", cfg)
	writer := &mockSessionWriter{}
	rc.SetSessionWriter(writer)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := rc.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer rc.Stop()

	// Run 3 cycles
	for i := 0; i < 3; i++ {
		rc.OnStopHook()
		if !waitForState(rc, StateWatching, 5*time.Second) {
			t.Fatalf("cycle %d: did not return to Watching; stuck in %s", i+1, rc.State())
		}
		// Drain events between cycles
		drainEvents(rc.Events(), 100*time.Millisecond)
	}

	if rc.CycleCount() != 3 {
		t.Errorf("expected 3 cycles, got %d", rc.CycleCount())
	}
}

func TestStuckDetection_WarningAndRecovery(t *testing.T) {
	cfg := fastConfig()
	cfg.ConfirmationDelayMs = 30000 // very long so we stay in confirming_idle
	cfg.StuckDetectionMs = 200       // warn after 200ms
	cfg.StuckRecoveryMs = 500        // recover after 500ms
	cfg.MaxStuckRecoveries = 1
	rc := NewController("test-session", cfg)
	writer := &mockSessionWriter{}
	rc.SetSessionWriter(writer)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := rc.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer rc.Stop()

	// Trigger idle detection to get to ConfirmingIdle
	rc.OnStopHook()

	time.Sleep(100 * time.Millisecond)
	if rc.State() != StateConfirmingIdle {
		t.Fatalf("expected ConfirmingIdle, got %s", rc.State())
	}

	// Wait for stuck detection to trigger recovery (returns to Watching)
	// Give enough time for stuck detector polls (interval = 200ms/4 = 50ms)
	if !waitForState(rc, StateWatching, 5*time.Second) {
		t.Fatalf("stuck recovery did not return to Watching; stuck in %s", rc.State())
	}

	// Check events for stuck warning and recovery
	events := drainEvents(rc.Events(), 500*time.Millisecond)
	hasWarning := false
	hasRecovery := false
	for _, event := range events {
		switch event.(type) {
		case StuckWarningEvent:
			hasWarning = true
		case StuckRecoveryEvent:
			hasRecovery = true
		}
	}
	if !hasWarning {
		t.Error("expected StuckWarningEvent")
	}
	if !hasRecovery {
		t.Error("expected StuckRecoveryEvent")
	}
}

func TestSignalAggregation_MultipleSignals(t *testing.T) {
	cfg := fastConfig()
	cfg.ConfidenceThreshold = 80 // high threshold
	rc := NewController("test-session", cfg)
	writer := &mockSessionWriter{}
	rc.SetSessionWriter(writer)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := rc.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer rc.Stop()

	// Single signal: completion message (weight 40) < 80 threshold
	rc.OnCompletionMessage("Worked for 2m 46s")
	time.Sleep(50 * time.Millisecond)
	if rc.State() != StateWatching {
		t.Errorf("40 confidence should not trigger at threshold 80, state = %s", rc.State())
	}

	// Add output silence (weight 25) -> 65 < 80
	rc.OnOutputSilence(10 * time.Second)
	time.Sleep(50 * time.Millisecond)
	if rc.State() != StateWatching {
		t.Errorf("65 confidence should not trigger at threshold 80, state = %s", rc.State())
	}

	// Add token stability (weight 20) -> 85 > 80
	rc.OnTokenStability()
	time.Sleep(50 * time.Millisecond)
	if rc.State() == StateWatching {
		t.Error("85 confidence should trigger at threshold 80")
	}
}

func TestCycleEvents_EmittedCorrectly(t *testing.T) {
	cfg := fastConfig()
	cfg.SendClear = false
	cfg.SendInit = false
	rc := NewController("test-session", cfg)
	writer := &mockSessionWriter{}
	rc.SetSessionWriter(writer)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := rc.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer rc.Stop()

	// Drain initial state change event
	drainEvents(rc.Events(), 100*time.Millisecond)

	rc.OnStopHook()

	if !waitForState(rc, StateWatching, 5*time.Second) {
		t.Fatalf("did not return to Watching; stuck in %s", rc.State())
	}

	events := drainEvents(rc.Events(), 500*time.Millisecond)

	hasIdleDetected := false
	hasStepSent := false
	hasCycleComplete := false
	stateChanges := 0

	for _, event := range events {
		switch event.(type) {
		case IdleDetectedEvent:
			hasIdleDetected = true
		case StepSentEvent:
			hasStepSent = true
		case CycleCompleteEvent:
			hasCycleComplete = true
		case StateChangedEvent:
			stateChanges++
		}
	}

	if !hasIdleDetected {
		t.Error("expected IdleDetectedEvent")
	}
	if !hasStepSent {
		t.Error("expected StepSentEvent")
	}
	if !hasCycleComplete {
		t.Error("expected CycleCompleteEvent")
	}
	if stateChanges < 3 {
		t.Errorf("expected at least 3 state changes, got %d", stateChanges)
	}
}

func TestConcurrentSignals(t *testing.T) {
	cfg := fastConfig()
	cfg.ConfidenceThreshold = 80
	rc := NewController("test-session", cfg)
	writer := &mockSessionWriter{}
	rc.SetSessionWriter(writer)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := rc.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer rc.Stop()

	// Send signals concurrently -- should not panic or deadlock
	var wg sync.WaitGroup
	var done atomic.Bool
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !done.Load() {
				rc.OnCompletionMessage("Worked for 1s")
				rc.OnOutputSilence(5 * time.Second)
				rc.OnTokenStability()
				rc.OnWorkingDetected("test")
				time.Sleep(5 * time.Millisecond)
			}
		}()
	}

	time.Sleep(200 * time.Millisecond)
	done.Store(true)
	wg.Wait()

	// Just verify we didn't deadlock or panic
	state := rc.State()
	t.Logf("final state after concurrent signals: %s", state)
}
