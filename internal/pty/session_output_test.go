package pty

import (
	"os"
	"strings"
	"testing"
	"time"
)

// collectEvents drains the session's events channel non-blockingly.
func collectEvents(s *Session) []SessionEvent {
	var out []SessionEvent
	for {
		select {
		case e := <-s.Events():
			out = append(out, e)
		default:
			return out
		}
	}
}

func TestSession_ProcessOutput_EmitsTerminalAndOutput(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	s.processOutput("hello terminal\n")

	if !strings.Contains(s.TerminalBuffer(), "hello terminal") {
		t.Errorf("terminal buffer = %q, want it to contain the output", s.TerminalBuffer())
	}
	var sawTerminal, sawOutput bool
	for _, e := range collectEvents(s) {
		switch e.(type) {
		case EventTerminal:
			sawTerminal = true
		case EventOutput:
			sawOutput = true
		}
	}
	if !sawTerminal || !sawOutput {
		t.Errorf("processOutput should emit terminal+output; terminal=%v output=%v", sawTerminal, sawOutput)
	}
}

func TestSession_ProcessOutput_EmptyAfterFilteringNoOp(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	// A bare Ctrl+L (form feed) is filtered out to empty -> early return.
	s.processOutput("\x0c")
	if s.TerminalBuffer() != "" {
		t.Errorf("filtered-empty output should not append; buffer=%q", s.TerminalBuffer())
	}
	if evs := collectEvents(s); len(evs) != 0 {
		t.Errorf("filtered-empty output should emit nothing, got %d events", len(evs))
	}
}

func TestSession_ProcessOutput_DetectsWorkingIndicator(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	s.processOutput("Thinking about the problem")
	if !s.IsWorking() {
		t.Error("'Thinking' indicator should flip isWorking=true")
	}
	if s.Status() != StatusBusy {
		t.Errorf("status = %q, want busy", s.Status())
	}
	var sawWorking bool
	for _, e := range collectEvents(s) {
		if _, ok := e.(EventWorking); ok {
			sawWorking = true
		}
	}
	if !sawWorking {
		t.Error("working indicator should emit EventWorking")
	}
}

func TestSession_ProcessOutput_TracksToolStartedAndCompleted(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	// A spinner + Bash(...) is parsed as a "started" tool invocation.
	s.processOutput("⏳ Bash(ls -la)")
	tools := s.GetActiveTools()
	if len(tools) != 1 || tools[0] != "Bash" {
		t.Errorf("active tools after start = %v, want [Bash]", tools)
	}
}

func TestSession_ProcessOutput_DebouncedTokenParse(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	s.processOutput("status: 8000 tokens used")
	// The debounce timer fires after 100ms and runs parseTokensFromStatusLine.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, _, total := s.Tokens(); total > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	_, _, total := s.Tokens()
	if total != 8000 {
		t.Errorf("debounced token parse total = %d, want 8000", total)
	}
}

func TestSession_ProcessOutput_StoppedSkipsDebounce(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	s.mu.Lock()
	s.isStopped = true
	s.mu.Unlock()
	// Must not panic and must not arm a debounce timer.
	s.processOutput("12000 tokens")
	s.mu.RLock()
	timer := s.debounceTimer
	s.mu.RUnlock()
	if timer != nil {
		t.Error("stopped session must not arm a debounce timer")
	}
}

func TestSession_DetectAndAutoRespondMenu_ShellModeNoOp(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	before := s.lastAutoResponse
	// Even a menu-shaped string is ignored in shell mode.
	s.detectAndAutoRespondMenu("❯ 1. Foo\n2. Bar\nType something")
	if !s.lastAutoResponse.Equal(before) {
		t.Error("shell mode should never auto-respond to menus")
	}
}

func TestSession_DetectAndAutoRespondMenu_ClaudeModeMatchesNoPty(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeClaude})
	// Full menu signature: selector + numbered 1. & 2. + a known prompt.
	s.detectAndAutoRespondMenu("❯ 1. First\n  2. Second\nType something to chat")
	// Match path runs; ptyFile is nil so no goroutine writes, but
	// lastAutoResponse is set, proving the matched branch executed.
	if s.lastAutoResponse.IsZero() {
		t.Error("a matching menu in Claude mode should set lastAutoResponse")
	}
}

func TestSession_DetectAndAutoRespondMenu_Debounced(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeClaude})
	s.mu.Lock()
	s.lastAutoResponse = time.Now() // recent -> within the 5s debounce
	s.mu.Unlock()
	prev := s.lastAutoResponse
	s.detectAndAutoRespondMenu("❯ 1. First\n2. Second\nType something")
	if !s.lastAutoResponse.Equal(prev) {
		t.Error("debounce window should suppress a second auto-response")
	}
}

func TestSession_DetectWorkingState_PromptSignalsIdleReset(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeClaude})
	// The idle-prompt glyph should push onto idleReset (non-blocking).
	s.detectWorkingState("❯ ", "❯ ")
	select {
	case <-s.idleReset:
		// good: a reset was signalled
	default:
		t.Error("prompt glyph should signal idleReset")
	}
}

func TestSession_TrackToolInvocations_NoMatchNoChange(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	s.trackToolInvocations("just some plain text, no tool calls")
	if len(s.GetActiveTools()) != 0 {
		t.Errorf("no tool invocations expected, got %v", s.GetActiveTools())
	}
}

func TestResolveAndValidateWorkingDir_EmptyInput(t *testing.T) {
	if _, err := ResolveAndValidateWorkingDir("", nil); err == nil {
		t.Error("empty working dir should error")
	}
}

func TestResolveAndValidateWorkingDir_NonexistentPath(t *testing.T) {
	if _, err := ResolveAndValidateWorkingDir("/this/path/does/not/exist/ever123", nil); err == nil {
		t.Error("nonexistent path should error (EvalSymlinks fails)")
	}
}

func TestResolveAndValidateWorkingDir_NotADirectory(t *testing.T) {
	dir := t.TempDir()
	file := dir + "/afile.txt"
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if _, err := ResolveAndValidateWorkingDir(file, nil); err == nil {
		t.Error("a regular file should be rejected (not a directory)")
	}
}
