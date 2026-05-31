package notifications

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestStore_LoadCorruptPrimaryFallsBackToBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "routes.json")

	// Write a valid backup with one route, and a corrupt primary. load()
	// should preserve the corrupt primary, then read the backup.
	good := []*Route{{ID: "r1", Name: "FromBackup", Enabled: true}}
	backupData, _ := json.Marshal(good)
	if err := os.WriteFile(path+".backup", backupData, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}

	s := NewStore(path)
	if got := s.Get("r1"); got == nil || got.Name != "FromBackup" {
		t.Fatalf("expected route recovered from backup, got %+v", got)
	}
	// The corrupt primary should have been renamed aside.
	matches, _ := filepath.Glob(path + ".corrupt-*")
	if len(matches) == 0 {
		t.Error("expected corrupt primary to be preserved with .corrupt- suffix")
	}
}

func TestStore_LoadMissingPrimaryUsesBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "routes.json")
	good := []*Route{{ID: "b1", Name: "Backup", Enabled: true}}
	data, _ := json.Marshal(good)
	if err := os.WriteFile(path+".backup", data, 0600); err != nil {
		t.Fatal(err)
	}
	// No primary file at all.
	s := NewStore(path)
	if got := s.Get("b1"); got == nil || got.Name != "Backup" {
		t.Fatalf("expected route loaded from backup when primary missing, got %+v", got)
	}
}

func TestStore_LoadCorruptPrimaryAndCorruptBackupStartsBlank(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "routes.json")
	if err := os.WriteFile(path, []byte("garbage"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".backup", []byte("also garbage"), 0600); err != nil {
		t.Fatal(err)
	}
	s := NewStore(path)
	if got := s.List(); len(got) != 0 {
		t.Fatalf("expected blank store when both primary and backup corrupt, got %d routes", len(got))
	}
}

func TestStore_LoadNoFilesStartsEmpty(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "fresh", "routes.json"))
	if got := len(s.List()); got != 0 {
		t.Fatalf("fresh store should be empty, got %d", got)
	}
}

func TestStore_SaveRotatesBackupOnSecondWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "routes.json")
	s := NewStore(path)

	// First create writes the primary (no prior primary -> no backup yet).
	s.Create(Route{Name: "first", Enabled: true})
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("primary should exist after first create: %v", err)
	}

	// Second create should rotate the existing primary into .backup.
	s.Create(Route{Name: "second", Enabled: true})
	if _, err := os.Stat(path + ".backup"); err != nil {
		t.Fatalf("backup should exist after second create: %v", err)
	}

	// Both routes must be present and round-trip through a reopen.
	reopened := NewStore(path)
	if len(reopened.List()) != 2 {
		t.Fatalf("expected 2 routes after reopen, got %d", len(reopened.List()))
	}
}

func TestStore_DeletePersistsRemoval(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "routes.json")
	s := NewStore(path)
	r := s.Create(Route{Name: "doomed", Enabled: true})

	if err := s.Delete(r.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if s.Get(r.ID) != nil {
		t.Error("route should be gone after delete")
	}
	// Removal must survive a reopen (delete called save()).
	reopened := NewStore(path)
	if reopened.Get(r.ID) != nil {
		t.Error("deleted route reappeared after reopen")
	}
	if len(reopened.List()) != 0 {
		t.Errorf("expected empty store after delete+reopen, got %d", len(reopened.List()))
	}
}

func TestStore_NewStoreMkdirFailureStillReturnsUsableStore(t *testing.T) {
	// Make a regular file, then ask NewStore to create a path *under* it.
	// MkdirAll fails (a file is not a directory); NewStore logs and still
	// returns a usable in-memory store.
	dir := t.TempDir()
	fileAsDir := filepath.Join(dir, "iamafile")
	if err := os.WriteFile(fileAsDir, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(fileAsDir, "subdir", "routes.json")
	s := NewStore(path)
	if s == nil {
		t.Fatal("NewStore should still return a store even when mkdir fails")
	}
	// In-memory operations still work even though persistence will fail.
	if got := len(s.List()); got != 0 {
		t.Errorf("expected empty store, got %d", got)
	}
}

func TestStore_SaveWriteFailureKeepsInMemoryState(t *testing.T) {
	// Point the store at a path whose parent is a regular file so the
	// staging WriteFile (path+".tmp") fails. Create still mutates the
	// in-memory map; persistence silently fails (logged).
	dir := t.TempDir()
	fileAsDir := filepath.Join(dir, "blockingfile")
	if err := os.WriteFile(fileAsDir, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(fileAsDir, "routes.json")
	s := NewStore(path) // mkdir already failed; dir is a file
	r := s.Create(Route{Name: "ephemeral", Enabled: true})
	// In-memory create succeeded despite the failed persistence.
	if s.Get(r.ID) == nil {
		t.Fatal("in-memory create should succeed even when save() fails to write")
	}
	// The tmp staging file must not exist (WriteFile failed).
	if _, err := os.Stat(path + ".tmp"); err == nil {
		t.Error("tmp file should not exist after WriteFile failure")
	}
}

func TestStore_UpdateNotFound(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "routes.json"))
	_, err := s.Update("nope", Route{Name: "x"})
	if err != ErrNotFound {
		t.Errorf("Update(unknown) = %v, want ErrNotFound", err)
	}
}

func TestStore_GetNotFoundReturnsNil(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "routes.json"))
	if got := s.Get("missing"); got != nil {
		t.Errorf("Get(missing) = %+v, want nil", got)
	}
}

func TestStore_ListReturnsSortedCopies(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "routes.json"))
	s.Create(Route{Name: "zebra"})
	s.Create(Route{Name: "alpha"})
	s.Create(Route{Name: "mango"})

	list := s.List()
	if len(list) != 3 {
		t.Fatalf("expected 3 routes, got %d", len(list))
	}
	if list[0].Name != "alpha" || list[1].Name != "mango" || list[2].Name != "zebra" {
		t.Errorf("List not sorted by name: %q, %q, %q", list[0].Name, list[1].Name, list[2].Name)
	}

	// Mutating a returned copy must not affect the stored route.
	list[0].Name = "MUTATED"
	again := s.List()
	if again[0].Name != "alpha" {
		t.Errorf("List should return copies; store was mutated to %q", again[0].Name)
	}
}
