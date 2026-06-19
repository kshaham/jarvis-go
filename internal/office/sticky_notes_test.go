package office

import (
	"os"
	"path/filepath"
	"testing"
)

// newStickyTestManager builds a bare Manager with an isolated HOME so
// sticky-notes.json writes land in a temp dir.
func newStickyTestManager(t *testing.T) *Manager {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	return NewManager(nil, func(string, interface{}) {}, nil)
}

func TestAddStickyNote_AppendsAssignsAndPersists(t *testing.T) {
	m := newStickyTestManager(t)

	note := m.AddStickyNote("Builder", "shipped the parser", "")
	if note.ID == "" {
		t.Fatal("expected a generated ID, got empty")
	}
	if note.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
	if note.Color == "" {
		t.Error("expected a default color to be assigned for blank input")
	}
	if note.Author != "Builder" || note.Content != "shipped the parser" {
		t.Errorf("unexpected note: %+v", note)
	}

	// Persisted: a fresh manager over the same HOME must reload it.
	m2 := NewManager(nil, func(string, interface{}) {}, nil)
	got := m2.ListStickyNotes()
	if len(got) != 1 {
		t.Fatalf("reloaded %d notes, want 1", len(got))
	}
	if got[0].ID != note.ID || got[0].Content != note.Content {
		t.Errorf("reloaded note %+v != original %+v", got[0], note)
	}
}

func TestAddStickyNote_BlankAuthorFallsBackToAgent(t *testing.T) {
	m := newStickyTestManager(t)
	note := m.AddStickyNote("", "anon note", "")
	if note.Author != "agent" {
		t.Errorf("Author = %q, want %q", note.Author, "agent")
	}
}

func TestAddStickyNote_ExplicitColorPreserved(t *testing.T) {
	m := newStickyTestManager(t)
	note := m.AddStickyNote("Reviewer", "looks good", "#123456")
	if note.Color != "#123456" {
		t.Errorf("Color = %q, want %q", note.Color, "#123456")
	}
}

func TestColorForAuthor_StablePerAuthor(t *testing.T) {
	if colorForAuthor("Alice") != colorForAuthor("Alice") {
		t.Error("expected the same author to map to the same color")
	}
	if colorForAuthor("") != stickyNotePalette[0] {
		t.Error("empty author should fall back to the first palette color")
	}
}

func TestListStickyNotes_ReturnsCopyOldestFirst(t *testing.T) {
	m := newStickyTestManager(t)
	first := m.AddStickyNote("A", "one", "")
	second := m.AddStickyNote("B", "two", "")

	list := m.ListStickyNotes()
	if len(list) != 2 {
		t.Fatalf("got %d notes, want 2", len(list))
	}
	if list[0].ID != first.ID || list[1].ID != second.ID {
		t.Errorf("expected oldest-first ordering, got %q then %q", list[0].ID, list[1].ID)
	}

	// Mutating the returned slice must not affect the manager's state.
	list[0].Content = "tampered"
	if m.ListStickyNotes()[0].Content == "tampered" {
		t.Error("ListStickyNotes returned a live reference; expected a copy")
	}
}

func TestAddStickyNote_TrimsOldestPastCap(t *testing.T) {
	m := newStickyTestManager(t)
	// Seed one identifiable oldest note, then overflow the cap.
	oldest := m.AddStickyNote("seed", "OLDEST", "")
	for i := 0; i < stickyNotesMaxKeep+5; i++ {
		m.AddStickyNote("bulk", "n", "")
	}
	list := m.ListStickyNotes()
	if len(list) != stickyNotesMaxKeep {
		t.Fatalf("len after overflow = %d, want %d", len(list), stickyNotesMaxKeep)
	}
	for _, n := range list {
		if n.ID == oldest.ID {
			t.Fatal("oldest note should have been trimmed past the cap")
		}
	}
}

func TestLoadStickyNotes_MissingFileIsEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := NewManager(nil, func(string, interface{}) {}, nil)
	if got := m.ListStickyNotes(); len(got) != 0 {
		t.Fatalf("expected empty list for missing file, got %d", len(got))
	}
}

func TestLoadStickyNotes_CorruptFileTolerated(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".jarvis", "office-memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sticky-notes.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewManager(nil, func(string, interface{}) {}, nil)
	if got := m.ListStickyNotes(); len(got) != 0 {
		t.Fatalf("corrupt file should yield empty list, got %d", len(got))
	}
	// And a subsequent add must still work + overwrite the corrupt file.
	if n := m.AddStickyNote("X", "recovered", ""); n.ID == "" {
		t.Fatal("add after corrupt load failed")
	}
}
