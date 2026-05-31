package office

import (
	"os"
	"path/filepath"
	"testing"
)

// TestHasTaskScreenshot_ListError drives the error branch of
// HasTaskScreenshot (task_screenshot.go lines ~171-173): when
// ListTaskScreenshots returns an error (screenshots path is a file, not
// a directory), HasTaskScreenshot must return false.
func TestHasTaskScreenshot_ListError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := TaskScreenshotsDir()
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	// Plant a FILE where the screenshots dir is expected so ReadDir
	// (inside ListTaskScreenshots) errors with ENOTDIR.
	if err := os.WriteFile(dir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if HasTaskScreenshot("t1") {
		t.Error("HasTaskScreenshot should return false when listing errors")
	}
}

// TestPruneOldScreenshots_DropsAndSkipsSubdir drives the IsDir-skip
// branch (lines ~190-191) AND the drop loop (lines ~203-208) of
// pruneOldScreenshots. We seed the dir with a subdir (to skip) plus
// more valid screenshots than `keep`, so the oldest ones are dropped.
func TestPruneOldScreenshots_DropsAndSkipsSubdir(t *testing.T) {
	dir := t.TempDir()
	// A subdirectory that prune must skip (exercises the IsDir continue).
	if err := os.MkdirAll(filepath.Join(dir, "a-subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 5 valid screenshots with monotonically-increasing timestamps.
	names := []string{
		"t1-1700000001.png",
		"t1-1700000002.png",
		"t1-1700000003.png",
		"t1-1700000004.png",
		"t1-1700000005.png",
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Keep only the 2 newest; the 3 oldest must be removed.
	if err := pruneOldScreenshots(dir, 2); err != nil {
		t.Fatalf("pruneOldScreenshots: %v", err)
	}

	// The 3 oldest are gone.
	for _, gone := range names[:3] {
		if _, err := os.Stat(filepath.Join(dir, gone)); !os.IsNotExist(err) {
			t.Errorf("expected %s to be pruned, stat err=%v", gone, err)
		}
	}
	// The 2 newest survive.
	for _, kept := range names[3:] {
		if _, err := os.Stat(filepath.Join(dir, kept)); err != nil {
			t.Errorf("expected %s to survive prune: %v", kept, err)
		}
	}
	// The subdir survives untouched.
	if _, err := os.Stat(filepath.Join(dir, "a-subdir")); err != nil {
		t.Errorf("subdir should not be pruned: %v", err)
	}
}

// TestPruneOldScreenshots_UnderKeepNoOp covers the early return when the
// valid count is at-or-below `keep`.
func TestPruneOldScreenshots_UnderKeepNoOp(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "t1-1700000001.png"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := pruneOldScreenshots(dir, 5); err != nil {
		t.Fatalf("pruneOldScreenshots: %v", err)
	}
	// File still present (nothing pruned).
	if _, err := os.Stat(filepath.Join(dir, "t1-1700000001.png")); err != nil {
		t.Errorf("file should survive when under keep: %v", err)
	}
}
