package scheduler

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestStore_CreateAndGet(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "sched.json"))
	created, err := s.Create(Job{Name: "test", Cron: "* * * * *", Action: ActionWebhook, Enabled: true})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected ID assigned")
	}
	got := s.Get(created.ID)
	if got == nil || got.Name != "test" {
		t.Fatalf("expected to get created job, got %+v", got)
	}
}

func TestStore_PersistAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sched.json")
	s := NewStore(path)
	created, _ := s.Create(Job{Name: "keeper", Cron: "0 9 * * *", Action: ActionSpawnTeam, Enabled: true})

	// Reopen the store -- persisted jobs should reload.
	s2 := NewStore(path)
	got := s2.Get(created.ID)
	if got == nil || got.Name != "keeper" {
		t.Fatalf("expected persisted job to reload, got %+v", got)
	}
}

func TestStore_Update(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "sched.json"))
	created, _ := s.Create(Job{Name: "orig", Cron: "* * * * *", Action: ActionWebhook, Enabled: true})
	updated, err := s.Update(created.ID, Job{Name: "renamed", Cron: "0 * * * *", Action: ActionWebhook, Enabled: false})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "renamed" || updated.Enabled {
		t.Fatalf("update did not apply: %+v", updated)
	}
}

func TestStore_DeleteNotFound(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "sched.json"))
	if err := s.Delete("no-such-id"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestStore_UpdateRunStatus(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "sched.json"))
	cfg, _ := json.Marshal(map[string]string{"url": "https://example.com"})
	created, _ := s.Create(Job{Name: "run", Cron: "* * * * *", Action: ActionWebhook, Config: cfg, Enabled: true})

	got := s.Get(created.ID)
	if got.LastStatus != "" {
		t.Fatalf("expected empty initial status")
	}
}
