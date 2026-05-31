package office

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRemoveStatusLine_RemovesJarvisOnly verifies RemoveStatusLine deletes
// jarvis-authored statusline/output-style files but leaves operator-authored
// ones, and that a missing dir is a no-op.
func TestRemoveStatusLine_RemovesJarvisOnly(t *testing.T) {
	// Missing .claude dir -> no error.
	if err := RemoveStatusLine(t.TempDir()); err != nil {
		t.Errorf("expected no error for missing dir, got %v", err)
	}

	// jarvis-authored: install via WriteStatusLine, then remove.
	dir := t.TempDir()
	if err := WriteStatusLine(dir, StatusLineParams{SessionID: "s1", DaemonURL: "http://jarvis:8080"}); err != nil {
		t.Fatalf("WriteStatusLine: %v", err)
	}
	scriptPath := filepath.Join(dir, ".claude", statuslineScriptName)
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("expected statusline script present: %v", err)
	}
	if err := RemoveStatusLine(dir); err != nil {
		t.Fatalf("RemoveStatusLine: %v", err)
	}
	if _, err := os.Stat(scriptPath); !os.IsNotExist(err) {
		t.Error("expected jarvis statusline script removed")
	}

	// operator-authored: a script WITHOUT the jarvis marker must be preserved.
	dir2 := t.TempDir()
	claudeDir := filepath.Join(dir2, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	operatorScript := filepath.Join(claudeDir, statuslineScriptName)
	if err := os.WriteFile(operatorScript, []byte("#!/bin/sh\necho mine\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := RemoveStatusLine(dir2); err != nil {
		t.Fatalf("RemoveStatusLine: %v", err)
	}
	if _, err := os.Stat(operatorScript); err != nil {
		t.Error("operator-authored statusline script should be preserved")
	}
}

// TestCleanupExpiredLocks removes only locks whose ExpiresAt is in the past.
func TestCleanupExpiredLocks(t *testing.T) {
	m := newTestManager()
	now := time.Now()
	m.mu.Lock()
	m.fileLocks["expired"] = &FileLock{Path: "expired", ExpiresAt: now.Add(-time.Minute)}
	m.fileLocks["active"] = &FileLock{Path: "active", ExpiresAt: now.Add(time.Hour)}
	m.mu.Unlock()

	removed := m.CleanupExpiredLocks()
	if removed != 1 {
		t.Errorf("expected 1 expired lock removed, got %d", removed)
	}
	m.mu.RLock()
	_, hasExpired := m.fileLocks["expired"]
	_, hasActive := m.fileLocks["active"]
	m.mu.RUnlock()
	if hasExpired {
		t.Error("expired lock should be gone")
	}
	if !hasActive {
		t.Error("active lock should remain")
	}
}

// TestLoadTextFile_ReadErrorPath exercises the non-existent-vs-unreadable
// branches. A missing file returns (",false,false); a directory passed as a
// file path triggers the read-error branch and a stateCorruption broadcast.
func TestLoadTextFile_ReadErrorPath(t *testing.T) {
	dir := t.TempDir()

	// Missing file -> present=false.
	if v, present, loaded := loadTextFile(filepath.Join(dir, "nope.txt"), nil); v != "" || present || loaded {
		t.Errorf("expected (\"\",false,false) for missing file, got (%q,%v,%v)", v, present, loaded)
	}

	// A directory path triggers a non-ENOENT read error -> present=true, loaded=false,
	// and the broadcast fires.
	var broadcasts int
	bc := func(event string, data interface{}) { broadcasts++ }
	subdir := filepath.Join(dir, "adir")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	v, present, loaded := loadTextFile(subdir, bc)
	if !present || loaded {
		t.Errorf("expected (present=true, loaded=false) for a dir path, got (present=%v, loaded=%v, v=%q)", present, loaded, v)
	}
	if broadcasts != 1 {
		t.Errorf("expected one stateCorruption broadcast, got %d", broadcasts)
	}
}

// TestLoadLearningDataUnsafe_QuarantinesCorrupt verifies a malformed learning
// file is quarantined and a fresh default is returned.
func TestLoadLearningDataUnsafe_QuarantinesCorrupt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// Missing file -> default.
	def := loadLearningDataUnsafe("ghost")
	if def == nil || def.ConfigID != "ghost" || def.Learnings == nil {
		t.Fatalf("expected default learning data, got %+v", def)
	}

	// Write a corrupt learning file at the canonical path, then load.
	path := learningPath("vex")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := loadLearningDataUnsafe("vex")
	if got == nil || got.ConfigID != "vex" {
		t.Fatalf("expected fresh default after quarantine, got %+v", got)
	}
	// The corrupt file must be quarantined (renamed away).
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("expected corrupt learning file to be quarantined (renamed)")
	}
	// A *.corrupt-* sibling should exist.
	matches, _ := filepath.Glob(path + ".corrupt-*")
	if len(matches) == 0 {
		t.Error("expected a quarantined .corrupt-* file on disk")
	}
}

// TestLoadTaskScreenshot_ErrorPaths covers the invalid-filename and
// missing-file branches.
func TestLoadTaskScreenshot_ErrorPaths(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := LoadTaskScreenshot("../escape.png"); err == nil {
		t.Error("expected error for invalid screenshot filename")
	}
	// Valid-looking but nonexistent file.
	if _, err := LoadTaskScreenshot("task-1-1700000000.png"); err == nil {
		t.Error("expected error for missing screenshot file")
	}
}

// TestParseEmbedded_NonexistentRootReturnsNil exercises the ReadDir-error
// branch of parseEmbeddedAgents/parseEmbeddedCommands (returns nil, no panic).
func TestParseEmbedded_NonexistentRootReturnsNil(t *testing.T) {
	if got := parseEmbeddedAgents(eccAgentsFS, "does/not/exist"); got != nil {
		t.Errorf("expected nil for nonexistent agents root, got %v", got)
	}
	if got := parseEmbeddedCommands(eccCommandsFS, "does/not/exist"); got != nil {
		t.Errorf("expected nil for nonexistent commands root, got %v", got)
	}
}

// TestAppendSystemPromptPath_VariantSanitized covers the variant branch and
// its filename-sanitization (only alnum + dash survive).
func TestAppendSystemPromptPath_VariantSanitized(t *testing.T) {
	// No variant: canonical name.
	base := AppendSystemPromptPath("/work", "")
	if !strings.HasSuffix(base, ".md") {
		t.Errorf("expected .md path, got %q", base)
	}

	// Variant with unsafe characters gets stripped to alnum/dash.
	got := AppendSystemPromptPath("/work", "co/dex..v2!")
	name := filepath.Base(got)
	if strings.ContainsAny(name, "/.!") && !strings.HasSuffix(name, ".md") {
		t.Errorf("expected sanitized filename, got %q", name)
	}
	if !strings.Contains(name, "codexv2") {
		t.Errorf("expected sanitized variant 'codexv2' in filename, got %q", name)
	}
}

// TestAgentSessionID_EmptyWhenUnset covers the nil-load branch of SessionID.
func TestAgentSessionID_EmptyWhenUnset(t *testing.T) {
	var a AgentState
	if got := a.SessionID(); got != "" {
		t.Errorf("expected empty session ID for zero-value agent, got %q", got)
	}
	a.setSessionID("sess-99")
	if got := a.SessionID(); got != "sess-99" {
		t.Errorf("expected updated session ID, got %q", got)
	}
}
