package office

// R19 · Core correctness tests for the waitSlot close-guard. These
// exercise the two invariants that justified the helper in the first
// place:
//   - a reply delivered before finish() is received
//   - a reply delivered after finish() is dropped (no cross-round
//     pollution of a subsequent slot)

import (
	"testing"
	"time"
)

func TestWaitSlot_DeliverBeforeFinish(t *testing.T) {
	slot := newWaitSlot()
	done := make(chan string, 1)
	go func() { done <- <-slot.ch() }()

	if !slot.deliver("hello") {
		t.Fatal("expected deliver before finish to succeed")
	}

	select {
	case got := <-done:
		if got != "hello" {
			t.Fatalf("expected hello, got %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("reader did not receive value")
	}
}

func TestWaitSlot_DeliverAfterFinishDrops(t *testing.T) {
	slot := newWaitSlot()
	slot.finish()
	if slot.deliver("late") {
		t.Fatal("expected deliver after finish to return false")
	}
}

func TestWaitSlot_DoubleDeliverSecondDrops(t *testing.T) {
	slot := newWaitSlot()
	if !slot.deliver("first") {
		t.Fatal("first deliver should succeed")
	}
	// Second deliver without a reader hits the full 1-buffer slot
	// and returns false. This is the "duplicate reply" case; not
	// considered a late response by the counter since the slot is
	// still open.
	if slot.deliver("second") {
		t.Fatal("second deliver should fail on full buffer")
	}
}

func TestWaitSlot_NilSafe(t *testing.T) {
	var slot *waitSlot
	if slot.deliver("x") {
		t.Fatal("deliver on nil should return false")
	}
	// finish on nil is a no-op; just should not panic.
	slot.finish()
}

func TestWaitSlot_LateResponseCounterIncrements(t *testing.T) {
	before := LateResponsesDropped()
	recordLateResponse("test", "sess-x")
	after := LateResponsesDropped()
	if after != before+1 {
		t.Fatalf("expected counter to increment by 1, got %d -> %d", before, after)
	}
}
