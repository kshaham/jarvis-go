package pty

import (
	"context"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/testutil/stubbin"
)

// TestSession_Start_AlreadyStarted covers the guard that rejects a second
// Start (session.go:224-227).
func TestSession_Start_AlreadyStarted(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	defer s.Stop()
	if err := s.Start(ctx); err == nil {
		t.Error("a second Start must return 'already started'")
	}
}

// TestSession_Start_ClaudeLookPathFallback drives the ModeClaude branch with an
// empty claudeBinaryPath and an emptied PATH so exec.LookPath("claude") fails
// and Start falls back to the literal "claude" (session.go:248-253). The PTY
// start then fails because "claude" isn't resolvable, which also covers the
// PTY-start error path (session.go:326-332).
func TestSession_Start_ClaudeLookPathFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PATH manipulation semantics differ on windows")
	}
	// Empty PATH so LookPath("claude") fails -> claudePath = "claude".
	t.Setenv("PATH", "")
	s := NewSession(SessionConfig{
		WorkingDir: "/tmp",
		Mode:       ModeClaude,
		// no ClaudeBinaryPath -> triggers the LookPath branch
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := s.Start(ctx)
	if err == nil {
		// If by some chance it started (claude on a default path), still tear down.
		s.Stop()
		t.Fatal("expected Start to fail: 'claude' is unresolvable with an empty PATH")
	}
	if s.Status() != StatusError {
		t.Errorf("status after a failed PTY start = %q, want error", s.Status())
	}
}

// TestSession_Start_DefaultModeArm starts a session whose Mode is neither
// Claude nor Shell, exercising the default switch arm that falls back to bash
// (session.go:316-317).
func TestSession_Start_DefaultModeArm(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: SessionMode("mystery")})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start with an unknown mode should fall back to bash and succeed: %v", err)
	}
	defer s.Stop()
	if s.PID() == 0 {
		t.Error("default-arm (bash) session should have a live PID")
	}
}

// TestSession_Start_PtyFailure_BadBinary points ModeClaude at a path that
// exists but isn't executable, so pty.StartWithSize fails and Start returns the
// wrapped error with status=error (session.go:326-332).
func TestSession_Start_PtyFailure_BadBinary(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires unix exec-permission semantics and a non-root user")
	}
	// A non-executable file: chmod 0644.
	dir := t.TempDir()
	bad := dir + "/not-exec"
	if err := os.WriteFile(bad, []byte("#!/bin/sh\necho hi\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	s := NewSession(SessionConfig{
		WorkingDir:       "/tmp",
		Mode:             ModeClaude,
		ClaudeBinaryPath: bad, // exists but not +x -> StartWithSize fails
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.Start(ctx); err == nil {
		s.Stop()
		t.Fatal("Start should fail when the claude binary is not executable")
	}
	if s.Status() != StatusError {
		t.Errorf("status = %q, want error after a failed PTY start", s.Status())
	}
}

// TestSession_ReadLoop_NilPtyFile calls readLoop directly with no ptyFile set,
// exercising the nil guard that reaps the (nil) process and returns
// (session.go:642-645). wg must be balanced for the deferred wg.Done().
func TestSession_ReadLoop_NilPtyFile(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	s.wg.Add(1) // readLoop's defer calls wg.Done()
	done := make(chan struct{})
	go func() {
		s.readLoop(context.Background())
		close(done)
	}()
	select {
	case <-done:
		// readLoop returned immediately because ptyFile is nil.
	case <-time.After(2 * time.Second):
		t.Fatal("readLoop with a nil ptyFile should return promptly")
	}
}

// TestSession_IdleDetector_ResetThenIdle exercises idleDetector's idleReset
// branch (timer reset) followed by the timer-fire idle transition
// (session.go:946-967). The session is marked busy so the timer-fire flips it
// to idle and emits EventIdle.
func TestSession_IdleDetector_ResetThenIdle(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	// Mark busy/working so the timer-fire branch performs the transition.
	s.mu.Lock()
	s.isWorking = true
	s.status = StatusBusy
	s.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.wg.Add(1) // idleDetector's defer calls wg.Done()
	go s.idleDetector(ctx)

	// Push a couple of resets to exercise the idleReset case (each resets the
	// 2s timer). Spaced under the delay so the timer never fires between them.
	for i := 0; i < 3; i++ {
		s.idleReset <- struct{}{}
		time.Sleep(50 * time.Millisecond)
	}

	// Now stop poking it; after idleDetectionDelay (2s) the timer fires and the
	// session transitions to idle with an EventIdle.
	sawIdle := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !sawIdle {
		select {
		case e := <-s.Events():
			if _, ok := e.(EventIdle); ok {
				sawIdle = true
			}
		case <-time.After(100 * time.Millisecond):
		}
	}
	if !sawIdle {
		t.Error("idleDetector should emit EventIdle after the timer fires on a busy session")
	}
	if s.IsWorking() {
		t.Error("idleDetector should clear isWorking on the idle transition")
	}
	if s.Status() != StatusIdle {
		t.Errorf("status = %q, want idle after the idle transition", s.Status())
	}
}

// TestSession_IdleDetector_InitialReadyTransition covers the isInitialReady
// branch: a session that is StatusBusy but never set isWorking still gets
// transitioned to idle on the first timer fire (session.go:958-964).
func TestSession_IdleDetector_InitialReadyTransition(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	s.mu.Lock()
	s.status = StatusBusy
	s.isWorking = false // initial-ready, not yet working
	s.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.wg.Add(1)
	go s.idleDetector(ctx)

	sawIdle := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !sawIdle {
		select {
		case e := <-s.Events():
			if _, ok := e.(EventIdle); ok {
				sawIdle = true
			}
		case <-time.After(100 * time.Millisecond):
		}
	}
	if !sawIdle {
		t.Error("an initial-ready (busy, not working) session should transition to idle")
	}
}

// TestSession_IdleDetector_NoTransitionWhenIdle covers the else branch of the
// timer-fire case: a session that is already idle (not busy, not working) does
// NOT emit a second EventIdle.
func TestSession_IdleDetector_NoTransitionWhenIdle(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	s.mu.Lock()
	s.status = StatusIdle
	s.isWorking = false
	s.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.wg.Add(1)
	go s.idleDetector(ctx)

	// Wait past the timer delay; no EventIdle should be emitted.
	time.Sleep(2500 * time.Millisecond)
	cancel()

	for _, e := range collectEvents(s) {
		if _, ok := e.(EventIdle); ok {
			t.Error("an already-idle session must not emit EventIdle on timer fire")
		}
	}
}

// TestSession_TrackToolInvocations_StartedThenCompleted covers both the
// "started" insert and the "completed" delete arms (session.go:984-987).
func TestSession_TrackToolInvocations_StartedThenCompleted(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	// A spinner-prefixed Bash(...) parses as started.
	s.trackToolInvocations("⏳ Bash(ls -la)")
	if tools := s.GetActiveTools(); len(tools) != 1 || tools[0] != "Bash" {
		t.Fatalf("after start, active tools = %v, want [Bash]", tools)
	}
	// A completed marker for the same tool removes it.
	s.trackToolInvocations("✓ Bash(ls -la)")
	if tools := s.GetActiveTools(); len(tools) != 0 {
		t.Errorf("after completion, active tools = %v, want empty", tools)
	}
}

// TestSession_ParseTokensFromStatusLine_DeltaTooLarge covers the guard that
// drops an implausibly large single-update delta (session.go:1003-1006): a
// jump straight to 1.5M tokens exceeds MaxDeltaPerUpdate (1M) from a zero base.
func TestSession_ParseTokensFromStatusLine_DeltaTooLarge(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	// 1,500,000 <= MaxSessionTokens (2M) so ParseTokenCount accepts it, but
	// delta 1.5M > MaxDeltaPerUpdate (1M) so the accumulation is rejected.
	s.parseTokensFromStatusLine("1500000 tokens")
	if _, _, total := s.Tokens(); total != 0 {
		t.Errorf("total = %d, want 0 (delta exceeded MaxDeltaPerUpdate, no accumulation)", total)
	}
	// No EventTokens should have been emitted either.
	for _, e := range collectEvents(s) {
		if _, ok := e.(EventTokens); ok {
			t.Error("an over-large delta must not emit EventTokens")
		}
	}
}

// TestSession_DetectAndAutoRespondMenu_SnapshotFallback drives the snapshot
// fallback branch (session.go:831-833): the per-chunk text carries a menu-ish
// marker (so the cheap pre-filter passes) but NOT the full signature, forcing a
// Snapshot() re-check. A live PTY is used so the emulator can hold a full menu.
func TestSession_DetectAndAutoRespondMenu_SnapshotFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub binaries are #!/bin/sh (unix-only)")
	}
	bin := stubbin.Install(t, "claude-snap.sh", "#!/bin/sh\nwhile read l; do :; done\n")
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeClaude, ClaudeBinaryPath: bin})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	// Prime the vt emulator with a full menu signature via processOutput-free
	// direct emulator write would bypass detection; instead feed the full menu
	// through the emulator so Snapshot() returns it, then call the detector with
	// a chunk that only carries the cheap pre-filter marker.
	s.mu.RLock()
	emu := s.vtEmulator
	s.mu.RUnlock()
	if emu == nil {
		t.Fatal("expected a vt emulator")
	}
	_, _ = emu.Write([]byte("\xe2\x9d\xaf 1. First option\r\n  2. Second option\r\nType something to chat\r\n"))

	// This chunk has a ">"/"1." marker (pre-filter passes) but no full signature,
	// so the detector falls back to Snapshot(), which DOES carry the signature.
	s.detectAndAutoRespondMenu("1. partial")

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.RLock()
		set := !s.lastAutoResponse.IsZero()
		s.mu.RUnlock()
		if set {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	s.mu.RLock()
	set := !s.lastAutoResponse.IsZero()
	s.mu.RUnlock()
	if !set {
		t.Error("the snapshot fallback should have matched the full menu signature and armed the auto-response")
	}
	time.Sleep(900 * time.Millisecond) // let the writer goroutine run
}

// TestSession_WriteIfPtyFileMatches_WriteError covers the write-failure branch
// (session.go:901-905): the file pointer matches but the underlying *os.File is
// already closed, so Write returns an error and the function returns false.
func TestSession_WriteIfPtyFileMatches_WriteError(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	// Open then immediately close a real file; writing to it errors.
	f, err := os.CreateTemp(t.TempDir(), "ptyfile-*")
	if err != nil {
		t.Fatalf("temp file: %v", err)
	}
	_ = f.Close() // closed -> Write returns os.ErrClosed
	s.mu.Lock()
	s.ptyFile = f
	s.mu.Unlock()

	if s.writeIfPtyFileMatches(f, []byte("x")) {
		t.Error("writeIfPtyFileMatches should return false when the Write fails on a closed file")
	}
}
