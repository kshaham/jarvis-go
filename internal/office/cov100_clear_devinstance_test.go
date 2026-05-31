package office

import (
	"os/exec"
	"testing"
)

// TestClearDevInstance_KillsProcessWhenRequested drives the
// `killProc && pid >= 100` branch of ClearDevInstance (manager.go:1620-1622).
// We store a synthetic dev instance with a PID >= 100, stub the kill +
// descendant-collection seams so no real process is signalled, and assert
// the instance is removed and the kill path ran.
func TestClearDevInstance_KillsProcessWhenRequested(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()

	// Keep collectDescendants from shelling out to a real `pgrep`.
	origExec := execCommand
	t.Cleanup(func() { execCommand = origExec })
	execCommand = func(string, ...string) *exec.Cmd { return noChildrenCmd() }
	withFastKillTimeout(t)

	const pid = 4242 // >= 100 so the kill branch fires
	f := &fakeKiller{
		alive:     map[int]bool{pid: true},
		termAfter: map[int]int{pid: 0}, // dies on first sig-0 probe (graceful)
		probes:    map[int]int{},
		signalErr: map[int]error{},
	}
	installFakeKiller(t, f)

	m.mu.Lock()
	m.teamDevInstances["app"] = &devInstance{URL: "http://localhost:3000", PID: pid}
	m.mu.Unlock()

	m.ClearDevInstance("app", true)

	// The kill path must have signalled our PID.
	if len(f.termSent) == 0 || f.termSent[0] != pid {
		t.Errorf("expected SIGTERM to pid %d, got %v", pid, f.termSent)
	}
	// The slot must be gone.
	m.mu.RLock()
	_, stillThere := m.teamDevInstances["app"]
	m.mu.RUnlock()
	if stillThere {
		t.Error("dev instance slot should have been deleted")
	}
}

// TestClearDevInstance_NoKillWhenPidTooLow documents the guard's lower
// bound: a PID < 100 must NOT trigger killProcessTree even when killProc
// is true (defends against signalling init / reused low PIDs).
func TestClearDevInstance_NoKillWhenPidTooLow(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()

	f := &fakeKiller{
		alive:     map[int]bool{},
		termAfter: map[int]int{},
		probes:    map[int]int{},
		signalErr: map[int]error{},
	}
	installFakeKiller(t, f)

	m.mu.Lock()
	m.teamDevInstances["low"] = &devInstance{PID: 42} // < 100
	m.mu.Unlock()

	m.ClearDevInstance("low", true)

	if len(f.termSent) != 0 {
		t.Errorf("low PID should not be signalled, got %v", f.termSent)
	}
	m.mu.RLock()
	_, stillThere := m.teamDevInstances["low"]
	m.mu.RUnlock()
	if stillThere {
		t.Error("dev instance slot should have been deleted even without a kill")
	}
}
