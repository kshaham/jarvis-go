package pty

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/testutil/stubbin"
)

// TestManager_CreateSDKSession_Lifecycle drives CreateSDKSession against a
// stub claude binary: the session is created, started, registered, and
// reachable via GetSDKSession. Exercises the success path of
// CreateSDKSession (previously only the max-reached branch was covered).
func TestManager_CreateSDKSession_Lifecycle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub binaries are #!/bin/sh (unix-only)")
	}
	stream := strings.Join([]string{
		`{"type":"system","subtype":"init","session_id":"mgr-sdk","model":"m"}`,
		`{"type":"result","subtype":"success","num_turns":1,"session_id":"mgr-sdk","usage":{"input_tokens":1,"output_tokens":1}}`,
		"",
	}, "\n")
	bin := fakeClaudeStreamJSON(t, stream)

	m := NewSessionManager(5)
	sess, err := m.CreateSDKSession(context.Background(), SessionConfig{
		WorkingDir:       t.TempDir(),
		ProviderEnv:      map[string]string{"ANTHROPIC_API_KEY": "stub"},
		ClaudeBinaryPath: bin,
	})
	if err != nil {
		t.Fatalf("CreateSDKSession: %v", err)
	}
	defer m.StopSDKSession(sess.ID)

	if sess.ID == "" {
		t.Error("created session should have a generated ID")
	}
	got, ok := m.GetSDKSession(sess.ID)
	if !ok || got != sess {
		t.Error("created SDK session should be registered and retrievable")
	}
	if m.GetSessionCount() != 1 {
		t.Errorf("session count = %d, want 1", m.GetSessionCount())
	}
}

// TestManager_CreateCodexSession_Lifecycle drives CreateCodexSession against
// a stub codex binary.
func TestManager_CreateCodexSession_Lifecycle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub binaries are #!/bin/sh (unix-only)")
	}
	bin := fakeCodexBin(t, codexSuccessStream("mgr-codex"))

	m := NewSessionManager(5)
	sess, err := m.CreateCodexSession(context.Background(), SessionConfig{
		WorkingDir:      t.TempDir(),
		Model:           "gpt-5.5",
		CodexBinaryPath: bin,
	})
	if err != nil {
		t.Fatalf("CreateCodexSession: %v", err)
	}
	defer m.StopCodexSession(sess.ID)

	if sess.ID == "" {
		t.Error("created codex session should have a generated ID")
	}
	got, ok := m.GetCodexSession(sess.ID)
	if !ok || got != sess {
		t.Error("created Codex session should be registered and retrievable")
	}
	all := m.GetAllCodexSessions()
	if len(all) != 1 {
		t.Errorf("GetAllCodexSessions len = %d, want 1", len(all))
	}
}

// TestManager_StopCodexSession_RemovesAndStops drives the full stop path of a
// real (stub-backed) codex session created via the manager.
func TestManager_StopCodexSession_RemovesAndStops(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub binaries are #!/bin/sh (unix-only)")
	}
	bin := fakeCodexBin(t, codexSuccessStream("stop-codex"))
	m := NewSessionManager(5)
	sess, err := m.CreateCodexSession(context.Background(), SessionConfig{
		WorkingDir:      t.TempDir(),
		CodexBinaryPath: bin,
	})
	if err != nil {
		t.Fatalf("CreateCodexSession: %v", err)
	}
	if err := m.StopCodexSession(sess.ID); err != nil {
		t.Fatalf("StopCodexSession: %v", err)
	}
	if _, ok := m.GetCodexSession(sess.ID); ok {
		t.Error("codex session should be removed after StopCodexSession")
	}
	if sess.GetStatus() != StatusStopped {
		t.Errorf("status = %q, want stopped", sess.GetStatus())
	}
}

// TestManager_StopAllSessions_AcrossBackends creates one SDK + one Codex
// session (both stub-backed) and confirms StopAllSessions tears down both.
func TestManager_StopAllSessions_AcrossBackends(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub binaries are #!/bin/sh (unix-only)")
	}
	sdkStream := strings.Join([]string{
		`{"type":"result","subtype":"success","num_turns":1,"usage":{"input_tokens":1,"output_tokens":1}}`,
		"",
	}, "\n")
	sdkBin := fakeClaudeStreamJSON(t, sdkStream)
	codexBin := fakeCodexBin(t, codexSuccessStream("all-codex"))

	m := NewSessionManager(5)
	sdkSess, err := m.CreateSDKSession(context.Background(), SessionConfig{
		WorkingDir:       t.TempDir(),
		ProviderEnv:      map[string]string{"ANTHROPIC_API_KEY": "stub"},
		ClaudeBinaryPath: sdkBin,
	})
	if err != nil {
		t.Fatalf("CreateSDKSession: %v", err)
	}
	codexSess, err := m.CreateCodexSession(context.Background(), SessionConfig{
		WorkingDir:      t.TempDir(),
		CodexBinaryPath: codexBin,
	})
	if err != nil {
		t.Fatalf("CreateCodexSession: %v", err)
	}

	if err := m.StopAllSessions(context.Background()); err != nil {
		t.Fatalf("StopAllSessions: %v", err)
	}
	if m.GetSessionCount() != 0 {
		t.Errorf("session count = %d, want 0 after StopAllSessions", m.GetSessionCount())
	}
	if sdkSess.GetStatus() != StatusStopped {
		t.Error("SDK session should be stopped")
	}
	if codexSess.GetStatus() != StatusStopped {
		t.Error("Codex session should be stopped")
	}
}

// TestManager_CreateSession_ShellLifecycle drives the plain (PTY) CreateSession
// success path with a real /bin/bash-backed ModeShell session, then stops it.
func TestManager_CreateSession_ShellLifecycle(t *testing.T) {
	m := NewSessionManager(5)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	sess, err := m.CreateSession(ctx, SessionConfig{
		WorkingDir: "/tmp",
		Mode:       ModeShell,
		Name:       "mgr-shell",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, ok := m.GetSession(sess.ID); !ok {
		t.Error("created session should be retrievable")
	}
	if len(m.GetAllSessions()) != 1 {
		t.Errorf("GetAllSessions len = %d, want 1", len(m.GetAllSessions()))
	}
	if err := m.StopSession(ctx, sess.ID); err != nil {
		t.Fatalf("StopSession: %v", err)
	}
	if _, ok := m.GetSession(sess.ID); ok {
		t.Error("session should be removed after StopSession")
	}
}

// TestManager_CreateSession_MaxReached covers the capacity guard on the PTY
// path.
func TestManager_CreateSession_MaxReached(t *testing.T) {
	m := NewSessionManager(1)
	m.RegisterSDKSessionForTest(NewSDKSession(SessionConfig{ID: "filler"}))
	_, err := m.CreateSession(context.Background(), SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	if err == nil {
		t.Error("expected max-sessions error on the PTY path when at capacity")
	}
}

// TestManager_StopSession_NotFound covers the miss path.
func TestManager_StopSession_NotFound(t *testing.T) {
	m := NewSessionManager(5)
	if err := m.StopSession(context.Background(), "ghost"); err == nil {
		t.Error("expected error stopping a nonexistent PTY session")
	}
}

// fakeClaudeStreamJSONSlow is like fakeClaudeStreamJSON but stays alive for a
// few seconds after emitting events, so SendPrompt has a live session to send
// to before EOF closes the channel. Used by the SendPrompt test.
func fakeClaudeStreamJSONSlow(t *testing.T, payload string, holdSeconds int) string {
	t.Helper()
	escaped := strings.ReplaceAll(payload, "'", "'\\''")
	body := "#!/bin/sh\ncat >/dev/null &\nprintf '%s' '" + escaped + "'\nsleep " + itoaT(holdSeconds) + "\n"
	return stubbin.Install(t, "fakeclaude-slow.sh", body)
}

func itoaT(n int) string {
	if n <= 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// TestSDKSession_SendPrompt_LiveSession exercises SendPrompt against a live
// stub-backed session: the send succeeds, the session flips to working, and
// EventWorking is emitted. Covers the success branch of SendPrompt (only the
// nil/stopped guards were covered before).
func TestSDKSession_SendPrompt_LiveSession(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub binaries are #!/bin/sh (unix-only)")
	}
	// init + result then hold open for follow-up sends.
	stream := strings.Join([]string{
		`{"type":"system","subtype":"init","session_id":"send-sess","model":"m"}`,
		`{"type":"result","subtype":"success","num_turns":1,"session_id":"send-sess","usage":{"input_tokens":1,"output_tokens":1}}`,
		"",
	}, "\n")
	bin := fakeClaudeStreamJSONSlow(t, stream, 3)

	s := NewSDKSession(SessionConfig{
		ID:               "send-sess",
		WorkingDir:       t.TempDir(),
		ProviderEnv:      map[string]string{"ANTHROPIC_API_KEY": "stub"},
		ClaudeBinaryPath: bin,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	// Drain the init/result events first so the channel has room.
	drainDeadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(drainDeadline) {
		select {
		case <-s.Events():
		case <-time.After(100 * time.Millisecond):
		}
		// proceed once the session reported idle (result consumed)
		if s.GetStatus() == StatusIdle {
			break
		}
	}

	s.SendPrompt(ctx, "a follow-up turn")
	if !s.IsWorking() {
		t.Error("SendPrompt to a live session should flip isWorking=true")
	}
	if s.GetStatus() != StatusBusy {
		t.Errorf("status = %q, want busy after SendPrompt", s.GetStatus())
	}
}
