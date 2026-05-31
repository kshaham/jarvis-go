package office

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestSanitizeInvariants_MultibyteTruncation forces the rune-boundary
// backup loop: an over-length invariant ending mid-multibyte rune must be
// truncated on a valid boundary.
func TestSanitizeInvariants_MultibyteTruncation(t *testing.T) {
	// maxInvariantLen=300. Place a 4-byte emoji straddling index 300 so
	// t[300] is a continuation byte and the loop backs up.
	long := strings.Repeat("a", 298) + "🚀" + strings.Repeat("b", 20)
	out := sanitizeInvariants([]string{long})
	if len(out) != 1 {
		t.Fatalf("expected 1 invariant, got %d", len(out))
	}
	if !utf8.ValidString(out[0]) {
		t.Errorf("sanitized invariant is not valid UTF-8: %q", out[0])
	}
	if len(out[0]) > maxInvariantLen {
		t.Errorf("invariant %d bytes exceeds max %d", len(out[0]), maxInvariantLen)
	}
}

// TestSanitizeInvariants_CapAndBlanks verifies blank lines are dropped and
// the list is capped at maxInvariants.
func TestSanitizeInvariants_CapAndBlanks(t *testing.T) {
	var items []string
	items = append(items, "   ") // blank → dropped
	for i := 0; i < maxInvariants+5; i++ {
		items = append(items, "rule")
	}
	out := sanitizeInvariants(items)
	if len(out) != maxInvariants {
		t.Errorf("expected cap at %d, got %d", maxInvariants, len(out))
	}
}

// TestPersistProjectInvariants_WriteFails makes the tmp path a directory so
// os.WriteFile fails (the function swallows the error; we assert no panic
// and that no final file is produced).
func TestPersistProjectInvariants_WriteFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".jarvis", "office-memory")
	if err := os.MkdirAll(filepath.Join(dir, invariantsFileName+".tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := newTestManager()
	m.SetProjectInvariants([]string{"rule one"})
	// The real file must not exist (write of tmp failed).
	if _, err := os.Stat(filepath.Join(dir, invariantsFileName)); !os.IsNotExist(err) {
		t.Error("expected no invariants file after write failure")
	}
}

// TestLoadPersistedInvariants_ReadError covers the ReadFile error branch
// (no file present → silent return).
func TestLoadPersistedInvariants_ReadError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.loadPersistedInvariants() // file absent → no-op, no panic
	if len(m.GetProjectInvariants()) != 0 {
		t.Error("expected no invariants loaded when file absent")
	}
}

// TestLoadPersistedInvariants_BadJSON covers the json.Unmarshal error.
func TestLoadPersistedInvariants_BadJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".jarvis", "office-memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, invariantsFileName), []byte("{not an array"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTestManager()
	m.loadPersistedInvariants()
	if len(m.GetProjectInvariants()) != 0 {
		t.Error("expected no invariants from corrupt JSON")
	}
}

// TestLoadPersistedInvariants_RoundTrip persists then reloads.
func TestLoadPersistedInvariants_RoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	m := newTestManager()
	m.SetProjectInvariants([]string{"never log secrets", "always validate input"})

	m2 := newTestManager()
	m2.loadPersistedInvariants()
	got := m2.GetProjectInvariants()
	if len(got) != 2 || got[0] != "never log secrets" {
		t.Errorf("round-trip mismatch: %v", got)
	}
}

// TestRenderInvariantsBlock_Caps verifies the 1500-byte render cap stops
// emitting lines once the block grows too large.
func TestRenderInvariantsBlock_Caps(t *testing.T) {
	var items []string
	for i := 0; i < 20; i++ {
		items = append(items, strings.Repeat("x", 200))
	}
	out := renderInvariantsBlock(items)
	if len(out) > 1700 { // header + capped body + trailing newline
		t.Errorf("render block not capped: %d bytes", len(out))
	}
	if !strings.Contains(out, "Project Invariants") {
		t.Error("expected header in rendered block")
	}
}

// TestRenderInvariantsBlock_Empty returns "" for empty input.
func TestRenderInvariantsBlock_Empty(t *testing.T) {
	if got := renderInvariantsBlock(nil); got != "" {
		t.Errorf("expected empty render for nil, got %q", got)
	}
}

// TestRemoveProjectInvariant_OutOfRange covers the index-bounds error.
func TestRemoveProjectInvariant_OutOfRange(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.SetProjectInvariants([]string{"a"})
	if err := m.RemoveProjectInvariant(5); err == nil {
		t.Error("expected out-of-range error")
	}
	if err := m.RemoveProjectInvariant(-1); err == nil {
		t.Error("expected out-of-range error for negative index")
	}
}
