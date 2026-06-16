package office

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// stubLearnRunner records calls and returns a fixed result.
type stubLearnRunner struct {
	mu    sync.Mutex
	calls []string
	res   HeadroomLearnResult
	err   error
}

func (s *stubLearnRunner) run(workingDir string) (HeadroomLearnResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, workingDir)
	return s.res, s.err
}

func (s *stubLearnRunner) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

// withLearnRunner swaps the package runner seam for the duration of a test.
func withLearnRunner(t *testing.T, fn func(string) (HeadroomLearnResult, error)) {
	prev := headroomLearnRunner
	t.Cleanup(func() { headroomLearnRunner = prev })
	headroomLearnRunner = fn
}

func TestMaybeRunHeadroomLearn_DisabledNoop(t *testing.T) {
	installHeadroomStub(t) // binary available...
	stub := &stubLearnRunner{}
	withLearnRunner(t, stub.run)
	m, _, _ := newCtrlManager(t, DefaultConfig())
	m.SetSpawnContext("tmpl", t.TempDir())
	// ...but the sweep is disabled (default).
	m.maybeRunHeadroomLearn()
	time.Sleep(50 * time.Millisecond)
	if stub.callCount() != 0 {
		t.Errorf("expected no learn run when disabled, got %d", stub.callCount())
	}
}

func TestMaybeRunHeadroomLearn_EnabledRuns(t *testing.T) {
	installHeadroomStub(t)
	stub := &stubLearnRunner{res: HeadroomLearnResult{Output: "wrote 2 corrections", Applied: true}}
	withLearnRunner(t, stub.run)
	m, _, _ := newCtrlManager(t, DefaultConfig())
	workDir := t.TempDir()
	m.SetSpawnContext("tmpl", workDir)
	m.SetHeadroomLearnEnabled(true)

	m.maybeRunHeadroomLearn()

	if !waitFor(2*time.Second, func() bool { return stub.callCount() == 1 }) {
		t.Fatalf("expected exactly one learn run, got %d", stub.callCount())
	}
	if stub.calls[0] != workDir {
		t.Errorf("expected learn run in %q, got %q", workDir, stub.calls[0])
	}
}

func TestMaybeRunHeadroomLearn_CooldownPreventsRerun(t *testing.T) {
	installHeadroomStub(t)
	stub := &stubLearnRunner{}
	withLearnRunner(t, stub.run)
	m, _, _ := newCtrlManager(t, DefaultConfig())
	m.SetSpawnContext("tmpl", t.TempDir())
	m.SetHeadroomLearnEnabled(true)

	m.maybeRunHeadroomLearn()
	waitFor(2*time.Second, func() bool { return stub.callCount() == 1 })
	// Immediate second call: still within the 6h cooldown window.
	m.maybeRunHeadroomLearn()
	time.Sleep(100 * time.Millisecond)
	if stub.callCount() != 1 {
		t.Errorf("expected cooldown to prevent a second run, got %d", stub.callCount())
	}
}

func TestMaybeRunHeadroomLearn_NoWorkingDirNoop(t *testing.T) {
	installHeadroomStub(t)
	stub := &stubLearnRunner{}
	withLearnRunner(t, stub.run)
	m, _, _ := newCtrlManager(t, DefaultConfig())
	m.SetHeadroomLearnEnabled(true) // enabled but no working dir set
	m.maybeRunHeadroomLearn()
	time.Sleep(50 * time.Millisecond)
	if stub.callCount() != 0 {
		t.Errorf("expected no run without a working dir, got %d", stub.callCount())
	}
}

func TestRunHeadroomLearnTick_ErrorDoesNotPanic(t *testing.T) {
	stub := &stubLearnRunner{err: errors.New("boom")}
	withLearnRunner(t, stub.run)
	m, _, _ := newCtrlManager(t, DefaultConfig())
	// Direct call; a runner error must just log, not panic.
	m.runHeadroomLearnTick(t.TempDir())
	if stub.callCount() != 1 {
		t.Errorf("expected runner invoked once, got %d", stub.callCount())
	}
}

func TestUpdateOfficeSettings_TogglesHeadroomLearn(t *testing.T) {
	m, _, _ := newCtrlManager(t, DefaultConfig())
	if m.IsHeadroomLearnEnabled() {
		t.Fatal("headroom learn should be OFF by default")
	}
	m.UpdateOfficeSettings(map[string]interface{}{"headroomLearnEnabled": true})
	if !m.IsHeadroomLearnEnabled() {
		t.Error("expected headroom learn enabled after settings update")
	}
}
