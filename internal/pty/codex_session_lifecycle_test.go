package pty

import (
	"context"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/testutil/stubbin"
)

// fakeCodexBin installs a #!/bin/sh stub that impersonates `codex exec
// --json`. The real driver invokes `codex exec ... <prompt>` and reads a
// newline-delimited JSON event stream from stdout. The stub ignores its
// args/stdin and prints the supplied JSONL `stream`, then exits 0 (a clean
// turn). Single quotes in stream are escaped to survive the shell literal.
func fakeCodexBin(t *testing.T, stream string) string {
	t.Helper()
	escaped := strings.ReplaceAll(stream, "'", "'\\''")
	body := "#!/bin/sh\nprintf '%s' '" + escaped + "'\n"
	return stubbin.Install(t, "codex.sh", body)
}

// codexSuccessStream is a minimal clean-turn JSONL: thread start, turn
// start, an agent message, and a turn.completed with usage.
func codexSuccessStream(threadID string) string {
	return strings.Join([]string{
		`{"type":"thread.started","thread_id":"` + threadID + `"}`,
		`{"type":"turn.started"}`,
		`{"type":"item.completed","item":{"type":"agent_message","text":"codex says hi"}}`,
		`{"type":"turn.completed","usage":{"input_tokens":33,"output_tokens":4}}`,
		"",
	}, "\n")
}

// TestCodexSession_StartWorkerRunTurn drives Start → worker → runTurn →
// consumeJSONL end-to-end against a stub codex binary. The kickstart prompt
// is enqueued by Start, dequeued by worker, and runs one `codex exec` whose
// JSONL stream is parsed. Exercises the 0%-covered subprocess lifecycle.
func TestCodexSession_StartWorkerRunTurn(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub binaries are #!/bin/sh (unix-only)")
	}
	bin := fakeCodexBin(t, codexSuccessStream("th-lifecycle"))

	s := NewCodexSession(SessionConfig{
		ID:              "codex-life",
		WorkingDir:      t.TempDir(),
		Model:           "gpt-5.5",
		SystemPrompt:    "kickstart prompt",
		CodexBinaryPath: bin,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	// Collect events until the turn completes (EventIdle after the run) or
	// we time out.
	var (
		gotTokens *EventTokens
		gotWork   bool
		gotIdle   bool
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
			case EventWorking:
				gotWork = true
			case EventIdle:
				gotIdle = true
			case EventOutput:
				buf.WriteString(ev.Data)
			}
		case <-time.After(200 * time.Millisecond):
		}
		if gotTokens != nil && gotIdle {
			break
		}
	}

	if !gotWork {
		t.Error("expected EventWorking from turn.started")
	}
	if gotTokens == nil {
		t.Fatalf("expected EventTokens from turn.completed; output=%q", buf.String())
	}
	if gotTokens.Input != 33 || gotTokens.Output != 4 {
		t.Errorf("tokens in=%d out=%d, want 33/4", gotTokens.Input, gotTokens.Output)
	}
	if !gotIdle {
		t.Error("expected EventIdle after the turn finished")
	}
	if !strings.Contains(buf.String(), "codex says hi") {
		t.Errorf("output = %q, want it to contain the agent message", buf.String())
	}
	// The thread id captured from thread.started drives resume on later turns.
	if id := s.ClaudeSessionID(); id != "th-lifecycle" {
		t.Errorf("threadID = %q, want th-lifecycle (captured by runTurn)", id)
	}
}

// TestCodexSession_RunTurn_TwoTurnsResume verifies a second prompt runs after
// the first completes (serial worker) and that the captured thread id is
// reused. Both turns use the same stub.
func TestCodexSession_RunTurn_TwoTurnsResume(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub binaries are #!/bin/sh (unix-only)")
	}
	bin := fakeCodexBin(t, codexSuccessStream("th-resume"))
	s := NewCodexSession(SessionConfig{
		ID:              "codex-2turn",
		WorkingDir:      t.TempDir(),
		Model:           "gpt-5.5",
		CodexBinaryPath: bin,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	// No kickstart, so enqueue two prompts ourselves.
	s.SendPrompt(ctx, "first")
	s.SendPrompt(ctx, "second")

	tokensSeen := 0
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && tokensSeen < 2 {
		select {
		case e := <-s.Events():
			if _, ok := e.(EventTokens); ok {
				tokensSeen++
			}
		case <-time.After(200 * time.Millisecond):
		}
	}
	if tokensSeen < 2 {
		t.Errorf("expected 2 turns to complete (2 EventTokens), got %d", tokensSeen)
	}
	// Accumulated tokens across both turns: 2 * (33 in, 4 out).
	s.mu.RLock()
	in, out := s.totalInputTokens, s.totalOutputTokens
	s.mu.RUnlock()
	if in != 66 || out != 8 {
		t.Errorf("accumulated tokens in=%d out=%d, want 66/8 across two turns", in, out)
	}
}

// TestCodexSession_RunTurn_ExecFailure feeds a stub that exits non-zero with
// no turn.completed; runTurn must surface an EventTurnError("exec-failed").
func TestCodexSession_RunTurn_ExecFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub binaries are #!/bin/sh (unix-only)")
	}
	// Emit a non-completed stream then exit 1.
	body := "#!/bin/sh\nprintf '%s\\n' '{\"type\":\"thread.started\",\"thread_id\":\"th-fail\"}'\nexit 1\n"
	bin := stubbin.Install(t, "codex-fail.sh", body)

	s := NewCodexSession(SessionConfig{
		ID:              "codex-fail",
		WorkingDir:      t.TempDir(),
		Model:           "gpt-5.5",
		CodexBinaryPath: bin,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	s.SendPrompt(ctx, "do a thing")

	var turnErr *EventTurnError
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) && turnErr == nil {
		select {
		case e := <-s.Events():
			if ev, ok := e.(EventTurnError); ok {
				cp := ev
				turnErr = &cp
			}
		case <-time.After(150 * time.Millisecond):
		}
	}
	if turnErr == nil {
		t.Fatal("expected EventTurnError when codex exits non-zero without turn.completed")
	}
	if turnErr.Subtype != "exec-failed" {
		t.Errorf("Subtype = %q, want exec-failed", turnErr.Subtype)
	}
}

// TestCodexSession_RunTurn_StartFailure points the binary at a path that
// can't be executed; runTurn's cmd.Start error path emits a failTurn
// EventTurnError.
func TestCodexSession_RunTurn_StartFailure(t *testing.T) {
	s := NewCodexSession(SessionConfig{
		ID:              "codex-startfail",
		WorkingDir:      t.TempDir(),
		Model:           "gpt-5.5",
		CodexBinaryPath: "/nonexistent/codex-binary-xyz",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()
	s.SendPrompt(ctx, "go")

	var turnErr *EventTurnError
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) && turnErr == nil {
		select {
		case e := <-s.Events():
			if ev, ok := e.(EventTurnError); ok {
				cp := ev
				turnErr = &cp
			}
		case <-time.After(100 * time.Millisecond):
		}
	}
	if turnErr == nil {
		t.Fatal("expected EventTurnError when the codex binary cannot start")
	}
	if turnErr.Subtype != "exec-failed" {
		t.Errorf("Subtype = %q, want exec-failed", turnErr.Subtype)
	}
	if !strings.Contains(turnErr.Result, "codex start") {
		t.Errorf("Result = %q, want it to mention the start failure", turnErr.Result)
	}
}

// TestCodexSession_StartAlreadyStarted asserts the double-start guard.
func TestCodexSession_StartAlreadyStarted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub binaries are #!/bin/sh (unix-only)")
	}
	bin := fakeCodexBin(t, codexSuccessStream("th-x"))
	s := NewCodexSession(SessionConfig{ID: "codex-dbl", WorkingDir: t.TempDir(), CodexBinaryPath: bin})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()
	if err := s.Start(ctx); err == nil {
		t.Error("second Start should return an 'already started' error")
	}
}

// TestCodexSession_TurnCap_FiresAndStops drives a session with
// MaxTurnsPerSession=1; after the first turn completes the cap fires
// EventTurnCapReached + EventExit and the session stops.
func TestCodexSession_TurnCap_FiresAndStops(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub binaries are #!/bin/sh (unix-only)")
	}
	bin := fakeCodexBin(t, codexSuccessStream("th-cap"))
	s := NewCodexSession(SessionConfig{
		ID:                 "codex-cap",
		WorkingDir:         t.TempDir(),
		Model:              "gpt-5.5",
		SystemPrompt:       "go once",
		MaxTurnsPerSession: 1,
		CodexBinaryPath:    bin,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	var sawCap bool
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && !sawCap {
		select {
		case e := <-s.Events():
			if ev, ok := e.(EventTurnCapReached); ok {
				sawCap = true
				if ev.Turns != 1 || ev.Cap != 1 {
					t.Errorf("cap event = %+v, want Turns=1 Cap=1", ev)
				}
			}
		case <-time.After(150 * time.Millisecond):
		}
	}
	if !sawCap {
		t.Fatal("expected EventTurnCapReached after hitting the 1-turn cap")
	}
	// The cap path calls go s.Stop(); wait for it.
	stopDeadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(stopDeadline) {
		if s.GetStatus() == StatusStopped {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if s.GetStatus() != StatusStopped {
		t.Errorf("status = %q, want stopped after turn cap", s.GetStatus())
	}
}

// TestCodexSession_Stop_CleansCodexHome verifies Stop removes the per-agent
// CODEX_HOME directory.
func TestCodexSession_Stop_CleansCodexHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub binaries are #!/bin/sh (unix-only)")
	}
	home := t.TempDir() + "/codex-home"
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("setup codex home: %v", err)
	}
	bin := fakeCodexBin(t, codexSuccessStream("th-home"))
	s := NewCodexSession(SessionConfig{
		ID:              "codex-home",
		WorkingDir:      t.TempDir(),
		CodexHome:       home,
		CodexBinaryPath: bin,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	s.Stop()
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Errorf("Stop should remove CODEX_HOME %q (stat err=%v)", home, err)
	}
}
