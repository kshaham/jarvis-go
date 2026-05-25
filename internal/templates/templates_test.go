package templates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewStore_CreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "tmpls")
	s := NewStore(dir)
	if s == nil {
		t.Fatal("nil store")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("dir not created: %v", err)
	}
}

func TestCreate_AssignsID(t *testing.T) {
	s := NewStore(t.TempDir())
	tpl := s.Create("Test", "desc", "Hello, {{name}}")
	if tpl.ID == "" {
		t.Fatal("empty ID")
	}
	if tpl.Name != "Test" {
		t.Errorf("Name=%q", tpl.Name)
	}
	if tpl.Content != "Hello, {{name}}" {
		t.Errorf("Content=%q", tpl.Content)
	}
	if tpl.CreatedAt.IsZero() || tpl.UpdatedAt.IsZero() {
		t.Error("timestamps not set")
	}
}

func TestGet_ReturnsCreated(t *testing.T) {
	s := NewStore(t.TempDir())
	tpl := s.Create("x", "y", "z")
	got := s.Get(tpl.ID)
	if got == nil || got.ID != tpl.ID {
		t.Errorf("Get returned %+v", got)
	}
}

func TestGet_ReturnsNilForMissing(t *testing.T) {
	s := NewStore(t.TempDir())
	if got := s.Get("ghost"); got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestUpdate_FieldsAndTimestamp(t *testing.T) {
	s := NewStore(t.TempDir())
	tpl := s.Create("Old", "OldDesc", "OldContent")
	created := tpl.UpdatedAt
	updated := s.Update(tpl.ID, map[string]string{
		"name":        "New",
		"description": "NewDesc",
		"content":     "NewContent",
	})
	if updated == nil {
		t.Fatal("Update returned nil")
	}
	if updated.Name != "New" || updated.Description != "NewDesc" || updated.Content != "NewContent" {
		t.Errorf("update missed: %+v", updated)
	}
	if !updated.UpdatedAt.After(created) {
		t.Errorf("UpdatedAt not bumped (%v then %v)", created, updated.UpdatedAt)
	}
}

func TestUpdate_PartialOnlyAffectsProvidedKeys(t *testing.T) {
	s := NewStore(t.TempDir())
	tpl := s.Create("Old", "OldDesc", "OldContent")
	updated := s.Update(tpl.ID, map[string]string{"name": "NewOnly"})
	if updated.Name != "NewOnly" {
		t.Errorf("Name=%q", updated.Name)
	}
	if updated.Description != "OldDesc" {
		t.Errorf("Description changed unexpectedly: %q", updated.Description)
	}
	if updated.Content != "OldContent" {
		t.Errorf("Content changed unexpectedly: %q", updated.Content)
	}
}

func TestUpdate_ReturnsNilForMissing(t *testing.T) {
	s := NewStore(t.TempDir())
	if got := s.Update("ghost", map[string]string{"name": "x"}); got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestDelete_HappyAndMissing(t *testing.T) {
	s := NewStore(t.TempDir())
	tpl := s.Create("X", "", "")
	if !s.Delete(tpl.ID) {
		t.Error("Delete returned false for existing template")
	}
	if s.Delete(tpl.ID) {
		t.Error("second Delete returned true")
	}
	if s.Get(tpl.ID) != nil {
		t.Error("Get after Delete returned non-nil")
	}
}

func TestDuplicate_AppendsCopyAndNewID(t *testing.T) {
	s := NewStore(t.TempDir())
	src := s.Create("Source", "d", "c")
	dup := s.Duplicate(src.ID)
	if dup == nil {
		t.Fatal("Duplicate returned nil")
	}
	if dup.ID == src.ID {
		t.Error("Duplicate kept the same ID")
	}
	if !strings.HasSuffix(dup.Name, " (copy)") {
		t.Errorf("Name=%q does not end in ' (copy)'", dup.Name)
	}
	if dup.Content != src.Content {
		t.Errorf("Content not copied: %q vs %q", dup.Content, src.Content)
	}
}

func TestList_SortedByName(t *testing.T) {
	s := NewStore(t.TempDir())
	for _, n := range []string{"charlie", "alpha", "bravo"} {
		s.Create(n, "", "")
	}
	got := s.List()
	want := []string{"alpha", "bravo", "charlie"}
	if len(got) != len(want) {
		t.Fatalf("len=%d", len(got))
	}
	for i, w := range want {
		if got[i].Name != w {
			t.Errorf("[%d] %q want %q", i, got[i].Name, w)
		}
	}
}

func TestStore_PersistsAcrossReload(t *testing.T) {
	dir := t.TempDir()
	s1 := NewStore(dir)
	tpl := s1.Create("Persisted", "d", "c")

	s2 := NewStore(dir)
	got := s2.Get(tpl.ID)
	if got == nil {
		t.Fatal("template not persisted across reload")
	}
	if got.Name != "Persisted" {
		t.Errorf("Name=%q", got.Name)
	}
}
