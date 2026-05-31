package peers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestIdentityStore_GetGeneratesAndPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "identity.json")
	store := NewIdentityStore(path)

	id, err := store.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if id.PeerID == "" {
		t.Fatal("expected a generated PeerID, got empty")
	}
	if id.DisplayName == "" {
		t.Fatal("expected a default DisplayName, got empty")
	}
	if id.CreatedAt.IsZero() {
		t.Fatal("expected CreatedAt to be set")
	}

	// The file must have been persisted to disk.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("identity file not written: %v", err)
	}
	var onDisk LocalIdentity
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatalf("on-disk identity invalid JSON: %v", err)
	}
	if onDisk.PeerID != id.PeerID {
		t.Fatalf("on-disk PeerID %q != returned %q", onDisk.PeerID, id.PeerID)
	}
}

func TestIdentityStore_GetIsStableAcrossCalls(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.json")
	store := NewIdentityStore(path)

	first, err := store.Get()
	if err != nil {
		t.Fatalf("first Get: %v", err)
	}
	second, err := store.Get()
	if err != nil {
		t.Fatalf("second Get: %v", err)
	}
	if first.PeerID != second.PeerID {
		t.Fatalf("PeerID changed between calls: %q vs %q", first.PeerID, second.PeerID)
	}
}

func TestIdentityStore_GetLoadsExistingFromDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "identity.json")
	want := LocalIdentity{
		PeerID:      "fixed-peer-id",
		DisplayName: "Frontend",
		PublicURL:   "https://example.test",
		CreatedAt:   time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC),
	}
	data, _ := json.Marshal(want)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	store := NewIdentityStore(path)
	got, err := store.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != want {
		t.Fatalf("loaded identity mismatch:\n got=%+v\nwant=%+v", got, want)
	}
}

func TestIdentityStore_GetReturnsErrorOnCorruptJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewIdentityStore(path)
	if _, err := store.Get(); err == nil {
		t.Fatal("expected error for corrupt identity JSON, got nil")
	}
}

func TestIdentityStore_UpdatePartialFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.json")
	store := NewIdentityStore(path)
	orig, err := store.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	// Update only the display name; PublicURL should remain empty.
	updated, err := store.Update("New Name", "")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.DisplayName != "New Name" {
		t.Fatalf("DisplayName not updated: %q", updated.DisplayName)
	}
	if updated.PublicURL != "" {
		t.Fatalf("PublicURL should stay empty, got %q", updated.PublicURL)
	}
	if updated.PeerID != orig.PeerID {
		t.Fatalf("PeerID must never change on Update: %q vs %q", updated.PeerID, orig.PeerID)
	}

	// Now update only the URL; the previously-set name must persist.
	updated2, err := store.Update("", "https://peer.test")
	if err != nil {
		t.Fatalf("Update2: %v", err)
	}
	if updated2.DisplayName != "New Name" {
		t.Fatalf("DisplayName should be preserved, got %q", updated2.DisplayName)
	}
	if updated2.PublicURL != "https://peer.test" {
		t.Fatalf("PublicURL not updated: %q", updated2.PublicURL)
	}

	// Empty/empty update leaves everything alone.
	updated3, err := store.Update("", "")
	if err != nil {
		t.Fatalf("Update3: %v", err)
	}
	if updated3.DisplayName != "New Name" || updated3.PublicURL != "https://peer.test" {
		t.Fatalf("no-op update altered fields: %+v", updated3)
	}
}

func TestIdentityStore_UpdatePersistsToDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.json")
	store := NewIdentityStore(path)
	if _, err := store.Update("Persisted", "https://p.test"); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// A fresh store reading the same file should see the update.
	reloaded := NewIdentityStore(path)
	got, err := reloaded.Get()
	if err != nil {
		t.Fatalf("reloaded Get: %v", err)
	}
	if got.DisplayName != "Persisted" || got.PublicURL != "https://p.test" {
		t.Fatalf("update not persisted: %+v", got)
	}
}

func TestIdentityStore_EmptyPathDegradedMode(t *testing.T) {
	store := NewIdentityStore("")
	id, err := store.Get()
	if err != nil {
		t.Fatalf("Get in degraded mode: %v", err)
	}
	if id.PeerID == "" {
		t.Fatal("degraded mode should still synthesize an identity")
	}
	// Update should be accepted but not persist (no file to read).
	updated, err := store.Update("Ephemeral", "")
	if err != nil {
		t.Fatalf("Update in degraded mode: %v", err)
	}
	if updated.DisplayName != "Ephemeral" {
		t.Fatalf("in-memory update lost: %+v", updated)
	}
}

func TestDefaultIdentityPath(t *testing.T) {
	got := DefaultIdentityPath()
	// In a normal test environment HOME resolves, so we expect a
	// .jarvis/identity.json suffix. If it can't resolve, "" is the
	// documented degraded return.
	if got != "" {
		if filepath.Base(got) != "identity.json" {
			t.Fatalf("unexpected default path: %q", got)
		}
		if filepath.Base(filepath.Dir(got)) != ".jarvis" {
			t.Fatalf("expected .jarvis dir in path: %q", got)
		}
	}
}

func TestIdentityStore_GetConcurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.json")
	store := NewIdentityStore(path)

	const n = 32
	var wg sync.WaitGroup
	ids := make([]string, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			id, err := store.Get()
			if err != nil {
				t.Errorf("concurrent Get: %v", err)
				return
			}
			ids[idx] = id.PeerID
		}(i)
	}
	wg.Wait()

	// All concurrent Gets must observe the SAME identity (no torn races).
	for i := 1; i < n; i++ {
		if ids[i] != ids[0] {
			t.Fatalf("concurrent Gets returned different PeerIDs: %q vs %q", ids[i], ids[0])
		}
	}
}

func TestIdentityStore_UpdateConcurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.json")
	store := NewIdentityStore(path)
	if _, err := store.Get(); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(16)
	for i := 0; i < 16; i++ {
		go func() {
			defer wg.Done()
			_, _ = store.Update("Racer", "https://r.test")
			_, _ = store.Get()
		}()
	}
	wg.Wait()

	got, err := store.Get()
	if err != nil {
		t.Fatalf("Get after concurrent updates: %v", err)
	}
	if got.DisplayName != "Racer" {
		t.Fatalf("expected Racer after updates, got %q", got.DisplayName)
	}
}
