package projects

import "testing"

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

func TestDelete(t *testing.T) {
	svc := New(newFakeStore())
	p := svc.Upsert(Project{Kind: KindLocal, Path: "/p/a", LastUsedAt: "2026-05-31T10:00:00Z"})
	svc.Delete(p.ID)
	if len(svc.List()) != 0 {
		t.Fatal("expected project removed")
	}
	svc.Delete("nonexistent") // no panic / no-op
}
