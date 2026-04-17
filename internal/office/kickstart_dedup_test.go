package office

import (
	"strings"
	"testing"
)

func TestDeduplicateMemory_RemovesDuplicates(t *testing.T) {
	in := "remember to run tests\nremember to run tests\nremember to run tests\nuse context budgeting"
	out := deduplicateMemory(in)
	if strings.Count(out, "remember to run tests") != 1 {
		t.Errorf("expected 1 occurrence, got: %s", out)
	}
	if !strings.Contains(out, "use context budgeting") {
		t.Errorf("unique line was dropped: %s", out)
	}
}

func TestDeduplicateMemory_PreservesHeaders(t *testing.T) {
	in := "## Section A\n- item\n## Section A\n- item"
	out := deduplicateMemory(in)
	// Headers should be preserved even if identical (they anchor sections).
	if strings.Count(out, "## Section A") != 2 {
		t.Errorf("headers should be preserved verbatim: %s", out)
	}
}

func TestDeduplicateMemory_NormalizesWhitespace(t *testing.T) {
	// Same content with different whitespace should dedupe down to one line.
	in := "use  context   budgeting\nuse context budgeting"
	out := deduplicateMemory(in)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 {
		t.Errorf("whitespace variants should dedupe to 1 line, got %d: %q", len(lines), out)
	}
}

func TestBuildProjectContext_NoDocs(t *testing.T) {
	dir := t.TempDir()
	if BuildProjectContext(dir) != "" {
		t.Error("expected empty context when no docs exist")
	}
}
