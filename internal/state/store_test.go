package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
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

// TestGetSettingsSnapshotAndUpdate verifies that GetSettings reflects updates
// and that concurrent reads under -race see no data races.
func TestGetSettingsSnapshotAndUpdate(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "state.json")
	s := NewStore(primary)

	// Initial get should return an empty map.
	got := s.GetSettings()
	if len(got) != 0 {
		t.Fatalf("expected empty settings, got %v", got)
	}

	// Update and verify the new value is reflected.
	s.UpdateSettings(map[string]interface{}{"color": "blue"})
	got = s.GetSettings()
	if v, _ := got["color"].(string); v != "blue" {
		t.Errorf("expected color=blue after update, got %v", got)
	}

	// Second update: old key overwritten, new key added.
	s.UpdateSettings(map[string]interface{}{"color": "red", "size": float64(42)})
	got = s.GetSettings()
	if v, _ := got["color"].(string); v != "red" {
		t.Errorf("expected color=red, got %v", got["color"])
	}
	if v, _ := got["size"].(float64); v != 42 {
		t.Errorf("expected size=42, got %v", got["size"])
	}

	// Concurrent reads while updates happen — -race detector must stay silent.
	var wg sync.WaitGroup
	const readers = 40
	const writers = 5
	wg.Add(readers + writers)
	for i := 0; i < readers; i++ {
		go func() {
			defer wg.Done()
			m := s.GetSettings()
			_ = m["color"]
		}()
	}
	for i := 0; i < writers; i++ {
		go func(n int) {
			defer wg.Done()
			s.UpdateSettings(map[string]interface{}{"concurrent": float64(n)})
		}(i)
	}
	wg.Wait()

	// Snapshot must reflect one of the concurrent writes.
	final := s.GetSettings()
	if _, ok := final["concurrent"]; !ok {
		t.Error("concurrent write was not reflected in final snapshot")
	}
}

// TestSaveAndRemoveSession exercises SaveSession + RemoveSession round-trip.
func TestSaveAndRemoveSession(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "state.json"))
	s.SaveSession(&SessionState{ID: "s1", Name: "one"})
	s.SaveSession(&SessionState{ID: "s2", Name: "two"})
	if len(s.GetSessions()) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(s.GetSessions()))
	}
	s.RemoveSession("s1")
	got := s.GetSessions()
	if len(got) != 1 {
		t.Fatalf("expected 1 session after remove, got %d", len(got))
	}
	if _, ok := got["s2"]; !ok {
		t.Fatal("expected s2 to remain")
	}
}

// TestIncrementSettingCounter_IntAndStringSeeds covers the int and string
// branches of the type switch (JSON seeds float64; these come from in-process
// writers or hand-written files).
func TestIncrementSettingCounter_IntAndStringSeeds(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "state.json"))
	s.UpdateSettings(map[string]interface{}{"intKey": int(5)})
	if got := s.IncrementSettingCounter("intKey"); got != 6 {
		t.Fatalf("int seed: expected 6, got %d", got)
	}
	s.UpdateSettings(map[string]interface{}{"strKey": "10"})
	if got := s.IncrementSettingCounter("strKey"); got != 11 {
		t.Fatalf("string seed: expected 11, got %d", got)
	}
}

// TestLoadFromFile_ReinitsNilMaps loads a state file that omits the settings,
// config, and sessions keys, exercising the nil-map re-init branches.
func TestLoadFromFile_ReinitsNilMaps(t *testing.T) {
	primary := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(primary, []byte(`{"version":"v"}`), 0600); err != nil {
		t.Fatal(err)
	}
	s := NewStore(primary)
	if s.GetSettings() == nil || s.GetConfig() == nil || s.GetSessions() == nil {
		t.Fatal("expected all maps re-initialized to non-nil")
	}
}

// TestLoad_ValidPrimary covers the happy path where the primary state file
// loads successfully (load returns early).
func TestLoad_ValidPrimary(t *testing.T) {
	primary := filepath.Join(t.TempDir(), "state.json")
	good := `{"settings":{"k":"v"},"config":{},"sessions":{},"version":"v"}`
	if err := os.WriteFile(primary, []byte(good), 0600); err != nil {
		t.Fatal(err)
	}
	s := NewStore(primary)
	if v, _ := s.GetSettings()["k"].(string); v != "v" {
		t.Fatalf("expected k=v from valid primary, got %v", s.GetSettings())
	}
}

// TestRecordDailyStats_TrimsTo30 pre-seeds 30 days of stats then records a new
// day, exercising the 30-entry cap.
func TestRecordDailyStats_TrimsTo30(t *testing.T) {
	primary := filepath.Join(t.TempDir(), "state.json")
	stats := make([]DailyTokenStats, 30)
	for i := range stats {
		stats[i] = DailyTokenStats{Date: "2026-01-" + twoDigit(i+1)}
	}
	seed := map[string]interface{}{
		"settings": map[string]interface{}{}, "config": map[string]interface{}{},
		"sessions": map[string]interface{}{}, "dailyStats": stats, "version": "v",
	}
	b, _ := json.Marshal(seed)
	if err := os.WriteFile(primary, b, 0600); err != nil {
		t.Fatal(err)
	}
	s := NewStore(primary)
	s.RecordDailyStats(100, 50, "")
	if got := len(s.GetDailyStats()); got != 30 {
		t.Fatalf("expected cap at 30 entries, got %d", got)
	}
}

func twoDigit(i int) string {
	if i < 10 {
		return "0" + string(rune('0'+i))
	}
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}

// TestSaveDebounceTimerFlushes verifies the debounced AfterFunc path actually
// writes to disk without an explicit Flush.
func TestSaveDebounceTimerFlushes(t *testing.T) {
	primary := filepath.Join(t.TempDir(), "state.json")
	s := NewStore(primary)
	s.UpdateSettings(map[string]interface{}{"debounced": "yes"})
	// Wait past the 500ms debounce window for the timer to fire.
	time.Sleep(debouncePersistDelay + 250*time.Millisecond)
	data, err := os.ReadFile(primary)
	if err != nil {
		t.Fatalf("expected state file written by debounce timer: %v", err)
	}
	if !contains(string(data), "debounced") {
		t.Fatalf("expected debounced write on disk, got: %s", string(data))
	}
}

// TestFlush_WriteErrorIsHandled points the state file under a path whose parent
// is a regular file, so MkdirAll + the atomic temp write both fail. Flush must
// swallow the error without panicking (the error branches in flushLocked).
func TestFlush_WriteErrorIsHandled(t *testing.T) {
	dir := t.TempDir()
	notADir := filepath.Join(dir, "blocker")
	if err := os.WriteFile(notADir, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	// state.json sits "inside" a regular file -> dir creation/write will fail.
	s := NewStore(filepath.Join(notADir, "state.json"))
	s.UpdateSettings(map[string]interface{}{"k": "v"})
	s.Flush() // must not panic; the write error is logged and swallowed.
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
