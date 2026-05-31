package office

import (
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/library"
)

// TestRecordExperiment_LibraryMirrorErrorLogged drives the
// library.WriteExperiment error branch in RecordExperiment's async mirror
// goroutine: the canonical jsonl write succeeds (HOME is set), but the library
// root is empty so the mirror write returns "library root unavailable" and the
// goroutine logs at debug. The canonical RecordExperiment call still returns nil.
func TestRecordExperiment_LibraryMirrorErrorLogged(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // canonical experiments path resolves here
	restore := library.SetDirForTest("")
	t.Cleanup(restore)

	if err := RecordExperiment(ExperimentRecord{
		Timestamp:    time.Now(),
		TeamTemplate: "t",
		Mission:      "ship it",
	}); err != nil {
		t.Fatalf("canonical RecordExperiment must succeed even when the library mirror fails: %v", err)
	}

	// Give the async library-mirror goroutine time to run its (failing) write.
	time.Sleep(80 * time.Millisecond)

	// The library dir is still empty (no experiment file written).
	if library.Dir() != "" {
		t.Errorf("expected library dir to remain empty, got %q", library.Dir())
	}
}
