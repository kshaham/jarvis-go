package pty

import (
	"context"
	"os"
	"testing"
	"time"
)

// zeroFile returns a non-nil *os.File whose Write panics with a nil pointer
// dereference. A zero-value os.File has a nil internal file descriptor handle,
// so any Write call dereferences nil and panics -- exactly the runtime fault
// the goroutine panic-recovery defers are meant to absorb. Plain Close()/Write()
// on a *closed* file returns an error (not a panic), so this zero-value trick is
// the honest way to provoke the recover() arm without an unsafe construction.
func zeroFile() *os.File { return &os.File{} }

// TestSession_ProcessOutput_PastedTextWritePanicRecovered drives the pasted-text
// auto-submit goroutine (session.go:757-770) into its panic-recovery arm
// (session.go:761-766). processOutput sees a "[Pasted text" marker in ModeClaude
// with a fresh lastAutoResponse, captures the (non-nil) ptyFile, and launches a
// goroutine that sleeps 1s then calls ptyFile.Write([]byte("\r")). We seat a
// zero-value *os.File so that Write panics; the deferred recover() must swallow
// it and keep the session goroutine alive (the test process must not crash).
func TestSession_ProcessOutput_PastedTextWritePanicRecovered(t *testing.T) {
	s := NewSession(SessionConfig{ID: "paste-panic", WorkingDir: "/tmp", Mode: ModeClaude})

	// Seat a ptyFile whose Write panics, and ensure the auto-submit branch is
	// not debounced (lastAutoResponse zero -> branch taken).
	s.mu.Lock()
	s.ptyFile = zeroFile()
	s.lastAutoResponse = time.Time{}
	s.mu.Unlock()

	// Feed output carrying the pasted-text marker. processOutput must take the
	// pasted-text branch, set lastAutoResponse, and spawn the writer goroutine.
	s.processOutput("user typed [Pasted text #1 +12 lines] and waited\n")

	// The branch must have armed (lastAutoResponse set within the call).
	s.mu.RLock()
	armed := !s.lastAutoResponse.IsZero()
	s.mu.RUnlock()
	if !armed {
		t.Fatal("pasted-text branch should have set lastAutoResponse")
	}

	// Wait past the goroutine's 1s sleep so the panic-on-Write fires and is
	// recovered. If recover() did not catch it, the test binary would crash.
	time.Sleep(1500 * time.Millisecond)

	// Session must still be usable: a second processOutput must not panic and
	// (because lastAutoResponse is now recent) must NOT re-arm a second writer.
	s.mu.Lock()
	s.ptyFile = nil // avoid a second panic on any future write
	prev := s.lastAutoResponse
	s.mu.Unlock()
	s.processOutput("more [Pasted text #2 +1 lines] text\n")
	s.mu.RLock()
	after := s.lastAutoResponse
	s.mu.RUnlock()
	if !after.Equal(prev) {
		t.Error("a recent lastAutoResponse should debounce the second pasted-text submit")
	}
}

// TestSession_AutoMenu_FirstWritePanicRecovered drives the auto-menu writer
// goroutine (session.go:864-886) into its panic-recovery arm (session.go:867-872).
// detectAndAutoRespondMenu matches a menu, captures the (non-nil) ptyFile +
// sessionCtx, and launches a goroutine that (after a 500ms sleep) calls
// writeIfPtyFileMatches(ptyFile, "1"). We seat the SAME zero-value *os.File as
// both s.ptyFile and the captured expected, so the pointer-equality guard passes
// and the inner s.ptyFile.Write panics. The deferred recover() must absorb it so
// the test process survives.
func TestSession_AutoMenu_FirstWritePanicRecovered(t *testing.T) {
	s := NewSession(SessionConfig{ID: "menu-panic", WorkingDir: "/tmp", Mode: ModeClaude})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pf := zeroFile()
	s.mu.Lock()
	s.ptyFile = pf
	s.sessionCtx = ctx
	s.lastAutoResponse = time.Time{}
	s.mu.Unlock()

	// A full menu signature so detectAndAutoRespondMenu matches and arms.
	s.detectAndAutoRespondMenu("❯ 1. First option\n  2. Second option\nType something to chat")

	// Confirm the match armed the goroutine.
	s.mu.RLock()
	armed := !s.lastAutoResponse.IsZero()
	s.mu.RUnlock()
	if !armed {
		t.Fatal("menu match should have set lastAutoResponse and armed the writer")
	}

	// Wait past the 500ms sleep so writeIfPtyFileMatches runs, panics on the
	// zero-file Write, and the deferred recover() catches it. A crash here means
	// the recovery arm was not taken.
	time.Sleep(800 * time.Millisecond)

	// Clear the panicking file so Stop()/future writes are safe.
	s.mu.Lock()
	s.ptyFile = nil
	s.mu.Unlock()
}
