package office

import (
	"os"
	"path/filepath"
	"testing"
)

// withNewProjectRoot redirects the package's NewProjectRoot at a temp dir
// for one test and restores it afterward.
func withNewProjectRoot(t *testing.T, root string) {
	t.Helper()
	orig := NewProjectRoot
	t.Cleanup(func() { NewProjectRoot = orig })
	NewProjectRoot = root
}

// TestPrepareNewProject_CreatesAndBroadcasts drives the create-dir +
// broadcast branch and the silent-reuse branch on the second call.
func TestPrepareNewProject_CreatesAndBroadcasts(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	withNewProjectRoot(t, root)

	var event string
	bcast := func(e string, _ interface{}) { event = e }

	target, err := PrepareNewProject("myproj", "ship it", bcast)
	if err != nil {
		t.Fatalf("PrepareNewProject: %v", err)
	}
	if target != filepath.Join(root, "myproj") {
		t.Errorf("unexpected target: %q", target)
	}
	if event != "office:projectCreated" {
		t.Errorf("expected office:projectCreated, got %q", event)
	}
	// A CLAUDE.md should have been seeded.
	if _, err := os.Stat(filepath.Join(target, "CLAUDE.md")); err != nil {
		t.Errorf("expected CLAUDE.md seeded: %v", err)
	}

	// Second call → dir already exists → no broadcast.
	event = ""
	if _, err := PrepareNewProject("myproj", "ship it", bcast); err != nil {
		t.Fatal(err)
	}
	if event != "" {
		t.Errorf("re-create should not broadcast, got %q", event)
	}
}

// TestPrepareNewProject_MkdirFails uses a read-only root so the target's
// Stat returns IsNotExist (proj absent) but the subsequent MkdirAll fails
// with EACCES.
func TestPrepareNewProject_MkdirFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: read-only dir would not block MkdirAll")
	}
	root := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	// Read-only root → Stat(root/proj) is IsNotExist, MkdirAll(root/proj) EACCES.
	if err := os.Chmod(root, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })
	withNewProjectRoot(t, root)
	if _, err := PrepareNewProject("proj", "m", nil); err == nil {
		t.Fatal("expected mkdir error")
	}
}

// TestPrepareNewProject_SeedFailsNonFatal drives the seed-error (non-fatal)
// branch: the project dir is created, but CLAUDE.md.tmp is a directory so
// the seed write fails — PrepareNewProject still returns success.
func TestPrepareNewProject_SeedFailsNonFatal(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	target := filepath.Join(root, "seedfail")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	// Pre-create CLAUDE.md.tmp as a directory so SeedNewProjectCLAUDEMd's
	// OpenFile fails — but PrepareNewProject swallows it (non-fatal).
	if err := os.MkdirAll(filepath.Join(target, "CLAUDE.md.tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	withNewProjectRoot(t, root)
	// The dir already exists, so created=false → no broadcast, no error.
	if _, err := PrepareNewProject("seedfail", "m", nil); err != nil {
		t.Fatalf("expected non-fatal seed failure, got %v", err)
	}
}

// TestSeedNewProjectCLAUDEMd_MkdirFails passes a dir whose parent is a file
// so the belt-and-suspenders MkdirAll fails.
func TestSeedNewProjectCLAUDEMd_MkdirFails(t *testing.T) {
	base := t.TempDir()
	parentFile := filepath.Join(base, "pf")
	if err := os.WriteFile(parentFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SeedNewProjectCLAUDEMd(filepath.Join(parentFile, "sub"), "proj", "m"); err == nil {
		t.Fatal("expected mkdir error")
	}
}

// TestSeedNewProjectCLAUDEMd_OpenFileFails makes CLAUDE.md.tmp a directory so
// os.OpenFile fails.
func TestSeedNewProjectCLAUDEMd_OpenFileFails(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "CLAUDE.md.tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SeedNewProjectCLAUDEMd(dir, "proj", "m"); err == nil {
		t.Fatal("expected OpenFile error when CLAUDE.md.tmp is a directory")
	}
}
