package office

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

// noChildrenCmd returns an exec.Cmd that exits with status 1, mimicking
// `pgrep -P` finding no children — so collectDescendants returns empty
// and killProcessTree operates on a single synthetic PID.
func noChildrenCmd() *exec.Cmd {
	return exec.Command("false")
}

// makeReadonlyOfficeMemory creates ~/.jarvis/office-memory as a FILE so
// that any os.MkdirAll(dir) on that path fails, exercising persist error
// branches without depending on uid-0 permission quirks.
func mkdirBlockedHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	jarvis := filepath.Join(home, ".jarvis")
	if err := os.MkdirAll(jarvis, 0o755); err != nil {
		t.Fatal(err)
	}
	// office-memory as a regular file → MkdirAll(office-memory) returns ENOTDIR.
	if err := os.WriteFile(filepath.Join(jarvis, "office-memory"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return home
}

// TestPersistMessages_NoHome hits the UserHomeDir error branch. We call
// the unexported persistMessages directly (same package) to avoid
// SendMessage's async goroutine racing the env restore.
func TestPersistMessages_NoHome(t *testing.T) {
	m := newTestManager()
	withNoHome(t)
	m.persistMessages()
}

// TestPersistMessages_MkdirFails hits the MkdirAll error branch.
func TestPersistMessages_MkdirFails(t *testing.T) {
	mkdirBlockedHome(t)
	m := newTestManager()
	m.mu.Lock()
	m.messages = append(m.messages, OfficeMessage{From: "a", To: "b", Content: "hi"})
	m.mu.Unlock()
	m.persistMessages() // should log+return without panic
}

// TestPersistMessages_WriteAndRenameFail makes messages.json.tmp a dir
// (write fails) — covers the WriteFile error branch.
func TestPersistMessages_WriteFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".jarvis", "office-memory")
	if err := os.MkdirAll(filepath.Join(dir, "messages.json.tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := newTestManager()
	m.persistMessages()
}

// TestPersistMessages_RenameFails makes the final target a non-empty dir.
func TestPersistMessages_RenameFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".jarvis", "office-memory")
	if err := os.MkdirAll(filepath.Join(dir, "messages.json", "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := newTestManager()
	m.persistMessages()
	// tmp should have been removed after the rename failure.
	if _, err := os.Stat(filepath.Join(dir, "messages.json.tmp")); !os.IsNotExist(err) {
		t.Error("expected tmp removed after rename failure")
	}
}

// TestPersistTeamMission_NoHome covers the UserHomeDir error branch and
// still broadcasts.
func TestPersistTeamMission_NoHome(t *testing.T) {
	m := newTestManager()
	var gotEvent string
	m.broadcast = func(event string, data interface{}) { gotEvent = event }
	withNoHome(t)
	m.persistTeamMission("mission x")
	if gotEvent != "office:missionUpdated" {
		t.Errorf("expected broadcast even with no home, got %q", gotEvent)
	}
}

// TestPersistTeamMission_WriteFails makes the tmp path a directory.
func TestPersistTeamMission_WriteFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".jarvis", "office-memory")
	if err := os.MkdirAll(filepath.Join(dir, "team-mission.txt.tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := newTestManager()
	m.persistTeamMission("mission y")
}

// TestPersistTeamMission_RenameFails makes the target a non-empty dir.
func TestPersistTeamMission_RenameFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".jarvis", "office-memory")
	if err := os.MkdirAll(filepath.Join(dir, "team-mission.txt", "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := newTestManager()
	m.persistTeamMission("mission z")
	if _, err := os.Stat(filepath.Join(dir, "team-mission.txt.tmp")); !os.IsNotExist(err) {
		t.Error("expected tmp removed after rename failure")
	}
}

// TestPersistDevInstance_WriteFails makes the slot tmp path a directory.
func TestPersistDevInstance_WriteFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".jarvis", "office-memory")
	if err := os.MkdirAll(filepath.Join(dir, "team-dev-app.json.tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := newTestManager()
	// persistDevInstance is called by SetDevInstance.
	_ = m.SetDevInstance("app", "http://x:3000", 0, true)
	// File should not exist (write of tmp failed).
	if _, err := os.Stat(filepath.Join(dir, "team-dev-app.json")); !os.IsNotExist(err) {
		t.Error("expected no slot file after write failure")
	}
}

// TestPersistDevInstance_RenameFails makes the target a non-empty dir.
func TestPersistDevInstance_RenameFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".jarvis", "office-memory")
	if err := os.MkdirAll(filepath.Join(dir, "team-dev-api.json", "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := newTestManager()
	_ = m.SetDevInstance("api", "http://y:4000", 0, false)
}

// TestPersistMetrics_NoHome covers the UserHomeDir error branch.
func TestPersistMetrics_NoHome(t *testing.T) {
	m := newTestManager()
	withNoHome(t)
	m.PersistMetrics() // returns silently
}

// TestPersistMetrics_MkdirFails covers the MkdirAll error branch.
func TestPersistMetrics_MkdirFails(t *testing.T) {
	mkdirBlockedHome(t)
	m := newTestManager()
	m.PersistMetrics()
}

// TestPersistMetrics_WriteFails makes metrics.json.tmp a directory.
func TestPersistMetrics_WriteFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".jarvis", "office-memory")
	if err := os.MkdirAll(filepath.Join(dir, "metrics.json.tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := newTestManager()
	m.PersistMetrics()
}

// TestPersistMetrics_RenameFails makes metrics.json a non-empty dir.
func TestPersistMetrics_RenameFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".jarvis", "office-memory")
	if err := os.MkdirAll(filepath.Join(dir, "metrics.json", "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := newTestManager()
	m.PersistMetrics()
	if _, err := os.Stat(filepath.Join(dir, "metrics.json.tmp")); !os.IsNotExist(err) {
		t.Error("expected tmp removed after rename failure")
	}
}

// TestSaveAgentMemory_WriteFails makes the memory path's tmp a directory.
func TestSaveAgentMemory_WriteFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := "writer"
	wd := t.TempDir()
	path := GetAgentMemoryPath(cfg, wd)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	// Make the tmp path a directory so os.WriteFile(tmp) fails.
	if err := os.MkdirAll(path+".tmp", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SaveAgentMemory(cfg, wd, "content"); err == nil {
		t.Fatal("expected write error when tmp path is a directory")
	}
}

// TestSaveAgentMemory_RenameFails makes the final path a non-empty dir.
func TestSaveAgentMemory_RenameFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := "renamer"
	wd := t.TempDir()
	path := GetAgentMemoryPath(cfg, wd)
	if err := os.MkdirAll(filepath.Join(path, "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SaveAgentMemory(cfg, wd, "content"); err == nil {
		t.Fatal("expected rename error when target is a non-empty directory")
	}
}

// TestAppendAgentMemory_OpenFails makes the memory path a directory so
// os.OpenFile fails.
func TestAppendAgentMemory_OpenFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := "appender"
	wd := t.TempDir()
	path := GetAgentMemoryPath(cfg, wd)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := AppendAgentMemory(cfg, wd, "entry\n"); err == nil {
		t.Fatal("expected open error when path is a directory")
	}
}

// --- killProcessTree seam tests ----------------------------------------

// fakeKiller simulates syscall.Kill so killProcessTree can be exercised
// against synthetic PIDs without signaling real processes.
type fakeKiller struct {
	mu        sync.Mutex
	alive     map[int]bool   // current liveness for sig-0 probes
	termAfter map[int]int    // sig-0 probes before the PID "dies"
	probes    map[int]int    // count of sig-0 probes per PID
	termSent  []int          // PIDs that received SIGTERM
	killSent  []int          // PIDs that received SIGKILL
	signalErr map[int]error  // forced error on a SIGKILL for a PID
}

func (f *fakeKiller) kill(pid int, sig syscall.Signal) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch sig {
	case 0:
		f.probes[pid]++
		// If termAfter is set, the PID dies once probes exceed the threshold.
		if n, ok := f.termAfter[pid]; ok && f.probes[pid] > n {
			f.alive[pid] = false
		}
		if f.alive[pid] {
			return nil
		}
		return syscall.ESRCH
	case syscall.SIGTERM:
		f.termSent = append(f.termSent, pid)
		return nil
	case syscall.SIGKILL:
		f.killSent = append(f.killSent, pid)
		if err, ok := f.signalErr[pid]; ok {
			return err
		}
		f.alive[pid] = false
		return nil
	}
	return nil
}

// installFakeKiller swaps sysKill for a fake and restores it on cleanup.
func installFakeKiller(t *testing.T, f *fakeKiller) {
	t.Helper()
	orig := sysKill
	t.Cleanup(func() { sysKill = orig })
	sysKill = f.kill
}

// withFastKillTimeout shortens the kill grace window so the escalation
// test doesn't spend 3 real seconds. Behavior-preserving: prod keeps 3s.
func withFastKillTimeout(t *testing.T) {
	t.Helper()
	origTO, origPoll := killProcessTreeGraceTimeout, killProcessTreePollInterval
	t.Cleanup(func() {
		killProcessTreeGraceTimeout = origTO
		killProcessTreePollInterval = origPoll
	})
	killProcessTreeGraceTimeout = 60 * time.Millisecond
	killProcessTreePollInterval = 10 * time.Millisecond
}

// TestKillProcessTree_GracefulExit exercises the SIGTERM + wait-loop path
// where the process dies before the deadline (no SIGKILL escalation).
func TestKillProcessTree_GracefulExit(t *testing.T) {
	// Stub execCommand so collectDescendants finds no children — keep the
	// tree a single PID and isolate the wait-loop behavior.
	orig := execCommand
	t.Cleanup(func() { execCommand = orig })
	execCommand = func(name string, args ...string) *exec.Cmd { return noChildrenCmd() }
	withFastKillTimeout(t)

	f := &fakeKiller{
		alive:     map[int]bool{5000: true},
		termAfter: map[int]int{5000: 0}, // dies on the FIRST sig-0 probe
		probes:    map[int]int{},
		signalErr: map[int]error{},
	}
	installFakeKiller(t, f)

	killProcessTree(5000)

	if len(f.termSent) != 1 || f.termSent[0] != 5000 {
		t.Errorf("expected SIGTERM to 5000, got %v", f.termSent)
	}
	if len(f.killSent) != 0 {
		t.Errorf("expected no SIGKILL on graceful exit, got %v", f.killSent)
	}
}

// TestKillProcessTree_EscalatesToKill exercises the SIGTERM → deadline →
// SIGKILL escalation path. The PID stays alive through every sig-0 probe.
func TestKillProcessTree_EscalatesToKill(t *testing.T) {
	orig := execCommand
	t.Cleanup(func() { execCommand = orig })
	execCommand = func(name string, args ...string) *exec.Cmd { return noChildrenCmd() }
	withFastKillTimeout(t)

	f := &fakeKiller{
		alive:     map[int]bool{6000: true}, // never dies on its own
		termAfter: map[int]int{},
		probes:    map[int]int{},
		signalErr: map[int]error{},
	}
	installFakeKiller(t, f)

	killProcessTree(6000)

	if len(f.killSent) != 1 || f.killSent[0] != 6000 {
		t.Errorf("expected SIGKILL escalation to 6000, got %v", f.killSent)
	}
}
