package pty

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/testutil/stubbin"
)

// TestSession_ReadLoop_ProcessExits drives the read-loop's process-exit branch
// (session.go:674-679): a stub that prints output then exits on its own. The
// PTY read returns an error, so readLoop reaps the process (populating
// ProcessState) and calls handleExit, which reads the real exit code
// (session.go:1026-1028) and emits EventExit.
func TestSession_ReadLoop_ProcessExits(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub binaries are #!/bin/sh (unix-only)")
	}
	// Stub prints a marker then exits 0 immediately — no stdin hold.
	bin := stubbin.Install(t, "claude-exit.sh", "#!/bin/sh\necho EXITING_NOW\nexit 0\n")
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeClaude, ClaudeBinaryPath: bin})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	// The process exits on its own -> readLoop should observe the error, reap,
	// and emit EventExit with the real (0) exit code.
	var exitCode = -999
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) && exitCode == -999 {
		select {
		case e := <-s.Events():
			if ex, ok := e.(EventExit); ok {
				exitCode = ex.ExitCode
			}
		case <-time.After(150 * time.Millisecond):
		}
	}
	if exitCode == -999 {
		t.Fatal("expected an EventExit after the stub process exited")
	}
	if exitCode != 0 {
		t.Errorf("EventExit.ExitCode = %d, want 0 (process exited cleanly; handleExit read ProcessState)", exitCode)
	}
	// After the process exit, the session should report stopped status.
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.Status() == StatusStopped {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if s.Status() != StatusStopped {
		t.Errorf("status = %q, want stopped after process exit", s.Status())
	}
}

// TestSession_WriteInkPrompt_NilPtyFirstChunkFails covers the WriteToPTY-fail
// branch inside WriteInkPrompt's chunk loop (session.go:504-506): with a nil
// ptyFile the very first chunk's WriteToPTY returns false, so WriteInkPrompt
// returns false.
func TestSession_WriteInkPrompt_NilPtyFirstChunkFails(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeClaude})
	// No Start -> ptyFile is nil -> first WriteToPTY fails.
	if s.WriteInkPrompt("some prompt text") {
		t.Error("WriteInkPrompt with no PTY should return false on the first chunk write")
	}
}

// TestSession_WriteInkPrompt_MultiChunkLargePrompt drives the inter-chunk delay
// branch (session.go:508-510) and the long-prompt baseDelay branch
// (session.go:516-518) on a live PTY with a prompt longer than 2000 chars
// (and thus > inkChunkSize=700, forcing multiple chunks).
func TestSession_WriteInkPrompt_MultiChunkLargePrompt(t *testing.T) {
	if testing.Short() {
		t.Skip("WriteInkPrompt has multi-second real sleeps; skipped in -short")
	}
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell, Name: "ink-big"})
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	// >2000 chars: 3+ chunks (inter-chunk delays) and the 3s baseDelay branch.
	big := "echo " + strings.Repeat("x", 2200)
	if !s.WriteInkPrompt(big) {
		t.Error("WriteInkPrompt of a large prompt over a live PTY should return true")
	}
}

// TestSession_DetectWorkingState_AlreadyWorking covers the "already working"
// else branch (session.go:928-930): a second working indicator on a session
// that is already working must NOT re-emit EventWorking.
func TestSession_DetectWorkingState_AlreadyWorking(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeClaude})
	// First indicator flips to working and emits.
	s.detectWorkingState("Thinking", "Thinking")
	if !s.IsWorking() {
		t.Fatal("first working indicator should flip isWorking")
	}
	// Drain the first EventWorking.
	_ = collectEvents(s)
	// Second REAL indicator while already working -> else branch, no new event.
	// "Writing" is in workingIndicators so the loop matches and runs the
	// already-working else arm.
	s.detectWorkingState("Writing", "Writing")
	for _, e := range collectEvents(s) {
		if _, ok := e.(EventWorking); ok {
			t.Error("a working indicator while already working must not re-emit EventWorking")
		}
	}
}

// TestSession_DetectWorkingState_IdleResetChannelFull covers the default arm of
// the idleReset send (session.go:914-916): the buffered idleReset channel
// already holds a token, so a second prompt-glyph signal hits the non-blocking
// default and is dropped without blocking.
func TestSession_DetectWorkingState_IdleResetChannelFull(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeClaude})
	// Pre-fill the idleReset channel (capacity 1).
	s.idleReset <- struct{}{}
	// A prompt glyph would push onto idleReset, but it's full -> default arm.
	s.detectWorkingState("❯ ready", "❯ ready") // ❯
	// The channel should still hold exactly one token (the second was dropped).
	select {
	case <-s.idleReset:
		// drained the one token
	default:
		t.Error("idleReset should still hold its single token")
	}
	select {
	case <-s.idleReset:
		t.Error("idleReset should not hold a second token (the full-channel send was dropped)")
	default:
	}
}

// TestSession_EmitEvent_ChannelFullDrops covers the channel-full default
// (drop) branch of Session.emitEvent (session.go:1055): once the events
// channel is saturated, further emits are silently dropped.
func TestSession_EmitEvent_ChannelFullDrops(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	// Fill the events channel to capacity.
	for i := 0; i < eventChannelSize; i++ {
		s.emitEvent(EventIdle{})
	}
	// This emit must hit the default (drop) arm without blocking or panicking.
	done := make(chan struct{})
	go func() {
		s.emitEvent(EventIdle{}) // dropped
		close(done)
	}()
	select {
	case <-done:
		// returned promptly -> the drop arm ran
	case <-time.After(time.Second):
		t.Fatal("emitEvent on a full channel should drop and return immediately")
	}
}

// TestSession_DetectAndAutoRespondMenu_WriterFirstWriteMismatch drives the
// writer goroutine's first writeIfPtyFileMatches-false early return
// (session.go:861-863): after the menu match arms the goroutine, the session's
// ptyFile pointer is swapped to nil so the captured `expected` no longer
// matches and the first auto-response write bails.
func TestSession_DetectAndAutoRespondMenu_WriterFirstWriteMismatch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub binaries are #!/bin/sh (unix-only)")
	}
	bin := stubbin.Install(t, "claude-mismatch.sh", "#!/bin/sh\nwhile read l; do :; done\n")
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeClaude, ClaudeBinaryPath: bin})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	// Wait for the PTY to be live.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.RLock()
		live := s.ptyFile != nil && s.sessionCtx != nil
		s.mu.RUnlock()
		if live {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Arm the auto-response (captures the current ptyFile as `expected`).
	s.detectAndAutoRespondMenu("❯ 1. First option\n  2. Second option\nType something to chat")

	// Immediately swap the session's ptyFile to a different pointer so the
	// writer goroutine's writeIfPtyFileMatches(expected) sees a mismatch when
	// its 500ms sleep elapses, and bails at the first write.
	s.mu.Lock()
	realFile := s.ptyFile
	s.ptyFile = nil // pointer now differs from the captured `expected`
	s.mu.Unlock()

	// Let the writer goroutine wake (500ms) and hit the mismatch.
	time.Sleep(700 * time.Millisecond)

	// Restore so Stop can close the real file cleanly.
	s.mu.Lock()
	s.ptyFile = realFile
	s.mu.Unlock()
}

// TestSession_DetectAndAutoRespondMenu_CancelledWriter drives the writer
// goroutine's sleepCtx-false early-returns (session.go:858-869): the session
// context is cancelled right after the menu match arms the goroutine, so each
// sleepCtx returns false and the writer bails out instead of writing.
func TestSession_DetectAndAutoRespondMenu_CancelledWriter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub binaries are #!/bin/sh (unix-only)")
	}
	bin := stubbin.Install(t, "claude-cancel.sh", "#!/bin/sh\nwhile read l; do :; done\n")
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeClaude, ClaudeBinaryPath: bin})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	// Wait for ptyFile + sessionCtx to be live.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.RLock()
		live := s.ptyFile != nil && s.sessionCtx != nil
		s.mu.RUnlock()
		if live {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Cancel the session context so the writer goroutine's sleepCtx returns
	// false on its first 500ms sleep.
	s.mu.RLock()
	sc := s.sessionCtx
	s.mu.RUnlock()
	_ = sc

	// Arm the auto-response, then immediately cancel via Stop's cancelFunc path
	// by cancelling the parent ctx (the writer's sleepCtx watches sessionCtx).
	s.detectAndAutoRespondMenu("❯ 1. First option\n  2. Second option\nType something to chat")
	// Cancel the parent context -> sessionCtx (derived) is cancelled -> the
	// writer goroutine's sleepCtx(500ms) returns false and it bails.
	cancel()

	// Give the goroutine time to run and bail; nothing to assert on output
	// (the point is the false-arms run without writing). lastAutoResponse was
	// set by the matched branch.
	time.Sleep(300 * time.Millisecond)
	s.mu.RLock()
	armed := !s.lastAutoResponse.IsZero()
	s.mu.RUnlock()
	if !armed {
		t.Error("the matched menu branch should have set lastAutoResponse before the writer bailed")
	}
}
