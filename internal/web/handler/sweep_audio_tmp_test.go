package handler

// Direct unit coverage for sweepStaleAudioTmp's removal loop: seed an orphaned
// *.mp3.tmp in the standup dir and assert the sweep deletes it.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kshaham/jarvis/internal/office"
)

func TestSweepStaleAudioTmp_RemovesOrphans(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := office.StandupDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir standup dir: %v", err)
	}
	orphan := filepath.Join(dir, "sx.mp3.tmp")
	if err := os.WriteFile(orphan, []byte("partial"), 0o644); err != nil {
		t.Fatalf("seed orphan: %v", err)
	}
	// A finished .mp3 must survive the sweep (only *.mp3.tmp is targeted).
	keep := filepath.Join(dir, "sx.mp3")
	if err := os.WriteFile(keep, []byte("done"), 0o644); err != nil {
		t.Fatalf("seed keep: %v", err)
	}

	sweepStaleAudioTmp()

	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Errorf("orphan .mp3.tmp should have been removed, stat err=%v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("finished .mp3 should survive the sweep, stat err=%v", err)
	}
}
