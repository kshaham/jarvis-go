package office

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validStatusParams() StatusLineParams {
	return StatusLineParams{SessionID: "sess-1", DaemonURL: "http://jarvis:8765"}
}

// TestWriteStatusLine_EmptyCwd covers the ErrNoCwd guard.
func TestWriteStatusLine_EmptyCwd(t *testing.T) {
	if err := WriteStatusLine("", validStatusParams()); err != ErrNoCwd {
		t.Fatalf("expected ErrNoCwd, got %v", err)
	}
}

// TestWriteStatusLine_MkdirFails makes .claude a file.
func TestWriteStatusLine_MkdirFails(t *testing.T) {
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, ".claude"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteStatusLine(cwd, validStatusParams()); err == nil || !strings.Contains(err.Error(), "mkdir .claude") {
		t.Fatalf("expected mkdir error, got %v", err)
	}
}

// TestWriteStatusLine_ScriptReadError makes the script path a directory so
// the read-existing check errors.
func TestWriteStatusLine_ScriptReadError(t *testing.T) {
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, ".claude", statuslineScriptName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteStatusLine(cwd, validStatusParams()); err == nil || !strings.Contains(err.Error(), "read existing statusline") {
		t.Fatalf("expected read-existing statusline error, got %v", err)
	}
}

// TestWriteStatusLine_StyleReadError makes the output-style path a directory
// (script writes fine first, then style read errors).
func TestWriteStatusLine_StyleReadError(t *testing.T) {
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, ".claude", outputStyleFileName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteStatusLine(cwd, validStatusParams()); err == nil || !strings.Contains(err.Error(), "read existing output-style") {
		t.Fatalf("expected read-existing output-style error, got %v", err)
	}
}

// TestWriteStatusLine_ScriptWriteFails makes the script .tmp path a dir so
// writeFileAtomic0755 fails.
func TestWriteStatusLine_ScriptWriteFails(t *testing.T) {
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, ".claude", statuslineScriptName+".tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteStatusLine(cwd, validStatusParams()); err == nil || !strings.Contains(err.Error(), "write tmp") {
		t.Fatalf("expected write-tmp error, got %v", err)
	}
}

// TestWriteFileAtomic_RenameFails makes the target a non-empty directory so
// os.Rename fails inside both writeFileAtomic helpers.
func TestWriteFileAtomic_RenameFails(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "out")
	if err := os.MkdirAll(filepath.Join(target, "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic0755(target, []byte("data")); err == nil || !strings.Contains(err.Error(), "rename") {
		t.Fatalf("expected 0755 rename error, got %v", err)
	}
	if err := writeFileAtomic0644(target, []byte("data")); err == nil || !strings.Contains(err.Error(), "rename") {
		t.Fatalf("expected 0644 rename error, got %v", err)
	}
}

// TestWriteFileAtomic_WriteFails makes the .tmp path a directory.
func TestWriteFileAtomic_WriteFails(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "out")
	if err := os.MkdirAll(target+".tmp", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic0755(target, []byte("data")); err == nil || !strings.Contains(err.Error(), "write tmp") {
		t.Fatalf("expected 0755 write error, got %v", err)
	}
	if err := writeFileAtomic0644(target, []byte("data")); err == nil || !strings.Contains(err.Error(), "write tmp") {
		t.Fatalf("expected 0644 write error, got %v", err)
	}
}

// TestRemoveStatusLine_ReadError makes the script path a directory so the
// read returns a non-NotExist error.
func TestRemoveStatusLine_ReadError(t *testing.T) {
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, ".claude", statuslineScriptName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := RemoveStatusLine(cwd); err == nil {
		t.Fatal("expected read error when script path is a directory")
	}
}

// TestRemoveStatusLine_MissingFilesNoError verifies absent files are a
// no-op (the IsNotExist continue branch).
func TestRemoveStatusLine_MissingFiles(t *testing.T) {
	cwd := t.TempDir()
	if err := RemoveStatusLine(cwd); err != nil {
		t.Fatalf("expected no error for missing files, got %v", err)
	}
}
