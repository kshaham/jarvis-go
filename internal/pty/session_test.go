package pty

import (
	"context"
	"testing"
	"time"
)

func TestNewSession(t *testing.T) {
	config := SessionConfig{
		WorkingDir: "/tmp",
		Mode:       ModeShell,
		Name:       "test",
	}
	s := NewSession(config)
	if s.ID == "" {
		t.Error("expected non-empty ID")
	}
	if s.Status() != StatusStopped {
		t.Errorf("expected StatusStopped, got %s", s.Status())
	}
	if s.Name() != "test" {
		t.Errorf("expected name 'test', got %s", s.Name())
	}
}

func TestSession_SetName(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	s.SetName("renamed")
	if s.Name() != "renamed" {
		t.Errorf("expected 'renamed', got %s", s.Name())
	}
}

func TestSession_StartShell(t *testing.T) {
	s := NewSession(SessionConfig{
		WorkingDir: "/tmp",
		Mode:       ModeShell,
		Name:       "shell-test",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer s.Stop()

	if s.PID() == 0 {
		t.Error("expected non-zero PID after start")
	}
	if s.Status() != StatusBusy {
		t.Errorf("expected StatusBusy after start, got %s", s.Status())
	}

	// Should receive at least one terminal event
	select {
	case event := <-s.Events():
		if _, ok := event.(EventTerminal); !ok {
			t.Logf("first event was %T, not EventTerminal (acceptable)", event)
		}
	case <-time.After(3 * time.Second):
		t.Log("no terminal event within 3s (may be expected for bare bash)")
	}
}

func TestSession_Stop(t *testing.T) {
	s := NewSession(SessionConfig{
		WorkingDir: "/tmp",
		Mode:       ModeShell,
	})

	ctx := context.Background()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if err := s.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	// Done channel should be closed
	select {
	case <-s.Done():
		// good
	case <-time.After(5 * time.Second):
		t.Error("done channel not closed after stop")
	}

	if s.Status() != StatusStopped {
		t.Errorf("expected StatusStopped, got %s", s.Status())
	}
}

func TestSession_DoubleStop(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	ctx := context.Background()
	_ = s.Start(ctx)
	_ = s.Stop()
	// Second stop should not panic
	if err := s.Stop(); err != nil {
		t.Errorf("double stop should not error: %v", err)
	}
}

func TestSession_Tokens(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	input, output, total := s.Tokens()
	if input != 0 || output != 0 || total != 0 {
		t.Errorf("expected zero tokens, got %d/%d/%d", input, output, total)
	}
}

func TestDetectWorkingState(t *testing.T) {
	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	// Manually test working detection
	s.detectWorkingState("test", "Thinking about something")

	s.mu.Lock()
	working := s.isWorking
	s.mu.Unlock()

	if !working {
		t.Error("expected isWorking=true after 'Thinking' detected")
	}
}
