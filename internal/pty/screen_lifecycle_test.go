package pty

import (
	"os/exec"
	"runtime"
	"testing"
)

// screenAvailable reports whether the `screen` binary is on PATH. The
// CreateScreen → SendInput → KillScreen integration path needs a real
// `screen`; skip on hosts without it (CI images vary).
func screenAvailable() bool {
	_, err := exec.LookPath("screen")
	return err == nil
}

// TestScreenManager_CreateSendKill_RealScreen drives the full GNU Screen
// lifecycle against a real `screen -dmS ... bash` session: CreateScreen
// (which runs the screen binary and resolves the PID via getScreenPID),
// SendInput (screen -X stuff), and KillScreen (quit + signal + map removal).
// This is the only path that covers CreateScreen's success branch, SendInput's
// chunk/CR loop, getScreenPID's parse, and KillScreen's signal strategies.
func TestScreenManager_CreateSendKill_RealScreen(t *testing.T) {
	if runtime.GOOS == "windows" || !screenAvailable() {
		t.Skip("requires the `screen` binary (unix-only)")
	}

	sm := NewScreenManager(t.TempDir())

	// Use ModeShell so the wrapped command is plain `bash` (no claude binary
	// needed). The session id is short so the derived screen name is valid.
	sess, err := sm.CreateScreen("sl-1", "/tmp", ModeShell, "agent-shell", NiceConfig{}, map[string]string{"JARVIS_TEST_VAR": "ok"})
	if err != nil {
		t.Fatalf("CreateScreen: %v", err)
	}
	// Always clean up the real screen session even if assertions fail.
	defer func() { _, _ = sm.KillScreen("sl-1") }()

	if sess.PID <= 0 {
		t.Errorf("CreateScreen should resolve a positive PID, got %d", sess.PID)
	}
	if sess.ScreenName != "jarvis-sl-1" {
		t.Errorf("screen name = %q, want jarvis-sl-1", sess.ScreenName)
	}
	if got, ok := sm.GetScreen("sl-1"); !ok || got.PID != sess.PID {
		t.Error("created screen should be tracked with the resolved PID")
	}

	// SendInput: text + CR. With a tracked, validly-named session and a live
	// screen, the chunked stuff + CR-retry path should succeed.
	if !sm.SendInput("sl-1", "echo hi\r") {
		t.Error("SendInput to a live screen session should return true")
	}

	// SendInput with text only (no CR) — exercises the no-CR return branch.
	if !sm.SendInput("sl-1", "plain text no newline") {
		t.Error("SendInput without CR should still return true")
	}

	// KillScreen tears it down and removes the map entry.
	ok, err := sm.KillScreen("sl-1")
	if !ok || err != nil {
		t.Errorf("KillScreen = (%v, %v), want (true, nil)", ok, err)
	}
	if _, found := sm.GetScreen("sl-1"); found {
		t.Error("KillScreen should remove the tracked entry")
	}
}

// TestScreenManager_CreateScreen_DuplicateConcurrent confirms a second
// CreateScreen for the same id returns the existing entry rather than
// spawning a second screen (covers the early-return-existing branch under
// the placeholder logic).
func TestScreenManager_CreateScreen_DuplicateLivePath(t *testing.T) {
	if runtime.GOOS == "windows" || !screenAvailable() {
		t.Skip("requires the `screen` binary (unix-only)")
	}
	sm := NewScreenManager(t.TempDir())
	first, err := sm.CreateScreen("dup-live", "/tmp", ModeShell, "a", NiceConfig{}, nil)
	if err != nil {
		t.Fatalf("CreateScreen: %v", err)
	}
	defer func() { _, _ = sm.KillScreen("dup-live") }()

	second, err := sm.CreateScreen("dup-live", "/tmp", ModeShell, "a", NiceConfig{}, nil)
	if err != nil {
		t.Fatalf("second CreateScreen: %v", err)
	}
	if second != first {
		t.Error("a duplicate CreateScreen should return the already-tracked screen")
	}
}

// TestScreenManager_SendInput_InvalidScreenName covers the SendInput guard
// that rejects a tracked session whose screen name is not shell-safe.
func TestScreenManager_SendInput_InvalidScreenName(t *testing.T) {
	sm := NewScreenManager(t.TempDir())
	sm.mu.Lock()
	sm.screens["bad-name"] = &ScreenSession{
		SessionID:  "bad-name",
		ScreenName: "jarvis bad name with spaces", // fails isValidScreenName
		PID:        12345,
		Mode:       ModeShell,
	}
	sm.mu.Unlock()
	if sm.SendInput("bad-name", "hi\r") {
		t.Error("SendInput must reject a session with an invalid screen name")
	}
}

// TestScreenManager_CreateScreen_InvalidSessionID rejects a session id whose
// derived screen name contains invalid characters.
func TestScreenManager_CreateScreen_InvalidSessionID(t *testing.T) {
	sm := NewScreenManager(t.TempDir())
	// A session id with a slash makes "jarvis-foo/bar" which fails the
	// validScreenNamePattern (slashes not allowed).
	_, err := sm.CreateScreen("foo/bar", "/tmp", ModeShell, "a", NiceConfig{}, nil)
	if err == nil {
		t.Error("CreateScreen should reject a session id that yields an invalid screen name")
	}
}

// TestBuildBaseCommand_DefaultArm covers the default switch arm (an unknown
// mode falls back to bash) which the existing TestBuildBaseCommand misses.
func TestBuildBaseCommand_DefaultArm(t *testing.T) {
	if got := buildBaseCommand(ModeCodexExec); got != "bash" {
		t.Errorf("buildBaseCommand(codex) = %q, want bash (default arm)", got)
	}
	if got := buildBaseCommand(SessionMode("nonsense")); got != "bash" {
		t.Errorf("buildBaseCommand(unknown) = %q, want bash (default arm)", got)
	}
}
