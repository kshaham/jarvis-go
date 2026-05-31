package office

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// memPathAsDir creates the agent's memory path as a directory so ReadFile /
// Open on it return a non-NotExist error.
func memPathAsDir(t *testing.T, configID, workdir string) {
	t.Helper()
	path := GetAgentMemoryPath(configID, workdir)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

// TestMemoryReadSection_LoadError covers the load (ReadFile) error branch.
func TestMemoryReadSection_LoadError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	wd := "/proj/mem-read-err"
	memPathAsDir(t, "cfg", wd)
	if _, err := MemoryReadSection("cfg", wd, "notes"); err == nil {
		t.Fatal("expected load error when memory path is a directory")
	}
}

// TestMemoryWriteSection_LoadError covers the load error in write.
func TestMemoryWriteSection_LoadError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	wd := "/proj/mem-write-err"
	memPathAsDir(t, "cfg", wd)
	if err := MemoryWriteSection("cfg", wd, "notes", "x"); err == nil {
		t.Fatal("expected load error in MemoryWriteSection")
	}
}

// TestMemoryAppendSection_LoadError covers the load error in append.
func TestMemoryAppendSection_LoadError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	wd := "/proj/mem-append-err"
	memPathAsDir(t, "cfg", wd)
	if err := MemoryAppendSection("cfg", wd, "notes", "x"); err == nil {
		t.Fatal("expected load error in MemoryAppendSection")
	}
}

// TestMemoryQuery_OpenError covers the non-NotExist open error.
func TestMemoryQuery_OpenError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	wd := "/proj/mem-query-err"
	memPathAsDir(t, "cfg", wd)
	if _, err := MemoryQuery("cfg", wd, "needle"); err == nil {
		t.Fatal("expected open error when memory path is a directory")
	}
}

// TestMemoryQuery_TrimsToMaxHits writes more than maxMemoryQueryHits
// matching lines and asserts the result is trimmed.
func TestMemoryQuery_TrimsToMaxHits(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	wd := "/proj/mem-query-trim"
	if err := os.MkdirAll(filepath.Dir(GetAgentMemoryPath("cfg", wd)), 0o755); err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	sb.WriteString("## section: notes\n")
	for i := 0; i < maxMemoryQueryHits+10; i++ {
		fmt.Fprintf(&sb, "needle line %d\n", i)
	}
	if err := os.WriteFile(GetAgentMemoryPath("cfg", wd), []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	hits, err := MemoryQuery("cfg", wd, "needle")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != maxMemoryQueryHits {
		t.Errorf("expected trim to %d hits, got %d", maxMemoryQueryHits, len(hits))
	}
}

// TestLoadMemorySections_PreludeMergesIntoNotes drives the prelude→notes
// merge branch: a memory file with leading prose AND an explicit notes
// section so the prelude is prepended to the existing notes.
func TestLoadMemorySections_PreludeMergesIntoNotes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	wd := "/proj/mem-prelude"
	path := GetAgentMemoryPath("cfg", wd)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	// Leading prose (becomes prelude) + an explicit notes section.
	body := "# Memory: cfg\n\nprelude line one\nprelude line two\n\n## section: notes\nexisting note\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := MemoryReadSection("cfg", wd, "notes")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "prelude line one") || !strings.Contains(got, "existing note") {
		t.Errorf("expected prelude merged with notes, got %q", got)
	}
}

// TestWriteMemorySections_WriteFails makes the memory file's .tmp path a
// directory so os.WriteFile inside writeMemorySections fails (load returns
// an empty section map first because the real file doesn't exist).
func TestWriteMemorySections_WriteFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	wd := "/proj/mem-write-fail"
	path := GetAgentMemoryPath("cfg", wd)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	// Pre-create the .tmp path as a directory → WriteFile fails (EISDIR).
	if err := os.MkdirAll(path+".tmp", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := MemoryWriteSection("cfg", wd, "notes", "content"); err == nil {
		t.Fatal("expected WriteFile error when .tmp path is a directory")
	}
}
