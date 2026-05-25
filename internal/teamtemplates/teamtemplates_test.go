package teamtemplates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// BuiltInTemplates is the single source of truth for the team-spawn
// modal. If a template is missing required fields, the modal will
// render broken cards and the spawn flow will fail with cryptic
// errors. This smoke test asserts every builtin has the shape
// callers depend on.

func TestBuiltInTemplates_ShapeInvariants(t *testing.T) {
	tpls := BuiltInTemplates()
	if len(tpls) == 0 {
		t.Fatal("no builtin templates")
	}
	seenIDs := map[string]bool{}
	for _, tpl := range tpls {
		t.Run(tpl.ID, func(t *testing.T) {
			if tpl.ID == "" {
				t.Error("empty ID")
			}
			if seenIDs[tpl.ID] {
				t.Errorf("duplicate ID %q", tpl.ID)
			}
			seenIDs[tpl.ID] = true
			if tpl.Name == "" {
				t.Error("empty Name")
			}
			if tpl.Description == "" {
				t.Error("empty Description")
			}
			if !tpl.IsBuiltIn {
				t.Error("IsBuiltIn=false on builtin")
			}
			if tpl.Category == "" {
				t.Error("empty Category")
			}
			if len(tpl.Members) == 0 {
				t.Error("no members")
			}
			memberIDs := map[string]bool{}
			for i, m := range tpl.Members {
				if m.ID == "" {
					t.Errorf("member[%d] empty ID", i)
				}
				if memberIDs[m.ID] {
					t.Errorf("member[%d] duplicate ID %q within template", i, m.ID)
				}
				memberIDs[m.ID] = true
				if m.Name == "" {
					t.Errorf("member[%d] empty Name", i)
				}
				if m.Role == "" {
					t.Errorf("member[%d] (%s) empty Role", i, m.Name)
				}
				if m.Tier == "" {
					t.Errorf("member[%d] (%s) empty Tier", i, m.Name)
				}
			}
		})
	}
}

func TestBuiltInTemplates_AtLeastOnePerCategory(t *testing.T) {
	categories := map[string]int{}
	for _, tpl := range BuiltInTemplates() {
		categories[tpl.Category]++
	}
	// Pre-Round-13 there were 3 categories: fullstack, backend, frontend,
	// research. We don't pin the exact set (templates grow), just that
	// the canonical four still ship.
	for _, want := range []string{"fullstack", "backend", "frontend", "research"} {
		if categories[want] == 0 {
			t.Errorf("no builtin template with category %q", want)
		}
	}
}

func TestStore_NewStoreCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "templates")
	s := NewStore(dir)
	if s == nil {
		t.Fatal("nil store")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("dir not created: %v", err)
	}
}

func TestStore_CreateAssignsIDAndCategory(t *testing.T) {
	s := NewStore(t.TempDir())
	tpl := s.Create(TeamTemplate{
		Name: "Test team",
		Members: []TeamTemplateMember{
			{ID: "m1", Name: "Alice", Role: "frontend", Tier: "worker"},
		},
	})
	if tpl.ID == "" {
		t.Fatal("ID was not assigned")
	}
	if tpl.Category != "custom" {
		t.Errorf("Category=%q want %q", tpl.Category, "custom")
	}
	if tpl.IsBuiltIn {
		t.Error("custom template marked IsBuiltIn")
	}
	if tpl.CreatedAt.IsZero() || tpl.UpdatedAt.IsZero() {
		t.Error("timestamps not set")
	}
}

func TestStore_GetByID(t *testing.T) {
	s := NewStore(t.TempDir())
	tpl := s.Create(TeamTemplate{Name: "Foo", Members: []TeamTemplateMember{{ID: "a", Name: "A", Role: "r", Tier: "t"}}})
	got := s.Get(tpl.ID)
	if got == nil || got.ID != tpl.ID {
		t.Errorf("Get returned %+v", got)
	}
	if s.Get("nope-not-an-id") != nil {
		t.Error("Get returned non-nil for unknown ID")
	}
}

func TestStore_UpdateMutatesFieldsAndTimestamp(t *testing.T) {
	s := NewStore(t.TempDir())
	created := s.Create(TeamTemplate{Name: "Old", Members: []TeamTemplateMember{{ID: "a", Name: "A", Role: "r", Tier: "t"}}})
	createdTime := created.UpdatedAt
	updated := s.Update(created.ID, TeamTemplate{
		Name:        "New",
		Description: "Updated description",
		Members:     []TeamTemplateMember{{ID: "b", Name: "B", Role: "r2", Tier: "t2"}},
	})
	if updated == nil {
		t.Fatal("Update returned nil")
	}
	if updated.Name != "New" {
		t.Errorf("Name=%q", updated.Name)
	}
	if updated.Description != "Updated description" {
		t.Errorf("Description=%q", updated.Description)
	}
	if len(updated.Members) != 1 || updated.Members[0].ID != "b" {
		t.Errorf("Members not replaced: %+v", updated.Members)
	}
	if !updated.UpdatedAt.After(createdTime) {
		t.Errorf("UpdatedAt not bumped (was %v, now %v)", createdTime, updated.UpdatedAt)
	}
}

func TestStore_UpdateReturnsNilForMissing(t *testing.T) {
	s := NewStore(t.TempDir())
	if got := s.Update("ghost-id", TeamTemplate{Name: "x"}); got != nil {
		t.Errorf("expected nil for unknown ID, got %+v", got)
	}
}

func TestStore_Delete(t *testing.T) {
	s := NewStore(t.TempDir())
	tpl := s.Create(TeamTemplate{Name: "ToDelete", Members: []TeamTemplateMember{{ID: "a", Name: "A", Role: "r", Tier: "t"}}})
	if !s.Delete(tpl.ID) {
		t.Fatal("Delete returned false for existing ID")
	}
	if s.Get(tpl.ID) != nil {
		t.Error("Get returned non-nil after Delete")
	}
	if s.Delete(tpl.ID) {
		t.Error("Delete returned true for already-deleted ID")
	}
}

func TestStore_DuplicateAppendsCopyToName(t *testing.T) {
	s := NewStore(t.TempDir())
	src := s.Create(TeamTemplate{Name: "Source", Members: []TeamTemplateMember{{ID: "a", Name: "A", Role: "r", Tier: "t"}}})
	dup := s.Duplicate(src.ID, nil)
	if dup == nil {
		t.Fatal("Duplicate returned nil")
	}
	if !strings.HasSuffix(dup.Name, " (copy)") {
		t.Errorf("Name=%q does not end in ' (copy)'", dup.Name)
	}
	if dup.ID == src.ID {
		t.Error("Duplicate kept same ID as source")
	}
}

func TestStore_DuplicateFromBuiltIn(t *testing.T) {
	s := NewStore(t.TempDir())
	// Grab a real builtin and pass it as the fallback.
	builtin := &BuiltInTemplates()[0]
	dup := s.Duplicate("nonexistent-custom-id", builtin)
	if dup == nil {
		t.Fatal("Duplicate returned nil when builtin fallback provided")
	}
	if !strings.HasSuffix(dup.Name, " (copy)") {
		t.Errorf("Name=%q does not end in ' (copy)'", dup.Name)
	}
	if dup.Category != builtin.Category {
		t.Errorf("Category=%q want %q", dup.Category, builtin.Category)
	}
}

func TestStore_PersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	s1 := NewStore(dir)
	created := s1.Create(TeamTemplate{Name: "Persistent", Members: []TeamTemplateMember{{ID: "a", Name: "A", Role: "r", Tier: "t"}}})

	s2 := NewStore(dir) // fresh instance reads from disk
	got := s2.Get(created.ID)
	if got == nil {
		t.Fatal("template not persisted across Store restart")
	}
	if got.Name != "Persistent" {
		t.Errorf("Name=%q", got.Name)
	}
}
