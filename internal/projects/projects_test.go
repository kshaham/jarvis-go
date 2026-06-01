package projects

import (
	"encoding/json"
	"testing"
)

// fakeStore is an in-memory SettingsStore for tests.
type fakeStore struct{ m map[string]interface{} }

func newFakeStore() *fakeStore { return &fakeStore{m: map[string]interface{}{}} }
func (f *fakeStore) GetSettings() map[string]interface{} {
	out := make(map[string]interface{}, len(f.m))
	for k, v := range f.m {
		out[k] = v
	}
	return out
}
func (f *fakeStore) UpdateSettings(updates map[string]interface{}) {
	for k, v := range updates {
		f.m[k] = v
	}
}

func TestListEmpty(t *testing.T) {
	svc := New(newFakeStore())
	if got := svc.List(); len(got) != 0 {
		t.Fatalf("expected empty list, got %d", len(got))
	}
}

func TestUpsertLocalDerivesNameAndDedups(t *testing.T) {
	svc := New(newFakeStore())
	a := svc.Upsert(Project{Kind: KindLocal, Path: "/Users/x/jarvis-go", LastUsedAt: "2026-05-31T10:00:00Z"})
	if a.Name != "jarvis-go" {
		t.Fatalf("expected derived name jarvis-go, got %q", a.Name)
	}
	if a.ID == "" || a.CreatedAt == "" {
		t.Fatal("expected id and createdAt to be assigned")
	}
	// Same path again with a newer timestamp must update, not duplicate.
	b := svc.Upsert(Project{Kind: KindLocal, Path: "/Users/x/jarvis-go", LastUsedAt: "2026-05-31T11:00:00Z"})
	if b.ID != a.ID {
		t.Fatalf("expected same id on re-upsert, got %q vs %q", b.ID, a.ID)
	}
	if got := svc.List(); len(got) != 1 {
		t.Fatalf("expected 1 project after dedup, got %d", len(got))
	}
	if svc.List()[0].LastUsedAt != "2026-05-31T11:00:00Z" {
		t.Fatal("expected lastUsedAt to advance on re-upsert")
	}
}

func TestUpsertGithubDedupsByRepoURL(t *testing.T) {
	svc := New(newFakeStore())
	a := svc.Upsert(Project{Kind: KindGitHub, RepoURL: "https://github.com/o/r", Name: "o/r", LastUsedAt: "2026-05-31T10:00:00Z"})
	b := svc.Upsert(Project{Kind: KindGitHub, RepoURL: "https://github.com/o/r", Path: "/app/workspace/o-r", LastUsedAt: "2026-05-31T11:00:00Z"})
	if a.ID != b.ID {
		t.Fatal("expected github dedup by repoUrl")
	}
	if svc.List()[0].Path != "/app/workspace/o-r" {
		t.Fatal("expected path to be written back on re-upsert")
	}
}

func TestUpsertCapEvictsOldest(t *testing.T) {
	svc := New(newFakeStore())
	for i := 0; i < maxProjects+5; i++ {
		ts := "2026-05-31T10:00:" + twoDigit(i) + "Z"
		svc.Upsert(Project{Kind: KindLocal, Path: "/p/" + twoDigit(i), LastUsedAt: ts})
	}
	if got := len(svc.List()); got != maxProjects {
		t.Fatalf("expected cap %d, got %d", maxProjects, got)
	}
	// Oldest ("/p/00") must have been evicted.
	for _, p := range svc.List() {
		if p.Path == "/p/00" {
			t.Fatal("expected oldest project to be evicted")
		}
	}
}

func twoDigit(i int) string {
	if i < 10 {
		return "0" + string(rune('0'+i))
	}
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}

// TestLoadInterfaceRoundTrip mimics the on-disk path: the settings value is
// the []interface{} form that survives a JSON round-trip through the state
// file, not a native []Project. load must still decode it.
func TestLoadInterfaceRoundTrip(t *testing.T) {
	store := newFakeStore()
	orig := []Project{{
		ID: "p1", Name: "repo", Kind: KindLocal, Path: "/x/repo",
		CreatedAt: "2026-05-31T10:00:00Z", LastUsedAt: "2026-05-31T10:00:00Z",
	}}
	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	var generic interface{}
	if err := json.Unmarshal(b, &generic); err != nil {
		t.Fatal(err)
	}
	store.m[settingsKey] = generic // []interface{} of map[string]interface{}

	got := New(store).List()
	if len(got) != 1 || got[0].ID != "p1" || got[0].Path != "/x/repo" {
		t.Fatalf("expected round-trip decode, got %#v", got)
	}
}

// TestLoadMalformedValue covers the json.Unmarshal error path: a value that is
// not an array under the key yields an empty list rather than a panic.
func TestLoadMalformedValue(t *testing.T) {
	store := newFakeStore()
	store.m[settingsKey] = "not-an-array"
	if got := New(store).List(); len(got) != 0 {
		t.Fatalf("expected empty list for malformed value, got %d", len(got))
	}
}

// TestSortTieBreaks exercises the createdAt and id tiebreaks of sortProjects:
// entries that tie on lastUsedAt fall back to createdAt, then to id.
func TestSortTieBreaks(t *testing.T) {
	ps := []Project{
		{ID: "a", LastUsedAt: "t1", CreatedAt: "c1"},
		{ID: "b", LastUsedAt: "t1", CreatedAt: "c2"}, // newer createdAt -> first
		{ID: "c", LastUsedAt: "t1", CreatedAt: "c2"}, // ties b on both -> id "c" > "b" first
	}
	sortProjects(ps)
	// c2 createdAt entries come before c1; among c2, higher id ("c") first.
	if ps[0].ID != "c" || ps[1].ID != "b" || ps[2].ID != "a" {
		t.Fatalf("unexpected order: %s,%s,%s", ps[0].ID, ps[1].ID, ps[2].ID)
	}
}

// TestUpsertGithubBlankPathKeepsExisting covers the branch where a re-upsert
// with an empty Path must not blank out the known clone path.
func TestUpsertGithubBlankPathKeepsExisting(t *testing.T) {
	svc := New(newFakeStore())
	svc.Upsert(Project{Kind: KindGitHub, RepoURL: "https://github.com/o/r", Path: "/app/o-r", LastUsedAt: "2026-05-31T10:00:00Z"})
	// Re-upsert with no Path: the stored record must retain /app/o-r.
	got := svc.Upsert(Project{Kind: KindGitHub, RepoURL: "https://github.com/o/r", LastUsedAt: "2026-05-31T11:00:00Z"})
	if got.Path != "/app/o-r" {
		t.Fatalf("expected existing path retained, got %q", got.Path)
	}
}

func TestDelete(t *testing.T) {
	svc := New(newFakeStore())
	p := svc.Upsert(Project{Kind: KindLocal, Path: "/p/a", LastUsedAt: "2026-05-31T10:00:00Z"})
	svc.Delete(p.ID)
	if len(svc.List()) != 0 {
		t.Fatal("expected project removed")
	}
	svc.Delete("nonexistent") // no panic / no-op
}

// TestDeleteKeepsOthers covers the keep branch of Delete's filter loop: a
// project whose id does not match must survive.
func TestDeleteKeepsOthers(t *testing.T) {
	svc := New(newFakeStore())
	a := svc.Upsert(Project{Kind: KindLocal, Path: "/p/a", LastUsedAt: "2026-05-31T10:00:00Z"})
	svc.Upsert(Project{Kind: KindLocal, Path: "/p/b", LastUsedAt: "2026-05-31T10:00:01Z"})
	svc.Delete(a.ID)
	got := svc.List()
	if len(got) != 1 || got[0].Path != "/p/b" {
		t.Fatalf("expected only /p/b to remain, got %#v", got)
	}
}

// TestUpsertEmptyKindDefaultsLocal covers the kind-defaulting branch.
func TestUpsertEmptyKindDefaultsLocal(t *testing.T) {
	svc := New(newFakeStore())
	got := svc.Upsert(Project{Path: "/p/x", LastUsedAt: "2026-05-31T10:00:00Z"})
	if got.Kind != KindLocal {
		t.Fatalf("expected empty kind to default to local, got %q", got.Kind)
	}
}

// TestIndexByIDMissing covers the defensive fallthrough return.
func TestIndexByIDMissing(t *testing.T) {
	list := []Project{{ID: "a"}, {ID: "b"}}
	if got := indexByID(list, "missing"); got != 0 {
		t.Fatalf("expected 0 for missing id, got %d", got)
	}
	if got := indexByID(list, "b"); got != 1 {
		t.Fatalf("expected index 1 for b, got %d", got)
	}
}
