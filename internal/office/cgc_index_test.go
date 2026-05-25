package office

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/claudeman/jarvis-go/internal/testutil/stubbin"
)

// waitForFile polls until path exists or the deadline elapses.
// Fails the test on timeout. Used to assert that a fire-and-forget
// goroutine actually ran without exposing the goroutine's own
// shutdown signal.
func waitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("file %s did not appear within %s", path, timeout)
}

// RunCGCIndex is fire-and-forget — it returns immediately and runs
// the cgc subprocess in a background goroutine. The test substitutes
// PATH so a stub binary stands in for `cgc`, then verifies the
// goroutine completes without panicking.

func TestRunCGCIndex_BackgroundCompletionWithStubBinary(t *testing.T) {
	// Stub `cgc` that writes its own pid to a sentinel file so we can
	// confirm the goroutine ran.
	sentinel := t.TempDir() + "/cgc-was-called"
	stubScript := `#!/bin/sh
echo "$$" > ` + sentinel + `
exit 0
`
	stubPath := stubbin.Install(t, "cgc", stubScript)
	t.Setenv("PATH", filepath.Dir(stubPath))

	RunCGCIndex(t.TempDir())

	// The goroutine should complete within a couple of seconds for
	// such a trivial stub. Poll the sentinel.
	waitForFile(t, sentinel, 5*time.Second)
}

func TestRunCGCIndex_FailureDoesNotPanic(t *testing.T) {
	// Stub that exits non-zero. The goroutine should log + return
	// without crashing the daemon.
	stubScript := `#!/bin/sh
echo "intentional failure for the test" >&2
exit 1
`
	stubPath := stubbin.Install(t, "cgc", stubScript)
	t.Setenv("PATH", filepath.Dir(stubPath))

	// No assertion on output; just verify it doesn't deadlock or
	// panic. The goroutine is detached, but the test process itself
	// continues past this call.
	RunCGCIndex(t.TempDir())
}

func TestRunCGCIndex_NoBinaryOnPathReturnsCleanly(t *testing.T) {
	// PATH that definitely doesn't include cgc.
	t.Setenv("PATH", t.TempDir())
	// Should not panic, should not block.
	RunCGCIndex(t.TempDir())
}
