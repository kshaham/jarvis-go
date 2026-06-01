package teamtemplates

import (
	"os"
	"path/filepath"
	"testing"
)

// TestStore_ListReturnsSortedCustom covers the public List wrapper.
func TestStore_ListReturnsSortedCustom(t *testing.T) {
	s := NewStore(t.TempDir())
	s.Create(TeamTemplate{Name: "Zeta"})
	s.Create(TeamTemplate{Name: "Alpha"})
	got := s.List()
	if len(got) != 2 || got[0].Name != "Alpha" || got[1].Name != "Zeta" {
		t.Fatalf("expected name-sorted list, got %#v", got)
	}
}

// TestStore_DuplicateMissingReturnsNil covers the both-nil branch of Duplicate.
func TestStore_DuplicateMissingReturnsNil(t *testing.T) {
	s := NewStore(t.TempDir())
	if got := s.Duplicate("does-not-exist", nil); got != nil {
		t.Fatalf("expected nil for missing source, got %#v", got)
	}
}

// TestStore_LoadFallsBackToBackup covers the unmarshal-error + .backup recovery
// path in load: a corrupt primary file with a valid .backup must restore the
// backup's templates.
func TestStore_LoadFallsBackToBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "team-templates.json")
	backup := path + ".backup"
	if err := os.WriteFile(path, []byte("{ not json"), 0644); err != nil {
		t.Fatal(err)
	}
	good := `[{"id":"t1","name":"Recovered","category":"custom"}]`
	if err := os.WriteFile(backup, []byte(good), 0644); err != nil {
		t.Fatal(err)
	}
	s := NewStore(dir)
	if got := s.Get("t1"); got == nil || got.Name != "Recovered" {
		t.Fatalf("expected template recovered from backup, got %#v", got)
	}
}

// TestStore_SaveWriteErrorIsLogged makes the store directory unwritable so the
// atomic temp write fails, exercising save's write-error branch without a
// panic.
func TestStore_SaveWriteErrorIsLogged(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	// Drop write permission on the dir so WriteFile(tmp) fails.
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755) // restore so TempDir cleanup can remove it
	// Create triggers save(); the write error must be swallowed (logged).
	if got := s.Create(TeamTemplate{Name: "NoDisk"}); got == nil {
		t.Fatal("Create should still return the in-memory template")
	}
}
