package office

import (
	"os"
	"path/filepath"
	"testing"
)

// TestEnsureWorktreesGitignored_OpenFails drives the OpenFile error branch:
// the .gitignore path is a directory, so opening it for append fails (EISDIR)
// and the function logs + returns without attempting a git add.
func TestEnsureWorktreesGitignored_OpenFails(t *testing.T) {
	dir := t.TempDir()
	gitignore := filepath.Join(dir, ".gitignore")
	// Pre-create .gitignore as a directory so OpenFile(...O_WRONLY) fails.
	if err := os.MkdirAll(gitignore, 0o755); err != nil {
		t.Fatal(err)
	}

	// Should not panic; the OpenFile error path logs and returns.
	ensureWorktreesGitignored(gitignore, dir)

	// The path is still a directory (nothing was written through it).
	info, err := os.Stat(gitignore)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Error("expected .gitignore to remain a directory after a failed open")
	}
}
