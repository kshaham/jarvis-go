package office

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteCommands_EmptyReturnsZero covers the len(cmds)==0 early return.
func TestWriteCommands_EmptyReturnsZero(t *testing.T) {
	n, err := WriteCommands(t.TempDir(), nil)
	if n != 0 || err != nil {
		t.Errorf("expected (0,nil) for empty cmds, got (%d,%v)", n, err)
	}
}

// TestWriteAgents_EmptyReturnsZero covers the len(agents)==0 early return.
func TestWriteAgents_EmptyReturnsZero(t *testing.T) {
	n, err := WriteAgents(t.TempDir(), nil)
	if n != 0 || err != nil {
		t.Errorf("expected (0,nil) for empty agents, got (%d,%v)", n, err)
	}
}

// TestWriteCommands_MkdirFails makes .claude a file so MkdirAll(commands)
// fails.
func TestWriteCommands_MkdirFails(t *testing.T) {
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, ".claude"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := WriteCommands(cwd, []ClaudeCommand{{Name: "c", Body: "b"}})
	if err == nil || !strings.Contains(err.Error(), "mkdir commands") {
		t.Fatalf("expected mkdir error, got %v", err)
	}
}

// TestWriteAgents_MkdirFails makes .claude a file so MkdirAll(agents) fails.
func TestWriteAgents_MkdirFails(t *testing.T) {
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, ".claude"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := WriteAgents(cwd, []ClaudeAgent{{Name: "a", Body: "b"}})
	if err == nil || !strings.Contains(err.Error(), "mkdir agents") {
		t.Fatalf("expected mkdir error, got %v", err)
	}
}

// TestWriteCommands_ReadExistingError makes the destination path a
// directory so os.ReadFile returns a non-NotExist error.
func TestWriteCommands_ReadExistingError(t *testing.T) {
	cwd := t.TempDir()
	dir := filepath.Join(cwd, ".claude", "commands")
	if err := os.MkdirAll(filepath.Join(dir, "cmd.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := WriteCommands(cwd, []ClaudeCommand{{Name: "cmd", Body: "b"}})
	if err == nil || !strings.Contains(err.Error(), "read existing") {
		t.Fatalf("expected read-existing error, got %v", err)
	}
}

// TestWriteAgents_ReadExistingError makes the destination path a directory.
func TestWriteAgents_ReadExistingError(t *testing.T) {
	cwd := t.TempDir()
	dir := filepath.Join(cwd, ".claude", "agents")
	if err := os.MkdirAll(filepath.Join(dir, "ag.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := WriteAgents(cwd, []ClaudeAgent{{Name: "ag", Body: "b"}})
	if err == nil || !strings.Contains(err.Error(), "read existing") {
		t.Fatalf("expected read-existing error, got %v", err)
	}
}

// TestWriteCommands_WriteTmpError makes the .tmp staging path a directory.
func TestWriteCommands_WriteTmpError(t *testing.T) {
	cwd := t.TempDir()
	dir := filepath.Join(cwd, ".claude", "commands")
	if err := os.MkdirAll(filepath.Join(dir, "cmd.md.tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := WriteCommands(cwd, []ClaudeCommand{{Name: "cmd", Body: "b"}})
	if err == nil || !strings.Contains(err.Error(), "write tmp") {
		t.Fatalf("expected write-tmp error, got %v", err)
	}
}

// TestWriteAppendSystemPromptFile_MkdirFails makes .claude a file.
func TestWriteAppendSystemPromptFile_MkdirFails(t *testing.T) {
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, ".claude"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := WriteAppendSystemPromptFile(cwd, "", "content")
	if err == nil || !strings.Contains(err.Error(), "mkdir .claude") {
		t.Fatalf("expected mkdir error, got %v", err)
	}
}

// TestWriteAppendSystemPromptFile_ReadExistingError makes the prompt path
// a directory so os.ReadFile returns a non-NotExist error.
func TestWriteAppendSystemPromptFile_ReadExistingError(t *testing.T) {
	cwd := t.TempDir()
	path := AppendSystemPromptPath(cwd, "")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := WriteAppendSystemPromptFile(cwd, "", "content")
	if err == nil || !strings.Contains(err.Error(), "read existing") {
		t.Fatalf("expected read-existing error, got %v", err)
	}
}

// TestCleanupClaudeFiles_ReadDirError makes .claude/commands a FILE so
// ReadDir returns a non-NotExist error captured as firstErr.
func TestCleanupClaudeFiles_ReadDirError(t *testing.T) {
	cwd := t.TempDir()
	claude := filepath.Join(cwd, ".claude")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	// commands is a FILE → ReadDir ENOTDIR (not IsNotExist).
	if err := os.WriteFile(filepath.Join(claude, "commands"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CleanupClaudeFiles(cwd); err == nil {
		t.Fatal("expected ReadDir error to surface as firstErr")
	}
}

// TestCleanupClaudeFiles_SkipsNonMarkerAndDirs verifies cleanup skips
// subdirectories, non-.md files, and operator-authored (non-marker) .md
// files while removing jarvis-authored ones.
func TestCleanupClaudeFiles_SkipsNonMarker(t *testing.T) {
	cwd := t.TempDir()
	dir := filepath.Join(cwd, ".claude", "commands")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Subdir → skipped.
	if err := os.MkdirAll(filepath.Join(dir, "nested.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Non-.md → skipped.
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Operator-authored (no marker) → kept.
	op := filepath.Join(dir, "operator.md")
	if err := os.WriteFile(op, []byte("hand written"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Jarvis-authored → removed.
	jv := filepath.Join(dir, "jarvis.md")
	if err := os.WriteFile(jv, []byte(jarvisFileMarker+"\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CleanupClaudeFiles(cwd); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if _, err := os.Stat(op); err != nil {
		t.Error("operator file should be kept")
	}
	if _, err := os.Stat(jv); !os.IsNotExist(err) {
		t.Error("jarvis file should be removed")
	}
}

// TestCleanupClaudeFiles_RemoveError covers the os.Remove failure branch:
// a jarvis-authored file in a read-only directory can be read but not
// removed, so the Remove error is captured as firstErr.
func TestCleanupClaudeFiles_RemoveError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: read-only dir would not block Remove")
	}
	cwd := t.TempDir()
	dir := filepath.Join(cwd, ".claude", "commands")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	jv := filepath.Join(dir, "jarvis.md")
	if err := os.WriteFile(jv, []byte(jarvisFileMarker+"\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Read-only dir: ReadFile(jv) still works, but os.Remove(jv) fails (EACCES).
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	if err := CleanupClaudeFiles(cwd); err == nil {
		t.Fatal("expected Remove error to surface as firstErr")
	}
}

// TestRenderCommand_WithModel covers the c.Model != "" branch.
func TestRenderCommand_WithModel(t *testing.T) {
	out := renderCommand(ClaudeCommand{Name: "c", Body: "do it", Model: "sonnet"})
	if !strings.Contains(out, "model: sonnet") {
		t.Errorf("expected model line, got:\n%s", out)
	}
}

// TestYamlScalar_Empty covers the empty-string branch.
func TestYamlScalar_Empty(t *testing.T) {
	if got := yamlScalar(""); got != `""` {
		t.Errorf("expected quoted empty string, got %q", got)
	}
}
