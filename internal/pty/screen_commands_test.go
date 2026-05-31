package pty

import (
	"context"
	"testing"
	"time"
)

func TestRunShellCommand_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := runShellCommand(ctx, "true"); err != nil {
		t.Errorf("runShellCommand(true) = %v, want nil", err)
	}
}

func TestRunShellCommand_FailingExitCode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := runShellCommand(ctx, "exit 3"); err == nil {
		t.Error("runShellCommand(exit 3) should return a non-nil error")
	}
}

func TestRunShellCommandTimeout_Success(t *testing.T) {
	if err := runShellCommandTimeout("true", 5*time.Second); err != nil {
		t.Errorf("runShellCommandTimeout(true) = %v, want nil", err)
	}
}

func TestRunShellCommandTimeout_TimesOut(t *testing.T) {
	// A 5s sleep against a 100ms deadline must fail (context deadline).
	if err := runShellCommandTimeout("sleep 5", 100*time.Millisecond); err == nil {
		t.Error("runShellCommandTimeout(sleep 5, 100ms) should time out with an error")
	}
}

func TestGetScreenPID_NotFound(t *testing.T) {
	// A screen name that surely does not exist must return an error.
	if _, err := getScreenPID("jarvis-nonexistent-screen-xyz-12345"); err == nil {
		t.Error("getScreenPID for an unknown screen should return an error")
	}
}

func TestScreenManager_SendInput_UnknownSession(t *testing.T) {
	sm := NewScreenManager(t.TempDir())
	if sm.SendInput("no-such-session", "hello") {
		t.Error("SendInput for an unknown session should return false")
	}
}

func TestScreenManager_KillScreen_UnknownSession(t *testing.T) {
	sm := NewScreenManager(t.TempDir())
	ok, err := sm.KillScreen("no-such-session")
	if ok {
		t.Error("KillScreen for an unknown session should return ok=false")
	}
	if err == nil {
		t.Error("KillScreen for an unknown session should return an error")
	}
}

func TestScreenManager_KillScreen_RemovesTrackedEntry(t *testing.T) {
	sm := NewScreenManager(t.TempDir())
	// Register a screen with PID 0 so the SIGTERM/SIGKILL strategies are
	// skipped (PID > 0 guards) and only the screen -X quit (harmless miss)
	// + map removal run.
	sm.mu.Lock()
	sm.screens["sess-kill"] = &ScreenSession{
		SessionID:  "sess-kill",
		ScreenName: "jarvis-sess-kill",
		PID:        0,
		Mode:       ModeShell,
	}
	sm.mu.Unlock()

	ok, err := sm.KillScreen("sess-kill")
	if !ok || err != nil {
		t.Errorf("KillScreen of a tracked entry = (%v, %v), want (true, nil)", ok, err)
	}
	if _, found := sm.GetScreen("sess-kill"); found {
		t.Error("KillScreen should remove the entry from the tracked map")
	}
}

func TestScreenManager_CreateScreen_InvalidWorkingDir(t *testing.T) {
	sm := NewScreenManager(t.TempDir())
	// A working dir containing a null byte fails isValidPath, so CreateScreen
	// rejects it before ever invoking the screen binary.
	_, err := sm.CreateScreen("sess-1", "/bad\x00path", ModeShell, "agent", NiceConfig{}, nil)
	if err == nil {
		t.Error("CreateScreen with an invalid working dir should return an error")
	}
}

func TestScreenManager_CreateScreen_ReturnsExisting(t *testing.T) {
	sm := NewScreenManager(t.TempDir())
	existing := &ScreenSession{SessionID: "dup", ScreenName: "jarvis-dup", PID: 1234, Mode: ModeShell}
	sm.mu.Lock()
	sm.screens["dup"] = existing
	sm.mu.Unlock()

	got, err := sm.CreateScreen("dup", "/tmp", ModeShell, "agent", NiceConfig{}, nil)
	if err != nil {
		t.Fatalf("CreateScreen for an existing session should not error: %v", err)
	}
	if got != existing {
		t.Error("CreateScreen should return the already-tracked screen for a duplicate id")
	}
}
