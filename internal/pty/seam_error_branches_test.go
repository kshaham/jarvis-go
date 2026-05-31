package pty

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// TestManager_StopSession_StopError forces the stopSession seam to return an
// error so StopSession surfaces it AND leaves the session in the map (the delete
// only happens after a successful Stop). The real (*Session).Stop never returns
// an error, so this seam is the only honest way to exercise manager.go's
// error-propagation arm.
func TestManager_StopSession_StopError(t *testing.T) {
	sentinel := errors.New("forced session stop failure")
	orig := stopSession
	stopSession = func(*Session) error { return sentinel }
	defer func() { stopSession = orig }()

	m := NewSessionManager(5)
	sess := NewSession(SessionConfig{ID: "stop-err", WorkingDir: "/tmp", Mode: ModeShell})
	m.mu.Lock()
	m.sessions["stop-err"] = sess
	m.mu.Unlock()

	err := m.StopSession(context.Background(), "stop-err")
	if !errors.Is(err, sentinel) {
		t.Fatalf("StopSession err = %v, want the sentinel stop error", err)
	}
	// On a Stop failure the session must remain registered (no delete).
	if _, ok := m.GetSession("stop-err"); !ok {
		t.Error("a failed Stop must leave the session in the map for a retry")
	}
}

// TestIsProcessAlive_FindProcessError forces the findProcess seam to return an
// error so isProcessAlive takes its `err != nil -> return false` arm. On unix
// os.FindProcess never errors, so the seam is the only honest way to reach it.
func TestIsProcessAlive_FindProcessError(t *testing.T) {
	orig := findProcess
	findProcess = func(int) (*os.Process, error) {
		return nil, errors.New("forced FindProcess failure")
	}
	defer func() { findProcess = orig }()

	if isProcessAlive(os.Getpid()) {
		t.Error("isProcessAlive must report false when findProcess errors")
	}
}

// TestSendInput_EmptyEscapedChunkContinues forces the shellEscapeFn seam to
// return "" for a non-empty chunk so SendInput's `escaped == "" -> continue`
// guard runs. Production shellEscape never returns "" for non-empty input, so
// this seam is the only honest way to reach the defensive continue. The CR-only
// suffix still exercises the success return after the skipped chunk.
func TestSendInput_EmptyEscapedChunkContinues(t *testing.T) {
	var crCalls int
	withShellSeams(t, nil,
		func(cmd string, _ time.Duration) error {
			if strings.Contains(cmd, `printf '\015'`) {
				crCalls++
			}
			return nil
		}, nil)

	origEsc := shellEscapeFn
	shellEscapeFn = func(string) string { return "" } // force the empty-escaped path
	defer func() { shellEscapeFn = origEsc }()

	sm := NewScreenManager(t.TempDir())
	sm.mu.Lock()
	sm.screens["empty-esc"] = &ScreenSession{
		SessionID:  "empty-esc",
		ScreenName: "jarvis-empty-esc",
		PID:        os.Getpid(),
		Mode:       ModeShell,
	}
	sm.mu.Unlock()

	// Text (chunked + skipped via the empty-escape guard) followed by a CR so we
	// still reach the success return after every text chunk is skipped.
	if !sm.SendInput("empty-esc", "abc\r") {
		t.Error("SendInput should still succeed when chunks are skipped and the CR sends")
	}
	if crCalls == 0 {
		t.Error("the CR stuff command should have been issued after the skipped text chunk")
	}
}
