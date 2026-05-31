package pty

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestIsCodexToolItem(t *testing.T) {
	toolTypes := []string{"command_execution", "file_change", "mcp_tool_call", "web_search"}
	for _, tt := range toolTypes {
		if !isCodexToolItem(tt) {
			t.Errorf("isCodexToolItem(%q) = false, want true", tt)
		}
	}
	nonTool := []string{"agent_message", "reasoning", "plan_update", "", "unknown"}
	for _, nt := range nonTool {
		if isCodexToolItem(nt) {
			t.Errorf("isCodexToolItem(%q) = true, want false", nt)
		}
	}
}

func TestCodexToolName(t *testing.T) {
	cases := []struct {
		item *codexItem
		want string
	}{
		{&codexItem{Type: "command_execution"}, "Bash"},
		{&codexItem{Type: "file_change"}, "Edit"},
		{&codexItem{Type: "web_search"}, "WebSearch"},
		{&codexItem{Type: "mcp_tool_call", Server: "ctx7", Tool: "query"}, "mcp__ctx7__query"},
		{&codexItem{Type: "mcp_tool_call"}, "mcp"},               // missing server/tool
		{&codexItem{Type: "mcp_tool_call", Server: "only"}, "mcp"}, // missing tool
		{&codexItem{Type: "plan_update"}, "plan_update"},          // unknown -> echoes type
	}
	for _, c := range cases {
		if got := codexToolName(c.item); got != c.want {
			t.Errorf("codexToolName(%+v) = %q, want %q", c.item, got, c.want)
		}
	}
}

func TestCodexSession_SetWorking_TransitionsOnce(t *testing.T) {
	s := NewCodexSession(SessionConfig{})
	if s.IsWorking() {
		t.Fatal("precondition: should start not working")
	}
	s.setWorking()
	if !s.IsWorking() {
		t.Error("setWorking should set isWorking=true")
	}
	if s.GetStatus() != StatusBusy {
		t.Errorf("status = %q, want busy", s.GetStatus())
	}
	var workingCount int
	for _, e := range drainEvents(s) {
		if _, ok := e.(EventWorking); ok {
			workingCount++
		}
	}
	if workingCount != 1 {
		t.Errorf("first setWorking should emit one EventWorking, got %d", workingCount)
	}
	// Second call while already working must not re-emit.
	s.setWorking()
	for _, e := range drainEvents(s) {
		if _, ok := e.(EventWorking); ok {
			t.Error("setWorking while already working must not re-emit EventWorking")
		}
	}
}

func TestCodexSession_SetIdle_ClearsState(t *testing.T) {
	s := NewCodexSession(SessionConfig{})
	s.mu.Lock()
	s.isWorking = true
	s.status = StatusBusy
	s.activeTools["Bash"] = true
	s.mu.Unlock()

	s.setIdle()
	if s.IsWorking() {
		t.Error("setIdle should clear isWorking")
	}
	if s.GetStatus() != StatusIdle {
		t.Errorf("status = %q, want idle", s.GetStatus())
	}
	if len(s.GetActiveTools()) != 0 {
		t.Errorf("setIdle should clear active tools, got %v", s.GetActiveTools())
	}
}

func TestCodexSession_FailTurn_EmitsErrorAndIdle(t *testing.T) {
	s := NewCodexSession(SessionConfig{})
	s.mu.Lock()
	s.isWorking = true
	s.status = StatusBusy
	s.mu.Unlock()

	s.failTurn("binary not found")

	if s.GetStatus() != StatusIdle {
		t.Errorf("failTurn should leave status idle, got %q", s.GetStatus())
	}
	var sawTurnError, sawIdle bool
	for _, e := range drainEvents(s) {
		switch ev := e.(type) {
		case EventTurnError:
			sawTurnError = true
			if ev.Subtype != "exec-failed" {
				t.Errorf("Subtype = %q, want exec-failed", ev.Subtype)
			}
			if ev.Result != "binary not found" {
				t.Errorf("Result = %q, want 'binary not found'", ev.Result)
			}
		case EventIdle:
			sawIdle = true
		}
	}
	if !sawTurnError {
		t.Error("failTurn should emit EventTurnError")
	}
	if !sawIdle {
		t.Error("failTurn should emit EventIdle")
	}
}

func TestCodexSession_AppendOutput_BuffersAndObservers(t *testing.T) {
	s := NewCodexSession(SessionConfig{})
	var mu sync.Mutex
	var seen string
	unsub := s.AddOutputObserver(func(d string) {
		mu.Lock()
		seen += d
		mu.Unlock()
	})
	defer unsub()

	s.appendOutput("foo")
	s.appendOutput("bar")
	if got := s.GetTerminalBuffer(); got != "foobar" {
		t.Errorf("terminal buffer = %q, want foobar", got)
	}
	mu.Lock()
	gotSeen := seen
	mu.Unlock()
	if gotSeen != "foobar" {
		t.Errorf("observer saw %q, want foobar", gotSeen)
	}
}

func TestCodexSession_EmitEvent_StoppedNoOp(t *testing.T) {
	s := NewCodexSession(SessionConfig{})
	s.mu.Lock()
	s.isStopped = true
	s.mu.Unlock()
	s.emitEvent(EventWorking{})
	if evs := drainEvents(s); len(evs) != 0 {
		t.Errorf("stopped codex session must not emit, got %d events", len(evs))
	}
}

func TestCodexSession_EmitEvent_DropsWhenFull(t *testing.T) {
	s := NewCodexSession(SessionConfig{})
	// events channel is eventChannelSize (256). Fill it.
	for i := 0; i < eventChannelSize; i++ {
		s.emitEvent(EventIdle{})
	}
	s.emitEvent(EventIdle{}) // overflow -> dropped
	if got := s.DroppedEvents(); got != 1 {
		t.Errorf("DroppedEvents = %d, want 1", got)
	}
}

func TestCodexSession_SendPrompt_StoppedDrops(t *testing.T) {
	s := NewCodexSession(SessionConfig{})
	s.mu.Lock()
	s.isStopped = true
	s.mu.Unlock()
	s.SendPrompt(context.Background(), "hi")
	// Prompt queue must remain empty since stopped guard returned early.
	select {
	case p := <-s.prompts:
		t.Errorf("stopped session must not enqueue prompt; got %q", p)
	default:
	}
}

func TestCodexSession_SendPrompt_Enqueues(t *testing.T) {
	s := NewCodexSession(SessionConfig{})
	s.SendPrompt(context.Background(), "do work")
	select {
	case p := <-s.prompts:
		if p != "do work" {
			t.Errorf("queued prompt = %q, want 'do work'", p)
		}
	default:
		t.Error("SendPrompt should have enqueued the prompt")
	}
}

func TestCodexSession_WriteToPTY(t *testing.T) {
	s := NewCodexSession(SessionConfig{})
	if s.WriteToPTY("") {
		t.Error("empty input -> false")
	}
	if s.WriteToPTY("\r") {
		t.Error("bare CR -> false")
	}
	if s.WriteToPTY("   ") {
		t.Error("whitespace -> false")
	}
	if !s.WriteToPTY("real input\r\n") {
		t.Error("real input -> true")
	}
	// Verify the trimmed prompt landed on the queue.
	select {
	case p := <-s.prompts:
		if p != "real input" {
			t.Errorf("enqueued %q, want 'real input' (trimmed)", p)
		}
	default:
		t.Error("WriteToPTY should enqueue a prompt")
	}
}

func TestCodexSession_WriteInkPrompt(t *testing.T) {
	s := NewCodexSession(SessionConfig{})
	if s.WriteInkPrompt("  ") {
		t.Error("whitespace-only -> false")
	}
	if !s.WriteInkPrompt("hello") {
		t.Error("real prompt -> true")
	}
}

func TestCodexSession_WriteViaScreen_StripsControls(t *testing.T) {
	s := NewCodexSession(SessionConfig{})
	if s.WriteViaScreen("\x03") {
		t.Error("Ctrl+C only -> false")
	}
	if !s.WriteViaScreen("ab\x03cd") {
		t.Error("text with control chars -> true")
	}
	select {
	case p := <-s.prompts:
		if p != "abcd" {
			t.Errorf("enqueued %q, want 'abcd' (controls stripped)", p)
		}
	default:
		t.Error("WriteViaScreen should enqueue cleaned prompt")
	}
}

func TestCodexSession_GettersAndSetters(t *testing.T) {
	s := NewCodexSession(SessionConfig{Name: "codex-1"})
	if s.GetName() != "codex-1" {
		t.Errorf("name = %q, want codex-1", s.GetName())
	}
	s.SetName("codex-renamed")
	if s.GetName() != "codex-renamed" {
		t.Errorf("name = %q, want codex-renamed", s.GetName())
	}
	s.SetClaudeSessionIDForTest("thread-xyz")
	if s.ClaudeSessionID() != "thread-xyz" {
		t.Errorf("threadID = %q, want thread-xyz", s.ClaudeSessionID())
	}
	if s.Events() == nil {
		t.Error("Events() must be non-nil")
	}
}

func TestCodexSession_AddOutputObserver_Unsubscribe(t *testing.T) {
	s := NewCodexSession(SessionConfig{})
	var mu sync.Mutex
	count := 0
	unsub := s.AddOutputObserver(func(string) {
		mu.Lock()
		count++
		mu.Unlock()
	})
	s.notifyOutputObservers("a")
	unsub()
	s.notifyOutputObservers("b")
	mu.Lock()
	final := count
	mu.Unlock()
	if final != 1 {
		t.Errorf("observer fired %d times, want 1", final)
	}
}

func TestCodexSession_Stop_Idempotent(t *testing.T) {
	s := NewCodexSession(SessionConfig{})
	s.Stop()
	if s.GetStatus() != StatusStopped {
		t.Errorf("status = %q, want stopped", s.GetStatus())
	}
	select {
	case <-s.done:
	case <-time.After(time.Second):
		t.Error("done channel should be closed after Stop")
	}
	s.Stop() // second stop must be a no-op (no panic)
}

func TestCodexConsumeJSONL_ToolItemsAndReasoning(t *testing.T) {
	// Install a deterministic cost function for the duration of the test.
	prev := CodexCostFunc
	CodexCostFunc = func(model string, in, out int64) float64 { return float64(in+out) * 0.001 }
	defer func() { CodexCostFunc = prev }()

	stream := strings.Join([]string{
		`{"type":"thread.started","thread_id":"th-1"}`,
		`{"type":"turn.started"}`,
		`{"type":"item.started","item":{"type":"command_execution","id":"cmd-1"}}`,
		`{"type":"item.completed","item":{"type":"reasoning","id":"r-1"}}`,
		`{"type":"item.completed","item":{"type":"command_execution","id":"cmd-1","status":"completed"}}`,
		`{"type":"item.completed","item":{"type":"web_search","id":"ws-1","status":"failed"}}`,
		`{"type":"item.completed","item":{"type":"agent_message","text":"all done"}}`,
		`{"type":"turn.completed","usage":{"input_tokens":100,"output_tokens":20,"reasoning_output_tokens":5}}`,
	}, "\n")

	s := NewCodexSession(SessionConfig{Model: "gpt-5.5"})
	if !s.consumeJSONL(strings.NewReader(stream)) {
		t.Fatal("expected sawCompleted=true for a stream ending in turn.completed")
	}
	if s.ClaudeSessionID() != "th-1" {
		t.Errorf("threadID = %q, want th-1", s.ClaudeSessionID())
	}

	// Token accounting: out = output_tokens + reasoning_output_tokens = 25.
	s.mu.RLock()
	in, out, cost := s.totalInputTokens, s.totalOutputTokens, s.totalCostUSD
	s.mu.RUnlock()
	if in != 100 || out != 25 {
		t.Errorf("tokens in=%d out=%d, want 100/25", in, out)
	}
	if cost < 0.124 || cost > 0.126 {
		t.Errorf("cost = %v, want ~0.125 (125 tokens * 0.001)", cost)
	}

	var toolNames []string
	var errFlags []bool
	var gotAgentMessage, gotTokens bool
	for _, e := range drainEvents(s) {
		switch ev := e.(type) {
		case EventToolInvocation:
			toolNames = append(toolNames, ev.Name)
			errFlags = append(errFlags, ev.IsError)
		case EventOutput:
			if strings.Contains(ev.Data, "all done") {
				gotAgentMessage = true
			}
		case EventTokens:
			gotTokens = true
			if ev.Model != "gpt-5.5" {
				t.Errorf("EventTokens.Model = %q, want gpt-5.5", ev.Model)
			}
		}
	}
	if !gotAgentMessage {
		t.Error("expected agent_message text 'all done' in output events")
	}
	if !gotTokens {
		t.Error("expected an EventTokens from turn.completed")
	}
	// command_execution -> Bash, web_search -> WebSearch.
	foundBash, foundWebSearch, webSearchErr := false, false, false
	for i, n := range toolNames {
		if n == "Bash" {
			foundBash = true
		}
		if n == "WebSearch" {
			foundWebSearch = true
			webSearchErr = errFlags[i]
		}
	}
	if !foundBash {
		t.Errorf("expected a Bash tool invocation, got %v", toolNames)
	}
	if !foundWebSearch {
		t.Errorf("expected a WebSearch tool invocation, got %v", toolNames)
	}
	if !webSearchErr {
		t.Error("web_search with status=failed should set IsError=true")
	}
}

func TestCodexConsumeJSONL_StreamErrorAndTurnFailed(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"error","message":"transient blip"}`,
		`{"type":"turn.failed","error":{"message":"hard stop"}}`,
	}, "\n")
	s := NewCodexSession(SessionConfig{Model: "gpt-5.5"})
	if s.consumeJSONL(strings.NewReader(stream)) {
		t.Error("no turn.completed -> sawCompleted should be false")
	}
	if !strings.Contains(s.GetTerminalBuffer(), "transient blip") {
		t.Errorf("buffer = %q, want the stream error appended", s.GetTerminalBuffer())
	}
	var turnErr *EventTurnError
	for _, e := range drainEvents(s) {
		if ev, ok := e.(EventTurnError); ok {
			cp := ev
			turnErr = &cp
		}
	}
	if turnErr == nil {
		t.Fatal("expected an EventTurnError from turn.failed")
	}
	if turnErr.Subtype != "turn.failed" {
		t.Errorf("Subtype = %q, want turn.failed", turnErr.Subtype)
	}
	if turnErr.Result != "hard stop" {
		t.Errorf("Result = %q, want 'hard stop' (from error.message)", turnErr.Result)
	}
}

func TestCodexConsumeJSONL_IgnoresGarbageLines(t *testing.T) {
	stream := strings.Join([]string{
		"not json at all",
		"",
		`{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}`,
	}, "\n")
	s := NewCodexSession(SessionConfig{Model: "gpt-5.5"})
	if !s.consumeJSONL(strings.NewReader(stream)) {
		t.Error("garbage lines should be skipped; turn.completed should still register")
	}
}

func TestCodexSession_GetActiveTools(t *testing.T) {
	s := NewCodexSession(SessionConfig{})
	if len(s.GetActiveTools()) != 0 {
		t.Error("new session should have no active tools")
	}
	s.mu.Lock()
	s.activeTools["WebSearch"] = true
	s.mu.Unlock()
	got := s.GetActiveTools()
	if len(got) != 1 || got[0] != "WebSearch" {
		t.Errorf("active tools = %v, want [WebSearch]", got)
	}
}
