package office

// R21 · Tool-level loop detector tests. Invariants identical to the
// text-level loop detector but keyed on (toolName + resultHash) so
// "different result" breaks the streak even if the tool name stays
// constant.

import (
	"testing"
)

func TestToolLoopDetector_FiresOnThreeIdenticalTuples(t *testing.T) {
	d := newToolLoopDetector()
	_, _ = d.observe("Bash", "hash-abc")
	_, _ = d.observe("Bash", "hash-abc")
	triggered, tool := d.observe("Bash", "hash-abc")
	if !triggered {
		t.Fatal("expected trigger on 3rd identical (tool, resultHash) tuple")
	}
	if tool != "Bash" {
		t.Fatalf("expected tool=Bash, got %q", tool)
	}
}

func TestToolLoopDetector_DifferentResultBreaksStreak(t *testing.T) {
	d := newToolLoopDetector()
	_, _ = d.observe("Bash", "hash-abc")
	_, _ = d.observe("Bash", "hash-xyz") // different result
	triggered, _ := d.observe("Bash", "hash-abc")
	if triggered {
		t.Fatal("should NOT trigger when only 2 of 3 hashes match")
	}
}

func TestToolLoopDetector_DifferentToolBreaksStreak(t *testing.T) {
	d := newToolLoopDetector()
	_, _ = d.observe("Bash", "hash-abc")
	_, _ = d.observe("Read", "hash-abc") // same hash, different tool name → different key
	triggered, _ := d.observe("Bash", "hash-abc")
	if triggered {
		t.Fatal("should NOT trigger when tool name changes mid-sequence")
	}
}

func TestToolLoopDetector_FiresOncePerStreak(t *testing.T) {
	d := newToolLoopDetector()
	_, _ = d.observe("Bash", "h")
	_, _ = d.observe("Bash", "h")
	first, _ := d.observe("Bash", "h")
	second, _ := d.observe("Bash", "h")
	if !first {
		t.Fatal("expected first trigger on 3rd")
	}
	if second {
		t.Fatal("4th identical tuple in same streak should NOT re-fire")
	}
}

func TestToolLoopDetector_IgnoresEmptyInputs(t *testing.T) {
	d := newToolLoopDetector()
	for i := 0; i < 5; i++ {
		triggered, _ := d.observe("", "h")
		if triggered {
			t.Fatalf("empty tool name fired at iteration %d", i)
		}
	}
	for i := 0; i < 5; i++ {
		triggered, _ := d.observe("Bash", "")
		if triggered {
			t.Fatalf("empty result hash fired at iteration %d", i)
		}
	}
}

func TestToolLoopDetector_ResetClears(t *testing.T) {
	d := newToolLoopDetector()
	_, _ = d.observe("Bash", "h")
	_, _ = d.observe("Bash", "h")
	_, _ = d.observe("Bash", "h")
	if !d.isFired() {
		t.Fatal("expected fired after 3 matches")
	}
	d.reset()
	if d.isFired() {
		t.Fatal("expected fired cleared after reset")
	}
	if d.loopingTool() != "" {
		t.Fatalf("expected loopingTool cleared, got %q", d.loopingTool())
	}
}

func TestManagerObserveTool_BroadcastsOnceAndSetsFlag(t *testing.T) {
	m, events := newSweepManager(t)
	m.RegisterAgent("sess-L", "cfg-L", &TeamMemberConfig{ID: "cfg-L", Name: "Stuck"})

	m.ObserveAgentToolInvocation("sess-L", "Bash", "h")
	m.ObserveAgentToolInvocation("sess-L", "Bash", "h")
	m.ObserveAgentToolInvocation("sess-L", "Bash", "h")
	m.ObserveAgentToolInvocation("sess-L", "Bash", "h") // redundant

	var count int
	for _, ev := range events() {
		if ev == "office:agentToolLoop" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected 1 broadcast, got %d (%v)", count, events())
	}

	a := m.GetAgent("sess-L")
	a.mu.Lock()
	inLoop := a.ToolInLoop
	tool := a.LoopingTool
	a.mu.Unlock()
	if !inLoop {
		t.Fatal("expected ToolInLoop=true")
	}
	if tool != "Bash" {
		t.Fatalf("expected LoopingTool=Bash, got %q", tool)
	}

	m.ResetAgentToolLoopDetector("sess-L")
	a.mu.Lock()
	inLoop = a.ToolInLoop
	a.mu.Unlock()
	if inLoop {
		t.Fatal("expected ToolInLoop=false after reset")
	}
}
