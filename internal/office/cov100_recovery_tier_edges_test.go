package office

import (
	"strings"
	"testing"
	"time"
)

// setDead marks a session as not-alive on the ctrlSessionStore.
func (c *ctrlSessionStore) setDead(sessionID string, v bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dead[sessionID] = v
}

// TestExecuteRecoveryTier_Tier2MonitorStops drives the
// `case <-monitorDone: return` arm inside the Tier2 deferred goroutine.
// We close monitorDone BEFORE calling executeRecoveryTier so the select
// resolves to the monitor-stopped branch immediately and the recovery
// prompt is never written.
func TestExecuteRecoveryTier_Tier2MonitorStops(t *testing.T) {
	cfg := stuckTestConfig()
	m, store, _ := newCtrlManager(t, cfg)

	done := make(chan struct{})
	close(done) // monitor already stopped
	m.mu.Lock()
	m.monitorDone = done
	m.mu.Unlock()

	m.executeRecoveryTier(store, "sess-mon2", RecoveryTier2)

	// Ctrl+C is sent synchronously before the goroutine.
	if got := store.screenInputs("sess-mon2"); len(got) == 0 || got[0] != "\x03" {
		t.Fatalf("Tier2: expected immediate Ctrl+C, got %v", got)
	}

	// Give the goroutine ample time; the [RECOVERY] prompt must NEVER
	// arrive because the monitor-stopped branch returns first.
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		for _, in := range store.screenInputs("sess-mon2") {
			if strings.Contains(in, "[RECOVERY]") {
				t.Fatalf("Tier2: recovery prompt fired despite stopped monitor: %v",
					store.screenInputs("sess-mon2"))
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Only the Ctrl+C should be present.
	if got := store.screenInputs("sess-mon2"); len(got) != 1 {
		t.Errorf("Tier2: expected only Ctrl+C, got %v", got)
	}
}

// TestExecuteRecoveryTier_Tier3MonitorStops mirrors the above for the
// Tier3 diagnostic goroutine.
func TestExecuteRecoveryTier_Tier3MonitorStops(t *testing.T) {
	cfg := stuckTestConfig()
	m, store, _ := newCtrlManager(t, cfg)

	done := make(chan struct{})
	close(done)
	m.mu.Lock()
	m.monitorDone = done
	m.mu.Unlock()

	m.executeRecoveryTier(store, "sess-mon3", RecoveryTier3)

	if got := store.screenInputs("sess-mon3"); len(got) == 0 || got[0] != "\x03" {
		t.Fatalf("Tier3: expected immediate Ctrl+C, got %v", got)
	}

	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		for _, in := range store.screenInputs("sess-mon3") {
			if strings.Contains(in, "[DIAGNOSTIC]") {
				t.Fatalf("Tier3: diagnostic prompt fired despite stopped monitor: %v",
					store.screenInputs("sess-mon3"))
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := store.screenInputs("sess-mon3"); len(got) != 1 {
		t.Errorf("Tier3: expected only Ctrl+C, got %v", got)
	}
}

// TestExecuteRecoveryTier_Tier2DeadSession drives the
// `!sessions.IsSessionAlive(...) -> return` arm: the timer fires (monitor
// still running) but the session has died, so the recovery prompt is
// skipped. We compress the wait by relying on the 3s timer; assert the
// prompt is absent after the timer would have fired.
func TestExecuteRecoveryTier_Tier2DeadSession(t *testing.T) {
	if testing.Short() {
		t.Skip("3s timer wait skipped in -short")
	}
	cfg := stuckTestConfig()
	m, store, _ := newCtrlManager(t, cfg)
	m.mu.Lock()
	m.monitorDone = make(chan struct{}) // open: timer wins the select
	m.mu.Unlock()
	store.setDead("sess-dead2", true)

	m.executeRecoveryTier(store, "sess-dead2", RecoveryTier2)

	// Wait past the 3s internal timer plus slack, then confirm no prompt.
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		for _, in := range store.screenInputs("sess-dead2") {
			if strings.Contains(in, "[RECOVERY]") {
				t.Fatalf("Tier2: recovery prompt sent to a dead session: %v",
					store.screenInputs("sess-dead2"))
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	if got := store.screenInputs("sess-dead2"); len(got) != 1 || got[0] != "\x03" {
		t.Errorf("Tier2 dead session: expected only Ctrl+C, got %v", got)
	}
}

// TestExecuteRecoveryTier_Tier3DeadSession mirrors the dead-session skip
// for the Tier3 diagnostic goroutine.
func TestExecuteRecoveryTier_Tier3DeadSession(t *testing.T) {
	if testing.Short() {
		t.Skip("3s timer wait skipped in -short")
	}
	cfg := stuckTestConfig()
	m, store, _ := newCtrlManager(t, cfg)
	m.mu.Lock()
	m.monitorDone = make(chan struct{})
	m.mu.Unlock()
	store.setDead("sess-dead3", true)

	m.executeRecoveryTier(store, "sess-dead3", RecoveryTier3)

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		for _, in := range store.screenInputs("sess-dead3") {
			if strings.Contains(in, "[DIAGNOSTIC]") {
				t.Fatalf("Tier3: diagnostic prompt sent to a dead session: %v",
					store.screenInputs("sess-dead3"))
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	if got := store.screenInputs("sess-dead3"); len(got) != 1 || got[0] != "\x03" {
		t.Errorf("Tier3 dead session: expected only Ctrl+C, got %v", got)
	}
}
