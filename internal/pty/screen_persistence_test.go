package pty

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestShellQuote(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"hello", "'hello'"},
		{"with space", "'with space'"},
		{"it's", `'it'\''s'`},      // single quote escaped
		{"", "''"},                 // empty -> two quotes
		{"a'b'c", `'a'\''b'\''c'`}, // multiple quotes
	}
	for _, c := range cases {
		if got := shellQuote(c.in); got != c.want {
			t.Errorf("shellQuote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIsProcessAlive(t *testing.T) {
	// Current process is alive.
	if !isProcessAlive(os.Getpid()) {
		t.Error("current process should be reported alive")
	}
	// PID 0 / negative are not real live processes on macOS/Linux for
	// the signal-0 check path used here.
	if isProcessAlive(-1) {
		t.Error("invalid pid -1 should not be alive")
	}
}

func TestNewScreenManager_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	sm := NewScreenManager(dir)
	if sm == nil {
		t.Fatal("NewScreenManager returned nil")
	}
	if sm.GetScreenCount() != 0 {
		t.Errorf("expected 0 screens for empty dir, got %d", sm.GetScreenCount())
	}
	if len(sm.GetScreens()) != 0 {
		t.Error("GetScreens should be empty")
	}
	if _, ok := sm.GetScreen("anything"); ok {
		t.Error("GetScreen should miss on empty manager")
	}
}

func TestScreenManager_SaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	sm := NewScreenManager(dir)
	// Insert a screen entry directly, then persist.
	sm.mu.Lock()
	sm.screens["sess-1"] = &ScreenSession{
		SessionID:  "sess-1",
		ScreenName: "jarvis-sess-1",
		PID:        os.Getpid(), // alive so purgeOrphans keeps it
		CreatedAt:  time.Now(),
		WorkingDir: "/work",
		Mode:       ModeClaude,
		Name:       "agent-a",
	}
	sm.mu.Unlock()
	sm.saveScreens()

	// File should exist and contain the entry.
	data, err := os.ReadFile(filepath.Join(dir, "screens.json"))
	if err != nil {
		t.Fatalf("screens.json not written: %v", err)
	}
	var persisted []*ScreenSession
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("persisted json invalid: %v", err)
	}
	if len(persisted) != 1 || persisted[0].SessionID != "sess-1" {
		t.Fatalf("persisted = %+v, want one entry sess-1", persisted)
	}

	// A fresh manager over the same dir loads the entry (PID alive ->
	// survives purgeOrphans).
	sm2 := NewScreenManager(dir)
	got, ok := sm2.GetScreen("sess-1")
	if !ok {
		t.Fatal("reloaded manager should find sess-1")
	}
	if got.Name != "agent-a" || got.WorkingDir != "/work" {
		t.Errorf("reloaded screen = %+v, want name agent-a wd /work", got)
	}
	if sm2.GetScreenCount() != 1 {
		t.Errorf("reloaded count = %d, want 1", sm2.GetScreenCount())
	}
}

func TestScreenManager_PurgeOrphans_DropsDeadPIDs(t *testing.T) {
	dir := t.TempDir()
	// Hand-write a screens.json with a dead PID (very high, unlikely to exist).
	deadEntry := []*ScreenSession{{
		SessionID:  "dead-1",
		ScreenName: "jarvis-dead-1",
		PID:        2000000000, // not a live process
		CreatedAt:  time.Now(),
		Mode:       ModeClaude,
	}}
	data, _ := json.MarshalIndent(deadEntry, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "screens.json"), data, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	// NewScreenManager runs loadScreens then purgeOrphans, which should
	// drop the dead-PID entry.
	sm := NewScreenManager(dir)
	if _, ok := sm.GetScreen("dead-1"); ok {
		t.Error("purgeOrphans should have removed the dead-PID screen")
	}
	if sm.GetScreenCount() != 0 {
		t.Errorf("count = %d, want 0 after purging orphans", sm.GetScreenCount())
	}
}

func TestScreenManager_LoadScreens_CorruptJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "screens.json"), []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("write corrupt fixture: %v", err)
	}
	// Must not panic; corrupt file is treated as no screens.
	sm := NewScreenManager(dir)
	if sm.GetScreenCount() != 0 {
		t.Errorf("corrupt file should yield 0 screens, got %d", sm.GetScreenCount())
	}
}
