package office

import (
	"archive/zip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// zipEntry describes one file to place in a test archive. A trailing
// slash in name marks a directory; symlink set makes it a symlink entry.
type zipEntry struct {
	name    string
	body    string
	symlink bool
}

// makeZip writes a zip with the given entries to a temp file and returns
// its path.
func makeZip(t *testing.T, entries []zipEntry) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "archive.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for _, e := range entries {
		hdr := &zip.FileHeader{Name: e.name}
		switch {
		case e.symlink:
			hdr.SetMode(os.ModeSymlink | 0o777)
		case len(e.name) > 0 && e.name[len(e.name)-1] == '/':
			hdr.SetMode(os.ModeDir | 0o755)
		default:
			hdr.SetMode(0o644)
		}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatalf("create header %q: %v", e.name, err)
		}
		if e.body != "" {
			if _, err := w.Write([]byte(e.body)); err != nil {
				t.Fatalf("write %q: %v", e.name, err)
			}
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return path
}

func TestExtractZipHappyPath(t *testing.T) {
	zipPath := makeZip(t, []zipEntry{
		{name: "main.go", body: "package main"},
		{name: "pkg/"},
		{name: "pkg/util.go", body: "package pkg"},
	})
	target := t.TempDir()
	if err := extractZip(zipPath, target); err != nil {
		t.Fatalf("extractZip: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(target, "main.go")); err != nil || string(b) != "package main" {
		t.Errorf("main.go: got %q err %v", b, err)
	}
	if b, err := os.ReadFile(filepath.Join(target, "pkg", "util.go")); err != nil || string(b) != "package pkg" {
		t.Errorf("pkg/util.go: got %q err %v", b, err)
	}
}

func TestExtractZipRejectsZipSlip(t *testing.T) {
	for _, bad := range []string{"../evil.txt", "../../etc/passwd", "/abs/evil"} {
		zipPath := makeZip(t, []zipEntry{{name: bad, body: "x"}})
		target := t.TempDir()
		err := extractZip(zipPath, target)
		if err == nil {
			t.Errorf("%q: expected error, got nil", bad)
		}
		// Nothing must be written outside target (sibling of the temp dir).
		if _, statErr := os.Stat(filepath.Join(filepath.Dir(target), "evil.txt")); statErr == nil {
			t.Errorf("%q: file escaped target", bad)
		}
	}
}

func TestExtractZipSkipsSymlink(t *testing.T) {
	zipPath := makeZip(t, []zipEntry{
		{name: "link", body: "/etc/passwd", symlink: true},
		{name: "real.txt", body: "ok"},
	})
	target := t.TempDir()
	if err := extractZip(zipPath, target); err != nil {
		t.Fatalf("extractZip: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(target, "link")); err == nil {
		t.Errorf("symlink entry was materialized")
	}
	if _, err := os.Stat(filepath.Join(target, "real.txt")); err != nil {
		t.Errorf("regular file alongside symlink missing: %v", err)
	}
}

func TestExtractZipFileCountLimit(t *testing.T) {
	old := maxExtractedFiles
	maxExtractedFiles = 2
	defer func() { maxExtractedFiles = old }()

	zipPath := makeZip(t, []zipEntry{
		{name: "a.txt", body: "a"},
		{name: "b.txt", body: "b"},
		{name: "c.txt", body: "c"},
	})
	if err := extractZip(zipPath, t.TempDir()); !errors.Is(err, ErrZipTooLarge) {
		t.Errorf("expected ErrZipTooLarge, got %v", err)
	}
}

func TestExtractZipByteLimit(t *testing.T) {
	old := maxExtractedBytes
	maxExtractedBytes = 4
	defer func() { maxExtractedBytes = old }()

	zipPath := makeZip(t, []zipEntry{{name: "big.txt", body: "way too many bytes"}})
	if err := extractZip(zipPath, t.TempDir()); !errors.Is(err, ErrZipTooLarge) {
		t.Errorf("expected ErrZipTooLarge, got %v", err)
	}
}

func TestPrepareZipProjectInvalidSlug(t *testing.T) {
	zipPath := makeZip(t, []zipEntry{{name: "a.txt", body: "a"}})
	if _, err := PrepareZipProject("../evil", zipPath, noopGit, nil); !errors.Is(err, ErrInvalidProjectName) {
		t.Errorf("expected ErrInvalidProjectName, got %v", err)
	}
}

func TestPrepareZipProjectExists(t *testing.T) {
	root := t.TempDir()
	oldRoot := NewProjectRoot
	NewProjectRoot = root
	defer func() { NewProjectRoot = oldRoot }()

	// Pre-populate the target so the upload refuses to merge.
	existing := filepath.Join(root, "myproj")
	if err := os.MkdirAll(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(existing, "keep.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	zipPath := makeZip(t, []zipEntry{{name: "a.txt", body: "a"}})
	if _, err := PrepareZipProject("myproj", zipPath, noopGit, nil); !errors.Is(err, ErrProjectExists) {
		t.Errorf("expected ErrProjectExists, got %v", err)
	}
}

func TestPrepareZipProjectStripsSingleRoot(t *testing.T) {
	root := t.TempDir()
	oldRoot := NewProjectRoot
	NewProjectRoot = root
	defer func() { NewProjectRoot = oldRoot }()

	// GitHub "Download ZIP" layout: everything under "<repo>-main/".
	zipPath := makeZip(t, []zipEntry{
		{name: "repo-main/"},
		{name: "repo-main/README.md", body: "hi"},
		{name: "repo-main/src/"},
		{name: "repo-main/src/app.go", body: "code"},
	})
	target, err := PrepareZipProject("proj", zipPath, noopGit, nil)
	if err != nil {
		t.Fatalf("PrepareZipProject: %v", err)
	}
	// The wrapper dir is gone; code sits at the project root.
	if _, err := os.Stat(filepath.Join(target, "repo-main")); err == nil {
		t.Errorf("wrapper dir was not stripped")
	}
	if b, err := os.ReadFile(filepath.Join(target, "README.md")); err != nil || string(b) != "hi" {
		t.Errorf("README.md at root: got %q err %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(target, "src", "app.go")); err != nil {
		t.Errorf("nested file missing after strip: %v", err)
	}
}

func TestPrepareZipProjectGitInit(t *testing.T) {
	root := t.TempDir()
	oldRoot := NewProjectRoot
	NewProjectRoot = root
	defer func() { NewProjectRoot = oldRoot }()

	seen := map[string]bool{}
	run := func(_ context.Context, _ string, args ...string) error {
		for _, a := range args {
			seen[a] = true
		}
		return nil
	}

	zipPath := makeZip(t, []zipEntry{{name: "main.go", body: "package main"}})
	if _, err := PrepareZipProject("proj", zipPath, run, nil); err != nil {
		t.Fatalf("PrepareZipProject: %v", err)
	}
	for _, want := range []string{"init", "add", "commit"} {
		if !seen[want] {
			t.Errorf("expected git %q to be invoked", want)
		}
	}
}

func TestPrepareZipProjectSkipsGitInitWhenRepoPresent(t *testing.T) {
	root := t.TempDir()
	oldRoot := NewProjectRoot
	NewProjectRoot = root
	defer func() { NewProjectRoot = oldRoot }()

	// Archive already carries a .git directory.
	zipPath := makeZip(t, []zipEntry{
		{name: ".git/"},
		{name: ".git/config", body: "[core]"},
		{name: "main.go", body: "package main"},
	})
	called := false
	run := func(_ context.Context, _ string, _ ...string) error {
		called = true
		return nil
	}
	if _, err := PrepareZipProject("proj", zipPath, run, nil); err != nil {
		t.Fatalf("PrepareZipProject: %v", err)
	}
	if called {
		t.Errorf("git init ran even though archive carried .git")
	}
}

// noopGit is a GitRunner that records nothing and always succeeds.
func noopGit(_ context.Context, _ string, _ ...string) error { return nil }
