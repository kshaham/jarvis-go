package pty

// Tests for PTY hot-loop efficiency fixes (Batch 7).
// Each test is behaviorally focused: it proves the fix is output-identical
// to the previous implementation, not merely that it compiles.

import (
	"sync"
	"testing"
)

// --- Fix 1: notifyOutputObservers slice-clone (SDK + Codex) ---

// TestSDKNotifyOutputObservers_AllObserversReceiveData asserts that
// after the slice-clone refactor, every registered observer still
// receives the exact data string.
func TestSDKNotifyOutputObservers_AllObserversReceiveData(t *testing.T) {
	s := NewSDKSession(SessionConfig{WorkingDir: "/tmp"})

	const payload = "hello from sdk"
	const numObservers = 5

	var mu sync.Mutex
	received := make(map[int]string)

	unsubs := make([]func(), numObservers)
	for i := 0; i < numObservers; i++ {
		idx := i
		unsubs[i] = s.AddOutputObserver(func(data string) {
			mu.Lock()
			received[idx] = data
			mu.Unlock()
		})
	}

	s.notifyOutputObservers(payload)

	// Unsub after notification to verify observers were called.
	for _, unsub := range unsubs {
		unsub()
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received) != numObservers {
		t.Fatalf("expected %d observers to receive data, got %d", numObservers, len(received))
	}
	for i, got := range received {
		if got != payload {
			t.Errorf("observer %d received %q, want %q", i, got, payload)
		}
	}
}

// TestCodexNotifyOutputObservers_AllObserversReceiveData is the Codex
// equivalent of the SDK test above.
func TestCodexNotifyOutputObservers_AllObserversReceiveData(t *testing.T) {
	s := NewCodexSession(SessionConfig{WorkingDir: "/tmp"})

	const payload = "hello from codex observer"
	const numObservers = 4

	var mu sync.Mutex
	received := make(map[int]string)

	unsubs := make([]func(), numObservers)
	for i := 0; i < numObservers; i++ {
		idx := i
		unsubs[i] = s.AddOutputObserver(func(data string) {
			mu.Lock()
			received[idx] = data
			mu.Unlock()
		})
	}

	s.notifyOutputObservers(payload)

	for _, unsub := range unsubs {
		unsub()
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received) != numObservers {
		t.Fatalf("expected %d observers to receive data, got %d", numObservers, len(received))
	}
	for i, got := range received {
		if got != payload {
			t.Errorf("observer %d received %q, want %q", i, got, payload)
		}
	}
}

// TestSDKNotifyOutputObservers_UnsubBeforeNotify verifies that an
// observer removed before the notification is NOT called.
func TestSDKNotifyOutputObservers_UnsubBeforeNotify(t *testing.T) {
	s := NewSDKSession(SessionConfig{WorkingDir: "/tmp"})

	called := false
	unsub := s.AddOutputObserver(func(_ string) { called = true })
	unsub() // remove before notify

	s.notifyOutputObservers("should not arrive")
	if called {
		t.Error("unsubscribed observer was called")
	}
}

// --- Fix 2: toolEventTracker.reset uses clear() ---

// TestToolEventTracker_ResetClearsViaClear checks that reset() leaves
// the map empty (same contract as the old reallocation path).
func TestToolEventTracker_ResetClearsViaClear(t *testing.T) {
	tracker := newToolEventTracker()
	// Seed a pending entry.
	tracker.pending["fake-id"] = &toolPending{name: "Bash", inputHash: "abc"}
	if len(tracker.pending) != 1 {
		t.Fatalf("setup: expected 1 entry, got %d", len(tracker.pending))
	}

	tracker.reset()

	tracker.mu.Lock()
	n := len(tracker.pending)
	tracker.mu.Unlock()
	if n != 0 {
		t.Errorf("after reset, expected 0 entries, got %d", n)
	}
}

// --- Fix 3: WriteInkPrompt empty-prompt guard ---

// TestWriteInkPrompt_ReturnsFalseForEmptyPrompt verifies the empty-prompt
// guard works synchronously.
func TestWriteInkPrompt_ReturnsFalseForEmptyPrompt(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	if s.WriteInkPrompt("") {
		t.Error("expected false for empty prompt")
	}
	if s.WriteInkPrompt("   ") {
		t.Error("expected false for whitespace-only prompt")
	}
}
