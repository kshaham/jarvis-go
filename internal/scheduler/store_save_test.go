package scheduler

import (
	"os"
	"path/filepath"
	"testing"
)

// TestStore_Save_WriteFails exercises the early-return branch where staging the
// tmp file fails (directory not writable). The in-memory mutation still
// succeeds; only the on-disk persistence is skipped.
func TestStore_Save_WriteFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sched.json")
	s := NewStore(path)
	// First create writes the primary successfully.
	created, err := s.Create(Job{Name: "j", Cron: "* * * * *", Action: ActionWebhook, Enabled: true})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Make the directory read-only so the next save's WriteFile(tmp) fails.
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatalf("chmod dir: %v", err)
	}
	// Restore perms so t.TempDir cleanup can remove the directory.
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })

	// This save hits the WriteFile-failure branch and returns early, but the
	// in-memory store is still updated.
	updated, err := s.Update(created.ID, Job{Name: "renamed", Cron: "* * * * *", Action: ActionWebhook, Enabled: true})
	if err != nil {
		t.Fatalf("update should still succeed in memory: %v", err)
	}
	if updated.Name != "renamed" {
		t.Fatalf("expected in-memory update to apply, got %q", updated.Name)
	}
}

// TestStore_Save_RenameFails exercises the branch where the final
// os.Rename(tmp, primary) fails and the store attempts to restore the primary
// from .backup. We force the rename failure by making the primary path a
// directory, which os.Rename(file -> dir) cannot overwrite.
func TestStore_Save_RenameFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sched.json")

	s := NewStore(path)
	// Two creates: the second rotates the primary file into .backup, so a
	// backup exists for the restore path.
	if _, err := s.Create(Job{Name: "a", Cron: "* * * * *", Action: ActionWebhook, Enabled: true}); err != nil {
		t.Fatalf("create a: %v", err)
	}
	if _, err := s.Create(Job{Name: "b", Cron: "* * * * *", Action: ActionWebhook, Enabled: true}); err != nil {
		t.Fatalf("create b: %v", err)
	}

	// Ensure a backup exists (copy current primary to .backup defensively).
	if good, rerr := os.ReadFile(path); rerr == nil {
		_ = os.WriteFile(path+".backup", good, 0600)
	}

	// Replace the primary file with a directory of the same name so the final
	// rename (tmp -> primary) fails.
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove primary: %v", err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatalf("mkdir over primary path: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(path) })

	// This save: writes tmp OK, rotates primary(dir) -> .backup may or may not
	// succeed, then rename(tmp -> primary-dir) fails, triggering the restore
	// branch that copies .backup back. Must not panic.
	if _, err := s.Create(Job{Name: "c", Cron: "* * * * *", Action: ActionWebhook, Enabled: true}); err != nil {
		t.Fatalf("create c should still succeed in memory: %v", err)
	}
	// The in-memory store should hold all three jobs regardless of disk outcome.
	if len(s.List()) != 3 {
		t.Fatalf("expected 3 jobs in memory, got %d", len(s.List()))
	}
}

// TestNewStore_MkdirAllOnNestedPath verifies NewStore creates missing parent
// directories (the MkdirAll branch).
func TestNewStore_MkdirAllOnNestedPath(t *testing.T) {
	nested := filepath.Join(t.TempDir(), "a", "b", "c", "sched.json")
	s := NewStore(nested)
	if _, err := s.Create(Job{Name: "deep", Cron: "* * * * *", Action: ActionWebhook, Enabled: true}); err != nil {
		t.Fatalf("create in nested dir: %v", err)
	}
	if _, err := os.Stat(nested); err != nil {
		t.Fatalf("expected file written to nested path: %v", err)
	}
}
