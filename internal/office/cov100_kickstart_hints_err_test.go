package office

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kshaham/jarvis/internal/library"
)

// TestBuildLibraryHintsSection_SearchError drives the library.Search
// error-return branch in BuildLibraryHintsSection. library.Search
// errors when the experiences path exists but is not a directory
// (os.ReadDir returns ENOTDIR, which is NOT os.IsNotExist). We force
// that by planting a regular file at <root>/experiences. The function
// must swallow the error and return "".
func TestBuildLibraryHintsSection_SearchError(t *testing.T) {
	root := t.TempDir()
	// Plant a FILE where the experiences directory is expected.
	if err := os.WriteFile(filepath.Join(root, "experiences"), []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	restore := library.SetDirForTest(root)
	t.Cleanup(restore)

	cfg := &TeamMemberConfig{ID: "vex", Name: "Vex", Role: "backend"}
	if got := BuildLibraryHintsSection(cfg, "/repo"); got != "" {
		t.Errorf("expected empty string on library.Search error, got %q", got)
	}
}

// TestBuildAntiPatternHintsSection_SearchError drives the
// library.SearchAntiPatterns error-return branch. Same ENOTDIR trick,
// but on the anti-patterns path.
func TestBuildAntiPatternHintsSection_SearchError(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "anti-patterns"), []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	restore := library.SetDirForTest(root)
	t.Cleanup(restore)

	cfg := &TeamMemberConfig{ID: "vex", Name: "Vex", Role: "backend"}
	if got := BuildAntiPatternHintsSection(cfg, "/repo"); got != "" {
		t.Errorf("expected empty string on SearchAntiPatterns error, got %q", got)
	}
}
