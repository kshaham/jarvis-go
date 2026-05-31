package office

import (
	"testing"

	"github.com/kshaham/jarvis/internal/library"
)

// TestGetDevInstance_NilMap drives the `m.teamDevInstances == nil`
// early-return branch in GetDevInstance (manager.go ~1424-1426).
func TestGetDevInstance_NilMap(t *testing.T) {
	m := newTestManager()
	m.mu.Lock()
	m.teamDevInstances = nil
	m.mu.Unlock()

	if _, ok := m.GetDevInstance("anything"); ok {
		t.Error("expected (zero, false) when teamDevInstances is nil")
	}
}

// TestSetDevInstanceCredentials_InitializesNilMap drives the nil-map
// lazy-init branch in SetDevInstanceCredentials (manager.go ~1562-1564):
// with teamDevInstances nil, the first call must allocate the map and
// create the slot.
func TestSetDevInstanceCredentials_InitializesNilMap(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.mu.Lock()
	m.teamDevInstances = nil
	m.mu.Unlock()

	if err := m.SetDevInstanceCredentials("app", "user:pass"); err != nil {
		t.Fatalf("SetDevInstanceCredentials: %v", err)
	}
	inst, ok := m.GetDevInstance("app")
	if !ok {
		t.Fatal("expected the slot to be created after nil-map init")
	}
	if inst.Credentials != "user:pass" {
		t.Errorf("expected credentials stored, got %q", inst.Credentials)
	}
}

// TestMaybeRunMemify_EmptyLibraryDirEarlyReturn drives the
// `library.Dir() == ""` early-return branch in maybeRunMemify
// (memify_tick.go ~52-54). With an empty library dir the method returns
// before spawning any goroutine, so this is race-free.
func TestMaybeRunMemify_EmptyLibraryDirEarlyReturn(t *testing.T) {
	restore := library.SetDirForTest("")
	t.Cleanup(restore)

	m := newTestManager()
	// Gate is open (lastRunAt zero) but Dir()=="" short-circuits first.
	// No goroutine is spawned, so the SetDirForTest restore can't race a
	// concurrent library.Dir() read.
	m.maybeRunMemify()
}
