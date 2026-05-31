package handler

// Extra coverage for forwardAgentEvents -- drives the event branches the
// existing forward_agent_events_test.go doesn't reach: EventTerminal,
// EventTurnCapReached + EventTurnError (with a named agent Config so the
// agentName lookup branch runs), and EventExit (which triggers
// HandleAgentDeath + cleanupSessionResources and sets sawExit). The agent is
// registered with a non-nil Config to exercise the `agent.Config != nil` name
// branches in the cap/error handlers.

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/state"
	"github.com/kshaham/jarvis/internal/web/sse"
)

func TestForwardAgentEvents_AllBranchesWithNamedAgent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	mgr := office.NewManager(stubSessions{}, func(string, interface{}) {}, nil)
	ctx := &Context{
		SSEBroker:      sse.NewBroker(100, 10),
		StateStore:     state.NewStore(filepath.Join(t.TempDir(), "s.json")),
		OfficeManager:  mgr,
		SessionManager: pty.NewSessionManager(10),
	}

	sessionID := "sdk-named-1"
	mgr.RegisterAgent(sessionID, "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})

	events := make(chan pty.SessionEvent, 32)
	dropped := func() uint64 { return 7 }

	done := make(chan struct{})
	go func() {
		forwardAgentEvents(sessionID, events, dropped, ctx)
		close(done)
	}()

	events <- pty.EventTerminal{Data: "raw terminal bytes"}
	events <- pty.EventWorking{}
	events <- pty.EventOutput{Data: "assistant chunk"}
	events <- pty.EventTokens{Input: 200, Output: 80, Total: 280, Cost: 0.05, Model: "claude-opus-4-8"}
	events <- pty.EventToolInvocation{Name: "Bash", ResultHash: "abc"}
	events <- pty.EventIdle{}
	events <- pty.EventTurnCapReached{Turns: 50, Cap: 50, Cost: 1.23}
	events <- pty.EventTurnError{Subtype: "rate_limit", StopReason: "429", Result: "Network error", NumTurns: 3}
	events <- pty.EventExit{ExitCode: 0}
	close(events)

	select {
	case <-done:
	case <-time.After(4 * time.Second):
		t.Fatal("forwardAgentEvents did not return")
	}

	// EventExit triggered HandleAgentDeath; the agent should be gone.
	if mgr.GetAgent(sessionID) != nil {
		t.Error("agent should be removed after EventExit -> HandleAgentDeath")
	}
}

// TestForwardAgentEvents_TurnCapAndErrorNoAgent drives the EventTurnCapReached
// and EventTurnError branches when NO agent is registered, so the agentName
// fallback (`agentName := sessionID`) runs and the broadcast still fires.
func TestForwardAgentEvents_TurnCapAndErrorNoAgent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	mgr := office.NewManager(stubSessions{}, func(string, interface{}) {}, nil)
	ctx := &Context{
		SSEBroker:      sse.NewBroker(100, 10),
		StateStore:     state.NewStore(filepath.Join(t.TempDir(), "s.json")),
		OfficeManager:  mgr,
		SessionManager: pty.NewSessionManager(10),
	}

	sessionID := "sdk-noagent"

	events := make(chan pty.SessionEvent, 8)
	done := make(chan struct{})
	go func() {
		forwardAgentEvents(sessionID, events, func() uint64 { return 0 }, ctx)
		close(done)
	}()
	events <- pty.EventTurnCapReached{Turns: 10, Cap: 10, Cost: 0.1}
	events <- pty.EventTurnError{Subtype: "error", Result: "boom"}
	events <- pty.EventExit{ExitCode: 1}
	close(events)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("forwardAgentEvents did not return")
	}
}
