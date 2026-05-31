package office

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadTaskScreenshot_EscapeRejected drives the HasPrefix containment
// rejection (task_screenshot.go:124-126): a valid-named symlink whose
// target lives OUTSIDE the screenshots dir resolves successfully but
// fails the prefix check.
func TestLoadTaskScreenshot_EscapeRejected(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := TaskScreenshotsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(home, "secret.png")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	name := "t1-1700000000.png"
	link := filepath.Join(dir, name)
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	_, err := LoadTaskScreenshot(name)
	if err == nil || !strings.Contains(err.Error(), "escaped") {
		t.Fatalf("expected escape rejection, got %v", err)
	}
}

// TestLoadTaskScreenshot_DirResolveFails drives task_screenshot.go:121-122:
// the FILE path resolves but EvalSymlinks(dir) errors. This is the
// TOCTOU-shaped branch — the file existed long enough to resolve, then
// the directory link broke before the second resolve. We force it with
// the evalSymlinks seam: succeed for the file path, fail for the dir.
func TestLoadTaskScreenshot_DirResolveFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := TaskScreenshotsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "t1-1700000000.png"
	full := filepath.Join(dir, name)
	if err := os.WriteFile(full, []byte("png-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	sentinel := errors.New("simulated broken dir symlink")
	prev := evalSymlinks
	evalSymlinks = func(p string) (string, error) {
		if p == dir {
			return "", sentinel // the second resolve (dir) fails
		}
		return prev(p) // the file path resolves normally
	}
	t.Cleanup(func() { evalSymlinks = prev })

	_, err := LoadTaskScreenshot(name)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected the dir-resolve sentinel error, got %v", err)
	}
}

// TestLoadWrapUpReport_DirResolveFails drives report.go:200-201 with the
// same seam approach.
func TestLoadWrapUpReport_DirResolveFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := WrapUpReportsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "wrap-up-20260101-120000.md"
	full := filepath.Join(dir, name)
	if err := os.WriteFile(full, []byte("# report"), 0o644); err != nil {
		t.Fatal(err)
	}

	sentinel := errors.New("simulated broken reports dir symlink")
	prev := evalSymlinks
	evalSymlinks = func(p string) (string, error) {
		if p == dir {
			return "", sentinel
		}
		return prev(p)
	}
	t.Cleanup(func() { evalSymlinks = prev })

	_, err := LoadWrapUpReport(name)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected the dir-resolve sentinel error, got %v", err)
	}
}

// TestLoadWrapUpReport_ReadFileFails drives report.go:206-208: the path
// resolves AND is in-bounds, but the final read fails. Forced via the
// readFile seam so we don't depend on platform-specific unreadable-file
// behaviour.
func TestLoadWrapUpReport_ReadFileFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := WrapUpReportsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "wrap-up-20260101-120000.md"
	full := filepath.Join(dir, name)
	if err := os.WriteFile(full, []byte("# report body"), 0o644); err != nil {
		t.Fatal(err)
	}

	sentinel := errors.New("simulated read failure")
	prev := readFile
	readFile = func(string) ([]byte, error) { return nil, sentinel }
	t.Cleanup(func() { readFile = prev })

	_, err := LoadWrapUpReport(name)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected the read-failure sentinel error, got %v", err)
	}
}
