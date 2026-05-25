package subagent

import (
	"context"
	"testing"
	"time"
)

// R3.2 · OnHookStart / OnHookStop tests. Asserts that hook-driven
// registration creates a tracked agent immediately (no scan tick
// needed) and that OnHookStop flips the status + emits a
// SubagentCompleted event.

func newTestWatcher(t *testing.T) *Watcher {
	t.Helper()
	w := NewWatcher(context.Background(), t.TempDir())
	t.Cleanup(w.Stop)
	return w
}

func TestOnHookStart_RegistersAgent(t *testing.T) {
	w := newTestWatcher(t)

	w.OnHookStart("s-1", "Explore", "agent-abc")

	got := w.GetAgent("agent-abc")
	if got == nil {
		t.Fatal("agent should be tracked after OnHookStart")
	}
	if got.SessionID != "s-1" {
		t.Errorf("SessionID=%q, want s-1", got.SessionID)
	}
	if got.Description != "Explore" {
		t.Errorf("Description=%q, want Explore", got.Description)
	}
	if got.Status != StatusActive {
		t.Errorf("Status=%q, want active", got.Status)
	}

	select {
	case ev := <-w.Events():
		if _, ok := ev.(SubagentDiscovered); !ok {
			t.Errorf("expected SubagentDiscovered, got %T", ev)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected a SubagentDiscovered event")
	}
}

func TestOnHookStart_DoubleCallIsIdempotent(t *testing.T) {
	w := newTestWatcher(t)
	w.OnHookStart("s-1", "Explore", "agent-x")
	w.OnHookStart("s-1", "Explore", "agent-x")
	if got := w.AgentCount(); got != 1 {
		t.Errorf("expected 1 agent, got %d", got)
	}
}

func TestOnHookStart_IgnoresEmptyID(t *testing.T) {
	w := newTestWatcher(t)
	w.OnHookStart("s-1", "Explore", "")
	if w.AgentCount() != 0 {
		t.Error("empty agentID should be ignored")
	}
}

func TestOnHookStop_FlipsCompletedAndEmitsEvent(t *testing.T) {
	w := newTestWatcher(t)
	w.OnHookStart("s-1", "Explore", "agent-y")
	// Drain the discovered event.
	<-w.Events()

	w.OnHookStop("s-1", "agent-y")
	got := w.GetAgent("agent-y")
	if got == nil {
		t.Fatal("agent should still be tracked")
	}
	if got.Status != StatusCompleted {
		t.Errorf("Status=%q, want completed", got.Status)
	}

	select {
	case ev := <-w.Events():
		if _, ok := ev.(SubagentCompleted); !ok {
			t.Errorf("expected SubagentCompleted, got %T", ev)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected a SubagentCompleted event")
	}
}

func TestOnHookStop_BeforeStartCreatesStub(t *testing.T) {
	w := newTestWatcher(t)
	w.OnHookStop("s-1", "agent-z") // no preceding OnHookStart
	got := w.GetAgent("agent-z")
	if got == nil {
		t.Fatal("OnHookStop should create a stub record for unseen agents")
	}
	if got.Status != StatusCompleted {
		t.Errorf("Status=%q, want completed", got.Status)
	}
}

func TestOnHookStop_IgnoresEmptyID(t *testing.T) {
	w := newTestWatcher(t)
	w.OnHookStop("s-1", "")
	if w.AgentCount() != 0 {
		t.Error("empty agentID should be ignored")
	}
}
