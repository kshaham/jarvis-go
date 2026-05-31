package office

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kshaham/jarvis/internal/library"
)

// These tests drive the two PageIndex branches that prior waves left
// uncovered:
//
//   - pageindex.go:PageIndexAvailable — the "npx is on PATH but the
//     `--version` probe exits non-zero" path (the err != nil branch of
//     the CombinedOutput call). Hit with a fake npx that LookPath finds
//     but that exits 1 on every invocation.
//   - worktree.go:hooksModeMcpServers — the "PageIndex available but
//     PageIndexMcpConfig returns nil because LibraryDir() is empty"
//     branch. Hit with a probe-passing fake npx AND library.SetDirForTest("").
//
// Both mutate the package-global one-shot caches + PATH, so they share
// the pageIndexTestMu mutex declared in cov100_worktree_gitignore_test.go
// and must not run in parallel with each other or with
// TestHooksModeMcpServers_PageIndexAvailable.

// installFakeNpx writes an executable `npx` shell script with the given
// body into a fresh temp dir, prepends that dir to PATH, and resets the
// npx + pageindex caches so the next PageIndexAvailable() call re-probes
// against the stub. Caches are reset again on cleanup.
func installFakeNpx(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	npxPath := filepath.Join(dir, "npx")
	if err := os.WriteFile(npxPath, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake npx: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	resetNpxCacheForTest()
	resetPageIndexCacheForTest()
	t.Cleanup(func() {
		resetNpxCacheForTest()
		resetPageIndexCacheForTest()
	})
}

func TestPageIndexAvailable_NpxPresentButProbeFails(t *testing.T) {
	pageIndexTestMu.Lock()
	defer pageIndexTestMu.Unlock()

	// Fake npx is on PATH (LookPath succeeds → NpxAvailable() true) but
	// exits non-zero for the `--version` probe, so CombinedOutput's err
	// is non-nil and PageIndexAvailable must report false.
	installFakeNpx(t, "#!/bin/sh\necho 'boom: package not found' 1>&2\nexit 1\n")

	// Sanity: the LookPath gate must pass so we actually reach the probe
	// (otherwise we'd be exercising the npx-absent branch instead).
	if !NpxAvailable() {
		t.Fatal("fake npx should be discoverable on PATH")
	}

	if PageIndexAvailable() {
		t.Fatal("probe exited non-zero; PageIndexAvailable must be false")
	}
	// Cached: a second call returns the same false without re-running.
	if PageIndexAvailable() {
		t.Fatal("cache not stable: expected false on repeat call")
	}
}

func TestHooksModeMcpServers_AvailableButEmptyLibraryDirReturnsNil(t *testing.T) {
	pageIndexTestMu.Lock()
	defer pageIndexTestMu.Unlock()

	// Probe-passing fake npx → PageIndexAvailable() true.
	installFakeNpx(t, "#!/bin/sh\necho '1.0.0'\nexit 0\n")

	if !PageIndexAvailable() {
		t.Skip("PageIndex probe did not succeed in this environment")
	}

	// Force LibraryDir() == "" so PageIndexMcpConfig returns nil, which
	// drives hooksModeMcpServers down its cfg==nil branch (return nil)
	// even though PageIndex is "available".
	restore := library.SetDirForTest("")
	t.Cleanup(restore)
	if LibraryDir() != "" {
		t.Fatalf("expected empty LibraryDir after SetDirForTest(\"\"), got %q", LibraryDir())
	}

	if got := hooksModeMcpServers(); got != nil {
		t.Fatalf("expected nil map when PageIndex available but library dir empty, got %v", got)
	}
}
