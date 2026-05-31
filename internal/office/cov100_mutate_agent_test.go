package office

import (
	"testing"

	"github.com/kshaham/jarvis/internal/claudehooks"
)

// TestMutateAgent_RegisteredAgentRunsFn drives the happy path of
// (*Manager).MutateAgent: a registered agent is found, fn runs against
// its MutableAgent surface, and the method returns true. We assert the
// mutation actually landed (SetActiveTool) so the test verifies a real
// side effect rather than just the boolean.
func TestMutateAgent_RegisteredAgentRunsFn(t *testing.T) {
	m := newTestManager()
	m.RegisterAgent("sess-1", "vex", &TeamMemberConfig{ID: "vex", Name: "Vex"})

	var called bool
	ok := m.MutateAgent("sess-1", func(ma claudehooks.MutableAgent) {
		called = true
		ma.SetActiveTool("Bash")
		ma.SetSDKWorking(true)
	})
	if !ok {
		t.Fatal("MutateAgent should return true for a registered agent")
	}
	if !called {
		t.Fatal("fn was not invoked")
	}

	// Side effect must be observable on the live AgentState.
	agent := m.GetAgent("sess-1")
	if agent == nil {
		t.Fatal("agent vanished after MutateAgent")
	}
	if got := agent.SnapshotConfig(); got.ID != "vex" {
		t.Errorf("unexpected agent identity after mutation: %+v", got)
	}
	if !agent.GetSDKWorking() {
		t.Error("SetSDKWorking(true) mutation did not persist on AgentState")
	}
}

// TestMutateAgent_UnknownSessionReturnsFalse drives the agent==nil
// fail-open branch: an unregistered sessionID yields false and never
// invokes fn (so the spawned claude's hook gets the benign 200).
func TestMutateAgent_UnknownSessionReturnsFalse(t *testing.T) {
	m := newTestManager()

	invoked := false
	ok := m.MutateAgent("ghost-session", func(claudehooks.MutableAgent) {
		invoked = true
	})
	if ok {
		t.Error("MutateAgent should return false for an unregistered session")
	}
	if invoked {
		t.Error("fn must NOT run when the agent is missing")
	}
}
