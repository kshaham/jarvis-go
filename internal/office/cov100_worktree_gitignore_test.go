package office

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeGitFailAdd succeeds rev-parse + worktree add but fails `git add`,
// so ensureWorktreesGitignored writes the .gitignore line, then the
// stage step (`git add .gitignore`) returns an error and the function
// returns early before the commit (drives lines 408-410).
const fakeGitFailAdd = `#!/bin/sh
case "$1" in
  add) echo "fatal: simulated add failure" 1>&2; exit 1 ;;
esac
exit 0
`

func TestEnsureWorktreesGitignored_GitAddFails(t *testing.T) {
	installFakeGit(t, fakeGitFailAdd)
	dir := t.TempDir()
	gitignore := filepath.Join(dir, ".gitignore")

	// No pre-existing .worktrees mention => function appends + tries to
	// `git add`, which our fake git fails on.
	ensureWorktreesGitignored(gitignore, dir)

	// The .gitignore line should still have been written (write happens
	// before the failing `git add`).
	content, err := os.ReadFile(gitignore)
	if err != nil {
		t.Fatalf("gitignore should exist after append: %v", err)
	}
	if want := ".worktrees/"; !strings.Contains(string(content), want) {
		t.Errorf("gitignore should contain %q, got %q", want, content)
	}
}

// TestHooksModeMcpServers_PageIndexAvailable drives the non-nil return
// path of hooksModeMcpServers (lines 432-440 true side): with a fake
// `npx` that satisfies the PageIndex probe, PageIndexAvailable() returns
// true, so the function builds and returns a map containing "pageindex".
//
// We install a fake npx that prints a version for any args and exits 0,
// reset both the npx and pageindex one-shot caches, and restore them
// after. This test must not run in parallel (it mutates package-global
// caches).
var pageIndexTestMu sync.Mutex

func TestHooksModeMcpServers_PageIndexAvailable(t *testing.T) {
	pageIndexTestMu.Lock()
	defer pageIndexTestMu.Unlock()

	// Fake npx: any invocation prints a version + exits 0, satisfying
	// both NpxAvailable's LookPath and PageIndexAvailable's probe.
	dir := t.TempDir()
	npxPath := filepath.Join(dir, "npx")
	if err := os.WriteFile(npxPath, []byte("#!/bin/sh\necho '1.0.0'\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	resetNpxCacheForTest()
	resetPageIndexCacheForTest()
	t.Cleanup(func() {
		resetNpxCacheForTest()
		resetPageIndexCacheForTest()
	})

	if !PageIndexAvailable() {
		t.Skip("PageIndex probe did not succeed in this environment; skipping non-nil branch")
	}

	got := hooksModeMcpServers()
	if got == nil {
		t.Fatal("expected non-nil mcp servers map when PageIndex is available")
	}
	if _, ok := got["pageindex"]; !ok {
		t.Errorf("expected pageindex key in mcp servers map, got %v", got)
	}
}
