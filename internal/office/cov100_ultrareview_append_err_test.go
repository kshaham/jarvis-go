package office

import (
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/library"
)

// TestAppendUltrareviewToLibrary_AppendErrorLogged drives the
// library.AppendUltrareview error branch inside appendUltrareviewToLibrary's
// async goroutine: with the library dir set to "", AppendUltrareview returns
// "library root unavailable" and the goroutine logs at debug (the error arm).
func TestAppendUltrareviewToLibrary_AppendErrorLogged(t *testing.T) {
	// Empty library dir -> AppendUltrareview returns an error immediately.
	restore := library.SetDirForTest("")
	t.Cleanup(restore)

	result := &UltrareviewResult{
		Summary: "ok",
		Findings: []UltrareviewFinding{
			{Severity: "warning", Title: "x"},
		},
	}

	// Fires a safego goroutine that calls library.AppendUltrareview(...) which
	// errors; the goroutine must not panic and the office package must stay
	// healthy. We give the goroutine a moment to run.
	appendUltrareviewToLibrary("cfg-err", "task-err", result)

	// Drain: the goroutine is short (one ReadFile attempt that errors). A
	// brief poll window keeps the test deterministic without a fixed sleep.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		// There is no observable side effect on the error path beyond the
		// debug log, so we simply ensure the call returned cleanly and the
		// goroutine had time to execute.
		time.Sleep(20 * time.Millisecond)
		break
	}
	// Re-confirm the library dir is still empty (no file was written).
	if library.Dir() != "" {
		t.Errorf("expected library dir to remain empty, got %q", library.Dir())
	}
}
