package handler

// Direct unit coverage for stopAgentSessionByID across all backends (SDK, PTY,
// Codex) plus the nil-manager early return. Each arm stops a real registered
// session and asserts it's gone from the manager afterward.

import (
	"context"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/pty"
)

func TestStopAgentSessionByID_NilManager(t *testing.T) {
	ctx := &Context{} // SessionManager nil
	// Must not panic; just returns.
	ctx.stopAgentSessionByID("anything")
}

func TestStopAgentSessionByID_SDK(t *testing.T) {
	ctx := &Context{SessionManager: pty.NewSessionManager(10)}
	sdk := pty.NewSDKSession(pty.SessionConfig{ID: "stop-sdk", Mode: pty.ModeClaudeSDK, WorkingDir: t.TempDir()})
	ctx.SessionManager.RegisterSDKSessionForTest(sdk)

	ctx.stopAgentSessionByID("stop-sdk")
	if _, ok := ctx.SessionManager.GetSDKSession("stop-sdk"); ok {
		t.Error("SDK session should be stopped + removed")
	}
}

func TestStopAgentSessionByID_PTY(t *testing.T) {
	ctx := &Context{SessionManager: pty.NewSessionManager(10)}
	sess, err := ctx.SessionManager.CreateSession(context.Background(), pty.SessionConfig{
		WorkingDir: t.TempDir(), Mode: pty.ModeShell, Name: "stop-pty",
	})
	if err != nil {
		t.Fatalf("create pty: %v", err)
	}
	ctx.stopAgentSessionByID(sess.ID)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := ctx.SessionManager.GetSession(sess.ID); !ok {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, ok := ctx.SessionManager.GetSession(sess.ID); ok {
		_ = ctx.SessionManager.StopSession(context.Background(), sess.ID)
		t.Error("PTY session should be stopped + removed")
	}
}

func TestStopAgentSessionByID_Codex(t *testing.T) {
	skipIfNoStubShell(t)
	t.Setenv("HOME", t.TempDir())
	ctx := &Context{SessionManager: pty.NewSessionManager(10)}
	installCodexOnPATH(t, "{\"type\":\"thread.started\",\"thread_id\":\"stop-th\"}\n")
	cx, err := ctx.SessionManager.CreateCodexSession(context.Background(), pty.SessionConfig{
		ID: "stop-codex", WorkingDir: t.TempDir(), Mode: pty.ModeCodexExec, Name: "stop-codex",
	})
	if err != nil {
		t.Fatalf("create codex: %v", err)
	}
	_ = cx
	ctx.stopAgentSessionByID("stop-codex")
	if _, ok := ctx.SessionManager.GetCodexSession("stop-codex"); ok {
		t.Error("Codex session should be stopped + removed")
	}
}
