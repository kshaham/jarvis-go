package pty

import (
	"context"
	"testing"
	"time"
)

func TestSessionManager_CreateSession(t *testing.T) {
	m := NewSessionManager(5)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	session, err := m.CreateSession(ctx, SessionConfig{
		WorkingDir: "/tmp",
		Mode:       ModeShell,
		Name:       "test-1",
	})
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	defer m.StopSession(ctx, session.ID)

	if m.GetSessionCount() != 1 {
		t.Errorf("expected 1 session, got %d", m.GetSessionCount())
	}

	got, ok := m.GetSession(session.ID)
	if !ok || got.ID != session.ID {
		t.Error("GetSession failed to return created session")
	}
}

func TestSessionManager_MaxSessions(t *testing.T) {
	m := NewSessionManager(2)
	ctx := context.Background()

	s1, err := m.CreateSession(ctx, SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	if err != nil {
		t.Fatalf("session 1: %v", err)
	}
	defer m.StopSession(ctx, s1.ID)

	s2, err := m.CreateSession(ctx, SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	if err != nil {
		t.Fatalf("session 2: %v", err)
	}
	defer m.StopSession(ctx, s2.ID)

	_, err = m.CreateSession(ctx, SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	if err == nil {
		t.Error("expected error when exceeding max sessions")
	}
}

func TestSessionManager_StopSession(t *testing.T) {
	m := NewSessionManager(5)
	ctx := context.Background()

	session, err := m.CreateSession(ctx, SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := m.StopSession(ctx, session.ID); err != nil {
		t.Fatalf("StopSession: %v", err)
	}

	if m.GetSessionCount() != 0 {
		t.Errorf("expected 0 sessions after stop, got %d", m.GetSessionCount())
	}
}

func TestSessionManager_StopNonexistent(t *testing.T) {
	m := NewSessionManager(5)
	err := m.StopSession(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent session")
	}
}

func TestSessionManager_GetAllSessions(t *testing.T) {
	m := NewSessionManager(5)
	ctx := context.Background()

	s1, _ := m.CreateSession(ctx, SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	defer m.StopSession(ctx, s1.ID)
	s2, _ := m.CreateSession(ctx, SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	defer m.StopSession(ctx, s2.ID)

	all := m.GetAllSessions()
	if len(all) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(all))
	}
}
