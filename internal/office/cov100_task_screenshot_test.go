package office

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTaskScreenshotsDir_NoHome covers the UserHomeDir error → "".
func TestTaskScreenshotsDir_NoHome(t *testing.T) {
	withNoHome(t)
	if d := TaskScreenshotsDir(); d != "" {
		t.Errorf("expected empty dir with no HOME, got %q", d)
	}
}

// TestSaveTaskScreenshot_NoHome covers the dir=="" branch.
func TestSaveTaskScreenshot_NoHome(t *testing.T) {
	withNoHome(t)
	if _, err := SaveTaskScreenshot("t1", "image/png", []byte("x"), 1700000000); err == nil {
		t.Fatal("expected error with no HOME")
	}
}

// TestSaveTaskScreenshot_EmptyData covers the empty-data guard.
func TestSaveTaskScreenshot_EmptyData(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := SaveTaskScreenshot("t1", "image/png", nil, 1700000000); err == nil {
		t.Fatal("expected empty-data error")
	}
}

// TestSaveTaskScreenshot_MkdirFails makes the screenshots parent a file.
func TestSaveTaskScreenshot_MkdirFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	mem := filepath.Join(home, ".jarvis", "office-memory")
	if err := os.MkdirAll(filepath.Dir(mem), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mem, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveTaskScreenshot("t1", "image/png", []byte("x"), 1700000000); err == nil {
		t.Fatal("expected mkdir error")
	}
}

// TestSaveTaskScreenshot_WriteFails makes the .tmp path a directory.
func TestSaveTaskScreenshot_WriteFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := TaskScreenshotsDir()
	if err := os.MkdirAll(filepath.Join(dir, "t1-1700000000.png.tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveTaskScreenshot("t1", "image/png", []byte("x"), 1700000000); err == nil {
		t.Fatal("expected write-tmp error")
	}
}

// TestLoadTaskScreenshot_InvalidName covers the filename-validation guard.
func TestLoadTaskScreenshot_InvalidName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := LoadTaskScreenshot("../../etc/passwd"); err == nil {
		t.Fatal("expected invalid-filename error")
	}
}

// TestLoadTaskScreenshot_NoHome covers the dir=="" branch.
func TestLoadTaskScreenshot_NoHome(t *testing.T) {
	withNoHome(t)
	if _, err := LoadTaskScreenshot("t1-1700000000.png"); err == nil {
		t.Fatal("expected error with no HOME")
	}
}

// TestLoadTaskScreenshot_MissingFile covers the EvalSymlinks error.
func TestLoadTaskScreenshot_MissingFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := LoadTaskScreenshot("t1-1700000000.png"); err == nil {
		t.Fatal("expected EvalSymlinks error for missing file")
	}
}

// TestLoadTaskScreenshot_Roundtrip saves then loads a screenshot.
func TestLoadTaskScreenshot_Roundtrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name, err := SaveTaskScreenshot("t1", "image/png", []byte("PNGDATA"), 1700000000)
	if err != nil {
		t.Fatal(err)
	}
	data, err := LoadTaskScreenshot(name)
	if err != nil {
		t.Fatalf("LoadTaskScreenshot: %v", err)
	}
	if string(data) != "PNGDATA" {
		t.Errorf("data mismatch: %q", data)
	}
}

// TestListTaskScreenshots_NoHome covers the dir=="" → nil,nil branch.
func TestListTaskScreenshots_NoHome(t *testing.T) {
	withNoHome(t)
	out, err := ListTaskScreenshots("t1")
	if err != nil || out != nil {
		t.Errorf("expected nil,nil with no HOME, got %v,%v", out, err)
	}
}

// TestListTaskScreenshots_MissingDir covers the IsNotExist branch.
func TestListTaskScreenshots_MissingDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	out, err := ListTaskScreenshots("t1")
	if err != nil || out != nil {
		t.Errorf("expected nil,nil for missing dir, got %v,%v", out, err)
	}
}

// TestListTaskScreenshots_ReadDirError makes the screenshots path a file.
func TestListTaskScreenshots_ReadDirError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := TaskScreenshotsDir()
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ListTaskScreenshots("t1"); err == nil {
		t.Fatal("expected ReadDir error when path is a file")
	}
}

// TestListTaskScreenshots_SkipsDirsAndInvalid verifies subdirs / invalid
// filenames / wrong-prefix files are skipped.
func TestListTaskScreenshots_SkipsEntries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := TaskScreenshotsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A subdir, an invalid filename, a valid-but-other-task file, and a match.
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "not-a-screenshot.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other-1700000000.png"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "t1-1700000001.png"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := ListTaskScreenshots("t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || !strings.HasPrefix(out[0], "t1-") {
		t.Errorf("expected one t1 screenshot, got %v", out)
	}
}

// TestHasTaskScreenshot covers the true/false return.
func TestHasTaskScreenshot_Behavior(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if HasTaskScreenshot("t1") {
		t.Error("expected false when no screenshots exist")
	}
	if _, err := SaveTaskScreenshot("t1", "image/png", []byte("x"), 1700000000); err != nil {
		t.Fatal(err)
	}
	if !HasTaskScreenshot("t1") {
		t.Error("expected true after saving a screenshot")
	}
}

// TestPruneOldScreenshots_ReadDirError covers the ReadDir error branch.
func TestPruneOldScreenshots_ReadDirError(t *testing.T) {
	if err := pruneOldScreenshots(filepath.Join(t.TempDir(), "missing"), 5); err == nil {
		t.Fatal("expected ReadDir error for missing dir")
	}
}
