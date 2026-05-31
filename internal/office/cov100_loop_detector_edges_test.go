package office

import (
	"testing"
)

// TestToolLoopDetector_AdvisoryResetsWhenMatchesDropToOne drives the
// `advisoryFired && matches == 1` reset branch: after the advisory fires on a
// streak, a single fresh (tool, hash) tuple makes the current key's match
// count drop to 1, which clears the advisory so a future streak can re-fire.
func TestToolLoopDetector_AdvisoryResetsWhenMatchesDropToOne(t *testing.T) {
	d := newToolLoopDetector()
	// Fire the advisory on 3 identical tuples.
	_, _ = d.observe("Bash", "h")
	_, _ = d.observe("Bash", "h")
	if triggered, _ := d.observe("Bash", "h"); !triggered {
		t.Fatal("expected advisory to fire on the 3rd identical tuple")
	}
	if !d.isFired() {
		t.Fatal("advisory should be marked fired")
	}

	// A single brand-new tuple: its match count is 1 while advisoryFired is
	// still true -> the reset branch clears the flag.
	_, _ = d.observe("Read", "different-hash")
	if d.isFired() {
		t.Error("expected advisory flag cleared after matches dropped to 1")
	}

	// Prove the reset took: a fresh 3-streak of the new key re-fires.
	_, _ = d.observe("Read", "different-hash")
	if triggered, _ := d.observe("Read", "different-hash"); !triggered {
		t.Error("expected the advisory to re-fire after the reset")
	}
}

// TestObserveAgentToolInvocation_UnknownSessionReturnsFalse drives the
// agent==nil guard in ObserveAgentToolInvocation.
func TestObserveAgentToolInvocation_UnknownSessionReturnsFalse(t *testing.T) {
	m := newTestManager()
	if m.ObserveAgentToolInvocation("ghost", "Bash", "h") {
		t.Error("expected false for an unknown session")
	}
}

// TestObserveAgentOutput_UnknownSessionReturnsFalse drives the agent==nil
// guard in ObserveAgentOutput (text-level loop detector forwarder).
func TestObserveAgentOutput_UnknownSessionReturnsFalse(t *testing.T) {
	m := newTestManager()
	if m.ObserveAgentOutput("ghost", "some output chunk") {
		t.Error("expected false for an unknown session")
	}
}
