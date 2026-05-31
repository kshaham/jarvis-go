package office

import (
	"errors"
	"io"
	"strings"
	"testing"
)

// TestAppendAgentMemory_WriteError drives the f.Write error branch in
// AppendAgentMemory (manager.go) via the agentMemoryOpenFile seam: a
// writer whose Write fails (disk full / EIO) surfaces the error after
// closing the file.
func TestAppendAgentMemory_WriteError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	prev := agentMemoryOpenFile
	t.Cleanup(func() { agentMemoryOpenFile = prev })
	agentMemoryOpenFile = func(string) (io.WriteCloser, error) {
		return &failWriteCloser{failWrite: true}, nil
	}
	wd := t.TempDir()
	if err := AppendAgentMemory("cfg", wd, "entry\n"); err == nil {
		t.Fatal("expected write error from the injected writer")
	}
}

// TestAppendAgentMemory_CloseError drives the f.Close error branch:
// Write succeeds, Close fails, and the error is wrapped + surfaced.
func TestAppendAgentMemory_CloseError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	prev := agentMemoryOpenFile
	t.Cleanup(func() { agentMemoryOpenFile = prev })
	agentMemoryOpenFile = func(string) (io.WriteCloser, error) {
		return &failWriteCloser{failClose: true}, nil
	}
	wd := t.TempDir()
	err := AppendAgentMemory("cfg", wd, "entry\n")
	if err == nil {
		t.Fatal("expected close error to surface")
	}
	if !strings.Contains(err.Error(), "close agent memory") {
		t.Errorf("expected wrapped close error, got %v", err)
	}
}

// TestAppendAgentMemory_OpenSeamError drives the seam open-failure path.
func TestAppendAgentMemory_OpenSeamError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	prev := agentMemoryOpenFile
	t.Cleanup(func() { agentMemoryOpenFile = prev })
	want := errors.New("simulated open failure")
	agentMemoryOpenFile = func(string) (io.WriteCloser, error) {
		return nil, want
	}
	wd := t.TempDir()
	if err := AppendAgentMemory("cfg", wd, "entry\n"); !errors.Is(err, want) {
		t.Fatalf("expected the injected open error, got %v", err)
	}
}
