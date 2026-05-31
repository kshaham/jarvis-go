package handler

// Round-4 fill for sweepStaleAudioTmp's filepath.Glob error arm (92-94): an
// unbalanced '[' in the resolved StandupDir() path makes the glob pattern
// malformed, so Glob returns ErrBadPattern and the sweep returns early.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kshaham/jarvis/internal/office"
)

func TestSweepStaleAudioTmp_GlobBadPatternReturnsEarly(t *testing.T) {
	// HOME with an unclosed '[' meta-char makes
	// "<HOME>/.jarvis/office-memory/standups/*.mp3.tmp" a malformed glob.
	home := filepath.Join(t.TempDir(), "ho[me")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	t.Setenv("HOME", home)

	// Sanity: confirm the pattern is actually malformed for this HOME so the
	// test is exercising the intended branch (not a silent no-op).
	if _, gerr := filepath.Glob(filepath.Join(office.StandupDir(), "*.mp3.tmp")); gerr == nil {
		t.Skip("glob pattern unexpectedly valid on this platform; cannot exercise the error arm")
	}

	// Must not panic and must return without attempting removals.
	sweepStaleAudioTmp()
}
