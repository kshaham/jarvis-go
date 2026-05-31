package office

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTeardownTeam_RecordExperimentErrorLogged drives the
// RecordExperiment-error branch (spawn_orchestrator.go:541-543): teardown
// still completes (spawned flag flips, agents unregister) even when the
// experiment snapshot write fails. We force the failure via the
// experimentsOpenFile seam.
func TestTeardownTeam_RecordExperimentErrorLogged(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.RegisterAgent("sess-x", "x", &TeamMemberConfig{ID: "x", Name: "X"})
	m.SetTeamSpawned(true)

	orig := experimentsOpenFile
	t.Cleanup(func() { experimentsOpenFile = orig })
	experimentsOpenFile = func(string) (io.WriteCloser, error) {
		return nil, errors.New("simulated open failure")
	}

	// Must not panic; teardown proceeds despite the experiment error.
	m.TeardownTeam(nil)

	if m.IsTeamSpawned() {
		t.Error("team should be torn down even when RecordExperiment fails")
	}
	if m.AgentCount() != 0 {
		t.Errorf("agents should be unregistered, got AgentCount=%d", m.AgentCount())
	}
}

// TestSnapshotRosterAsTeamMemberConfigs_SkipsNilConfig drives the
// a.Config==nil skip (spawn_orchestrator.go:702-703): a registered agent
// whose Config is nil must be omitted from the snapshot.
func TestSnapshotRosterAsTeamMemberConfigs_NilConfigEntrySkipped(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.RegisterAgent("sess-good", "good", &TeamMemberConfig{ID: "good", Name: "Good"})
	m.RegisterAgent("sess-nilcfg", "nilcfg", &TeamMemberConfig{ID: "nilcfg", Name: "NoCfg"})

	// Null out one agent's Config to drive the skip branch.
	bad := m.GetAgent("sess-nilcfg")
	bad.mu.Lock()
	bad.Config = nil
	bad.mu.Unlock()

	got := m.SnapshotRosterAsTeamMemberConfigs()
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 config (nil-config agent skipped), got %d", len(got))
	}
	if got[0].ID != "good" {
		t.Errorf("expected surviving config 'good', got %q", got[0].ID)
	}
}

// TestLoadPersistedStandup_SizeLimitExceeded drives the >maxStandupFileBytes
// guard (standup.go:915-917). We write a standup file just over the limit.
func TestLoadPersistedStandup_SizeLimitExceeded(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := standupDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// One byte over the limit so the LimitReader yields maxStandupFileBytes+1.
	big := make([]byte, maxStandupFileBytes+1)
	for i := range big {
		big[i] = 'a'
	}
	if err := os.WriteFile(filepath.Join(dir, "huge.json"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadPersistedStandup("huge")
	if err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("expected size-limit error, got %v", err)
	}
}

// TestLoadPersistedStandup_ReadError drives the io.ReadAll error branch
// (standup.go:912-914): os.Open succeeds on a directory, but reading its
// contents as a file fails with EISDIR.
func TestLoadPersistedStandup_ReadError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := standupDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Create a DIRECTORY named like a standup file. os.Open succeeds on a
	// dir, but io.ReadAll on the resulting *os.File returns an error
	// (EISDIR on Linux / "is a directory" on macOS).
	if err := os.MkdirAll(filepath.Join(dir, "dirstandup.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPersistedStandup("dirstandup"); err == nil {
		t.Fatal("expected a read error when the standup path is a directory")
	}
}
