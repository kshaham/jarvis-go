package pty

import (
	"context"
	"testing"
)

// TestManager_CreateSession_StartFailure points a ModeClaude session at a
// nonexistent binary so Session.Start fails and CreateSession returns the
// wrapped error (manager.go:58-60).
func TestManager_CreateSession_StartFailure(t *testing.T) {
	m := NewSessionManager(5)
	_, err := m.CreateSession(context.Background(), SessionConfig{
		WorkingDir:       "/tmp",
		Mode:             ModeClaude,
		ClaudeBinaryPath: "/nonexistent/claude-xyz-binary",
	})
	if err == nil {
		t.Fatal("CreateSession should fail when the session cannot start")
	}
	if m.GetSessionCount() != 0 {
		t.Errorf("a failed CreateSession must not register a session; count = %d", m.GetSessionCount())
	}
}

// TestManager_CreateSDKSession_StartFailure forces SDKSession.Start to fail via
// a bad binary so CreateSDKSession returns the wrapped error (manager.go:145-147).
func TestManager_CreateSDKSession_StartFailure(t *testing.T) {
	m := NewSessionManager(5)
	_, err := m.CreateSDKSession(context.Background(), SessionConfig{
		WorkingDir:       t.TempDir(),
		ClaudeBinaryPath: "/nonexistent/claude-xyz-binary",
	})
	if err == nil {
		t.Fatal("CreateSDKSession should fail when the SDK session cannot start")
	}
	if len(m.GetAllSDKSessions()) != 0 {
		t.Error("a failed CreateSDKSession must not register a session")
	}
}

// TestManager_GetAllSessionIDs_AllBackends registers one of each backend kind
// (PTY, SDK, codex) and asserts GetAllSessionIDs returns all three ids,
// exercising the codex + PTY id loops in GetAllSessionIDs (manager.go:215-223)
// that the SDK-only test misses.
func TestManager_GetAllSessionIDs_AllBackends(t *testing.T) {
	m := NewSessionManager(10)
	// PTY session: insert directly into the map (no Start needed for the id loop).
	pty := NewSession(SessionConfig{ID: "pty-1", WorkingDir: "/tmp", Mode: ModeShell})
	m.mu.Lock()
	m.sessions["pty-1"] = pty
	m.codexSessions["codex-1"] = NewCodexSession(SessionConfig{ID: "codex-1"})
	m.mu.Unlock()
	m.RegisterSDKSessionForTest(NewSDKSession(SessionConfig{ID: "sdk-1"}))

	ids := m.GetAllSessionIDs()
	want := map[string]bool{"pty-1": true, "sdk-1": true, "codex-1": true}
	if len(ids) != 3 {
		t.Fatalf("GetAllSessionIDs len = %d, want 3 (%v)", len(ids), ids)
	}
	for _, id := range ids {
		if !want[id] {
			t.Errorf("unexpected id %q in %v", id, ids)
		}
		delete(want, id)
	}
	if len(want) != 0 {
		t.Errorf("missing ids: %v", want)
	}
}

// TestManager_StopAllSessions_AcrossAllBackends registers PTY + SDK + codex
// sessions (no live subprocess) and confirms StopAllSessions tears down all
// three backends and returns nil. Drives the PTY/SDK/codex stop loops
// (manager.go:109-124) on the success side.
func TestManager_StopAllSessions_AcrossAllBackends(t *testing.T) {
	m := NewSessionManager(10)
	pty := NewSession(SessionConfig{ID: "pty-stop", WorkingDir: "/tmp", Mode: ModeShell})
	sdk := NewSDKSession(SessionConfig{ID: "sdk-stop"})
	codex := NewCodexSession(SessionConfig{ID: "codex-stop"})
	m.mu.Lock()
	m.sessions["pty-stop"] = pty
	m.sdkSessions["sdk-stop"] = sdk
	m.codexSessions["codex-stop"] = codex
	m.mu.Unlock()

	if err := m.StopAllSessions(context.Background()); err != nil {
		t.Fatalf("StopAllSessions: %v", err)
	}
	if m.GetSessionCount() != 0 {
		t.Errorf("session count = %d, want 0 after StopAllSessions", m.GetSessionCount())
	}
	if sdk.GetStatus() != StatusStopped {
		t.Error("SDK session should be stopped")
	}
	if codex.GetStatus() != StatusStopped {
		t.Error("Codex session should be stopped")
	}
	if pty.Status() != StatusStopped {
		t.Error("PTY session should be stopped")
	}
}
