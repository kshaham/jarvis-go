package static

import (
	"os"
	"path/filepath"
	"testing"
)

// FileExists is consumed by the /office-v2 route handler in server.go
// to decide between serving the React shell and a placeholder. The
// behaviour split (dev path = disk, prod path = embedded FS) needs to
// keep working for both modes.

func TestFileExists_DevModeHit(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "react"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "react", "index.html"), []byte("<html/>"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !FileExists(dir, "react/index.html") {
		t.Fatal("expected FileExists=true for an on-disk file")
	}
}

func TestFileExists_DevModeMiss(t *testing.T) {
	dir := t.TempDir()
	if FileExists(dir, "react/index.html") {
		t.Fatal("expected FileExists=false when dev dir exists but file is absent")
	}
}

func TestFileExists_FallsThroughToEmbedWhenDevDirMissing(t *testing.T) {
	// Pointing dev path at a non-existent dir should fall through to the
	// embed.FS lookup. The embedded set ships at least one known file
	// (office.html), so probing for it should succeed.
	if !FileExists("/nonexistent/path/that/should/never/exist", "office.html") {
		t.Fatal("expected embed-FS fallback to find office.html")
	}
}

func TestFileExists_EmbedMissReturnsFalse(t *testing.T) {
	if FileExists("/nonexistent/path", "does-not-exist-anywhere.html") {
		t.Fatal("expected FileExists=false for an unknown embedded file")
	}
}
