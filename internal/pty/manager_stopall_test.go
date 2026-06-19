package pty

import (
	"context"
	"testing"
)

// TestManager_StopAllSessions_NotFoundWarnBranches drives all three "failed to
// stop ... during shutdown" warn arms (manager.go:110-112 / 115-117 / 120-122).
// These fire when a session is removed from its map between StopAllSessions's
// id snapshot and the per-id stop loop -- the real concurrent-shutdown race. We
// reproduce it deterministically via the stopAllAfterCollect seam, which deletes
// every registered session after the snapshot so each Stop*Session returns
// "not found". StopAllSessions must still return nil (warnings are non-fatal).
func TestManager_StopAllSessions_NotFoundWarnBranches(t *testing.T) {
	m := NewSessionManager(10)
	m.mu.Lock()
	m.sessions["pty-race"] = NewSession(SessionConfig{ID: "pty-race", WorkingDir: "/tmp", Mode: ModeShell})
	m.sdkSessions["sdk-race"] = NewSDKSession(SessionConfig{ID: "sdk-race"})
	m.codexSessions["codex-race"] = NewCodexSession(SessionConfig{ID: "codex-race"})
	m.mu.Unlock()

	orig := stopAllAfterCollect
	stopAllAfterCollect = func(mgr *SessionManager) {
		// Simulate a concurrent shutdown that already removed every session.
		mgr.mu.Lock()
		mgr.sessions = make(map[string]*Session)
		mgr.sdkSessions = make(map[string]*SDKSession)
		mgr.codexSessions = make(map[string]*CodexSession)
		mgr.mu.Unlock()
	}
	t.Cleanup(func() { stopAllAfterCollect = orig })

	if err := m.StopAllSessions(context.Background()); err != nil {
		t.Fatalf("StopAllSessions must return nil even when every id is already gone: %v", err)
	}
	if m.GetSessionCount() != 0 {
		t.Errorf("session count = %d, want 0", m.GetSessionCount())
	}
}

// TestManager_StopAllSessions_RemovesEverything asserts the happy-path: given
// multiple registered sessions across all three backends, StopAllSessions
// stops them (now concurrently, via one goroutine per id) and removes every
// one from its map, so the manager is empty when the call returns. None of
// these fixtures own a real subprocess, so Stop is deterministic.
func TestManager_StopAllSessions_RemovesEverything(t *testing.T) {
	// Arrange.
	m := NewSessionManager(20)
	m.mu.Lock()
	for _, id := range []string{"pty-1", "pty-2", "pty-3"} {
		m.sessions[id] = NewSession(SessionConfig{ID: id, WorkingDir: "/tmp", Mode: ModeShell})
	}
	for _, id := range []string{"sdk-1", "sdk-2", "sdk-3"} {
		m.sdkSessions[id] = NewSDKSession(SessionConfig{ID: id})
	}
	for _, id := range []string{"codex-1", "codex-2"} {
		m.codexSessions[id] = NewCodexSession(SessionConfig{ID: id})
	}
	m.mu.Unlock()
	if m.GetSessionCount() != 8 {
		t.Fatalf("setup: session count = %d, want 8", m.GetSessionCount())
	}

	// Act.
	if err := m.StopAllSessions(context.Background()); err != nil {
		t.Fatalf("StopAllSessions returned error: %v", err)
	}

	// Assert: every map drained.
	if m.GetSessionCount() != 0 {
		t.Errorf("session count = %d, want 0 after StopAllSessions", m.GetSessionCount())
	}
	if got := len(m.GetAllSessions()); got != 0 {
		t.Errorf("PTY sessions = %d, want 0", got)
	}
	if got := len(m.GetAllSDKSessions()); got != 0 {
		t.Errorf("SDK sessions = %d, want 0", got)
	}
	if got := len(m.GetAllCodexSessions()); got != 0 {
		t.Errorf("Codex sessions = %d, want 0", got)
	}
}

// stuckCodexSession is a CodexSession whose status is pre-advanced so Start()
// takes its "already started" guard and returns an error -- the only failure
// mode CreateCodexSession can surface.
func newAlreadyStartedCodexSession(config SessionConfig) *CodexSession {
	s := NewCodexSession(config)
	s.status = StatusBusy // != StatusStopped -> Start() returns the guard error
	return s
}

// TestManager_CreateCodexSession_StartFailure drives the CreateCodexSession
// start-error branch (manager.go:277-279). CodexSession.Start only fails when
// the session is not in StatusStopped, which a freshly-built session never is,
// so we inject an already-started session through the newCodexSession seam.
func TestManager_CreateCodexSession_StartFailure(t *testing.T) {
	m := NewSessionManager(5)
	orig := newCodexSession
	newCodexSession = newAlreadyStartedCodexSession
	t.Cleanup(func() { newCodexSession = orig })

	_, err := m.CreateCodexSession(context.Background(), SessionConfig{WorkingDir: t.TempDir()})
	if err == nil {
		t.Fatal("CreateCodexSession should fail when the session cannot start")
	}
	if len(m.GetAllCodexSessions()) != 0 {
		t.Errorf("a failed CreateCodexSession must not register a session; got %d", len(m.GetAllCodexSessions()))
	}
}
