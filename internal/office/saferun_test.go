package office

// R19 · safeRun captures panics, emits a structured log, and — when a
// broadcaster is wired up — fires an office:panicRecovered SSE event.
// These tests verify both effects without relying on log capture.

import (
	"sync"
	"testing"
)

func TestSafeRun_NoPanicPassesThrough(t *testing.T) {
	ran := false
	safeRun("test", nil, func() { ran = true })
	if !ran {
		t.Fatal("expected fn to run")
	}
}

func TestSafeRun_RecoversAndBroadcasts(t *testing.T) {
	var mu sync.Mutex
	events := []string{}
	broadcast := func(event string, data interface{}) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, event)
	}

	// Should not panic out of the test — safeRun swallows it.
	safeRun("panicky", broadcast, func() { panic("boom") })

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 1 || events[0] != "office:panicRecovered" {
		t.Fatalf("expected office:panicRecovered, got %v", events)
	}
}

func TestSafeRun_RecoversWithoutBroadcasterDoesNotPanic(t *testing.T) {
	// nil broadcaster is allowed for tests and early init; safeRun
	// should still swallow the panic without dereferencing nil.
	safeRun("nil-broadcast", nil, func() { panic("oops") })
}
