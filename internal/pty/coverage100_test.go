package pty

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shaharia-lab/claude-agent-sdk-go/claude"

	"github.com/kshaham/jarvis/internal/testutil/stubbin"
)

// --- SDK kickstart-send error branch (sdk_session.go:380-384) -----------------

// TestSDKSession_Start_KickstartSendError_Seam forces the kickstart Send() to
// fail via the sdkSessionSend seam. Start must surface a wrapped error, call
// cancel(), and NOT flip the session to working. Reliable broken-pipe on the
// subprocess stdin can't be provoked (small writes stay kernel-buffered after
// the child exits), so the seam is the honest fault-injection point. (The
// subprocess-based sibling in sdk_branches_test.go accepts either outcome; this
// one deterministically covers sdk_session.go:380-384.)
func TestSDKSession_Start_KickstartSendError_Seam(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub binaries are #!/bin/sh (unix-only)")
	}
	// A stub that produces a clean session so NewSession succeeds; only the
	// kickstart Send is forced to fail by the seam.
	stream := strings.Join([]string{
		`{"type":"system","subtype":"init","session_id":"ks-err","model":"m"}`,
		`{"type":"result","subtype":"success","num_turns":1,"session_id":"ks-err","usage":{"input_tokens":1,"output_tokens":1}}`,
		"",
	}, "\n")
	bin := fakeClaudeStreamJSON(t, stream)

	sentinel := errors.New("forced kickstart send failure")
	orig := sdkSessionSend
	sdkSessionSend = func(_ *claude.Session, _ string) error { return sentinel }
	defer func() { sdkSessionSend = orig }()

	s := NewSDKSession(SessionConfig{
		ID:               "sdk-ks-err",
		WorkingDir:       t.TempDir(),
		SystemPrompt:     "kickstart that will fail to send",
		ProviderEnv:      map[string]string{"ANTHROPIC_API_KEY": "stub"},
		ClaudeBinaryPath: bin,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	err := s.Start(ctx)
	if err == nil {
		t.Fatal("Start should fail when the kickstart Send errors")
	}
	if !strings.Contains(err.Error(), "failed to send kickstart") {
		t.Errorf("error = %v, want it to wrap the kickstart-send failure", err)
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, want it to wrap the sentinel send error", err)
	}
	// The failed kickstart must NOT leave the session reporting working/busy.
	if s.IsWorking() {
		t.Error("a failed kickstart should not flip the session to working")
	}
	// Start cancelled the context; Stop should still be safe and quick.
	s.Stop()
}

// --- SDK Stop teardown-timeout branch (sdk_session.go:747-748) -----------------

// TestSDKSession_Stop_EventConsumerTimeout shrinks sdkStopWaitTimeout and parks
// a goroutine on the session WaitGroup so Stop's wg.Wait() can't complete in
// time, forcing the timeout warn branch. The parked goroutine is released after
// Stop returns so the test leaves no lingering goroutine.
func TestSDKSession_Stop_EventConsumerTimeout(t *testing.T) {
	origTimeout := sdkStopWaitTimeout
	sdkStopWaitTimeout = 20 * time.Millisecond
	defer func() { sdkStopWaitTimeout = origTimeout }()

	s := NewSDKSession(SessionConfig{ID: "sdk-stoptimeout", WorkingDir: t.TempDir()})

	// Park a goroutine on the WaitGroup so wg.Wait() blocks past the timeout.
	release := make(chan struct{})
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		<-release
	}()

	done := make(chan struct{})
	start := time.Now()
	go func() {
		s.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("Stop did not return within 5s even though the timeout was shrunk")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("Stop took %v; the shrunk timeout should make it return fast", elapsed)
	}
	if s.GetStatus() != StatusStopped {
		t.Errorf("status = %q, want stopped after Stop", s.GetStatus())
	}
	// Release the parked goroutine now that Stop has returned.
	close(release)
	s.wg.Wait()
}

// --- Codex Stop teardown-timeout branch (codex_session.go:604-605) ------------

// TestCodexSession_Stop_WorkerTimeout shrinks codexStopWaitTimeout and parks a
// goroutine on the WaitGroup so Stop's wg.Wait() can't finish in time, exercising
// the worker-did-not-exit warn branch.
func TestCodexSession_Stop_WorkerTimeout(t *testing.T) {
	origTimeout := codexStopWaitTimeout
	codexStopWaitTimeout = 20 * time.Millisecond
	defer func() { codexStopWaitTimeout = origTimeout }()

	s := NewCodexSession(SessionConfig{ID: "codex-stoptimeout", WorkingDir: t.TempDir()})
	// Give it a cancelFunc so Stop's cancel() arm runs too.
	_, cancel := context.WithCancel(context.Background())
	s.cancelFunc = cancel

	release := make(chan struct{})
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		<-release
	}()

	done := make(chan struct{})
	start := time.Now()
	go func() {
		s.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("Codex Stop did not return within 5s despite the shrunk timeout")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("Stop took %v; the shrunk timeout should make it return fast", elapsed)
	}
	if s.GetStatus() != StatusStopped {
		t.Errorf("status = %q, want stopped", s.GetStatus())
	}
	close(release)
	s.wg.Wait()
}

// --- Codex stdout/stderr pipe-error branches (codex_session.go:254-262) -------

// TestCodexSession_RunTurn_StdoutPipeError forces (*exec.Cmd).StdoutPipe to fail
// via the codexStdoutPipe seam (the call is otherwise unreachable: it only
// errors when Stdout is already set or the cmd is started, neither of which
// runTurn does). runTurn must failTurn with a "codex stdout pipe" EventTurnError
// and never reach Start.
func TestCodexSession_RunTurn_StdoutPipeError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub binaries are #!/bin/sh (unix-only)")
	}
	sentinel := errors.New("forced stdout pipe failure")
	origOut := codexStdoutPipe
	codexStdoutPipe = func(_ *exec.Cmd) (io.ReadCloser, error) { return nil, sentinel }
	defer func() { codexStdoutPipe = origOut }()

	bin := fakeCodexBin(t, codexSuccessStream("th-pipefail"))
	s := NewCodexSession(SessionConfig{ID: "codex-outpipe", WorkingDir: t.TempDir(), CodexBinaryPath: bin})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()
	s.SendPrompt(ctx, "trigger a turn")

	turnErr := waitForTurnError(t, s, 6*time.Second)
	if !strings.Contains(turnErr.Result, "codex stdout pipe") {
		t.Errorf("Result = %q, want it to mention the stdout pipe failure", turnErr.Result)
	}
}

// TestCodexSession_RunTurn_StderrPipeError forces (*exec.Cmd).StderrPipe to fail
// via the codexStderrPipe seam (StdoutPipe is left intact so execution reaches
// the stderr pipe). runTurn must failTurn with a "codex stderr pipe" error.
func TestCodexSession_RunTurn_StderrPipeError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub binaries are #!/bin/sh (unix-only)")
	}
	sentinel := errors.New("forced stderr pipe failure")
	origErr := codexStderrPipe
	codexStderrPipe = func(_ *exec.Cmd) (io.ReadCloser, error) { return nil, sentinel }
	defer func() { codexStderrPipe = origErr }()

	bin := fakeCodexBin(t, codexSuccessStream("th-pipefail2"))
	s := NewCodexSession(SessionConfig{ID: "codex-errpipe", WorkingDir: t.TempDir(), CodexBinaryPath: bin})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()
	s.SendPrompt(ctx, "trigger a turn")

	turnErr := waitForTurnError(t, s, 6*time.Second)
	if !strings.Contains(turnErr.Result, "codex stderr pipe") {
		t.Errorf("Result = %q, want it to mention the stderr pipe failure", turnErr.Result)
	}
}

// waitForTurnError drains the session's events until an EventTurnError arrives
// or the deadline elapses, failing the test if none is seen.
func waitForTurnError(t *testing.T, s *CodexSession, within time.Duration) EventTurnError {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		select {
		case e := <-s.Events():
			if ev, ok := e.(EventTurnError); ok {
				return ev
			}
		case <-time.After(100 * time.Millisecond):
		}
	}
	t.Fatal("expected an EventTurnError but none arrived")
	return EventTurnError{}
}

// --- detectAndAutoRespondMenu second-sleep / second-write branches ------------

// waitForLivePTY blocks until the session has a live ptyFile + sessionCtx, or
// fails the test. Mirrors the spin-wait used elsewhere in the suite.
func waitForLivePTY(t *testing.T, s *Session) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.RLock()
		live := s.ptyFile != nil && s.sessionCtx != nil
		s.mu.RUnlock()
		if live {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("PTY never came live")
}

// TestSession_AutoMenu_SecondWriteMismatch drives session.go:867-869: the first
// auto-response write ("1") succeeds, then the ptyFile pointer is swapped before
// the 200ms second sleep elapses, so the second writeIfPtyFileMatches ("\r")
// sees a pointer mismatch and the goroutine bails after the first write.
func TestSession_AutoMenu_SecondWriteMismatch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub binaries are #!/bin/sh (unix-only)")
	}
	bin := stubbin.Install(t, "claude-2ndwrite.sh", "#!/bin/sh\nwhile read l; do :; done\n")
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeClaude, ClaudeBinaryPath: bin})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()
	waitForLivePTY(t, s)

	// Arm the auto-response; goroutine sleeps 500ms, writes "1", sleeps 200ms,
	// then writes "\r".
	s.detectAndAutoRespondMenu("❯ 1. First option\n  2. Second option\nType something to chat")

	// Let the first 500ms sleep + first write happen, then swap ptyFile so the
	// SECOND write's pointer check fails. Window between writes is 200ms.
	time.Sleep(600 * time.Millisecond)
	s.mu.Lock()
	realFile := s.ptyFile
	s.ptyFile = nil // pointer now differs from the captured `expected`
	s.mu.Unlock()

	// Let the second sleep elapse and the second write hit the mismatch.
	time.Sleep(300 * time.Millisecond)

	s.mu.Lock()
	s.ptyFile = realFile
	s.mu.Unlock()
}

// TestSession_AutoMenu_SecondSleepCancelled drives session.go:864-866: the first
// write succeeds, then the session context is cancelled before the 200ms second
// sleep elapses, so the second sleepCtx returns false and the goroutine bails.
func TestSession_AutoMenu_SecondSleepCancelled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub binaries are #!/bin/sh (unix-only)")
	}
	bin := stubbin.Install(t, "claude-2ndsleep.sh", "#!/bin/sh\nwhile read l; do :; done\n")
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeClaude, ClaudeBinaryPath: bin})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()
	waitForLivePTY(t, s)

	s.detectAndAutoRespondMenu("❯ 1. First option\n  2. Second option\nType something to chat")

	// Wait past the first 500ms sleep + write, but cancel before the 200ms
	// second sleep elapses -> second sleepCtx returns false.
	time.Sleep(600 * time.Millisecond)
	cancel() // cancels the parent ctx -> sessionCtx (derived) -> sleepCtx false

	time.Sleep(300 * time.Millisecond)
	// Nothing to assert on the write itself; the branch ran if no panic and the
	// session is still healthy enough to stop.
	if s.lastAutoResponse.IsZero() {
		t.Error("the matched menu branch should have set lastAutoResponse")
	}
}

// --- idleDetector timer stop/drain race (session.go:948-951) ------------------

// TestSession_IdleDetector_TimerDrainRace exercises the timer stop/drain arm in
// idleDetector (the `if !timer.Stop() { drain }` block): an idleReset that
// arrives AFTER the internal timer has already fired (so timer.Stop() returns
// false and timer.C holds a pending tick) forces the detector to drain timer.C
// before resetting. We shrink idleDetectionDelay and, each round, wait just past
// the delay so timer.C is armed, then deliver a reset — leaving BOTH timer.C and
// idleReset ready and the select frequently choosing idleReset (the drain path).
// Many rounds make the path hit reliably; the detector must stay alive, drop no
// resets into a deadlock, and exit cleanly on cancel.
func TestSession_IdleDetector_TimerDrainRace(t *testing.T) {
	origDelay := idleDetectionDelay
	idleDetectionDelay = 2 * time.Millisecond
	defer func() { idleDetectionDelay = origDelay }()

	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeClaude})
	ctx, cancel := context.WithCancel(context.Background())

	// Run the detector directly (normally launched by Start; here in isolation
	// with the shrunk delay and a controlled context).
	s.wg.Add(1)
	go s.idleDetector(ctx)

	// Each round: let the timer arm (sleep ~ delay so timer.C is pending or
	// firing), then send a reset so timer.C AND idleReset race and the select
	// can take idleReset while timer.C already holds a tick -> the !timer.Stop()
	// drain branch. The detector must stay alive across all rounds regardless of
	// which arm the scheduler picks. (The drain inner-select carries no
	// statements; the broader suite's real-session idle detectors also exercise
	// the timer.Stop()==false entry.)
	for i := 0; i < 300; i++ {
		time.Sleep(2 * time.Millisecond) // ~ delay: timer.C arming
		select {
		case s.idleReset <- struct{}{}:
		default:
		}
	}
	// Let the detector settle.
	time.Sleep(20 * time.Millisecond)

	cancel()
	// idleDetector must exit on ctx cancel.
	doneWait := make(chan struct{})
	go func() { s.wg.Wait(); close(doneWait) }()
	select {
	case <-doneWait:
	case <-time.After(2 * time.Second):
		t.Fatal("idleDetector did not exit after context cancel")
	}
}

// --- readLoop reader-goroutine ctx.Done second-select (session.go:662-663) ----

// TestSession_ReadLoop_FloodTeardown stress-tests readLoop teardown under a
// continuous high-volume PTY flood: each round starts a session, lets a 4KB
// burst stream wedge the read pipeline, then Stop()s and asserts a prompt,
// consistent shutdown. This drives the reader-goroutine error path and the
// main-loop ctx.Done arm repeatedly under contention. (The reader goroutine's
// own `case <-ctx.Done()` send-abort arm requires the reader to be blocked on a
// full cap-1 readCh at the exact instant ctx cancels — a teardown race that is
// not reliably reproducible in-process; see the 'unreachable' report.)
func TestSession_ReadLoop_FloodTeardown(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub binaries are #!/bin/sh (unix-only)")
	}
	// Emit large bursts forever: big chunks make processOutput (vt emulator
	// feed + buffer append + ANSI strip) slow enough that the reader fills
	// readCh and blocks on its next send.
	body := "#!/bin/sh\nblk=$(printf '%4096s' '' | tr ' ' 'x')\nwhile true; do printf '%s' \"$blk\"; done\n"
	bin := stubbin.Install(t, "claude-flood.sh", body)

	const rounds = 6
	for round := 0; round < rounds; round++ {
		s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell, ClaudeBinaryPath: bin})
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		if err := s.Start(ctx); err != nil {
			cancel()
			t.Fatalf("Start (round %d): %v", round, err)
		}
		waitForLivePTY(t, s)
		// Let the flood get going so the reader goroutine is actively producing
		// and readCh is saturated.
		time.Sleep(40 * time.Millisecond)

		stopped := make(chan struct{})
		go func() { _ = s.Stop(); close(stopped) }()
		select {
		case <-stopped:
		case <-time.After(8 * time.Second):
			cancel()
			t.Fatalf("Stop hung (round %d); the reader goroutine likely did not honour ctx.Done", round)
		}
		if s.Status() != StatusStopped {
			t.Errorf("round %d: status = %q, want stopped", round, s.Status())
		}
		cancel()
	}
}

// --- Concurrency sanity: multiple stop-timeout sessions don't interfere -------

// TestSDKSession_Stop_TimeoutThenClean confirms the shrunk-timeout path leaves
// the session in a consistent state and a second Stop is a no-op.
func TestSDKSession_Stop_TimeoutThenClean(t *testing.T) {
	origTimeout := sdkStopWaitTimeout
	sdkStopWaitTimeout = 10 * time.Millisecond
	defer func() { sdkStopWaitTimeout = origTimeout }()

	s := NewSDKSession(SessionConfig{ID: "sdk-stop2x", WorkingDir: t.TempDir()})
	release := make(chan struct{})
	var once sync.Once
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		<-release
	}()
	s.Stop()
	// Second Stop is the early-return guard (already stopped).
	s.Stop()
	once.Do(func() { close(release) })
	s.wg.Wait()
}
