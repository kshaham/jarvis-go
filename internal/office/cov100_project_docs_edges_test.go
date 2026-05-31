package office

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildProjectContextUncached_ExcerptExceedsRemaining drives the
// `len(excerpt) > remaining` truncation: three ~1000-char docs consume the
// total budget so the third doc's excerpt is larger than the remaining
// allowance and gets clipped to `remaining`.
func TestBuildProjectContextUncached_ExcerptExceedsRemaining(t *testing.T) {
	dir := t.TempDir()

	// Each doc body is ~1000 chars of prose (under the 1200 per-file cap, so
	// no per-file truncation). Three of them overflow the 2500 total cap.
	body := strings.Repeat("the quick brown fox jumps over the lazy dog. ", 22) // ~990 chars
	for _, name := range []string{"CLAUDE.md", "README.md", "ARCHITECTURE.md"} {
		content := "# Title\n\n" + body + "\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got := buildProjectContextUncached(dir)
	if got == "" {
		t.Fatal("expected non-empty project context")
	}
	// The output ends with a "\n..." truncation marker from the remaining-budget
	// clip on the last doc.
	if !strings.Contains(got, "...") {
		t.Errorf("expected a truncation marker from the remaining-budget clip, got:\n%s", got)
	}
	// Total output must respect the budget ceiling.
	if len(got) > 2700 {
		t.Errorf("project context exceeded the budget unexpectedly: %d bytes", len(got))
	}
}
