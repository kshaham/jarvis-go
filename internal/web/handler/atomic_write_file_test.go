package handler

// Direct unit coverage for atomicWriteFile's error branches. The happy path is
// exercised via the plan-file handlers; here we force the failure modes that
// those handlers can't easily trigger: a non-existent parent directory makes
// os.CreateTemp fail, and a read-only parent directory makes the temp-file
// creation fail too. Both must return the underlying error without leaving a
// partial file behind.

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAtomicWriteFile_HappyRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plan.md")
	if err := atomicWriteFile(path, []byte("hello plan"), 0o644); err != nil {
		t.Fatalf("atomicWriteFile: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "hello plan" {
		t.Fatalf("readback=%q err=%v", string(got), err)
	}
	// Mode should match what we requested.
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o644 {
		t.Errorf("mode=%v, want 0644", info.Mode().Perm())
	}
}

func TestAtomicWriteFile_CreateTempFailsMissingDir(t *testing.T) {
	// Parent directory does not exist -> os.CreateTemp(dir, ...) fails.
	missing := filepath.Join(t.TempDir(), "does", "not", "exist", "plan.md")
	err := atomicWriteFile(missing, []byte("x"), 0o644)
	if err == nil {
		t.Fatal("expected error when parent dir is missing")
	}
	// Nothing should have been created.
	if _, statErr := os.Stat(missing); !os.IsNotExist(statErr) {
		t.Error("no file should exist after a failed atomic write")
	}
}

func TestAtomicWriteFile_CreateTempFailsReadOnlyDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file-mode semantics")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory write permissions")
	}
	dir := t.TempDir()
	// Strip write permission so CreateTemp can't make its temp file.
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	err := atomicWriteFile(filepath.Join(dir, "plan.md"), []byte("x"), 0o644)
	if err == nil {
		t.Fatal("expected error writing into a read-only directory")
	}
}
