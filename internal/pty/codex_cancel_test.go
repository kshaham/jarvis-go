package pty

import (
	"os/exec"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// TestKillProcessGroup_NilProcess covers the nil-process guard in
// killProcessGroup (codex_session.go cmd.Cancel). A *exec.Cmd that was never
// Start()ed has a nil Process; Cancel must no-op (return nil) rather than
// dereference it. The runtime only calls Cancel after Start, so this guard is
// unreachable through the normal exec flow -- we invoke the extracted helper
// directly.
func TestKillProcessGroup_NilProcess(t *testing.T) {
	cmd := exec.Command("true") // not started -> Process is nil
	if err := killProcessGroup(cmd); err != nil {
		t.Errorf("killProcessGroup(unstarted) = %v, want nil", err)
	}
}

// TestKillProcessGroup_LiveProcess covers the real kill path: a started process
// in its own group is SIGKILLed and reaped. Asserts Wait reports the kill.
func TestKillProcessGroup_LiveProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process-group SIGKILL is unix-only")
	}
	// A long sleep we will kill. Setpgid mirrors runTurn so the negative-pid
	// group signal in killProcessGroup has a group to target.
	cmd := exec.Command("sleep", "60")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	if cmd.Process == nil {
		t.Fatal("expected a non-nil Process after Start")
	}
	if err := killProcessGroup(cmd); err != nil {
		t.Fatalf("killProcessGroup(live) = %v, want nil", err)
	}
	// The process must actually die.
	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()
	select {
	case err := <-waitErr:
		if err == nil {
			t.Error("Wait should report the SIGKILL, got nil error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit after killProcessGroup")
	}
}
