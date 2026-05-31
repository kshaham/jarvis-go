package office

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// loadHandoffStaleThreshold
// ---------------------------------------------------------------------------

// TestLoadHandoffStaleThreshold_MissingReturnsDefault verifies the default
// is returned when the override file is absent.
func TestLoadHandoffStaleThreshold_MissingReturnsDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if got := loadHandoffStaleThreshold(); got != DefaultHandoffStale {
		t.Errorf("expected default %s, got %s", DefaultHandoffStale, got)
	}
}

// TestLoadHandoffStaleThreshold_ValidOverride verifies a valid positive
// seconds value is honored.
func TestLoadHandoffStaleThreshold_ValidOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeHandoffStaleFile(t, home, "120")

	if got := loadHandoffStaleThreshold(); got != 120*time.Second {
		t.Errorf("expected 120s, got %s", got)
	}
}

// TestLoadHandoffStaleThreshold_InvalidFallsBack verifies non-numeric and
// non-positive values fall back to the default.
func TestLoadHandoffStaleThreshold_InvalidFallsBack(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	for _, bad := range []string{"not-a-number", "0", "-5"} {
		writeHandoffStaleFile(t, home, bad)
		if got := loadHandoffStaleThreshold(); got != DefaultHandoffStale {
			t.Errorf("value %q: expected default %s, got %s", bad, DefaultHandoffStale, got)
		}
	}
}

func writeHandoffStaleFile(t *testing.T, home, content string) {
	t.Helper()
	dir := filepath.Join(home, ".jarvis", "office-memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "handoff-stale-seconds.txt"), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// ---------------------------------------------------------------------------
// savePersistedHandoffs / loadPersistedHandoffs
// ---------------------------------------------------------------------------

// TestPersistedHandoffs_RoundTrip verifies a saved audit slice is read back
// and that a missing file yields nil.
func TestPersistedHandoffs_RoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// Missing file -> nil.
	if got := loadPersistedHandoffs(); got != nil {
		t.Errorf("expected nil for missing handoffs file, got %+v", got)
	}

	entries := []HandoffRequest{
		{TaskID: "t-1", TaskTitle: "Refactor", FromName: "Vex", ToName: "Bolt", Status: HandoffAccepted, CreatedAt: time.Now()},
		{TaskID: "t-2", TaskTitle: "Tests", FromName: "Bolt", ToName: "Vex", Status: HandoffRejected, CreatedAt: time.Now()},
	}
	if err := savePersistedHandoffs(entries); err != nil {
		t.Fatalf("save: %v", err)
	}

	got := loadPersistedHandoffs()
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got))
	}
	if got[0].TaskID != "t-1" || got[1].Status != HandoffRejected {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

// TestLoadPersistedHandoffs_CorruptQuarantined verifies a malformed
// handoffs.json is quarantined (renamed) and load returns nil.
func TestLoadPersistedHandoffs_CorruptQuarantined(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".jarvis", "office-memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, "handoffs.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if got := loadPersistedHandoffs(); got != nil {
		t.Errorf("expected nil for corrupt file, got %+v", got)
	}

	// The original file should have been renamed away (quarantined).
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("expected corrupt handoffs.json to be quarantined (renamed)")
	}
	// A .corrupt-* sibling should now exist.
	matches, _ := filepath.Glob(filepath.Join(dir, "handoffs.json.corrupt-*"))
	if len(matches) == 0 {
		t.Error("expected a quarantined .corrupt-* file")
	}
}
