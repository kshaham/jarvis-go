package handler

// Coverage for forwardAgentEvents -- the backend-agnostic SSE forwarder body
// shared by the SDK and Codex sessions. Driven directly with a constructed
// event channel + a registered office agent so the per-agent mutation branches
// (working state, token accumulation, loop-detector resets) run without an SDK
// subprocess.

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/state"
	"github.com/kshaham/jarvis/internal/web/sse"
)

func TestForwardAgentEvents_DrivesAgentState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	mgr := office.NewManager(stubSessions{}, func(string, interface{}) {}, nil)
	ctx := &Context{
		SSEBroker:     sse.NewBroker(100, 10),
		StateStore:    state.NewStore(filepath.Join(t.TempDir(), "s.json")),
		OfficeManager: mgr,
	}

	sessionID := "sdk-1"
	mgr.RegisterAgent(sessionID, "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})

	events := make(chan pty.SessionEvent, 16)
	dropped := func() uint64 { return 3 }

	// Run the forwarder in the background; close the channel to make it return.
	done := make(chan struct{})
	go func() {
		forwardAgentEvents(sessionID, events, dropped, ctx)
		close(done)
	}()

	// Feed a representative event sequence.
	events <- pty.EventWorking{}
	events <- pty.EventOutput{Data: "assistant text chunk"}
	events <- pty.EventToolInvocation{Name: "Bash", ResultHash: "h1"}
	events <- pty.EventTokens{Input: 100, Output: 50, Total: 150, Cost: 0.01, Model: "claude-opus-4-8"}
	events <- pty.EventIdle{}
	close(events)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("forwardAgentEvents did not return after channel close")
	}

	// After the final EventIdle the agent's SDK working flag is cleared
	// (EventWorking set it, EventIdle cleared it).
	agent := mgr.GetAgent(sessionID)
	if agent == nil {
		t.Fatal("agent vanished")
	}
	if agent.GetSDKWorking() {
		t.Error("agent should not be SDK-working after EventIdle")
	}
}

func TestForwardAgentEvents_NoAgentStillDrains(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := &Context{
		SSEBroker:     sse.NewBroker(100, 10),
		StateStore:    state.NewStore(filepath.Join(t.TempDir(), "s.json")),
		OfficeManager: office.NewManager(stubSessions{}, func(string, interface{}) {}, nil),
	}

	events := make(chan pty.SessionEvent, 4)
	done := make(chan struct{})
	go func() {
		forwardAgentEvents("orphan", events, func() uint64 { return 0 }, ctx)
		close(done)
	}()
	// Events for an unregistered session should be broadcast but skip the
	// agent-mutation branches without panicking.
	events <- pty.EventTerminal{Data: "t"}
	events <- pty.EventWorking{}
	events <- pty.EventIdle{}
	close(events)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("forwarder did not return for orphan session")
	}
	_ = context.Background()
}
