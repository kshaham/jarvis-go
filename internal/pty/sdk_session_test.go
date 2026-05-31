package pty

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shaharia-lab/claude-agent-sdk-go/claude"
)

// drainSDKEvents non-blockingly collects everything currently buffered on
// the SDK session's events channel. Named distinctly from the CodexSession
// drainEvents helper to avoid a redeclaration collision.
func drainSDKEvents(s *SDKSession) []SessionEvent {
	var out []SessionEvent
	for {
		select {
		case ev := <-s.events:
			out = append(out, ev)
		default:
			return out
		}
	}
}

// strPtr returns a pointer to s, for optional SDK fields.
func strPtr(s string) *string { return &s }

func TestNewSDKSession_Defaults(t *testing.T) {
	s := NewSDKSession(SessionConfig{Name: "alpha", WorkingDir: "/work"})
	if s.ID == "" {
		t.Error("expected a generated ID when none supplied")
	}
	if s.GetName() != "alpha" {
		t.Errorf("name = %q, want alpha", s.GetName())
	}
	if s.WorkingDir != "/work" {
		t.Errorf("workingDir = %q, want /work", s.WorkingDir)
	}
	if s.Mode != ModeClaudeSDK {
		t.Errorf("mode = %q, want %q", s.Mode, ModeClaudeSDK)
	}
	if s.GetStatus() != StatusStopped {
		t.Errorf("initial status = %q, want stopped", s.GetStatus())
	}
	if s.IsWorking() {
		t.Error("new session should not be working")
	}
	if s.terminalBuffer == nil || s.textOutput == nil {
		t.Error("buffers must be initialized")
	}
	if s.events == nil || s.done == nil {
		t.Error("channels must be initialized")
	}
	if s.toolTracker == nil {
		t.Error("toolTracker must be initialized")
	}
	if s.DroppedEvents() != 0 {
		t.Errorf("droppedEvents = %d, want 0", s.DroppedEvents())
	}
}

func TestNewSDKSession_RespectsSuppliedID(t *testing.T) {
	s := NewSDKSession(SessionConfig{ID: "fixed-id-123"})
	if s.ID != "fixed-id-123" {
		t.Errorf("ID = %q, want fixed-id-123", s.ID)
	}
}

// --- processEvent: TypeSystem ----------------------------------------------

func TestProcessEvent_SystemInit_SetsClaudeSessionIDAndOutput(t *testing.T) {
	s := NewSDKSession(SessionConfig{})
	s.processEvent(claude.Event{
		Type: claude.TypeSystem,
		System: &claude.SystemMessage{
			Subtype:   claude.SubtypeInit,
			SessionID: "claude-sess-42",
			Model:     "claude-opus-4-8",
		},
	})

	if got := s.ClaudeSessionID(); got != "claude-sess-42" {
		t.Errorf("claudeSessionID = %q, want claude-sess-42", got)
	}
	buf := s.GetTerminalBuffer()
	if !strings.Contains(buf, "claude-sess-42") || !strings.Contains(buf, "claude-opus-4-8") {
		t.Errorf("terminal buffer = %q, want it to mention session id and model", buf)
	}

	// Init output should have produced terminal + output events.
	var sawTerminal, sawOutput bool
	for _, e := range drainSDKEvents(s) {
		switch e.(type) {
		case EventTerminal:
			sawTerminal = true
		case EventOutput:
			sawOutput = true
		}
	}
	if !sawTerminal || !sawOutput {
		t.Errorf("init should emit terminal+output events; terminal=%v output=%v", sawTerminal, sawOutput)
	}
}

func TestProcessEvent_SystemError_AppendsError(t *testing.T) {
	s := NewSDKSession(SessionConfig{})
	s.processEvent(claude.Event{
		Type: claude.TypeSystem,
		System: &claude.SystemMessage{
			Subtype: "error",
			Message: "auth failed: 401",
		},
	})
	buf := s.GetTerminalBuffer()
	if !strings.Contains(buf, "[Error]") || !strings.Contains(buf, "auth failed: 401") {
		t.Errorf("buffer = %q, want it to contain the error message", buf)
	}
}

func TestProcessEvent_SystemNil_NoPanic(t *testing.T) {
	s := NewSDKSession(SessionConfig{})
	// System==nil must be handled gracefully (no deref panic).
	s.processEvent(claude.Event{Type: claude.TypeSystem, System: nil})
	if s.GetTerminalBuffer() != "" {
		t.Errorf("nil system event should not write output, got %q", s.GetTerminalBuffer())
	}
}

// --- processEvent: TypeStreamEvent -----------------------------------------

func TestProcessEvent_StreamTextDelta_AppendsOutput(t *testing.T) {
	s := NewSDKSession(SessionConfig{})
	s.processEvent(claude.Event{
		Type: claude.TypeStreamEvent,
		StreamEvent: &claude.StreamEventMessage{
			Event: claude.StreamEvent{
				Delta: &claude.StreamEventDelta{Type: "text_delta", Text: "hello world"},
			},
		},
	})
	if !strings.Contains(s.GetTerminalBuffer(), "hello world") {
		t.Errorf("buffer = %q, want it to contain the streamed text", s.GetTerminalBuffer())
	}
}

func TestProcessEvent_ThinkingDelta_TransitionsToWorking(t *testing.T) {
	s := NewSDKSession(SessionConfig{})
	if s.IsWorking() {
		t.Fatal("precondition: session should start not-working")
	}
	s.processEvent(claude.Event{
		Type: claude.TypeStreamEvent,
		StreamEvent: &claude.StreamEventMessage{
			Event: claude.StreamEvent{
				Delta: &claude.StreamEventDelta{Type: "thinking_delta", Thinking: "..."},
			},
		},
	})
	if !s.IsWorking() {
		t.Error("thinking_delta should flip isWorking=true")
	}
	if s.GetStatus() != StatusBusy {
		t.Errorf("status = %q, want busy", s.GetStatus())
	}
	var sawWorking bool
	for _, e := range drainSDKEvents(s) {
		if _, ok := e.(EventWorking); ok {
			sawWorking = true
		}
	}
	if !sawWorking {
		t.Error("first thinking_delta should emit EventWorking")
	}
}

func TestProcessEvent_ThinkingDelta_WhileWorking_NoDuplicateEvent(t *testing.T) {
	s := NewSDKSession(SessionConfig{})
	// Prime: first thinking delta sets working and emits one EventWorking.
	thinking := claude.Event{
		Type: claude.TypeStreamEvent,
		StreamEvent: &claude.StreamEventMessage{
			Event: claude.StreamEvent{Delta: &claude.StreamEventDelta{Type: "thinking_delta"}},
		},
	}
	s.processEvent(thinking)
	_ = drainSDKEvents(s) // discard the first EventWorking
	// Second thinking delta while already working must NOT emit again.
	s.processEvent(thinking)
	for _, e := range drainSDKEvents(s) {
		if _, ok := e.(EventWorking); ok {
			t.Error("subsequent thinking_delta while working must not re-emit EventWorking")
		}
	}
}

func TestProcessEvent_StreamNilDelta_NoPanic(t *testing.T) {
	s := NewSDKSession(SessionConfig{})
	s.processEvent(claude.Event{
		Type:        claude.TypeStreamEvent,
		StreamEvent: &claude.StreamEventMessage{Event: claude.StreamEvent{Delta: nil}},
	})
	// no output, no crash
	if s.GetTerminalBuffer() != "" {
		t.Errorf("nil delta should not write, got %q", s.GetTerminalBuffer())
	}
}

// --- processEvent: TypeAssistant -------------------------------------------

func TestProcessEvent_Assistant_AppendsTextAndStashesToolUse(t *testing.T) {
	s := NewSDKSession(SessionConfig{})
	raw := json.RawMessage(`{"type":"assistant","message":{"role":"assistant","content":[` +
		`{"type":"text","text":"thinking out loud"},` +
		`{"type":"tool_use","id":"tu-1","name":"Bash","input":{"command":"ls"}}]}}`)
	// AssistantMessage.Text() reads from Message payload, so build it too.
	var msg claude.AssistantMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("unmarshal assistant: %v", err)
	}
	s.processEvent(claude.Event{
		Type:      claude.TypeAssistant,
		Assistant: &msg,
		Raw:       raw,
	})

	// The tool_use block must now be pending in the tracker.
	s.toolTracker.mu.Lock()
	_, pending := s.toolTracker.pending["tu-1"]
	s.toolTracker.mu.Unlock()
	if !pending {
		t.Error("assistant tool_use block should be stashed as pending")
	}
}

func TestProcessEvent_AssistantNil_NoPanic(t *testing.T) {
	s := NewSDKSession(SessionConfig{})
	// Assistant nil but Raw present (tool_use only, no text).
	raw := json.RawMessage(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"tu-9","name":"Read","input":{}}]}}`)
	s.processEvent(claude.Event{Type: claude.TypeAssistant, Assistant: nil, Raw: raw})
	s.toolTracker.mu.Lock()
	_, pending := s.toolTracker.pending["tu-9"]
	s.toolTracker.mu.Unlock()
	if !pending {
		t.Error("tool_use should still be tracked even when typed Assistant is nil")
	}
}

// --- processEvent: TypeRateLimitEvent --------------------------------------

func TestProcessEvent_RateLimit_AppendsNotice(t *testing.T) {
	s := NewSDKSession(SessionConfig{})
	s.processEvent(claude.Event{Type: claude.TypeRateLimitEvent})
	if !strings.Contains(s.GetTerminalBuffer(), "Rate Limited") {
		t.Errorf("buffer = %q, want a rate-limit notice", s.GetTerminalBuffer())
	}
}

// --- processEvent: TypeResult — token accounting ---------------------------

func TestProcessEvent_Result_AccumulatesTokensCostAndGoesIdle(t *testing.T) {
	s := NewSDKSession(SessionConfig{})
	s.mu.Lock()
	s.isWorking = true
	s.status = StatusBusy
	s.mu.Unlock()

	s.processEvent(claude.Event{
		Type: claude.TypeResult,
		Result: &claude.Result{
			NumTurns:     3,
			TotalCostUSD: 0.25,
			SessionID:    "claude-result-sess",
			Usage:        claude.Usage{InputTokens: 100, OutputTokens: 40},
			ModelUsages: map[string]claude.ModelUsage{
				"claude-opus-4-8": {InputTokens: 100, OutputTokens: 40},
			},
		},
	})

	s.mu.RLock()
	in, out, cost := s.totalInputTokens, s.totalOutputTokens, s.totalCostUSD
	s.mu.RUnlock()
	if in != 100 || out != 40 {
		t.Errorf("tokens accumulated in=%d out=%d, want 100/40", in, out)
	}
	if cost != 0.25 {
		t.Errorf("cost = %v, want 0.25", cost)
	}
	if s.ClaudeSessionID() != "claude-result-sess" {
		t.Errorf("result should update claudeSessionID, got %q", s.ClaudeSessionID())
	}
	if s.IsWorking() {
		t.Error("result should clear isWorking")
	}
	if s.GetStatus() != StatusIdle {
		t.Errorf("status = %q, want idle after result", s.GetStatus())
	}

	var gotTokens *EventTokens
	var sawIdle bool
	for _, e := range drainSDKEvents(s) {
		switch ev := e.(type) {
		case EventTokens:
			cp := ev
			gotTokens = &cp
		case EventIdle:
			sawIdle = true
		}
	}
	if gotTokens == nil {
		t.Fatal("expected EventTokens after a result")
	}
	if gotTokens.Input != 100 || gotTokens.Output != 40 || gotTokens.Total != 140 {
		t.Errorf("EventTokens = %+v, want in=100 out=40 total=140", *gotTokens)
	}
	if gotTokens.Cost != 0.25 {
		t.Errorf("EventTokens.Cost = %v, want 0.25", gotTokens.Cost)
	}
	if gotTokens.Model != "claude-opus-4-8" {
		t.Errorf("EventTokens.Model = %q, want claude-opus-4-8 (largest usage)", gotTokens.Model)
	}
	if !sawIdle {
		t.Error("expected EventIdle after a result")
	}
}

func TestProcessEvent_Result_PicksLargestModelUsage(t *testing.T) {
	s := NewSDKSession(SessionConfig{})
	s.processEvent(claude.Event{
		Type: claude.TypeResult,
		Result: &claude.Result{
			NumTurns: 1,
			Usage:    claude.Usage{InputTokens: 10, OutputTokens: 10},
			ModelUsages: map[string]claude.ModelUsage{
				"small-model": {InputTokens: 1, OutputTokens: 1},
				"big-model":   {InputTokens: 500, OutputTokens: 500},
			},
		},
	})
	var model string
	for _, e := range drainSDKEvents(s) {
		if ev, ok := e.(EventTokens); ok {
			model = ev.Model
		}
	}
	if model != "big-model" {
		t.Errorf("model = %q, want big-model (largest token usage)", model)
	}
}

func TestProcessEvent_Result_AccumulatesAcrossTurns(t *testing.T) {
	s := NewSDKSession(SessionConfig{})
	mk := func(in, out int, cost float64) claude.Event {
		return claude.Event{
			Type: claude.TypeResult,
			Result: &claude.Result{
				NumTurns:     1,
				TotalCostUSD: cost,
				Usage:        claude.Usage{InputTokens: in, OutputTokens: out},
			},
		}
	}
	s.processEvent(mk(50, 20, 0.10))
	s.processEvent(mk(30, 10, 0.05))

	s.mu.RLock()
	in, out, cost := s.totalInputTokens, s.totalOutputTokens, s.totalCostUSD
	s.mu.RUnlock()
	if in != 80 || out != 30 {
		t.Errorf("accumulated tokens in=%d out=%d, want 80/30", in, out)
	}
	if cost < 0.149 || cost > 0.151 {
		t.Errorf("accumulated cost = %v, want ~0.15", cost)
	}
}

func TestProcessEvent_Result_Error_EmitsTurnError(t *testing.T) {
	s := NewSDKSession(SessionConfig{})
	longResult := strings.Repeat("x", 600) // exceeds the 500-char truncation
	s.processEvent(claude.Event{
		Type: claude.TypeResult,
		Result: &claude.Result{
			NumTurns:   2,
			IsError:    true,
			Subtype:    "error_during_execution",
			StopReason: strPtr("rate_limit"),
			Result:     longResult,
			Errors:     []string{"429 Too Many Requests"},
			Usage:      claude.Usage{InputTokens: 5, OutputTokens: 0},
		},
	})
	var te *EventTurnError
	for _, e := range drainSDKEvents(s) {
		if ev, ok := e.(EventTurnError); ok {
			cp := ev
			te = &cp
		}
	}
	if te == nil {
		t.Fatal("expected an EventTurnError when result IsError=true")
	}
	if te.Subtype != "error_during_execution" {
		t.Errorf("Subtype = %q, want error_during_execution", te.Subtype)
	}
	if te.StopReason != "rate_limit" {
		t.Errorf("StopReason = %q, want rate_limit", te.StopReason)
	}
	if te.NumTurns != 2 {
		t.Errorf("NumTurns = %d, want 2", te.NumTurns)
	}
	if !strings.HasSuffix(te.Result, "...") {
		t.Errorf("long result should be truncated with an ellipsis, got len=%d", len(te.Result))
	}
	if len(te.Result) != 503 { // 500 chars + "..."
		t.Errorf("truncated result len = %d, want 503", len(te.Result))
	}
}

func TestProcessEvent_Result_ErrorWithNilStopReason(t *testing.T) {
	s := NewSDKSession(SessionConfig{})
	s.processEvent(claude.Event{
		Type: claude.TypeResult,
		Result: &claude.Result{
			NumTurns:   1,
			IsError:    true,
			StopReason: nil, // must not panic
			Result:     "short failure",
		},
	})
	var te *EventTurnError
	for _, e := range drainSDKEvents(s) {
		if ev, ok := e.(EventTurnError); ok {
			cp := ev
			te = &cp
		}
	}
	if te == nil {
		t.Fatal("expected EventTurnError")
	}
	if te.StopReason != "" {
		t.Errorf("nil StopReason should yield empty string, got %q", te.StopReason)
	}
}

func TestProcessEvent_ResultNil_NoPanic(t *testing.T) {
	s := NewSDKSession(SessionConfig{})
	s.processEvent(claude.Event{Type: claude.TypeResult, Result: nil})
	// No events, no panic.
	if evs := drainSDKEvents(s); len(evs) != 0 {
		t.Errorf("nil result should emit nothing, got %d events", len(evs))
	}
}

// --- processEvent: TypeResult — turn cap -----------------------------------

func TestProcessEvent_Result_TurnCapReached_FiresOnceAndStops(t *testing.T) {
	s := NewSDKSession(SessionConfig{MaxTurnsPerSession: 5})
	// Must be "started" so Stop() has a cancelFunc to call and the
	// goroutine path is exercised cleanly.
	_, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.cancelFunc = cancel
	s.mu.Unlock()

	s.processEvent(claude.Event{
		Type: claude.TypeResult,
		Result: &claude.Result{
			NumTurns:     5, // hits the cap
			TotalCostUSD: 1.23,
			Usage:        claude.Usage{InputTokens: 1, OutputTokens: 1},
		},
	})

	var cap1 *EventTurnCapReached
	var sawExit bool
	for _, e := range drainSDKEvents(s) {
		switch ev := e.(type) {
		case EventTurnCapReached:
			cp := ev
			cap1 = &cp
		case EventExit:
			sawExit = true
		}
	}
	if cap1 == nil {
		t.Fatal("expected EventTurnCapReached when NumTurns >= cap")
	}
	if cap1.Turns != 5 || cap1.Cap != 5 {
		t.Errorf("cap event = %+v, want Turns=5 Cap=5", *cap1)
	}
	if cap1.Cost != 1.23 {
		t.Errorf("cap event cost = %v, want 1.23", cap1.Cost)
	}
	if !sawExit {
		t.Error("turn cap should also emit EventExit so the agent unwinds cleanly")
	}

	s.mu.RLock()
	fired := s.turnCapFired
	s.mu.RUnlock()
	if !fired {
		t.Error("turnCapFired flag should be set")
	}

	// Give the async go s.Stop() a moment, then confirm it stopped.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.GetStatus() == StatusStopped {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if s.GetStatus() != StatusStopped {
		t.Errorf("status = %q, want stopped after turn cap fired", s.GetStatus())
	}
}

func TestProcessEvent_Result_TurnCap_BelowCap_DoesNotFire(t *testing.T) {
	s := NewSDKSession(SessionConfig{MaxTurnsPerSession: 10})
	s.processEvent(claude.Event{
		Type:   claude.TypeResult,
		Result: &claude.Result{NumTurns: 3, Usage: claude.Usage{InputTokens: 1}},
	})
	for _, e := range drainSDKEvents(s) {
		if _, ok := e.(EventTurnCapReached); ok {
			t.Error("turn cap must not fire below the cap")
		}
	}
	s.mu.RLock()
	fired := s.turnCapFired
	s.mu.RUnlock()
	if fired {
		t.Error("turnCapFired should remain false below the cap")
	}
}

func TestProcessEvent_Result_TurnCap_Disabled_WhenZero(t *testing.T) {
	s := NewSDKSession(SessionConfig{MaxTurnsPerSession: 0})
	s.processEvent(claude.Event{
		Type:   claude.TypeResult,
		Result: &claude.Result{NumTurns: 999, Usage: claude.Usage{InputTokens: 1}},
	})
	for _, e := range drainSDKEvents(s) {
		if _, ok := e.(EventTurnCapReached); ok {
			t.Error("cap=0 means unlimited; must never fire EventTurnCapReached")
		}
	}
}

func TestProcessEvent_Result_TurnCap_AlreadyFired_DoesNotRefire(t *testing.T) {
	s := NewSDKSession(SessionConfig{MaxTurnsPerSession: 2})
	s.mu.Lock()
	s.turnCapFired = true // simulate a prior firing
	s.mu.Unlock()
	s.processEvent(claude.Event{
		Type:   claude.TypeResult,
		Result: &claude.Result{NumTurns: 50, Usage: claude.Usage{InputTokens: 1}},
	})
	for _, e := range drainSDKEvents(s) {
		if _, ok := e.(EventTurnCapReached); ok {
			t.Error("turn cap already fired; must not fire a second time")
		}
	}
}

// --- processEvent: tool invocation pairing (default branch) ----------------

func TestProcessEvent_UserToolResult_EmitsToolInvocation(t *testing.T) {
	s := NewSDKSession(SessionConfig{})
	// First: assistant message carrying a tool_use block.
	assistantRaw := json.RawMessage(`{"type":"assistant","message":{"role":"assistant","content":[` +
		`{"type":"tool_use","id":"tu-77","name":"Bash","input":{"command":"echo hi"}}]}}`)
	s.processEvent(claude.Event{Type: claude.TypeAssistant, Raw: assistantRaw})

	// Then: user message carrying the matching tool_result. This arrives
	// as an unknown MessageType (default branch).
	userRaw := json.RawMessage(`{"type":"user","message":{"role":"user","content":[` +
		`{"type":"tool_result","tool_use_id":"tu-77","content":"hi\n","is_error":false}]}}`)
	s.processEvent(claude.Event{Type: claude.MessageType("user"), Raw: userRaw})

	var inv *EventToolInvocation
	for _, e := range drainSDKEvents(s) {
		if ev, ok := e.(EventToolInvocation); ok {
			cp := ev
			inv = &cp
		}
	}
	if inv == nil {
		t.Fatal("expected an EventToolInvocation when tool_use pairs with tool_result")
	}
	if inv.Name != "Bash" {
		t.Errorf("invocation Name = %q, want Bash", inv.Name)
	}
	if inv.ToolUseID != "tu-77" {
		t.Errorf("invocation ToolUseID = %q, want tu-77", inv.ToolUseID)
	}
	if inv.InputHash == "" || inv.ResultHash == "" {
		t.Errorf("invocation hashes must be populated: in=%q result=%q", inv.InputHash, inv.ResultHash)
	}
	if inv.IsError {
		t.Error("IsError should be false for a successful tool result")
	}
}

func TestProcessEvent_UserToolResult_ErrorFlag(t *testing.T) {
	s := NewSDKSession(SessionConfig{})
	assistantRaw := json.RawMessage(`{"type":"assistant","message":{"role":"assistant","content":[` +
		`{"type":"tool_use","id":"tu-err","name":"Read","input":{"path":"/missing"}}]}}`)
	s.processEvent(claude.Event{Type: claude.TypeAssistant, Raw: assistantRaw})
	userRaw := json.RawMessage(`{"type":"user","message":{"role":"user","content":[` +
		`{"type":"tool_result","tool_use_id":"tu-err","content":"no such file","is_error":true}]}}`)
	s.processEvent(claude.Event{Type: claude.MessageType("user"), Raw: userRaw})

	var inv *EventToolInvocation
	for _, e := range drainSDKEvents(s) {
		if ev, ok := e.(EventToolInvocation); ok {
			cp := ev
			inv = &cp
		}
	}
	if inv == nil {
		t.Fatal("expected EventToolInvocation")
	}
	if !inv.IsError {
		t.Error("IsError should be true when tool_result.is_error=true")
	}
}

func TestProcessEvent_Result_ResetsToolTracker(t *testing.T) {
	s := NewSDKSession(SessionConfig{})
	// Stash a pending tool_use.
	assistantRaw := json.RawMessage(`{"type":"assistant","message":{"role":"assistant","content":[` +
		`{"type":"tool_use","id":"tu-stale","name":"Grep","input":{}}]}}`)
	s.processEvent(claude.Event{Type: claude.TypeAssistant, Raw: assistantRaw})
	s.toolTracker.mu.Lock()
	before := len(s.toolTracker.pending)
	s.toolTracker.mu.Unlock()
	if before != 1 {
		t.Fatalf("precondition: expected 1 pending tool_use, got %d", before)
	}

	// A result event should clear stale pending pairings.
	s.processEvent(claude.Event{
		Type:   claude.TypeResult,
		Result: &claude.Result{NumTurns: 1, Usage: claude.Usage{InputTokens: 1}},
	})
	s.toolTracker.mu.Lock()
	after := len(s.toolTracker.pending)
	s.toolTracker.mu.Unlock()
	if after != 0 {
		t.Errorf("result event should reset tool tracker; pending=%d, want 0", after)
	}
}

func TestProcessEvent_UnknownNonUserType_Ignored(t *testing.T) {
	s := NewSDKSession(SessionConfig{})
	// An assistant-shaped Raw arriving under an unknown type but with
	// type:"assistant" (not "user") must NOT pair tool_results.
	raw := json.RawMessage(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hi"}]}}`)
	s.processEvent(claude.Event{Type: claude.MessageType("compact_boundary"), Raw: raw})
	if evs := drainSDKEvents(s); len(evs) != 0 {
		t.Errorf("non-user unknown type should emit nothing, got %d events", len(evs))
	}
}

// --- emitEvent / appendOutput / observers ----------------------------------

func TestEmitEvent_DropsWhenChannelFull_AndCounts(t *testing.T) {
	s := NewSDKSession(SessionConfig{})
	// Fill the 256-slot buffer entirely.
	for i := 0; i < eventChannelSize; i++ {
		s.emitEvent(EventIdle{})
	}
	if s.DroppedEvents() != 0 {
		t.Fatalf("no drops expected yet, got %d", s.DroppedEvents())
	}
	// The next emits must be dropped (non-blocking default branch).
	s.emitEvent(EventIdle{})
	s.emitEvent(EventIdle{})
	if got := s.DroppedEvents(); got != 2 {
		t.Errorf("DroppedEvents = %d, want 2", got)
	}
}

func TestEmitEvent_NoOpWhenStopped(t *testing.T) {
	s := NewSDKSession(SessionConfig{})
	s.mu.Lock()
	s.isStopped = true
	s.mu.Unlock()
	s.emitEvent(EventWorking{})
	if evs := drainSDKEvents(s); len(evs) != 0 {
		t.Errorf("stopped session must not emit; got %d events", len(evs))
	}
}

func TestAppendOutput_WritesBuffersAndNotifiesObservers(t *testing.T) {
	s := NewSDKSession(SessionConfig{})
	var mu sync.Mutex
	var observed string
	unsub := s.AddOutputObserver(func(data string) {
		mu.Lock()
		observed += data
		mu.Unlock()
	})
	defer unsub()

	s.appendOutput("chunk-1 ")
	s.appendOutput("chunk-2")

	if got := s.GetTerminalBuffer(); got != "chunk-1 chunk-2" {
		t.Errorf("terminal buffer = %q, want 'chunk-1 chunk-2'", got)
	}
	mu.Lock()
	gotObserved := observed
	mu.Unlock()
	if gotObserved != "chunk-1 chunk-2" {
		t.Errorf("observer saw %q, want 'chunk-1 chunk-2'", gotObserved)
	}

	// terminal + output events both emitted per appendOutput call.
	var terminalCount, outputCount int
	for _, e := range drainSDKEvents(s) {
		switch e.(type) {
		case EventTerminal:
			terminalCount++
		case EventOutput:
			outputCount++
		}
	}
	if terminalCount != 2 || outputCount != 2 {
		t.Errorf("expected 2 terminal + 2 output events, got %d/%d", terminalCount, outputCount)
	}
}

func TestAddOutputObserver_UnsubscribeStopsCallbacks(t *testing.T) {
	s := NewSDKSession(SessionConfig{})
	var count int
	var mu sync.Mutex
	unsub := s.AddOutputObserver(func(string) {
		mu.Lock()
		count++
		mu.Unlock()
	})
	s.appendOutput("a")
	unsub()
	s.appendOutput("b")
	mu.Lock()
	final := count
	mu.Unlock()
	if final != 1 {
		t.Errorf("observer fired %d times, want 1 (unsubscribed before second append)", final)
	}
}

func TestNotifyOutputObservers_MultipleObservers(t *testing.T) {
	s := NewSDKSession(SessionConfig{})
	var mu sync.Mutex
	hits := map[string]int{}
	u1 := s.AddOutputObserver(func(string) { mu.Lock(); hits["a"]++; mu.Unlock() })
	u2 := s.AddOutputObserver(func(string) { mu.Lock(); hits["b"]++; mu.Unlock() })
	defer u1()
	defer u2()
	s.notifyOutputObservers("ping")
	mu.Lock()
	a, b := hits["a"], hits["b"]
	mu.Unlock()
	if a != 1 || b != 1 {
		t.Errorf("both observers should fire once each, got a=%d b=%d", a, b)
	}
}

// --- SendPrompt / Write* guards (no live subprocess) -----------------------

func TestSendPrompt_NilSession_NoPanic(t *testing.T) {
	s := NewSDKSession(SessionConfig{})
	// persistentSession is nil (never Started). Must warn-and-return.
	s.SendPrompt(context.Background(), "hello")
	// No status change because send was dropped.
	if s.GetStatus() != StatusStopped {
		t.Errorf("status = %q, want stopped (send dropped)", s.GetStatus())
	}
	if s.IsWorking() {
		t.Error("dropped prompt must not flip isWorking")
	}
}

func TestSendPrompt_StoppedSession_Drops(t *testing.T) {
	s := NewSDKSession(SessionConfig{})
	s.mu.Lock()
	s.isStopped = true
	s.mu.Unlock()
	s.SendPrompt(context.Background(), "hello")
	// Stopped guard returns before touching persistentSession (nil), so
	// no panic and no working transition.
	if s.IsWorking() {
		t.Error("stopped session must not flip isWorking on SendPrompt")
	}
}

func TestWriteToPTY_RejectsEmptyAndCR(t *testing.T) {
	s := NewSDKSession(SessionConfig{})
	if s.WriteToPTY("") {
		t.Error("empty input should return false")
	}
	if s.WriteToPTY("   ") {
		t.Error("whitespace-only input should return false")
	}
	if s.WriteToPTY("\r") {
		t.Error("bare CR should return false")
	}
}

func TestWriteToPTY_AcceptsRealInput(t *testing.T) {
	s := NewSDKSession(SessionConfig{})
	// persistentSession is nil so the underlying send no-ops, but the
	// trimming/validation path returns true for real content.
	if !s.WriteToPTY("do something\r\n") {
		t.Error("non-empty input should return true")
	}
}

func TestWriteInkPrompt_Validation(t *testing.T) {
	s := NewSDKSession(SessionConfig{})
	if s.WriteInkPrompt("") {
		t.Error("empty prompt should return false")
	}
	if s.WriteInkPrompt("  \t ") {
		t.Error("whitespace-only prompt should return false")
	}
	if !s.WriteInkPrompt("real prompt") {
		t.Error("real prompt should return true")
	}
}

func TestWriteViaScreen_StripsControlChars(t *testing.T) {
	s := NewSDKSession(SessionConfig{})
	// Pure control sequence (Ctrl+C) -> nothing printable -> false.
	if s.WriteViaScreen("\x03") {
		t.Error("Ctrl+C only should return false (no printable content)")
	}
	if s.WriteViaScreen("\r") {
		t.Error("bare CR should return false")
	}
	// Real text mixed with control chars -> true.
	if !s.WriteViaScreen("hel\x03lo\r") {
		t.Error("text with embedded control chars should still return true")
	}
}

// --- Stop -------------------------------------------------------------------

func TestStop_Idempotent_NoStart(t *testing.T) {
	s := NewSDKSession(SessionConfig{})
	s.Stop() // never started: cancelFunc nil, wg empty -> must not hang
	if s.GetStatus() != StatusStopped {
		t.Errorf("status = %q, want stopped", s.GetStatus())
	}
	// done channel must be closed.
	select {
	case <-s.done:
	case <-time.After(time.Second):
		t.Error("done channel should be closed after Stop")
	}
	// Second Stop is a no-op (idempotent), must not panic or re-close.
	s.Stop()
}

func TestStop_ClearsObservers(t *testing.T) {
	s := NewSDKSession(SessionConfig{})
	s.AddOutputObserver(func(string) {})
	s.Stop()
	s.observerMu.Lock()
	observers := s.outputObservers
	s.observerMu.Unlock()
	if observers != nil {
		t.Errorf("Stop should clear observers, got %v", observers)
	}
}

func TestStop_GuardsFurtherEmits(t *testing.T) {
	s := NewSDKSession(SessionConfig{})
	s.Stop()
	// After stop, emitEvent must be a no-op (isStopped guard).
	s.emitEvent(EventWorking{})
	if evs := drainSDKEvents(s); len(evs) != 0 {
		t.Errorf("no events should be emitted after Stop, got %d", len(evs))
	}
}

// --- getters / setters ------------------------------------------------------

func TestSetName_RoundTrips(t *testing.T) {
	s := NewSDKSession(SessionConfig{Name: "orig"})
	s.SetName("renamed")
	if s.GetName() != "renamed" {
		t.Errorf("name = %q, want renamed", s.GetName())
	}
}

func TestSetClaudeSessionIDForTest(t *testing.T) {
	s := NewSDKSession(SessionConfig{})
	s.SetClaudeSessionIDForTest("seeded-id")
	if s.ClaudeSessionID() != "seeded-id" {
		t.Errorf("claudeSessionID = %q, want seeded-id", s.ClaudeSessionID())
	}
}

func TestGetActiveTools_EmptyInitially(t *testing.T) {
	s := NewSDKSession(SessionConfig{})
	if got := s.GetActiveTools(); len(got) != 0 {
		t.Errorf("expected no active tools, got %v", got)
	}
}

func TestGetActiveTools_ReflectsMap(t *testing.T) {
	s := NewSDKSession(SessionConfig{})
	s.mu.Lock()
	s.activeTools["Bash"] = true
	s.activeTools["Read"] = true
	s.mu.Unlock()
	got := s.GetActiveTools()
	if len(got) != 2 {
		t.Fatalf("expected 2 active tools, got %v", got)
	}
	found := map[string]bool{}
	for _, tool := range got {
		found[tool] = true
	}
	if !found["Bash"] || !found["Read"] {
		t.Errorf("active tools = %v, want Bash and Read", got)
	}
}

func TestEvents_ReturnsChannel(t *testing.T) {
	s := NewSDKSession(SessionConfig{})
	if s.Events() == nil {
		t.Error("Events() must return a non-nil channel")
	}
	// Emitting then reading via the public channel.
	s.emitEvent(EventWorking{})
	select {
	case e := <-s.Events():
		if _, ok := e.(EventWorking); !ok {
			t.Errorf("expected EventWorking, got %T", e)
		}
	case <-time.After(time.Second):
		t.Error("expected to read the emitted event from Events()")
	}
}

// --- normaliseEffort / stripControlChars (pure helpers) --------------------

func TestNormaliseEffort_AllTiers(t *testing.T) {
	cases := map[string]claude.EffortLevel{
		"low":     claude.EffortLow,
		"LOW":     claude.EffortLow,
		"medium":  claude.EffortMedium,
		"med":     claude.EffortMedium,
		"high":    claude.EffortHigh,
		" High ":  claude.EffortHigh,
		"xhigh":   claude.EffortMax,
		"max":     claude.EffortMax,
		"maximum": claude.EffortMax,
		"":        claude.EffortLevel(""),
		"bogus":   claude.EffortLevel(""),
	}
	for in, want := range cases {
		if got := normaliseEffort(in); got != want {
			t.Errorf("normaliseEffort(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStripControlChars(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"hello", "hello"},
		{"a\x03b", "ab"},         // Ctrl+C stripped
		{"keep\nnewline", "keep\nnewline"},
		{"keep\ttab", "keep\ttab"},
		{"del\x7fhere", "delhere"}, // DEL stripped
		{"\x00\x01\x02", ""},        // all C0 controls stripped
		{"unicode-café", "unicode-café"},
	}
	for _, c := range cases {
		if got := stripControlChars(c.in); got != c.want {
			t.Errorf("stripControlChars(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// --- concurrency under -race -----------------------------------------------

func TestSDKSession_ConcurrentProcessEventAndReaders(t *testing.T) {
	s := NewSDKSession(SessionConfig{})
	var wg sync.WaitGroup

	// Drain the events channel continuously so emits never block/drop
	// the whole run (and so the race detector exercises the channel).
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-s.events:
			case <-stop:
				return
			}
		}
	}()

	// Writers: feed a mix of event types concurrently.
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				s.processEvent(claude.Event{
					Type: claude.TypeStreamEvent,
					StreamEvent: &claude.StreamEventMessage{
						Event: claude.StreamEvent{Delta: &claude.StreamEventDelta{Type: "text_delta", Text: "x"}},
					},
				})
				s.processEvent(claude.Event{
					Type:   claude.TypeResult,
					Result: &claude.Result{NumTurns: 1, Usage: claude.Usage{InputTokens: 1, OutputTokens: 1}},
				})
			}
		}()
	}
	// Readers: hammer the lock-guarded getters concurrently.
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				_ = s.GetStatus()
				_ = s.IsWorking()
				_ = s.ClaudeSessionID()
				_ = s.GetTerminalBuffer()
				_ = s.GetActiveTools()
				_ = s.DroppedEvents()
			}
		}()
	}
	wg.Wait()
	close(stop)

	// Tokens must have accumulated deterministically: 4 writers * 50
	// result events, each contributing 1 in / 1 out.
	s.mu.RLock()
	in, out := s.totalInputTokens, s.totalOutputTokens
	s.mu.RUnlock()
	if in != 200 || out != 200 {
		t.Errorf("accumulated tokens in=%d out=%d, want 200/200", in, out)
	}
}
