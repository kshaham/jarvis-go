package office

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteAppendSystemPromptFile_WritesAndReturnsPath(t *testing.T) {
	dir := t.TempDir()
	got, err := WriteAppendSystemPromptFile(dir, "stable kickstart content")
	if err != nil {
		t.Fatalf("WriteAppendSystemPromptFile: %v", err)
	}
	want := AppendSystemPromptPath(dir)
	if got != want {
		t.Errorf("returned path %q, want %q", got, want)
	}
	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.HasPrefix(string(data), jarvisFileMarker) {
		t.Errorf("file should start with jarvis marker: %q", string(data)[:40])
	}
	if !strings.Contains(string(data), "stable kickstart content") {
		t.Error("file should contain the supplied content")
	}
}

func TestWriteAppendSystemPromptFile_EmptyContentRejected(t *testing.T) {
	_, err := WriteAppendSystemPromptFile(t.TempDir(), "")
	if err == nil {
		t.Fatal("expected error for empty content")
	}
}

func TestWriteAppendSystemPromptFile_EmptyCwdRejected(t *testing.T) {
	_, err := WriteAppendSystemPromptFile("", "x")
	if err == nil {
		t.Fatal("expected error for empty cwd")
	}
}

func TestWriteAppendSystemPromptFile_LeavesOperatorAuthored(t *testing.T) {
	dir := t.TempDir()
	target := AppendSystemPromptPath(dir)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	operator := []byte("operator wrote this -- jarvis must not clobber")
	if err := os.WriteFile(target, operator, 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, err := WriteAppendSystemPromptFile(dir, "jarvis content")
	if err != nil {
		t.Fatalf("WriteAppendSystemPromptFile: %v", err)
	}
	if got != target {
		t.Errorf("expected operator path returned, got %q", got)
	}
	final, _ := os.ReadFile(target)
	if !strings.Contains(string(final), "operator wrote this") {
		t.Errorf("operator file was overwritten: %q", final)
	}
}

func TestWriteAppendSystemPromptFile_OverwritesJarvisAuthored(t *testing.T) {
	dir := t.TempDir()
	if _, err := WriteAppendSystemPromptFile(dir, "first content"); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if _, err := WriteAppendSystemPromptFile(dir, "second content"); err != nil {
		t.Fatalf("second write: %v", err)
	}
	final, _ := os.ReadFile(AppendSystemPromptPath(dir))
	if !strings.Contains(string(final), "second content") {
		t.Errorf("jarvis-authored file should have been overwritten: %q", final)
	}
}
