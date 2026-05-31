package peers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestPeer(id string) Peer {
	return Peer{
		PeerID:    id,
		PublicURL: "https://" + id + ".test",
		PairToken: "token-" + id,
	}
}

func TestRegistry_AddAndGet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peers", "peers.json")
	reg, err := NewRegistry(path)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	p := newTestPeer("alice")
	if err := reg.Add(p); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got, ok := reg.Get("alice")
	if !ok {
		t.Fatal("Get returned not-found for added peer")
	}
	if got.PeerID != "alice" || got.PublicURL != p.PublicURL || got.PairToken != p.PairToken {
		t.Fatalf("Get returned wrong peer: %+v", got)
	}
	// Add fills defaults.
	if got.Status != PeerStatusUnknown {
		t.Fatalf("expected default status unknown, got %q", got.Status)
	}
	if got.AddedAt.IsZero() {
		t.Fatal("expected AddedAt to be defaulted")
	}
}

func TestRegistry_AddValidation(t *testing.T) {
	reg, err := NewRegistry(filepath.Join(t.TempDir(), "peers.json"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		peer Peer
		want string
	}{
		{"missing peerID", Peer{PublicURL: "u", PairToken: "t"}, "peerID is required"},
		{"missing publicURL", Peer{PeerID: "x", PairToken: "t"}, "publicURL is required"},
		{"missing pairToken", Peer{PeerID: "x", PublicURL: "u"}, "pairToken is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := reg.Add(tc.peer)
			if err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}

func TestRegistry_AddDuplicateRejected(t *testing.T) {
	reg, err := NewRegistry(filepath.Join(t.TempDir(), "peers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Add(newTestPeer("dup")); err != nil {
		t.Fatalf("first Add: %v", err)
	}
	err = reg.Add(newTestPeer("dup"))
	if err == nil {
		t.Fatal("expected duplicate Add to error")
	}
	if !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("unexpected duplicate error: %v", err)
	}
}

func TestRegistry_AddPreservesProvidedDefaults(t *testing.T) {
	reg, err := NewRegistry(filepath.Join(t.TempDir(), "peers.json"))
	if err != nil {
		t.Fatal(err)
	}
	when := time.Date(2023, 5, 1, 0, 0, 0, 0, time.UTC)
	p := newTestPeer("withdefaults")
	p.AddedAt = when
	p.Status = PeerStatusOK
	if err := reg.Add(p); err != nil {
		t.Fatalf("Add: %v", err)
	}
	got, _ := reg.Get("withdefaults")
	if !got.AddedAt.Equal(when) {
		t.Fatalf("AddedAt was overwritten: %v", got.AddedAt)
	}
	if got.Status != PeerStatusOK {
		t.Fatalf("Status was overwritten: %q", got.Status)
	}
}

func TestRegistry_GetMissing(t *testing.T) {
	reg, err := NewRegistry(filepath.Join(t.TempDir(), "peers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Get("nope"); ok {
		t.Fatal("expected not-found for missing peer")
	}
}

func TestRegistry_FindByToken(t *testing.T) {
	reg, err := NewRegistry(filepath.Join(t.TempDir(), "peers.json"))
	if err != nil {
		t.Fatal(err)
	}
	p := newTestPeer("bob")
	p.PairToken = "secret-bob-token"
	if err := reg.Add(p); err != nil {
		t.Fatal(err)
	}

	got, ok := reg.FindByToken("secret-bob-token")
	if !ok {
		t.Fatal("FindByToken did not find the registered token")
	}
	if got.PeerID != "bob" {
		t.Fatalf("FindByToken returned wrong peer: %+v", got)
	}

	if _, ok := reg.FindByToken("wrong-token"); ok {
		t.Fatal("FindByToken matched a wrong token")
	}
	if _, ok := reg.FindByToken(""); ok {
		t.Fatal("FindByToken matched the empty token")
	}
}

func TestRegistry_Update(t *testing.T) {
	reg, err := NewRegistry(filepath.Join(t.TempDir(), "peers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Add(newTestPeer("carol")); err != nil {
		t.Fatal(err)
	}
	err = reg.Update("carol", func(p *Peer) {
		p.DisplayName = "Carol Display"
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _ := reg.Get("carol")
	if got.DisplayName != "Carol Display" {
		t.Fatalf("Update did not apply: %+v", got)
	}
}

func TestRegistry_UpdateMissing(t *testing.T) {
	reg, err := NewRegistry(filepath.Join(t.TempDir(), "peers.json"))
	if err != nil {
		t.Fatal(err)
	}
	err = reg.Update("ghost", func(p *Peer) {})
	if err == nil {
		t.Fatal("expected error updating missing peer")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRegistry_MarkStatusOK(t *testing.T) {
	reg, err := NewRegistry(filepath.Join(t.TempDir(), "peers.json"))
	if err != nil {
		t.Fatal(err)
	}
	p := newTestPeer("dave")
	p.LastError = "previous error"
	if err := reg.Add(p); err != nil {
		t.Fatal(err)
	}
	reg.MarkStatus("dave", PeerStatusOK, "ignored")
	got, _ := reg.Get("dave")
	if got.Status != PeerStatusOK {
		t.Fatalf("status not OK: %q", got.Status)
	}
	if got.LastError != "" {
		t.Fatalf("LastError should be cleared on OK, got %q", got.LastError)
	}
	if got.LastSeenAt == nil {
		t.Fatal("LastSeenAt should be set on OK")
	}
}

func TestRegistry_MarkStatusError(t *testing.T) {
	reg, err := NewRegistry(filepath.Join(t.TempDir(), "peers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Add(newTestPeer("erin")); err != nil {
		t.Fatal(err)
	}
	reg.MarkStatus("erin", PeerStatusUnreachable, "connection refused")
	got, _ := reg.Get("erin")
	if got.Status != PeerStatusUnreachable {
		t.Fatalf("status mismatch: %q", got.Status)
	}
	if got.LastError != "connection refused" {
		t.Fatalf("LastError not set: %q", got.LastError)
	}
	if got.LastSeenAt != nil {
		t.Fatal("LastSeenAt should not be set on error")
	}
}

func TestRegistry_MarkStatusTruncatesError(t *testing.T) {
	reg, err := NewRegistry(filepath.Join(t.TempDir(), "peers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Add(newTestPeer("frank")); err != nil {
		t.Fatal(err)
	}
	longErr := strings.Repeat("x", 500)
	reg.MarkStatus("frank", PeerStatusUnauthorized, longErr)
	got, _ := reg.Get("frank")
	if len(got.LastError) != 200 {
		t.Fatalf("expected LastError truncated to 200, got %d", len(got.LastError))
	}
}

func TestRegistry_MarkStatusMissingNoPanic(t *testing.T) {
	reg, err := NewRegistry(filepath.Join(t.TempDir(), "peers.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Should be a no-op (Update returns error, swallowed) — must not panic.
	reg.MarkStatus("nonexistent", PeerStatusOK, "")
}

func TestRegistry_Remove(t *testing.T) {
	reg, err := NewRegistry(filepath.Join(t.TempDir(), "peers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Add(newTestPeer("grace")); err != nil {
		t.Fatal(err)
	}
	if err := reg.Remove("grace"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, ok := reg.Get("grace"); ok {
		t.Fatal("peer still present after Remove")
	}
	// Idempotent: removing a missing peer is fine.
	if err := reg.Remove("grace"); err != nil {
		t.Fatalf("idempotent Remove errored: %v", err)
	}
}

func TestRegistry_ListSortedByAddedAt(t *testing.T) {
	reg, err := NewRegistry(filepath.Join(t.TempDir(), "peers.json"))
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	// Insert out of chronological order.
	p3 := newTestPeer("c")
	p3.AddedAt = base.Add(3 * time.Hour)
	p1 := newTestPeer("a")
	p1.AddedAt = base.Add(1 * time.Hour)
	p2 := newTestPeer("b")
	p2.AddedAt = base.Add(2 * time.Hour)
	for _, p := range []Peer{p3, p1, p2} {
		if err := reg.Add(p); err != nil {
			t.Fatal(err)
		}
	}
	list := reg.List()
	if len(list) != 3 {
		t.Fatalf("expected 3 peers, got %d", len(list))
	}
	if list[0].PeerID != "a" || list[1].PeerID != "b" || list[2].PeerID != "c" {
		t.Fatalf("list not sorted by AddedAt: %v", []string{list[0].PeerID, list[1].PeerID, list[2].PeerID})
	}
}

func TestRegistry_ListTieBreakByPeerID(t *testing.T) {
	reg, err := NewRegistry(filepath.Join(t.TempDir(), "peers.json"))
	if err != nil {
		t.Fatal(err)
	}
	same := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	for _, id := range []string{"zeta", "alpha", "mid"} {
		p := newTestPeer(id)
		p.AddedAt = same
		if err := reg.Add(p); err != nil {
			t.Fatal(err)
		}
	}
	list := reg.List()
	if list[0].PeerID != "alpha" || list[1].PeerID != "mid" || list[2].PeerID != "zeta" {
		t.Fatalf("tie-break not by PeerID: %v", []string{list[0].PeerID, list[1].PeerID, list[2].PeerID})
	}
}

func TestRegistry_ListEmpty(t *testing.T) {
	reg, err := NewRegistry(filepath.Join(t.TempDir(), "peers.json"))
	if err != nil {
		t.Fatal(err)
	}
	list := reg.List()
	if len(list) != 0 {
		t.Fatalf("expected empty list, got %d", len(list))
	}
}

func TestRegistry_PersistAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peers", "peers.json")
	reg, err := NewRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Add(newTestPeer("persisted")); err != nil {
		t.Fatal(err)
	}

	// File should exist and contain valid JSON.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("peers file not written: %v", err)
	}
	var list []Peer
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatalf("invalid peers JSON: %v", err)
	}

	// Reload into a fresh registry.
	reg2, err := NewRegistry(path)
	if err != nil {
		t.Fatalf("reload NewRegistry: %v", err)
	}
	got, ok := reg2.Get("persisted")
	if !ok {
		t.Fatal("reloaded registry missing peer")
	}
	if got.PeerID != "persisted" {
		t.Fatalf("reloaded wrong peer: %+v", got)
	}
}

func TestRegistry_LoadEmptyPath(t *testing.T) {
	// Empty path is degraded in-memory mode; load is a no-op.
	reg, err := NewRegistry("")
	if err != nil {
		t.Fatalf("NewRegistry empty path: %v", err)
	}
	if err := reg.Add(newTestPeer("inmem")); err != nil {
		t.Fatalf("Add in degraded mode: %v", err)
	}
	if _, ok := reg.Get("inmem"); !ok {
		t.Fatal("degraded-mode peer not retrievable")
	}
}

func TestRegistry_LoadCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peers.json")
	if err := os.WriteFile(path, []byte("[not valid"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRegistry(path); err == nil {
		t.Fatal("expected error loading corrupt peers file")
	}
}

func TestRegistry_LoadSkipsBlankAndDefaultsStatus(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peers.json")
	// One valid peer with no status, one with empty PeerID (skipped),
	// and one null entry (skipped).
	raw := `[
		{"peerId":"valid","publicUrl":"https://v.test","pairToken":"t1"},
		{"peerId":"","publicUrl":"https://blank.test","pairToken":"t2"},
		null
	]`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := NewRegistry(path)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	list := reg.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 peer after skipping invalid entries, got %d", len(list))
	}
	if list[0].Status != PeerStatusUnknown {
		t.Fatalf("expected defaulted status unknown, got %q", list[0].Status)
	}
}

func TestRegistry_GetReturnsCopy(t *testing.T) {
	reg, err := NewRegistry(filepath.Join(t.TempDir(), "peers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Add(newTestPeer("copyme")); err != nil {
		t.Fatal(err)
	}
	got, _ := reg.Get("copyme")
	got.DisplayName = "mutated locally"
	// The internal copy must be unaffected.
	again, _ := reg.Get("copyme")
	if again.DisplayName == "mutated locally" {
		t.Fatal("Get returned a shared pointer; mutation leaked into registry")
	}
}

func TestGeneratePairToken(t *testing.T) {
	tok, err := GeneratePairToken()
	if err != nil {
		t.Fatalf("GeneratePairToken: %v", err)
	}
	// 32 bytes hex-encoded = 64 hex chars.
	if len(tok) != 64 {
		t.Fatalf("expected 64-char hex token, got %d (%q)", len(tok), tok)
	}
	for _, c := range tok {
		if !strings.ContainsRune("0123456789abcdef", c) {
			t.Fatalf("token contains non-hex char %q", c)
		}
	}
	// Tokens must be unique across calls.
	tok2, _ := GeneratePairToken()
	if tok == tok2 {
		t.Fatal("two GeneratePairToken calls returned the same token")
	}
}

func TestRegistry_ConcurrentAddGetRemove(t *testing.T) {
	reg, err := NewRegistry(filepath.Join(t.TempDir(), "peers.json"))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	const n = 20
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			id := "peer-" + string(rune('a'+idx%26)) + string(rune('0'+idx/26))
			_ = reg.Add(newTestPeer(id))
			_, _ = reg.Get(id)
			reg.MarkStatus(id, PeerStatusOK, "")
			_ = reg.List()
		}(i)
	}
	wg.Wait()
	// Sanity: at least the list call works after the storm.
	_ = reg.List()
}

func TestDefaultRegistryPath(t *testing.T) {
	got := DefaultRegistryPath()
	if got != "" {
		if filepath.Base(got) != "peers.json" {
			t.Fatalf("unexpected default registry path: %q", got)
		}
	}
}
