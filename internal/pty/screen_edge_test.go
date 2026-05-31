package pty

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// startTrappingSleep spawns a bash process that traps (ignores) SIGINT and
// sleeps. os.Interrupt won't kill it, so KillScreen's SIGKILL strategy is the
// only thing that reaps it — exactly the branch we want to exercise.
func startTrappingSleep(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("bash", "-c", "trap '' INT; sleep 30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start trapping sleep: %v", err)
	}
	// Give bash a moment to install the trap before the test signals it.
	time.Sleep(100 * time.Millisecond)
	return cmd
}

// withShellSeams temporarily swaps the package-level command/PID seams and
// restores them on cleanup. Lets a test force start/PID/stuff failures without
// a flaky real `screen` binary.
func withShellSeams(t *testing.T,
	run func(ctx context.Context, command string) error,
	runTimeout func(command string, timeout time.Duration) error,
	pidFn func(screenName string) (int, error),
) {
	t.Helper()
	prevRun, prevRunTimeout, prevPID := runShellCommand, runShellCommandTimeout, getScreenPID
	if run != nil {
		runShellCommand = run
	}
	if runTimeout != nil {
		runShellCommandTimeout = runTimeout
	}
	if pidFn != nil {
		getScreenPID = pidFn
	}
	t.Cleanup(func() {
		runShellCommand = prevRun
		runShellCommandTimeout = prevRunTimeout
		getScreenPID = prevPID
	})
}

// TestScreenManager_CreateScreen_RunShellFails forces runShellCommand to fail so
// CreateScreen takes the "failed to create screen" branch, removes the
// placeholder, and returns an error (screen.go:98-104).
func TestScreenManager_CreateScreen_RunShellFails(t *testing.T) {
	withShellSeams(t,
		func(context.Context, string) error { return errors.New("boom: screen unavailable") },
		nil, nil)

	sm := NewScreenManager(t.TempDir())
	sess, err := sm.CreateScreen("cs-fail", "/tmp", ModeShell, "a", NiceConfig{}, nil)
	if err == nil {
		t.Fatal("CreateScreen should error when the screen command fails")
	}
	if sess != nil {
		t.Errorf("CreateScreen should return nil session on failure, got %+v", sess)
	}
	// The placeholder must have been removed so a retry isn't blocked.
	if _, ok := sm.GetScreen("cs-fail"); ok {
		t.Error("placeholder must be removed after a creation failure")
	}
	if sm.GetScreenCount() != 0 {
		t.Errorf("count = %d, want 0 after failed create", sm.GetScreenCount())
	}
}

// TestScreenManager_CreateScreen_PIDNotFound makes the screen command succeed
// but getScreenPID return 0, exercising the "screen created but PID not found"
// branch (screen.go:109-115).
func TestScreenManager_CreateScreen_PIDNotFound(t *testing.T) {
	withShellSeams(t,
		func(context.Context, string) error { return nil }, // create "succeeds"
		nil,
		func(string) (int, error) { return 0, errors.New("not in screen -ls") })

	sm := NewScreenManager(t.TempDir())
	sess, err := sm.CreateScreen("cs-nopid", "/tmp", ModeShell, "a", NiceConfig{}, nil)
	if err == nil {
		t.Fatal("CreateScreen should error when the PID cannot be resolved")
	}
	if sess != nil {
		t.Error("CreateScreen should return nil session when PID resolution fails")
	}
	if _, ok := sm.GetScreen("cs-nopid"); ok {
		t.Error("placeholder must be removed when PID resolution fails")
	}
}

// TestScreenManager_SendInput_ChunkCommandFails forces the stuff command to
// fail so SendInput returns false from inside the chunk loop (screen.go:167-169).
func TestScreenManager_SendInput_ChunkCommandFails(t *testing.T) {
	withShellSeams(t, nil,
		func(string, time.Duration) error { return errors.New("stuff failed") },
		nil)

	sm := NewScreenManager(t.TempDir())
	sm.mu.Lock()
	sm.screens["si-fail"] = &ScreenSession{
		SessionID:  "si-fail",
		ScreenName: "jarvis-si-fail",
		PID:        os.Getpid(),
		Mode:       ModeShell,
	}
	sm.mu.Unlock()

	if sm.SendInput("si-fail", "hello") {
		t.Error("SendInput should return false when the stuff command fails")
	}
}

// TestScreenManager_SendInput_MultiChunkWithCR drives the multi-chunk path (a
// payload longer than stuffChunkSize forces the inter-chunk delay branch,
// screen.go:170-172) plus the CR submit branch — all stuff commands succeed.
func TestScreenManager_SendInput_MultiChunkWithCR(t *testing.T) {
	var calls int
	withShellSeams(t, nil,
		func(string, time.Duration) error { calls++; return nil },
		nil)

	sm := NewScreenManager(t.TempDir())
	sm.mu.Lock()
	sm.screens["si-multi"] = &ScreenSession{
		SessionID:  "si-multi",
		ScreenName: "jarvis-si-multi",
		PID:        os.Getpid(),
		Mode:       ModeShell,
	}
	sm.mu.Unlock()

	// 700 chars -> 3 chunks of <=300, plus a CR command at the end.
	payload := strings.Repeat("x", 700) + "\r"
	if !sm.SendInput("si-multi", payload) {
		t.Error("SendInput over a working screen should return true")
	}
	if calls < 4 { // 3 chunks + at least 1 CR attempt
		t.Errorf("expected >=4 stuff calls for a 3-chunk + CR send, got %d", calls)
	}
}

// TestScreenManager_SendInput_CRRetriesThenSucceeds makes the CR command fail
// the first two attempts then succeed, exercising the retry sleep branch
// (screen.go:184) and the eventual success return.
func TestScreenManager_SendInput_CRRetriesThenSucceeds(t *testing.T) {
	var crAttempts int
	withShellSeams(t, nil,
		func(cmd string, _ time.Duration) error {
			if strings.Contains(cmd, `printf '\015'`) {
				crAttempts++
				if crAttempts < 3 {
					return errors.New("transient CR failure")
				}
			}
			return nil
		}, nil)

	sm := NewScreenManager(t.TempDir())
	sm.mu.Lock()
	sm.screens["si-cr"] = &ScreenSession{
		SessionID:  "si-cr",
		ScreenName: "jarvis-si-cr",
		PID:        os.Getpid(),
		Mode:       ModeShell,
	}
	sm.mu.Unlock()

	// CR only (no text) so textPart=="" -> the post-text sleep is skipped and
	// we go straight to the CR retry loop.
	if !sm.SendInput("si-cr", "\r") {
		t.Error("SendInput should succeed once a later CR attempt works")
	}
	if crAttempts != 3 {
		t.Errorf("CR attempts = %d, want 3 (two failures then success)", crAttempts)
	}
}

// TestScreenManager_SendInput_CRAllAttemptsFail makes every CR command fail so
// SendInput returns false after exhausting the 3 retries (screen.go:186).
func TestScreenManager_SendInput_CRAllAttemptsFail(t *testing.T) {
	withShellSeams(t, nil,
		func(cmd string, _ time.Duration) error {
			if strings.Contains(cmd, `printf '\015'`) {
				return errors.New("CR permanently broken")
			}
			return nil
		}, nil)

	sm := NewScreenManager(t.TempDir())
	sm.mu.Lock()
	sm.screens["si-crfail"] = &ScreenSession{
		SessionID:  "si-crfail",
		ScreenName: "jarvis-si-crfail",
		PID:        os.Getpid(),
		Mode:       ModeShell,
	}
	sm.mu.Unlock()

	if sm.SendInput("si-crfail", "text\r") {
		t.Error("SendInput should return false when all CR attempts fail")
	}
}

// TestScreenManager_SendInput_NotTracked covers the early miss when the session
// id isn't in the screens map.
func TestScreenManager_SendInput_NotTracked(t *testing.T) {
	sm := NewScreenManager(t.TempDir())
	if sm.SendInput("ghost", "hi\r") {
		t.Error("SendInput on an unknown session id should return false")
	}
}

// TestScreenManager_KillScreen_SigkillStillAlive drives the SIGKILL strategy:
// a long-lived child process that ignores SIGINT remains alive after the
// interrupt, so KillScreen falls through to proc.Kill() (screen.go:218-222).
func TestScreenManager_KillScreen_SigkillStillAlive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signal semantics are unix-only")
	}
	// Spawn a real `sleep` we control; it ignores os.Interrupt poorly but Kill
	// always works. Use a background process via bash that traps SIGINT.
	withShellSeams(t,
		func(context.Context, string) error { return nil }, // -X quit is a no-op
		func(string, time.Duration) error { return nil },
		nil)

	cmd := startTrappingSleep(t)
	defer func() { _ = cmd.Process.Kill() }()

	sm := NewScreenManager(t.TempDir())
	sm.mu.Lock()
	sm.screens["kill-1"] = &ScreenSession{
		SessionID:  "kill-1",
		ScreenName: "jarvis-kill-1",
		PID:        cmd.Process.Pid, // a real, live, SIGINT-trapping process
		Mode:       ModeShell,
	}
	sm.mu.Unlock()

	ok, err := sm.KillScreen("kill-1")
	if !ok || err != nil {
		t.Errorf("KillScreen = (%v, %v), want (true, nil)", ok, err)
	}
	if _, found := sm.GetScreen("kill-1"); found {
		t.Error("KillScreen should remove the tracked entry")
	}
	// The process should be dead (SIGKILL delivered).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !isProcessAlive(cmd.Process.Pid) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestScreenManager_KillScreen_NotFound covers the miss path.
func TestScreenManager_KillScreen_NotFound(t *testing.T) {
	sm := NewScreenManager(t.TempDir())
	ok, err := sm.KillScreen("ghost")
	if ok || err == nil {
		t.Errorf("KillScreen(ghost) = (%v, %v), want (false, err)", ok, err)
	}
}

// TestScreenManager_GetScreens_NonEmpty covers the GetScreens append loop body.
func TestScreenManager_GetScreens_NonEmpty(t *testing.T) {
	sm := NewScreenManager(t.TempDir())
	sm.mu.Lock()
	sm.screens["a"] = &ScreenSession{SessionID: "a", ScreenName: "jarvis-a"}
	sm.screens["b"] = &ScreenSession{SessionID: "b", ScreenName: "jarvis-b"}
	sm.mu.Unlock()
	got := sm.GetScreens()
	if len(got) != 2 {
		t.Errorf("GetScreens len = %d, want 2", len(got))
	}
}

// TestScreenManager_LoadScreens_ReadErrorNotNotExist points the screens file at
// a directory, so os.ReadFile fails with a non-NotExist error and loadScreens
// takes the warn branch (screen.go:265-267) without panicking.
func TestScreenManager_LoadScreens_ReadErrorNotNotExist(t *testing.T) {
	dir := t.TempDir()
	// Make screens.json itself a directory -> ReadFile returns EISDIR, not
	// NotExist.
	if err := os.Mkdir(filepath.Join(dir, "screens.json"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	sm := NewScreenManager(dir) // must not panic
	if sm.GetScreenCount() != 0 {
		t.Errorf("count = %d, want 0 when the screens file is unreadable", sm.GetScreenCount())
	}
}

// TestScreenManager_SaveScreens_WriteError makes the data dir read-only so
// os.WriteFile fails and saveScreens takes the warn branch (screen.go:294-296)
// without panicking.
func TestScreenManager_SaveScreens_WriteError(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires unix file permissions and a non-root user")
	}
	dir := t.TempDir()
	sm := NewScreenManager(dir)
	sm.mu.Lock()
	sm.screens["x"] = &ScreenSession{SessionID: "x", ScreenName: "jarvis-x"}
	sm.mu.Unlock()

	// Drop write permission on the dir so MkdirAll succeeds (dir exists) but
	// WriteFile to screens.json fails with EACCES.
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	// Must not panic; the warn branch swallows the error.
	sm.saveScreens()
}
