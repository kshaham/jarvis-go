package pty

import (
	"context"
	"testing"
)

func TestManager_RegisterAndGetSDKSession(t *testing.T) {
	m := NewSessionManager(5)
	sess := NewSDKSession(SessionConfig{ID: "sdk-abc", Name: "alpha"})
	m.RegisterSDKSessionForTest(sess)

	got, ok := m.GetSDKSession("sdk-abc")
	if !ok {
		t.Fatal("expected to find registered SDK session")
	}
	if got.ID != "sdk-abc" {
		t.Errorf("ID = %q, want sdk-abc", got.ID)
	}

	_, ok = m.GetSDKSession("nonexistent")
	if ok {
		t.Error("expected miss for unknown SDK session id")
	}
}

func TestManager_RegisterSDKSession_NilIgnored(t *testing.T) {
	m := NewSessionManager(5)
	m.RegisterSDKSessionForTest(nil) // must not panic or insert
	if len(m.GetAllSDKSessions()) != 0 {
		t.Error("nil registration should not add a session")
	}
}

func TestManager_GetAllSDKSessions(t *testing.T) {
	m := NewSessionManager(5)
	m.RegisterSDKSessionForTest(NewSDKSession(SessionConfig{ID: "a"}))
	m.RegisterSDKSessionForTest(NewSDKSession(SessionConfig{ID: "b"}))

	all := m.GetAllSDKSessions()
	if len(all) != 2 {
		t.Fatalf("expected 2 SDK sessions, got %d", len(all))
	}
	ids := map[string]bool{}
	for _, s := range all {
		ids[s.ID] = true
	}
	if !ids["a"] || !ids["b"] {
		t.Errorf("expected sessions a and b, got %v", ids)
	}
}

func TestManager_ForEachSDKSession(t *testing.T) {
	m := NewSessionManager(5)
	m.RegisterSDKSessionForTest(NewSDKSession(SessionConfig{ID: "x"}))
	m.RegisterSDKSessionForTest(NewSDKSession(SessionConfig{ID: "y"}))

	visited := map[string]bool{}
	m.ForEachSDKSession(func(id string, s *SDKSession) {
		visited[id] = true
		if s.ID != id {
			t.Errorf("callback id %q != session.ID %q", id, s.ID)
		}
	})
	if !visited["x"] || !visited["y"] {
		t.Errorf("ForEachSDKSession should visit x and y, got %v", visited)
	}
}

func TestManager_StopSDKSession(t *testing.T) {
	m := NewSessionManager(5)
	sess := NewSDKSession(SessionConfig{ID: "to-stop"})
	m.RegisterSDKSessionForTest(sess)

	if err := m.StopSDKSession("to-stop"); err != nil {
		t.Fatalf("StopSDKSession: %v", err)
	}
	if _, ok := m.GetSDKSession("to-stop"); ok {
		t.Error("session should be removed after StopSDKSession")
	}
	if sess.GetStatus() != StatusStopped {
		t.Errorf("stopped session status = %q, want stopped", sess.GetStatus())
	}
}

func TestManager_StopSDKSession_NotFound(t *testing.T) {
	m := NewSessionManager(5)
	if err := m.StopSDKSession("ghost"); err == nil {
		t.Error("expected error stopping nonexistent SDK session")
	}
}

func TestManager_CreateSDKSession_MaxReached(t *testing.T) {
	m := NewSessionManager(1)
	// Pre-fill to the cap via the test-registration shortcut (counts
	// toward sessionCount).
	m.RegisterSDKSessionForTest(NewSDKSession(SessionConfig{ID: "filler"}))

	_, err := m.CreateSDKSession(context.Background(), SessionConfig{})
	if err == nil {
		t.Error("expected max-sessions error when at capacity")
	}
}

func TestManager_CreateCodexSession_MaxReached(t *testing.T) {
	m := NewSessionManager(1)
	m.RegisterSDKSessionForTest(NewSDKSession(SessionConfig{ID: "filler"}))

	_, err := m.CreateCodexSession(context.Background(), SessionConfig{})
	if err == nil {
		t.Error("expected max-sessions error when at capacity")
	}
}

func TestManager_GetCodexSession_Miss(t *testing.T) {
	m := NewSessionManager(5)
	if _, ok := m.GetCodexSession("none"); ok {
		t.Error("expected miss for unknown codex session")
	}
	if len(m.GetAllCodexSessions()) != 0 {
		t.Error("expected no codex sessions")
	}
}

func TestManager_StopCodexSession_NotFound(t *testing.T) {
	m := NewSessionManager(5)
	if err := m.StopCodexSession("ghost"); err == nil {
		t.Error("expected error stopping nonexistent codex session")
	}
}

func TestManager_StopAllSessions_StopsSDK(t *testing.T) {
	m := NewSessionManager(5)
	s1 := NewSDKSession(SessionConfig{ID: "all-1"})
	s2 := NewSDKSession(SessionConfig{ID: "all-2"})
	m.RegisterSDKSessionForTest(s1)
	m.RegisterSDKSessionForTest(s2)

	if err := m.StopAllSessions(context.Background()); err != nil {
		t.Fatalf("StopAllSessions: %v", err)
	}
	if m.GetSessionCount() != 0 {
		t.Errorf("session count = %d, want 0 after StopAllSessions", m.GetSessionCount())
	}
	if s1.GetStatus() != StatusStopped || s2.GetStatus() != StatusStopped {
		t.Error("all SDK sessions should be stopped")
	}
}

func TestManager_Events_NonNil(t *testing.T) {
	m := NewSessionManager(5)
	if m.Events() == nil {
		t.Error("Events() must return a non-nil aggregated channel")
	}
}

func TestManager_GetAllSessionIDs_AcrossBackends(t *testing.T) {
	m := NewSessionManager(10)
	m.RegisterSDKSessionForTest(NewSDKSession(SessionConfig{ID: "sdk-1"}))
	m.RegisterSDKSessionForTest(NewSDKSession(SessionConfig{ID: "sdk-2"}))

	ids := m.GetAllSessionIDs()
	found := map[string]bool{}
	for _, id := range ids {
		found[id] = true
	}
	if !found["sdk-1"] || !found["sdk-2"] {
		t.Errorf("GetAllSessionIDs should include SDK ids, got %v", ids)
	}
	// SDK sessions count toward the manager's session count.
	if m.GetSessionCount() != 2 {
		t.Errorf("session count = %d, want 2", m.GetSessionCount())
	}
}
