package pty

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSession_HooksEnabled(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell, HooksEnabled: true})
	if !s.HooksEnabled() {
		t.Error("HooksEnabled should reflect the config flag (true)")
	}
	s2 := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	if s2.HooksEnabled() {
		t.Error("HooksEnabled should default to false")
	}
}

func TestSession_IsWorking_Default(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	if s.IsWorking() {
		t.Error("new session should not be working")
	}
}

func TestSession_Buffers_StartEmpty(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	if s.TerminalBuffer() != "" {
		t.Errorf("terminal buffer = %q, want empty", s.TerminalBuffer())
	}
	if s.TextOutput() != "" {
		t.Errorf("text output = %q, want empty", s.TextOutput())
	}
}

func TestSession_GetActiveTools_EmptyAndPopulated(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	if got := s.GetActiveTools(); len(got) != 0 {
		t.Errorf("expected no active tools, got %v", got)
	}
	s.mu.Lock()
	if s.activeTools == nil {
		s.activeTools = map[string]bool{}
	}
	s.activeTools["Bash"] = true
	s.mu.Unlock()
	got := s.GetActiveTools()
	if len(got) != 1 || got[0] != "Bash" {
		t.Errorf("active tools = %v, want [Bash]", got)
	}
}

func TestSession_GetActiveTools_NilMap(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	s.mu.Lock()
	s.activeTools = nil
	s.mu.Unlock()
	if got := s.GetActiveTools(); got == nil || len(got) != 0 {
		t.Errorf("nil activeTools should yield empty (non-nil) slice, got %v", got)
	}
}

func TestSession_WriteToPTY_NoFile(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	// ptyFile is nil before Start; write must return false (not panic).
	if s.WriteToPTY("hello") {
		t.Error("WriteToPTY on a session with no PTY file should return false")
	}
}

func TestSession_WriteViaScreen_DirectFallback(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	// No screen manager configured -> falls through to WriteToPTY, which
	// returns false because ptyFile is nil.
	if s.WriteViaScreen("hi") {
		t.Error("WriteViaScreen with no screen manager and no PTY should return false")
	}
}

// fakeScreenManager is a minimal ScreenManagerInterface that records the
// last SendInput call and returns a configurable result.
type fakeScreenManager struct {
	sendResult bool
	lastInput  string
	lastID     string
}

func (f *fakeScreenManager) CreateScreen(string, string, SessionMode, string, NiceConfig, map[string]string) (*ScreenSession, error) {
	return nil, nil
}
func (f *fakeScreenManager) KillScreen(string) (bool, error) { return true, nil }
func (f *fakeScreenManager) SendInput(sessionID, input string) bool {
	f.lastID = sessionID
	f.lastInput = input
	return f.sendResult
}
func (f *fakeScreenManager) GetScreen(string) (*ScreenSession, bool) { return nil, false }

func TestSession_WriteViaScreen_DelegatesToScreenManager(t *testing.T) {
	fake := &fakeScreenManager{sendResult: true}
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell, ScreenManager: fake})
	if !s.WriteViaScreen("ping") {
		t.Error("WriteViaScreen should return the screen manager's SendInput result (true)")
	}
	if fake.lastInput != "ping" {
		t.Errorf("screen manager received %q, want 'ping'", fake.lastInput)
	}
	if fake.lastID != s.ID {
		t.Errorf("screen manager received id %q, want %q", fake.lastID, s.ID)
	}

	fake.sendResult = false
	if s.WriteViaScreen("ping2") {
		t.Error("WriteViaScreen should propagate a false SendInput result")
	}
}

func TestSession_Snapshot(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	// Fresh session: the emulator renders a fixed-size blank grid (no
	// printable content). Trimming whitespace yields an empty string.
	if got := strings.TrimSpace(s.Snapshot()); got != "" {
		t.Errorf("fresh snapshot has printable content %q, want blank", got)
	}
	// With a nil emulator, Snapshot must return "" without panicking.
	s.vtEmulator = nil
	if got := s.Snapshot(); got != "" {
		t.Errorf("nil-emulator snapshot = %q, want empty", got)
	}
}

func TestSession_WriteInkPrompt_RejectsEmpty(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	if s.WriteInkPrompt("") {
		t.Error("empty prompt should return false")
	}
	if s.WriteInkPrompt("  \t\r\n") {
		t.Error("whitespace-only prompt should return false")
	}
}

func TestSession_AddOutputObserver_NotifyAndUnsubscribe(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	var mu sync.Mutex
	var seen string
	unsub := s.AddOutputObserver(func(d string) {
		mu.Lock()
		seen += d
		mu.Unlock()
	})
	s.notifyOutputObservers("alpha")
	unsub()
	s.notifyOutputObservers("beta")
	mu.Lock()
	got := seen
	mu.Unlock()
	if got != "alpha" {
		t.Errorf("observer saw %q, want 'alpha' (beta after unsubscribe)", got)
	}
}

func TestSession_EmitEvent_StoppedNoOp(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	s.mu.Lock()
	s.isStopped = true
	s.mu.Unlock()
	s.emitEvent(EventWorking{})
	select {
	case e := <-s.Events():
		t.Errorf("stopped session emitted %T, want nothing", e)
	default:
	}
}

func TestSession_HandleExit_EmitsExitAndStops(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	// cmd is nil -> exitCode defaults to -1.
	s.handleExit()
	if s.Status() != StatusStopped {
		t.Errorf("status = %q, want stopped after handleExit", s.Status())
	}
	if s.IsWorking() {
		t.Error("handleExit should clear isWorking")
	}
	if s.PID() != 0 {
		t.Errorf("PID = %d, want 0 after handleExit", s.PID())
	}
	select {
	case e := <-s.Events():
		ex, ok := e.(EventExit)
		if !ok {
			t.Fatalf("expected EventExit, got %T", e)
		}
		if ex.ExitCode != -1 {
			t.Errorf("ExitCode = %d, want -1 (no process state)", ex.ExitCode)
		}
	case <-time.After(time.Second):
		t.Error("expected an EventExit from handleExit")
	}
}

func TestSession_ParseTokensFromStatusLine_AccumulatesAndEmits(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	s.parseTokensFromStatusLine("5000 tokens")

	in, out, total := s.Tokens()
	if total != 5000 {
		t.Errorf("total tokens = %d, want 5000", total)
	}
	// 60/40 split.
	if in != 3000 || out != 2000 {
		t.Errorf("split in=%d out=%d, want 3000/2000", in, out)
	}
	var gotTokens *EventTokens
	for {
		select {
		case e := <-s.Events():
			if ev, ok := e.(EventTokens); ok {
				cp := ev
				gotTokens = &cp
			}
			continue
		default:
		}
		break
	}
	if gotTokens == nil {
		t.Fatal("expected an EventTokens emission")
	}
	if gotTokens.Total != 5000 {
		t.Errorf("EventTokens.Total = %d, want 5000", gotTokens.Total)
	}
}

func TestSession_ParseTokensFromStatusLine_NoRegressionOnLowerCount(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	s.parseTokensFromStatusLine("5000 tokens")
	// A lower subsequent count must NOT reduce the accumulated total.
	s.parseTokensFromStatusLine("1000 tokens")
	_, _, total := s.Tokens()
	if total != 5000 {
		t.Errorf("total = %d, want 5000 (lower count must not regress)", total)
	}
}

func TestSession_ParseTokensFromStatusLine_IgnoresUnparseable(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	s.parseTokensFromStatusLine("no token info here")
	_, _, total := s.Tokens()
	if total != 0 {
		t.Errorf("total = %d, want 0 for an unparseable line", total)
	}
}

func TestNormaliseEffortString(t *testing.T) {
	cases := map[string]string{
		"low":     "low",
		"LOW":     "low",
		" Low ":   "low",
		"medium":  "medium",
		"med":     "medium",
		"high":    "high",
		"xhigh":   "xhigh",
		"max":     "max",
		"maximum": "max",
		"":        "",
		"garbage": "",
	}
	for in, want := range cases {
		if got := normaliseEffortString(in); got != want {
			t.Errorf("normaliseEffortString(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSession_BuildEnv_IncludesSessionAndProvider(t *testing.T) {
	s := NewSession(SessionConfig{
		WorkingDir:  "/tmp",
		Mode:        ModeShell,
		ProviderEnv: map[string]string{"MY_KEY": "my_val"},
	})
	env := s.buildEnv()
	if !envHasKey(env, "JARVIS_SESSION_ID") {
		t.Error("buildEnv must set JARVIS_SESSION_ID")
	}
	if !envHasKey(env, "MY_KEY") {
		t.Error("buildEnv must include providerEnv entries")
	}
}

func TestSleepCtx_CompletesNormally(t *testing.T) {
	if !sleepCtx(context.Background(), 5*time.Millisecond) {
		t.Error("sleepCtx should return true when the timer fires")
	}
}

func TestSleepCtx_CancelledReturnsFalse(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled
	if sleepCtx(ctx, 5*time.Second) {
		t.Error("sleepCtx should return false when context is cancelled")
	}
}

func TestEventTypeStrings(t *testing.T) {
	cases := []struct {
		ev   SessionEvent
		want string
	}{
		{EventTerminal{}, "terminal"},
		{EventOutput{}, "output"},
		{EventIdle{}, "idle"},
		{EventWorking{}, "working"},
		{EventExit{}, "exit"},
		{EventTokens{}, "tokens"},
		{EventTurnCapReached{}, "turnCapReached"},
		{EventToolInvocation{}, "toolInvocation"},
		{EventTurnError{}, "turnError"},
		{EventError{Err: errors.New("boom")}, "error"},
	}
	for _, c := range cases {
		if got := c.ev.eventType(); got != c.want {
			t.Errorf("%T.eventType() = %q, want %q", c.ev, got, c.want)
		}
	}
}
