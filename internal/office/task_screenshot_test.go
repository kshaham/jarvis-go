package office

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateTaskScreenshotFilename(t *testing.T) {
	cases := []struct {
		name string
		ok   bool
	}{
		{"task-123-1700000000.png", true},
		{"t-abc-1700000000.jpg", true},
		{"t1-1700000000.webp", true},
		{"t1-1700000000.jpeg", true},
		{"../etc/passwd", false},
		{"t1-1700000000.gif", false},
		{"t1-abc.png", false},
		{"-t1-1700000000.png", false},
		{"", false},
	}
	for _, c := range cases {
		if got := ValidateTaskScreenshotFilename(c.name); got != c.ok {
			t.Errorf("Validate(%q) = %v, want %v", c.name, got, c.ok)
		}
	}
}

// tinyPNG is a 1x1 transparent PNG. Used for upload happy paths.
var tinyPNG = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
	0x89, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE,
	0x42, 0x60, 0x82,
}

func TestSaveTaskScreenshot_HappyPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name, err := SaveTaskScreenshot("task-1", "image/png", tinyPNG, 1700000000)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if name != "task-1-1700000000.png" {
		t.Errorf("filename = %q, want task-1-1700000000.png", name)
	}
	got, err := LoadTaskScreenshot(name)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !bytes.Equal(got, tinyPNG) {
		t.Error("loaded bytes do not match saved bytes")
	}
}

func TestSaveTaskScreenshot_OversizeRejected(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	big := make([]byte, TaskScreenshotMaxBytes+1)
	_, err := SaveTaskScreenshot("task-1", "image/png", big, 1700000000)
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("expected too-large error, got %v", err)
	}
}

func TestSaveTaskScreenshot_UnsupportedMime(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_, err := SaveTaskScreenshot("task-1", "image/gif", tinyPNG, 1700000000)
	if err == nil || !strings.Contains(err.Error(), "unsupported mime") {
		t.Fatalf("expected unsupported-mime error, got %v", err)
	}
}

func TestSaveTaskScreenshot_EmptyTaskID(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_, err := SaveTaskScreenshot("", "image/png", tinyPNG, 1700000000)
	if err == nil {
		t.Fatal("expected error for empty taskID")
	}
}

func TestSaveTaskScreenshot_InvalidTaskID(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_, err := SaveTaskScreenshot("../escape", "image/png", tinyPNG, 1700000000)
	if err == nil {
		t.Fatal("expected error for path-traversal taskID")
	}
}

func TestListTaskScreenshots_Filters(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := SaveTaskScreenshot("task-1", "image/png", tinyPNG, 1700000001); err != nil {
		t.Fatalf("save 1: %v", err)
	}
	if _, err := SaveTaskScreenshot("task-1", "image/png", tinyPNG, 1700000002); err != nil {
		t.Fatalf("save 2: %v", err)
	}
	if _, err := SaveTaskScreenshot("task-2", "image/png", tinyPNG, 1700000003); err != nil {
		t.Fatalf("save 3 (other task): %v", err)
	}
	list, err := ListTaskScreenshots("task-1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 for task-1, got %d (%v)", len(list), list)
	}
	// Newest first.
	if list[0] != "task-1-1700000002.png" {
		t.Errorf("expected newest-first ordering; got %v", list)
	}
	if !HasTaskScreenshot("task-1") {
		t.Error("HasTaskScreenshot should return true for task-1")
	}
	if HasTaskScreenshot("task-unknown") {
		t.Error("HasTaskScreenshot should return false for unknown task")
	}
}

func TestPruneOldScreenshots(t *testing.T) {
	dir := t.TempDir()
	// Seed 7 valid files with increasing timestamps.
	for i := int64(0); i < 7; i++ {
		name := filepath.Join(dir, "t1-170000000"+string('0'+byte(i))+".png")
		if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	if err := pruneOldScreenshots(dir, 3); err != nil {
		t.Fatalf("prune: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 3 {
		t.Fatalf("expected 3 files after prune, got %d", len(entries))
	}
}
