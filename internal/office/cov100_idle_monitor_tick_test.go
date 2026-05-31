package office

import (
	"strings"
	"sync/atomic"
	"testing"
)

// TestRunIdleMonitorTick_RecoversPanic injects a panicking monitor sweep and
// verifies the tick's defer/recover swallows it (the goroutine survives) --
// the function returns normally instead of propagating the panic.
func TestRunIdleMonitorTick_RecoversPanic(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m, _, _ := newCtrlManager(t, DefaultConfig())

	var called int32
	m.monitorChecks = func() {
		atomic.AddInt32(&called, 1)
		panic("boom from a monitoring sweep")
	}

	// Must not panic out of the tick.
	m.runIdleMonitorTick(1)

	if atomic.LoadInt32(&called) != 1 {
		t.Fatalf("expected injected monitor sweep to run once, got %d", called)
	}
}

// TestRunIdleMonitorTick_BroadcastsTickerMessageEveryFifthTick verifies the
// tickCount%5==0 broadcast fires and counts agents with active tasks via the
// active++ branch.
func TestRunIdleMonitorTick_BroadcastsTickerMessageEveryFifthTick(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m, _, _ := newCtrlManager(t, DefaultConfig())

	// Replace the heavy sweep with a no-op so the tick is fast + deterministic.
	m.monitorChecks = func() {}

	// Capture tickerMessage broadcasts with their payloads.
	var lastMsg string
	var tickerCount int32
	m.broadcast = func(event string, data interface{}) {
		if event != "office:tickerMessage" {
			return
		}
		atomic.AddInt32(&tickerCount, 1)
		if payload, ok := data.(map[string]interface{}); ok {
			if msg, ok := payload["message"].(string); ok {
				lastMsg = msg
			}
		}
	}

	// One agent with an active task, one idle -> "1/2 agents active".
	m.RegisterAgent("sess-active", "active", &TeamMemberConfig{ID: "active", Name: "Active", Tier: "worker"})
	m.RegisterAgent("sess-idle", "idle", &TeamMemberConfig{ID: "idle", Name: "Idle", Tier: "worker"})
	m.GetAgent("sess-active").AddTask(&Task{ID: "t1", Title: "running", Status: TaskInProgress})

	// Ticks 1-4 must NOT broadcast; tick 5 must.
	for i := 1; i <= 4; i++ {
		m.runIdleMonitorTick(i)
	}
	if atomic.LoadInt32(&tickerCount) != 0 {
		t.Fatalf("expected no ticker broadcast before the 5th tick, got %d", tickerCount)
	}

	m.runIdleMonitorTick(5)
	if atomic.LoadInt32(&tickerCount) != 1 {
		t.Fatalf("expected one ticker broadcast on the 5th tick, got %d", tickerCount)
	}
	if !strings.Contains(lastMsg, "1/2 agents active") {
		t.Errorf("expected '1/2 agents active' (active++ branch), got %q", lastMsg)
	}
}

// TestRunIdleMonitorTick_DefaultSweepRuns verifies the production seam path:
// with monitorChecks nil, defaultMonitorChecks executes the real sweep without
// panicking on a freshly-built manager with no agents.
func TestRunIdleMonitorTick_DefaultSweepRuns(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m, _, _ := newCtrlManager(t, DefaultConfig())
	// monitorChecks left nil -> defaultMonitorChecks path.
	m.runIdleMonitorTick(1)
	// No assertion beyond "did not panic / deadlock"; the sweep is a series
	// of guarded no-ops on an empty roster.
}
