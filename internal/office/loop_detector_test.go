package office

// R20 · Loop-detector unit tests. The invariants to lock in:
//   - 3 identical hashes in a 5-slot ring fire exactly once
//   - Chunks shorter than loopMinHashBytes are ignored
//   - A non-matching hash breaks the streak and re-arms the detector
//   - reset() clears the ring and the fired flag

import (
	"strings"
	"testing"
)

func longLine(marker string) string {
	// 64-char repeatable chunk, long enough to clear loopMinHashBytes.
	return strings.Repeat("x", 64) + marker
}

func TestLoopDetector_FiresOnThreeMatches(t *testing.T) {
	d := newLoopDetector()
	chunk := longLine("A")
	_, _ = d.observe(chunk)
	_, _ = d.observe(chunk)
	triggered, _ := d.observe(chunk)
	if !triggered {
		t.Fatal("expected trigger on 3rd identical chunk")
	}
}

func TestLoopDetector_FiresOnlyOncePerStreak(t *testing.T) {
	d := newLoopDetector()
	chunk := longLine("A")
	_, _ = d.observe(chunk)
	_, _ = d.observe(chunk)
	triggered1, _ := d.observe(chunk)
	triggered2, _ := d.observe(chunk)
	if !triggered1 {
		t.Fatal("expected trigger on 3rd")
	}
	if triggered2 {
		t.Fatal("4th identical chunk in same streak should NOT re-fire")
	}
}

func TestLoopDetector_StreakResetsOnMismatch(t *testing.T) {
	d := newLoopDetector()
	matchA := longLine("A")
	matchB := longLine("B")
	// Fill ring with A three times -> fires.
	_, _ = d.observe(matchA)
	_, _ = d.observe(matchA)
	triggered, _ := d.observe(matchA)
	if !triggered {
		t.Fatal("expected trigger after 3 As")
	}
	// Four Bs to push all As out of the ring and clear the fired
	// flag on the first B (when count drops back to 1).
	for i := 0; i < 4; i++ {
		d.observe(matchB)
	}
	// Start a new A streak — should trigger again.
	_, _ = d.observe(matchA)
	_, _ = d.observe(matchA)
	trig2, _ := d.observe(matchA)
	if !trig2 {
		t.Fatal("expected re-trigger after streak reset and new A streak")
	}
}

func TestLoopDetector_IgnoresShortChunks(t *testing.T) {
	d := newLoopDetector()
	// Short chunks should never populate the ring.
	for i := 0; i < 5; i++ {
		triggered, _ := d.observe("short")
		if triggered {
			t.Fatalf("short chunk fired at iteration %d", i)
		}
	}
}

func TestLoopDetector_ResetClearsState(t *testing.T) {
	d := newLoopDetector()
	chunk := longLine("A")
	_, _ = d.observe(chunk)
	_, _ = d.observe(chunk)
	_, _ = d.observe(chunk)
	if !d.isFired() {
		t.Fatal("expected fired after 3 matches")
	}
	d.reset()
	if d.isFired() {
		t.Fatal("expected reset to clear fired flag")
	}
	// Post-reset, need 3 more matches to fire again.
	triggered, _ := d.observe(chunk)
	if triggered {
		t.Fatal("single observation post-reset should not fire")
	}
}

func TestManagerObserveAgentOutput_BroadcastsOnce(t *testing.T) {
	m, events := newSweepManager(t)
	m.RegisterAgent("sess-A", "cfg-A", &TeamMemberConfig{ID: "cfg-A", Name: "Loopy"})
	chunk := longLine("loop")

	m.ObserveAgentOutput("sess-A", chunk)
	m.ObserveAgentOutput("sess-A", chunk)
	m.ObserveAgentOutput("sess-A", chunk)
	m.ObserveAgentOutput("sess-A", chunk) // should NOT re-broadcast

	var count int
	for _, ev := range events() {
		if ev == "office:agentPossibleLoop" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected 1 broadcast, got %d (%v)", count, events())
	}

	// Payload side effect: agent.InLoop should be true.
	a := m.GetAgent("sess-A")
	a.mu.Lock()
	inLoop := a.InLoop
	a.mu.Unlock()
	if !inLoop {
		t.Fatal("expected agent.InLoop=true after advisory fired")
	}

	// Reset clears the flag.
	m.ResetAgentLoopDetector("sess-A")
	a.mu.Lock()
	inLoop = a.InLoop
	a.mu.Unlock()
	if inLoop {
		t.Fatal("expected agent.InLoop=false after reset")
	}
}
