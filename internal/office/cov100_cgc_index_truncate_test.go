package office

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/testutil/stubbin"
)

// TestRunCGCIndex_FailureWithLongOutputTruncates drives the output-truncation
// branch in RunCGCIndex's failure path: a stub `cgc` that exits non-zero and
// emits more than cgcIndexLogTruncate (200) bytes, so the logged output is
// clipped. A sentinel file confirms the background goroutine actually ran.
func TestRunCGCIndex_FailureWithLongOutputTruncates(t *testing.T) {
	sentinel := filepath.Join(t.TempDir(), "cgc-ran")
	// Emit ~500 chars to stdout (CombinedOutput captures it), then fail.
	longLine := strings.Repeat("x", 500)
	stubScript := "#!/bin/sh\n" +
		"echo '" + longLine + "'\n" +
		"echo ran > " + sentinel + "\n" +
		"exit 1\n"
	stubPath := stubbin.Install(t, "cgc", stubScript)
	t.Setenv("PATH", filepath.Dir(stubPath))

	RunCGCIndex(t.TempDir())

	// Wait for the background goroutine to run the stub (which writes the
	// sentinel), proving the failure+truncation path executed.
	waitForFile(t, sentinel, 5*time.Second)
	// Brief settle so the slog.Warn after the truncation has fired.
	time.Sleep(30 * time.Millisecond)
}
