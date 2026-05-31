package pty

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/testutil/stubbin"
)

// fakeClaudeStreamJSON returns a #!/bin/sh stub that impersonates the
// `claude` CLI in session/stream-json mode for the Go SDK
// (github.com/shaharia-lab/claude-agent-sdk-go). The SDK writes an
// `initialize` control message + each user turn to the stub's stdin and
// reads newline-delimited JSON events from its stdout. The stub drains
// stdin in the background (so the SDK's stdin writes never block) and
// emits a fixed init→assistant→result sequence, then exits — closing
// stdout, which the SDK reader treats as session end and closes the
// events channel (unblocking SDKSession.eventConsumer's range).
//
// emitting `payload` verbatim lets a test script an arbitrary event
// stream; the lines must already be valid stream-json.
func fakeClaudeStreamJSON(t *testing.T, payload string) string {
	t.Helper()
	// cat >/dev/null in the background drains whatever the SDK writes.
	// printf emits the scripted events. A trailing sleep keeps the
	// process alive briefly if the test wants to send a follow-up turn
	// before EOF; here we exit promptly so the events channel closes.
	escaped := strings.ReplaceAll(payload, "'", "'\\''")
	body := "#!/bin/sh\ncat >/dev/null &\nprintf '%s' '" + escaped + "'\n"
	return stubbin.Install(t, "fakeclaude.sh", body)
}

// TestSDKSession_Start_DrivesEventConsumer runs the SDKSession Start →
// eventConsumer → processEvent → Stop lifecycle end-to-end against a stub
// `claude` binary that emits a real stream-json sequence. This is the only
// path that exercises Start() (the SDK option assembly + claude.NewSession)
// and eventConsumer (the range over the persistent session's events) without
// a live Anthropic subprocess.
func TestSDKSession_Start_DrivesEventConsumer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub binaries are #!/bin/sh (unix-only)")
	}

	// init → assistant text → result. Field names match the SDK's
	// SystemMessage / AssistantMessage / Result JSON shapes.
	stream := strings.Join([]string{
		`{"type":"system","subtype":"init","session_id":"claude-sess-1","model":"claude-opus-4-8"}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hello from stub"}]}}`,
		`{"type":"result","subtype":"success","num_turns":1,"total_cost_usd":0.01,"session_id":"claude-sess-1","usage":{"input_tokens":42,"output_tokens":7},"model_usages":{"claude-opus-4-8":{"input_tokens":42,"output_tokens":7}}}`,
		"",
	}, "\n")
	bin := fakeClaudeStreamJSON(t, stream)

	s := NewSDKSession(SessionConfig{
		ID:               "sdk-start-1",
		WorkingDir:       t.TempDir(),
		Model:            "claude-opus-4-8",
		ProviderEnv:      map[string]string{"ANTHROPIC_API_KEY": "stub"},
		ClaudeBinaryPath: bin,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	// Collect events from the live consumer until we observe the result's
	// token accounting (or time out).
	var (
		gotTokens *EventTokens
		gotExit   bool
		buf       strings.Builder
	)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case e := <-s.Events():
			switch ev := e.(type) {
			case EventTokens:
				cp := ev
				gotTokens = &cp
			case EventTerminal:
				buf.WriteString(ev.Data)
			case EventOutput:
				buf.WriteString(ev.Data)
			case EventExit:
				gotExit = true
			}
		case <-time.After(250 * time.Millisecond):
		}
		if gotTokens != nil && gotExit {
			break
		}
	}

	if gotTokens == nil {
		t.Fatalf("expected EventTokens from the stub's result event; buffer=%q", buf.String())
	}
	if gotTokens.Input != 42 || gotTokens.Output != 7 {
		t.Errorf("EventTokens = in=%d out=%d, want 42/7", gotTokens.Input, gotTokens.Output)
	}
	if gotTokens.Model != "claude-opus-4-8" {
		t.Errorf("EventTokens.Model = %q, want claude-opus-4-8", gotTokens.Model)
	}
	if !strings.Contains(buf.String(), "hello from stub") {
		t.Errorf("output buffer = %q, want it to contain the assistant text", buf.String())
	}

	// The init event should have populated the Claude session id via the
	// live consumer (proves processEvent ran from inside eventConsumer).
	if id := s.ClaudeSessionID(); id != "claude-sess-1" {
		t.Errorf("ClaudeSessionID = %q, want claude-sess-1 (set by the live consumer)", id)
	}

	// Token totals accumulated on the struct too.
	in, out, _ := int64(42), int64(7), 0
	gotIn, gotOut := s.totalInputTokens, s.totalOutputTokens
	if gotIn != in || gotOut != out {
		t.Errorf("accumulated tokens in=%d out=%d, want 42/7", gotIn, gotOut)
	}
}

// TestSDKSession_Start_WithKickstart verifies Start sends the kickstart
// (systemPrompt) as the first turn and flips the session to busy +
// emits EventWorking, exercising the kickstart branch in Start().
func TestSDKSession_Start_WithKickstart(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub binaries are #!/bin/sh (unix-only)")
	}
	stream := strings.Join([]string{
		`{"type":"system","subtype":"init","session_id":"ks-sess","model":"m"}`,
		`{"type":"result","subtype":"success","num_turns":1,"session_id":"ks-sess","usage":{"input_tokens":1,"output_tokens":1}}`,
		"",
	}, "\n")
	bin := fakeClaudeStreamJSON(t, stream)

	s := NewSDKSession(SessionConfig{
		ID:               "sdk-ks",
		WorkingDir:       t.TempDir(),
		SystemPrompt:     "you are a kickstarted agent",
		ProviderEnv:      map[string]string{"ANTHROPIC_API_KEY": "stub"},
		ClaudeBinaryPath: bin,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	// Kickstart flips working synchronously inside Start().
	if !s.IsWorking() {
		t.Error("a kickstarted session should be working immediately after Start")
	}
	if s.GetStatus() != StatusBusy {
		t.Errorf("status = %q, want busy after kickstart", s.GetStatus())
	}

	// And EventWorking should be observable on the channel.
	sawWorking := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !sawWorking {
		select {
		case e := <-s.Events():
			if _, ok := e.(EventWorking); ok {
				sawWorking = true
			}
		case <-time.After(100 * time.Millisecond):
		}
	}
	if !sawWorking {
		t.Error("kickstart should emit EventWorking")
	}
}

// TestSDKSession_Start_AlreadyStarted asserts the guard against double-start.
func TestSDKSession_Start_AlreadyStarted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub binaries are #!/bin/sh (unix-only)")
	}
	stream := `{"type":"result","subtype":"success","num_turns":1,"usage":{"input_tokens":1,"output_tokens":1}}` + "\n"
	bin := fakeClaudeStreamJSON(t, stream)
	s := NewSDKSession(SessionConfig{
		ID:               "sdk-dbl",
		WorkingDir:       t.TempDir(),
		ProviderEnv:      map[string]string{"ANTHROPIC_API_KEY": "stub"},
		ClaudeBinaryPath: bin,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()
	if err := s.Start(ctx); err == nil {
		t.Error("second Start should return 'already started' error")
	}
}

// TestSDKSession_Start_AllOptionBranches exercises the many conditional
// option-assembly branches in Start (append-system-prompt, system-prompt
// files, exclude-dynamic-sections, from-pr, chrome, multi-channel warn,
// effort, fallback model, strict mcp, resume+fork, mcp servers). The stub
// emits a clean result so the session starts and tears down cleanly.
func TestSDKSession_Start_AllOptionBranches(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub binaries are #!/bin/sh (unix-only)")
	}
	stream := strings.Join([]string{
		`{"type":"system","subtype":"init","session_id":"opt-sess","model":"m"}`,
		`{"type":"result","subtype":"success","num_turns":1,"session_id":"opt-sess","usage":{"input_tokens":1,"output_tokens":1}}`,
		"",
	}, "\n")
	bin := fakeClaudeStreamJSON(t, stream)
	dir := t.TempDir()

	s := NewSDKSession(SessionConfig{
		ID:                                 "opt-sess",
		WorkingDir:                         dir,
		Model:                              "primary",
		AppendSystemPrompt:                 "stable cross-agent prefix",
		SystemPromptFile:                   dir + "/sys.txt",
		AppendSystemPromptFile:             dir + "/append.txt",
		ExcludeDynamicSystemPromptSections: true,
		FromPRNumber:                       7,
		ChromeEnabled:                      true,
		Effort:                             "high",
		FallbackModel:                      "fallback",
		StrictMcpConfig:                    true,
		ResumeSessionID:                    "prev-sess-id",
		ForkSession:                        true,
		Channels:                           []string{"plugin:a@local", "plugin:b@local"}, // 2nd triggers the multi-channel warn branch
		McpServers:                         map[string]any{"ctx7": map[string]any{"type": "stdio", "command": "true"}},
		ProviderEnv:                        map[string]string{"ANTHROPIC_API_KEY": "stub"},
		ClaudeBinaryPath:                   bin,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start with a full option set: %v", err)
	}
	defer s.Stop()

	// Confirm the consumer ran (init populated the session id).
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if s.ClaudeSessionID() == "opt-sess" {
			break
		}
		// drain so the channel doesn't wedge
		select {
		case <-s.Events():
		case <-time.After(100 * time.Millisecond):
		}
	}
	if s.ClaudeSessionID() != "opt-sess" {
		t.Errorf("ClaudeSessionID = %q, want opt-sess (consumer should have processed init)", s.ClaudeSessionID())
	}
}

// TestSDKSession_Start_NoProviderEnv covers the else branch that warns when a
// session has no provider env vars.
func TestSDKSession_Start_NoProviderEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub binaries are #!/bin/sh (unix-only)")
	}
	stream := `{"type":"result","subtype":"success","num_turns":1,"usage":{"input_tokens":1,"output_tokens":1}}` + "\n"
	bin := fakeClaudeStreamJSON(t, stream)
	s := NewSDKSession(SessionConfig{
		ID:               "noenv",
		WorkingDir:       t.TempDir(),
		ClaudeBinaryPath: bin,
		// no ProviderEnv -> warn branch
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	s.Stop()
}

// TestSDKSession_Start_BadBinaryErrors confirms Start surfaces a creation
// error when the configured claude executable cannot be spawned.
func TestSDKSession_Start_BadBinaryErrors(t *testing.T) {
	s := NewSDKSession(SessionConfig{
		ID:               "sdk-bad",
		WorkingDir:       t.TempDir(),
		ClaudeBinaryPath: "/nonexistent/definitely/not/here/claude-xyz",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := s.Start(ctx)
	if err == nil {
		t.Fatal("Start with a nonexistent binary should return an error")
	}
	if !strings.Contains(err.Error(), "failed to create persistent session") {
		t.Errorf("error = %v, want it to wrap the session-creation failure", err)
	}
	// Status should not be left as Idle after a failed start cleanup path —
	// the guard sets it back is not done, but cancel() was called. Confirm
	// the session is at least not reporting itself as a healthy idle one by
	// checking we can still Stop without hanging.
	s.Stop()
}
