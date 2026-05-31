package office

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// IsValidMemorySectionName
// ---------------------------------------------------------------------------

func TestIsValidMemorySectionName(t *testing.T) {
	valid := []string{"notes", "current_focus", "decisions", "a", "A1-b_c", strings.Repeat("x", 64)}
	for _, name := range valid {
		if !IsValidMemorySectionName(name) {
			t.Errorf("expected %q to be valid", name)
		}
	}
	invalid := []string{"", "../escape", "has space", "with/slash", "-leading", strings.Repeat("y", 65), "dot.section"}
	for _, name := range invalid {
		if IsValidMemorySectionName(name) {
			t.Errorf("expected %q to be invalid", name)
		}
	}
}

// ---------------------------------------------------------------------------
// Sectioned read/write/append round-trip
// ---------------------------------------------------------------------------

// TestMemorySections_WriteReadRoundTrip verifies writing a section then
// reading it back returns the trimmed content, and unknown sections read
// as empty.
func TestMemorySections_WriteReadRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const cfg, wd = "agent-1", "/tmp/project-a"

	// Unknown section on a missing file reads as "".
	got, err := MemoryReadSection(cfg, wd, "decisions")
	if err != nil {
		t.Fatalf("read missing: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty for missing section, got %q", got)
	}

	if err := MemoryWriteSection(cfg, wd, "decisions", "use postgres\n\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err = MemoryReadSection(cfg, wd, "decisions")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != "use postgres" {
		t.Errorf("expected trailing newlines trimmed, got %q", got)
	}
}

// TestMemorySections_WritePreservesOtherSections verifies that writing one
// section does not clobber a previously written section.
func TestMemorySections_WritePreservesOtherSections(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const cfg, wd = "agent-2", "/tmp/project-b"

	if err := MemoryWriteSection(cfg, wd, "current_focus", "ship auth"); err != nil {
		t.Fatalf("write focus: %v", err)
	}
	if err := MemoryWriteSection(cfg, wd, "decisions", "use jwt"); err != nil {
		t.Fatalf("write decisions: %v", err)
	}

	focus, _ := MemoryReadSection(cfg, wd, "current_focus")
	decisions, _ := MemoryReadSection(cfg, wd, "decisions")
	if focus != "ship auth" {
		t.Errorf("expected current_focus preserved, got %q", focus)
	}
	if decisions != "use jwt" {
		t.Errorf("expected decisions written, got %q", decisions)
	}
}

// TestMemorySections_Append verifies appending creates a section and then
// adds newline-separated lines.
func TestMemorySections_Append(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const cfg, wd = "agent-3", "/tmp/project-c"

	if err := MemoryAppendSection(cfg, wd, "notes", "first lesson"); err != nil {
		t.Fatalf("append 1: %v", err)
	}
	if err := MemoryAppendSection(cfg, wd, "notes", "second lesson"); err != nil {
		t.Fatalf("append 2: %v", err)
	}
	got, _ := MemoryReadSection(cfg, wd, "notes")
	if got != "first lesson\nsecond lesson" {
		t.Errorf("expected two newline-separated lines, got %q", got)
	}
}

// TestMemorySections_InvalidNameRejected verifies the read/write/append
// helpers reject an invalid section name before touching disk.
func TestMemorySections_InvalidNameRejected(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const cfg, wd = "agent-4", "/tmp/project-d"

	if _, err := MemoryReadSection(cfg, wd, "../bad"); err == nil {
		t.Error("expected read error for invalid section name")
	}
	if err := MemoryWriteSection(cfg, wd, "bad name", "x"); err == nil {
		t.Error("expected write error for invalid section name")
	}
	if err := MemoryAppendSection(cfg, wd, "bad/name", "x"); err == nil {
		t.Error("expected append error for invalid section name")
	}
}

// ---------------------------------------------------------------------------
// MemoryQuery
// ---------------------------------------------------------------------------

// TestMemoryQuery_FindsMatchesNewestFirst verifies a case-insensitive
// substring search returns hits with section + line, newest (highest line)
// first, and that section headers themselves are not returned as hits.
func TestMemoryQuery_FindsMatchesNewestFirst(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const cfg, wd = "agent-5", "/tmp/project-e"

	if err := MemoryWriteSection(cfg, wd, "notes", "Postgres chosen\nredis cache\nPOSTGRES tuning"); err != nil {
		t.Fatalf("write: %v", err)
	}

	hits, err := MemoryQuery(cfg, wd, "postgres")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected 2 case-insensitive hits, got %d: %+v", len(hits), hits)
	}
	// Newest-first: the later "POSTGRES tuning" line has the higher line no.
	if !strings.Contains(hits[0].Text, "tuning") {
		t.Errorf("expected newest hit first, got %+v", hits)
	}
	for _, h := range hits {
		if h.Section != "notes" {
			t.Errorf("expected section=notes, got %q", h.Section)
		}
	}
}

// TestMemoryQuery_EmptyKeywordRejected verifies an empty keyword errors.
func TestMemoryQuery_EmptyKeywordRejected(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := MemoryQuery("agent-6", "/tmp/p", "   "); err == nil {
		t.Error("expected error for empty keyword")
	}
}

// TestMemoryQuery_MissingFileReturnsNil verifies a query against a
// non-existent memory file returns (nil, nil).
func TestMemoryQuery_MissingFileReturnsNil(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	hits, err := MemoryQuery("agent-7", "/tmp/nowhere", "anything")
	if err != nil {
		t.Fatalf("expected nil error for missing file, got %v", err)
	}
	if hits != nil {
		t.Errorf("expected nil hits for missing file, got %+v", hits)
	}
}

// ---------------------------------------------------------------------------
// Legacy flat LoadAgentMemory / SaveAgentMemory / AppendAgentMemory
// ---------------------------------------------------------------------------

// TestAgentMemory_LegacyFlatRoundTrip verifies the flat blob helpers
// round-trip and that AppendAgentMemory adds to the existing content.
func TestAgentMemory_LegacyFlatRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const cfg, wd = "agent-8", "/tmp/project-f"

	// Missing file loads as "".
	if got := LoadAgentMemory(cfg, wd); got != "" {
		t.Errorf("expected empty for missing memory, got %q", got)
	}

	if err := SaveAgentMemory(cfg, wd, "line one\n"); err != nil {
		t.Fatalf("save: %v", err)
	}
	if got := LoadAgentMemory(cfg, wd); got != "line one\n" {
		t.Errorf("expected saved content, got %q", got)
	}

	if err := AppendAgentMemory(cfg, wd, "line two\n"); err != nil {
		t.Fatalf("append: %v", err)
	}
	if got := LoadAgentMemory(cfg, wd); got != "line one\nline two\n" {
		t.Errorf("expected appended content, got %q", got)
	}
}
