package pty

import (
	"context"
	"testing"

	"github.com/shaharia-lab/claude-agent-sdk-go/claude"
)

// TestProcessEvent_LatchesProviderError verifies the lastEventWasError latch
// that drives the non-lossy exit signal (Step 3a): a system "error" event and
// an errored TypeResult both latch it, and a subsequent clean TypeResult
// clears it. The latch is what lets eventConsumer attribute an unexpected
// Events() close as a provider death rather than a clean code-0 exit.
func TestProcessEvent_LatchesProviderError(t *testing.T) {
	s := NewSDKSession(SessionConfig{})

	// System error latches.
	s.processEvent(claude.Event{
		Type:   claude.TypeSystem,
		System: &claude.SystemMessage{Subtype: "error", Message: "529 overloaded"},
	})
	if !readErrored(s) {
		t.Fatal("system error event should latch lastEventWasError")
	}

	// A clean result clears it.
	s.processEvent(cleanResult())
	if readErrored(s) {
		t.Fatal("clean result should clear lastEventWasError")
	}

	// An errored result re-latches it.
	s.processEvent(claude.Event{
		Type: claude.TypeResult,
		Result: &claude.Result{
			NumTurns: 1,
			IsError:  true,
			Subtype:  "rate_limit",
			Usage:    claude.Usage{InputTokens: 1, OutputTokens: 1},
		},
	})
	if !readErrored(s) {
		t.Fatal("errored result should latch lastEventWasError")
	}
}

// TestShouldReconnect_TableDriven covers the reconnect-vs-exit decision. Only a
// strictly transient unexpected death (feature enabled, not stopped, live
// context, error-latched, resumable session id, under the cap) warrants an
// in-place reconnect; every other case must fall through to the EventExit path
// so the office watchdog stays the backstop.
func TestShouldReconnect_TableDriven(t *testing.T) {
	live := context.Background()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name      string
		enabled   bool
		ctx       context.Context
		stopped   bool
		errored   bool
		claudeID  string
		attempts  int
		maxBefore int // override sdkReconnectMaxAttempts for this case (0 = default 3)
		want      bool
	}{
		{name: "happy path reconnects", enabled: true, ctx: live, errored: true, claudeID: "c1", want: true},
		{name: "feature disabled", enabled: false, ctx: live, errored: true, claudeID: "c1", want: false},
		{name: "context cancelled", enabled: true, ctx: cancelled, errored: true, claudeID: "c1", want: false},
		{name: "intentionally stopped", enabled: true, ctx: live, stopped: true, errored: true, claudeID: "c1", want: false},
		{name: "graceful close (no error latched)", enabled: true, ctx: live, errored: false, claudeID: "c1", want: false},
		{name: "no transcript to resume", enabled: true, ctx: live, errored: true, claudeID: "", want: false},
		{name: "cap already reached", enabled: true, ctx: live, errored: true, claudeID: "c1", attempts: 3, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prev := sdkReconnectEnabled
			sdkReconnectEnabled = tt.enabled
			defer func() { sdkReconnectEnabled = prev }()

			s := NewSDKSession(SessionConfig{})
			s.mu.Lock()
			s.isStopped = tt.stopped
			s.lastEventWasError = tt.errored
			s.claudeSessionID = tt.claudeID
			s.reconnectAttempts = tt.attempts
			s.mu.Unlock()

			if got := s.shouldReconnect(tt.ctx); got != tt.want {
				t.Errorf("shouldReconnect = %v, want %v", got, tt.want)
			}
		})
	}
}

// readErrored reads the error latch under the session lock.
func readErrored(s *SDKSession) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastEventWasError
}

// cleanResult builds a non-error TypeResult event.
func cleanResult() claude.Event {
	return claude.Event{
		Type: claude.TypeResult,
		Result: &claude.Result{
			NumTurns: 1,
			IsError:  false,
			Usage:    claude.Usage{InputTokens: 10, OutputTokens: 5},
			ModelUsages: map[string]claude.ModelUsage{
				"claude-opus-4-8": {InputTokens: 10, OutputTokens: 5},
			},
		},
	}
}
