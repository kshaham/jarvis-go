package state

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoad_FallsBackToBackup verifies that when state.json is corrupt but
// state.json.backup is valid, the load uses the backup.
func TestLoad_FallsBackToBackup(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "state.json")
	backup := primary + ".backup"

	// Write a good backup.
	goodState := `{"settings":{"foo":"bar"},"config":{},"sessions":{},"version":"v"}`
	if err := os.WriteFile(backup, []byte(goodState), 0600); err != nil {
		t.Fatalf("write backup: %v", err)
	}
	// Write a corrupt primary.
	if err := os.WriteFile(primary, []byte("not-valid-json"), 0600); err != nil {
		t.Fatalf("write primary: %v", err)
	}

	s := NewStore(primary)
	if v, _ := s.GetSettings()["foo"].(string); v != "bar" {
		t.Errorf("expected settings.foo=bar from backup, got %v", s.GetSettings())
	}
}

// TestLoad_PreservesCorruptFile verifies the corrupt primary is renamed
// aside so a subsequent flush won't overwrite it.
func TestLoad_PreservesCorruptFile(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "state.json")
	if err := os.WriteFile(primary, []byte("not-valid-json"), 0600); err != nil {
		t.Fatalf("write primary: %v", err)
	}

	_ = NewStore(primary)

	// The corrupt file should have been moved aside.
	entries, _ := os.ReadDir(dir)
	found := false
	for _, e := range entries {
		if len(e.Name()) > len("state.json.corrupt-") && e.Name()[:len("state.json.corrupt-")] == "state.json.corrupt-" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected state.json.corrupt-<timestamp> file, got entries: %v", entries)
	}
}

// TestFlush_RotatesBackup verifies that the backup file is created on each
// successful flush so corruption recovery always has a recent snapshot.
func TestFlush_RotatesBackup(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "state.json")
	s := NewStore(primary)

	s.UpdateSettings(map[string]interface{}{"k": "v1"})
	s.Flush()

	s.UpdateSettings(map[string]interface{}{"k": "v2"})
	s.Flush()

	backup := primary + ".backup"
	data, err := os.ReadFile(backup)
	if err != nil {
		t.Fatalf("expected backup file: %v", err)
	}
	// Backup should contain the prior value (v1), since the *current* flush
	// writes v2 to primary.
	if !contains(string(data), "v1") {
		t.Errorf("backup should contain v1, got: %s", string(data))
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
