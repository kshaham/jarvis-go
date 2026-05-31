package subagent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// These tests mutate the package-level scanInterval/staleCleanupInterval vars.
// They must NOT run in parallel with each other or with other watcher tests.

// TestWatcher_ScanLoopTickerFires drives the ticker.C branch of scanLoop:
// a fixture written AFTER the initial scan is picked up on a subsequent tick.
func TestWatcher_ScanLoopTickerFires(t *testing.T) {
	origScan := scanInterval
	scanInterval = 15 * time.Millisecond
	defer func() { scanInterval = origScan }()

	dir := t.TempDir()
	w := NewWatcher(context.Background(), dir)
	w.Start()
	defer w.Stop()

	// Drain events so emitters never block.
	go func() {
		for range w.Events() {
		}
	}()

	// Write the fixture AFTER Start so the initial scan misses it; only a
	// later ticker.C tick can discover it.
	subDir := filepath.Join(dir, "proj", "sess", "subagents")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(subDir, "agent-ticktest.jsonl"),
		[]byte(`{"type":"user","message":{"role":"user","content":"tick"}}`+"\n"),
		0644,
	); err != nil {
		t.Fatalf("write: %v", err)
	}

	deadline := time.After(3 * time.Second)
	for {
		if w.GetAgent("ticktest") != nil {
			return
		}
		select {
		case <-deadline:
			t.Fatal("scanLoop ticker never discovered the late fixture")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// TestWatcher_CleanupLoopTickerFires drives cleanupLoop's ticker.C branch:
// a stale completed agent is removed by the periodic cleanup tick.
func TestWatcher_CleanupLoopTickerFires(t *testing.T) {
	origCleanup := staleCleanupInterval
	staleCleanupInterval = 15 * time.Millisecond
	defer func() { staleCleanupInterval = origCleanup }()

	w := NewWatcher(context.Background(), t.TempDir())

	// Seed a stale completed agent BEFORE Start so the first cleanup tick
	// removes it.
	w.mu.Lock()
	w.agents["stale"] = &SubagentInfo{
		AgentID:        "stale",
		Status:         StatusCompleted,
		FilePath:       "/tmp/stale.jsonl",
		LastActivityAt: time.Now().Add(-2 * time.Hour),
	}
	w.accessOrder["stale"] = time.Now().Add(-2 * time.Hour)
	w.mu.Unlock()

	w.Start()
	defer w.Stop()
	go func() {
		for range w.Events() {
		}
	}()

	deadline := time.After(3 * time.Second)
	for {
		if w.GetAgent("stale") == nil {
			return
		}
		select {
		case <-deadline:
			t.Fatal("cleanupLoop ticker never removed the stale agent")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// TestWatcher_OnHookStartChannelFull covers the slog.Warn drop branch in
// OnHookStart when the events buffer is saturated.
func TestWatcher_OnHookStartChannelFull(t *testing.T) {
	w := NewWatcher(context.Background(), t.TempDir())
	// Saturate the 64-deep buffer.
	for i := 0; i < 64; i++ {
		w.emit(SubagentProgress{AgentID: "x"})
	}
	// New agent → discovered event cannot be enqueued → default warn branch.
	w.OnHookStart("s", "Explore", "full-start")
	if w.GetAgent("full-start") == nil {
		t.Error("agent should still be registered even when event drops")
	}
}

// TestWatcher_OnHookStopChannelFull covers the slog.Warn drop branch in
// OnHookStop when the events buffer is saturated.
func TestWatcher_OnHookStopChannelFull(t *testing.T) {
	w := NewWatcher(context.Background(), t.TempDir())
	w.mu.Lock()
	w.agents["full-stop"] = &SubagentInfo{
		AgentID: "full-stop", Status: StatusActive, StartedAt: time.Now(),
	}
	w.mu.Unlock()
	for i := 0; i < 64; i++ {
		w.emit(SubagentProgress{AgentID: "x"})
	}
	w.OnHookStop("s", "full-stop")
	got := w.GetAgent("full-stop")
	if got == nil || got.Status != StatusCompleted {
		t.Error("agent should be marked completed even when event drops")
	}
}

// TestWatcher_OnHookStopZeroStartedAt covers the prevStart.IsZero() branch:
// an existing agent with a zero StartedAt still produces a non-negative
// duration.
func TestWatcher_OnHookStopZeroStartedAt(t *testing.T) {
	w := NewWatcher(context.Background(), t.TempDir())
	w.mu.Lock()
	w.agents["zerostart"] = &SubagentInfo{
		AgentID: "zerostart", Status: StatusActive,
		// StartedAt deliberately left as zero value.
	}
	w.mu.Unlock()

	w.OnHookStop("s", "zerostart")

	select {
	case ev := <-w.Events():
		c, ok := ev.(SubagentCompleted)
		if !ok {
			t.Fatalf("expected SubagentCompleted, got %T", ev)
		}
		if c.Duration < 0 {
			t.Errorf("expected non-negative duration, got %v", c.Duration)
		}
	case <-time.After(time.Second):
		t.Fatal("expected a completion event")
	}
}
