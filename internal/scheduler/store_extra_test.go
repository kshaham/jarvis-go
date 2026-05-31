package scheduler

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStore_List_SortedByName(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "sched.json"))
	s.Create(Job{Name: "charlie", Cron: "* * * * *", Action: ActionWebhook, Enabled: true})
	s.Create(Job{Name: "alpha", Cron: "* * * * *", Action: ActionWebhook, Enabled: true})
	s.Create(Job{Name: "bravo", Cron: "* * * * *", Action: ActionWebhook, Enabled: true})

	list := s.List()
	if len(list) != 3 {
		t.Fatalf("expected 3 jobs, got %d", len(list))
	}
	if list[0].Name != "alpha" || list[1].Name != "bravo" || list[2].Name != "charlie" {
		t.Fatalf("expected name-sorted order, got %s,%s,%s", list[0].Name, list[1].Name, list[2].Name)
	}
	// List returns copies; mutating one must not touch the store.
	list[0].Name = "MUTATED"
	again := s.List()
	if again[0].Name != "alpha" {
		t.Fatalf("List should return copies; store was mutated to %q", again[0].Name)
	}
}

func TestStore_List_Empty(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "sched.json"))
	if got := s.List(); len(got) != 0 {
		t.Fatalf("expected empty list, got %d", len(got))
	}
}

func TestStore_Get_NotFound(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "sched.json"))
	if got := s.Get("missing"); got != nil {
		t.Fatalf("expected nil for missing id, got %+v", got)
	}
}

func TestStore_Create_DuplicateID(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "sched.json"))
	first, err := s.Create(Job{ID: "fixed-id", Name: "one", Cron: "* * * * *", Action: ActionWebhook, Enabled: true})
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	if first.ID != "fixed-id" {
		t.Fatalf("expected provided id preserved, got %q", first.ID)
	}
	if first.CreatedAt.IsZero() || first.UpdatedAt.IsZero() {
		t.Fatal("expected timestamps set on create")
	}
	_, err = s.Create(Job{ID: "fixed-id", Name: "dup", Cron: "* * * * *", Action: ActionWebhook, Enabled: true})
	if err != ErrDuplicateID {
		t.Fatalf("expected ErrDuplicateID, got %v", err)
	}
}

func TestStore_Update_NotFound(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "sched.json"))
	if _, err := s.Update("nope", Job{Name: "x"}); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestStore_Update_PreservesCreatedAtAndID(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "sched.json"))
	created, _ := s.Create(Job{Name: "orig", Cron: "* * * * *", Action: ActionWebhook, Enabled: true})
	origCreated := created.CreatedAt
	time.Sleep(2 * time.Millisecond)
	updated, err := s.Update(created.ID, Job{Name: "new", Cron: "0 1 * * *", Action: ActionOneShot, Enabled: false})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.ID != created.ID {
		t.Fatalf("ID should be preserved, got %q want %q", updated.ID, created.ID)
	}
	if !updated.CreatedAt.Equal(origCreated) {
		t.Fatalf("CreatedAt should be preserved, got %v want %v", updated.CreatedAt, origCreated)
	}
	if !updated.UpdatedAt.After(origCreated) {
		t.Fatalf("UpdatedAt should advance, got %v", updated.UpdatedAt)
	}
	if updated.Name != "new" || updated.Cron != "0 1 * * *" || updated.Action != ActionOneShot || updated.Enabled {
		t.Fatalf("update did not apply mutable fields: %+v", updated)
	}
}

func TestStore_Delete_Existing(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "sched.json"))
	created, _ := s.Create(Job{Name: "to-del", Cron: "* * * * *", Action: ActionWebhook, Enabled: true})
	if err := s.Delete(created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if s.Get(created.ID) != nil {
		t.Fatal("expected job gone after delete")
	}
}

func TestStore_UpdateRunStatus_Persists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sched.json")
	s := NewStore(path)
	created, _ := s.Create(Job{Name: "run", Cron: "* * * * *", Action: ActionWebhook, Enabled: true})

	at := time.Now()
	s.UpdateRunStatus(created.ID, at, "ok", "")
	got := s.Get(created.ID)
	if got.LastStatus != "ok" {
		t.Fatalf("expected LastStatus ok, got %q", got.LastStatus)
	}
	if got.LastRunAt == nil || !got.LastRunAt.Equal(at) {
		t.Fatalf("expected LastRunAt=%v, got %v", at, got.LastRunAt)
	}

	// Error case.
	s.UpdateRunStatus(created.ID, time.Now(), "error", "boom")
	got = s.Get(created.ID)
	if got.LastStatus != "error" || got.LastError != "boom" {
		t.Fatalf("expected error status/message, got %q/%q", got.LastStatus, got.LastError)
	}

	// Persisted to disk: reopen and verify.
	s2 := NewStore(path)
	reloaded := s2.Get(created.ID)
	if reloaded.LastStatus != "error" || reloaded.LastError != "boom" {
		t.Fatalf("run status did not persist across reopen: %q/%q", reloaded.LastStatus, reloaded.LastError)
	}
}

func TestStore_UpdateRunStatus_MissingID_NoOp(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "sched.json"))
	// Should not panic or error for a missing id.
	s.UpdateRunStatus("ghost", time.Now(), "ok", "")
	if len(s.List()) != 0 {
		t.Fatal("missing-id status update should not create a job")
	}
}

func TestStore_SetNextRun_PersistsAndMissingNoOp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sched.json")
	s := NewStore(path)
	created, _ := s.Create(Job{Name: "next", Cron: "* * * * *", Action: ActionWebhook, Enabled: true})
	next := time.Now().Add(time.Hour)
	s.SetNextRun(created.ID, next)
	got := s.Get(created.ID)
	if got.NextRunAt == nil || !got.NextRunAt.Equal(next) {
		t.Fatalf("expected NextRunAt=%v, got %v", next, got.NextRunAt)
	}
	// Missing id: no-op.
	s.SetNextRun("ghost", time.Now())

	// Survives reopen.
	s2 := NewStore(path)
	reloaded := s2.Get(created.ID)
	if reloaded.NextRunAt == nil || !reloaded.NextRunAt.Equal(next) {
		t.Fatalf("NextRunAt did not persist across reopen: %v", reloaded.NextRunAt)
	}
}

func TestStore_Load_CorruptPrimaryFallsBackToBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sched.json")

	// Seed a good store, which writes primary + (on second save) a backup.
	s := NewStore(path)
	s.Create(Job{Name: "j1", Cron: "* * * * *", Action: ActionWebhook, Enabled: true})
	s.Create(Job{Name: "j2", Cron: "* * * * *", Action: ActionWebhook, Enabled: true}) // second save rotates primary->backup

	// Ensure a valid backup exists by copying the current good primary.
	good, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read primary: %v", err)
	}
	if err := os.WriteFile(path+".backup", good, 0600); err != nil {
		t.Fatalf("seed backup: %v", err)
	}

	// Corrupt the primary.
	if err := os.WriteFile(path, []byte("{not valid json"), 0600); err != nil {
		t.Fatalf("corrupt primary: %v", err)
	}

	// Reopen: load() should detect corruption, preserve a .corrupt copy, and
	// restore from the backup.
	s2 := NewStore(path)
	if len(s2.List()) != 2 {
		t.Fatalf("expected 2 jobs restored from backup, got %d", len(s2.List()))
	}

	// A .corrupt-* file should have been preserved.
	entries, _ := os.ReadDir(dir)
	foundCorrupt := false
	for _, e := range entries {
		if len(e.Name()) > len("sched.json.corrupt-") && e.Name()[:len("sched.json.corrupt-")] == "sched.json.corrupt-" {
			foundCorrupt = true
		}
	}
	if !foundCorrupt {
		t.Fatal("expected a preserved .corrupt-* file")
	}
}

func TestStore_Load_NoPrimaryNoBackup_StartsBlank(t *testing.T) {
	// Path that doesn't exist; load() should silently start empty.
	s := NewStore(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if len(s.List()) != 0 {
		t.Fatalf("expected blank store, got %d", len(s.List()))
	}
}

func TestStore_Load_PrimaryMissingButBackupExists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sched.json")

	// Write only a backup (primary absent).
	jobs := []*Job{{ID: "b1", Name: "backed", Cron: "* * * * *", Action: ActionWebhook, Enabled: true}}
	data, _ := json.MarshalIndent(jobs, "", "  ")
	if err := os.WriteFile(path+".backup", data, 0600); err != nil {
		t.Fatalf("seed backup: %v", err)
	}

	s := NewStore(path)
	if got := s.Get("b1"); got == nil || got.Name != "backed" {
		t.Fatalf("expected job loaded from backup, got %+v", got)
	}
}

func TestStore_Load_CorruptPrimaryAndCorruptBackup_StartsBlank(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sched.json")
	if err := os.WriteFile(path, []byte("garbage"), 0600); err != nil {
		t.Fatalf("seed corrupt primary: %v", err)
	}
	if err := os.WriteFile(path+".backup", []byte("also garbage"), 0600); err != nil {
		t.Fatalf("seed corrupt backup: %v", err)
	}
	s := NewStore(path)
	if len(s.List()) != 0 {
		t.Fatalf("expected blank store when both files corrupt, got %d", len(s.List()))
	}
}

func TestStore_Load_CorruptPrimaryNoBackup_StartsBlank(t *testing.T) {
	// Corrupt primary with NO backup file present: load() preserves a .corrupt
	// copy then hits the "no backup -> return blank" branch (store.go:177).
	dir := t.TempDir()
	path := filepath.Join(dir, "sched.json")
	if err := os.WriteFile(path, []byte("{not json"), 0600); err != nil {
		t.Fatalf("seed corrupt primary: %v", err)
	}
	s := NewStore(path)
	if len(s.List()) != 0 {
		t.Fatalf("expected blank store with corrupt primary and no backup, got %d", len(s.List()))
	}
	// The corrupt primary should have been renamed to a .corrupt-* file.
	entries, _ := os.ReadDir(dir)
	foundCorrupt := false
	for _, e := range entries {
		if len(e.Name()) > len("sched.json.corrupt-") && e.Name()[:len("sched.json.corrupt-")] == "sched.json.corrupt-" {
			foundCorrupt = true
		}
	}
	if !foundCorrupt {
		t.Fatal("expected preserved .corrupt-* file when no backup exists")
	}
}

func TestNewStore_MkdirFails_StillUsable(t *testing.T) {
	// Place a regular file where the parent directory should be, so MkdirAll
	// fails (NewStore logs the error and proceeds with an empty in-memory store).
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatalf("seed blocker file: %v", err)
	}
	// parent path is the file itself -> MkdirAll must fail.
	path := filepath.Join(blocker, "nested", "sched.json")
	s := NewStore(path)
	if s == nil {
		t.Fatal("NewStore returned nil even though mkdir failed")
	}
	if len(s.List()) != 0 {
		t.Fatalf("expected empty store when mkdir fails, got %d", len(s.List()))
	}
}

func TestStore_Save_RotatesBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sched.json")
	s := NewStore(path)
	s.Create(Job{Name: "first", Cron: "* * * * *", Action: ActionWebhook, Enabled: true})
	// Second save: primary exists, so it should rotate to .backup.
	s.Create(Job{Name: "second", Cron: "* * * * *", Action: ActionWebhook, Enabled: true})

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected primary to exist: %v", err)
	}
	if _, err := os.Stat(path + ".backup"); err != nil {
		t.Fatalf("expected backup to exist after second save: %v", err)
	}
	// Tmp file should be cleaned up (renamed away).
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("expected no leftover .tmp file, stat err=%v", err)
	}
}
