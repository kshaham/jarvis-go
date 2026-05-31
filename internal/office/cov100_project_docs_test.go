package office

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildProjectContext_EmptyDirGuard drives the workingDir=="" guard
// (project_docs.go ~48-50).
func TestBuildProjectContext_EmptyDirGuard(t *testing.T) {
	if got := BuildProjectContext(""); got != "" {
		t.Errorf("expected empty context for empty workingDir, got %q", got)
	}
}

// TestBuildProjectContext_CacheHit drives the cache-hit return path: a
// second call within the TTL returns the cached content without
// re-reading disk. We prove it by mutating the doc between calls and
// asserting the cached (stale) content is returned.
func TestBuildProjectContext_CacheHit(t *testing.T) {
	dir := t.TempDir()
	// Clear any stale cache entry from a prior test using the same path
	// (TempDir is unique per test, so this is belt-and-suspenders).
	projectDocsMu.Lock()
	delete(projectDocsCache, dir)
	projectDocsMu.Unlock()

	claude := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(claude, []byte("# Title\n\nFirst body paragraph here.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first := BuildProjectContext(dir)
	if !strings.Contains(first, "First body paragraph") {
		t.Fatalf("expected first call to read the doc, got %q", first)
	}
	// Mutate the file; the cached value should still be returned.
	if err := os.WriteFile(claude, []byte("# Title\n\nCOMPLETELY DIFFERENT.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := BuildProjectContext(dir)
	if second != first {
		t.Errorf("expected cached content on second call, got %q (first %q)", second, first)
	}
}

// TestBuildProjectContextUncached_BudgetExhaustion drives the budget
// branches: the per-doc `len(excerpt) > remaining` truncation (~88-90)
// and the `remaining <= 200` early break (~73-74). We seed all four
// candidate docs with large bodies so the running budget is exhausted
// before the last candidate is reached.
func TestBuildProjectContextUncached_BudgetExhaustion(t *testing.T) {
	dir := t.TempDir()
	bigBody := strings.Repeat("lorem ipsum dolor sit amet. ", 200) // ~5600 chars
	docs := map[string]string{
		"CLAUDE.md":            "# C\n\n" + bigBody,
		"README.md":            "# R\n\n" + bigBody,
		"ARCHITECTURE.md":      "# A\n\n" + bigBody,
		"docs/ARCHITECTURE.md": "# D\n\n" + bigBody,
	}
	for name, body := range docs {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got := buildProjectContextUncached(dir)
	// The total output is bounded by projectDocsTotalCap (2500) plus a
	// little framing; it must be far smaller than the combined raw input.
	if len(got) > projectDocsTotalCap+500 {
		t.Errorf("output exceeded the total budget: %d chars", len(got))
	}
	// At least the first doc's truncated excerpt must be present.
	if !strings.Contains(got, "lorem ipsum") {
		t.Errorf("expected truncated doc content, got %q", got)
	}
	// The "..." truncation marker should appear (budget/per-file cap hit).
	if !strings.Contains(got, "...") {
		t.Errorf("expected a truncation marker in the output, got %q", got)
	}
}

// TestBuildProjectContextUncached_EmptyExcerptSkipped drives the
// `excerpt == ""` continue (~82-83): a doc that is ONLY a title (no
// prose body) yields an empty excerpt and is skipped, while a sibling
// doc with real prose is still included.
func TestBuildProjectContextUncached_EmptyExcerptSkipped(t *testing.T) {
	dir := t.TempDir()
	// CLAUDE.md is title-only => extractDocExcerpt returns "".
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# Just A Title\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// README.md has real prose.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# R\n\nReal prose content.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := buildProjectContextUncached(dir)
	if strings.Contains(got, "Just A Title") {
		t.Error("title-only doc should have been skipped (empty excerpt)")
	}
	if !strings.Contains(got, "Real prose content") {
		t.Errorf("expected the prose doc included, got %q", got)
	}
}
