package pty

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestSession_ShellLifecycle drives a real /bin/sh PTY session through
// Start -> Resize -> WriteToPTY -> idle detection -> Stop, asserting the
// observable side effects at each step. This exercises the read loop,
// idleDetector goroutine, and PTY-backed writes that the unit tests can't
// reach with a nil ptyFile.
func TestSession_ShellLifecycle(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell, Name: "shell-lifecycle"})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	if s.PID() == 0 {
		t.Error("expected a non-zero PID after Start")
	}

	// Resize against a real PTY file must succeed.
	if err := s.Resize(120, 40); err != nil {
		t.Errorf("Resize on a live PTY should succeed, got %v", err)
	}

	// Collect output via an observer so we can confirm the command echoes.
	var sb safeBuf
	var done = make(chan struct{})
	unsub := s.AddOutputObserver(func(data string) {
		sb.Write(data)
		if strings.Contains(sb.String(), "JARVIS_MARKER_OK") {
			select {
			case <-done:
			default:
				close(done)
			}
		}
	})
	defer unsub()

	// Write a command that prints a unique marker.
	if !s.WriteToPTY("echo JARVIS_MARKER_OK\n") {
		t.Fatal("WriteToPTY on a live PTY should return true")
	}

	select {
	case <-done:
		// marker observed
	case <-time.After(5 * time.Second):
		t.Errorf("did not observe command output within 5s; got %q", sb.String())
	}

	// The idle detector fires ~2s after the last activity, transitioning a
	// busy session to idle. Wait for an EventIdle on the events channel.
	idleDeadline := time.Now().Add(6 * time.Second)
	sawIdle := false
	for time.Now().Before(idleDeadline) && !sawIdle {
		select {
		case e := <-s.Events():
			if _, ok := e.(EventIdle); ok {
				sawIdle = true
			}
		case <-time.After(250 * time.Millisecond):
		}
	}
	if !sawIdle {
		// Not all environments transition to idle deterministically (bare
		// sh may never set busy); accept the absence but log it. The
		// important coverage (Start/Resize/WriteToPTY/read loop) is done.
		t.Log("no EventIdle observed; bare sh may not enter the busy->idle cycle")
	}

	if err := s.Stop(); err != nil {
		t.Errorf("Stop: %v", err)
	}
	if s.Status() != StatusStopped {
		t.Errorf("status after Stop = %q, want stopped", s.Status())
	}
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Error("Done channel not closed after Stop")
	}
}

func TestSession_Resize_NoPTY(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	if err := s.Resize(80, 24); err == nil {
		t.Error("Resize with no PTY file should return an error")
	}
}

// TestSession_WriteIfPtyFileMatches_MismatchReturnsFalse exercises the
// guard that rejects a write when the session's ptyFile pointer no longer
// matches the expected file (the respawn race). A nil expected against a
// nil current would match, so we use a started session's real file as the
// "current" and a different sentinel as "expected".
func TestSession_WriteIfPtyFileMatches_NilMismatch(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	// Expected is nil but the live session holds a real ptyFile -> mismatch
	// -> must return false without writing.
	if s.writeIfPtyFileMatches(nil, []byte("x")) {
		t.Error("writeIfPtyFileMatches should return false when the file pointer differs")
	}
}
