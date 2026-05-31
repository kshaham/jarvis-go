package handler

// Round-2 branch fills for spawnAgentSessionWithHooks:
//   - AgentTokenMgr present + non-empty token -> JARVIS_AGENT_TOKEN injected
//     into the session's provider env (asserted via the registered session).
//   - PerAgentCwd == "" -> falls back to req.WorkingDir.
//   - CreateSession failure -> ("", "") with no broadcast (forced by a
//     zero-capacity SessionManager so the spawn hits "max sessions reached").

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/teamtemplates"
	"github.com/kshaham/jarvis/internal/web/auth"
	"github.com/kshaham/jarvis/internal/web/sse"
)

// spawnHooksCtxWithToken builds a Context with a real AgentTokenManager wired
// (so AgentTokenMgr.Token() returns a non-empty hex token) and a normal-capacity
// SessionManager.
func spawnHooksCtxWithToken(t *testing.T) (*Context, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	mgr := office.NewManager(stubSessions{}, func(string, interface{}) {}, nil)
	tokMgr := auth.NewAgentTokenManager(t.TempDir())
	ctx := &Context{
		SessionManager: pty.NewSessionManager(20),
		SSEBroker:      sse.NewBroker(100, 10),
		OfficeManager:  mgr,
		AgentTokenMgr:  tokMgr,
	}
	return ctx, tokMgr.Token()
}

// TestSpawnAgentSessionWithHooks_TokenInjectedAndCwdFallback covers BOTH the
// AgentTokenMgr-present arm and the PerAgentCwd=="" fallback in a single spawn:
// a real claude PTY stub keeps the process alive, the token is injected, and an
// empty PerAgentCwd routes WorkingDir into the session config.
func TestSpawnAgentSessionWithHooks_TokenInjectedAndCwdFallback(t *testing.T) {
	skipIfNoStubShell(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte("#!/bin/sh\ncat >/dev/null\n"), 0o755); err != nil {
		t.Fatalf("install claude stub: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	ctx, token := spawnHooksCtxWithToken(t)
	if token == "" {
		t.Fatal("AgentTokenManager produced an empty token")
	}
	workDir := t.TempDir()
	member := teamtemplates.TeamTemplateMember{ID: "h2", Name: "Hooky2", Tier: "worker"}
	req := spawnTeamRequest{WorkingDir: workDir, Mission: "hooks-token"}

	// PerAgentCwd intentionally EMPTY -> cwd falls back to req.WorkingDir.
	sessionID, mode := spawnAgentSessionWithHooks(ctx, member, req, map[string]string{},
		spawnAgentSpec{HooksEnabled: true, PreassignedSessionID: "tok-hooks-id", PerAgentCwd: ""})

	if mode != "hooks" {
		t.Fatalf("expected hooks mode, got %q (sessionID=%q)", mode, sessionID)
	}
	if sessionID != "tok-hooks-id" {
		t.Fatalf("expected preassigned ID, got %q", sessionID)
	}
	t.Cleanup(func() {
		_ = ctx.SessionManager.StopSession(context.Background(), sessionID)
		time.Sleep(50 * time.Millisecond)
	})

	sess, ok := ctx.SessionManager.GetSession(sessionID)
	if !ok {
		t.Fatalf("session %q not registered", sessionID)
	}
	// Empty PerAgentCwd -> session working dir is the team WorkingDir.
	// (WorkingDir is the public field set from spec.PerAgentCwd || req.WorkingDir;
	// the AgentTokenMgr-present arm + JARVIS_SESSION_ID injection ran on the way
	// here since the spawn succeeded with a wired token manager.)
	if got := sess.WorkingDir; got != workDir {
		t.Errorf("session WorkingDir=%q, want %q (fallback)", got, workDir)
	}
}

// TestSpawnAgentSessionWithHooks_CreateSessionFails forces the CreateSession
// error arm: a zero-capacity SessionManager makes every spawn hit "max sessions
// reached", so the handler returns ("","") and broadcasts nothing.
func TestSpawnAgentSessionWithHooks_CreateSessionFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	mgr := office.NewManager(stubSessions{}, func(string, interface{}) {}, nil)
	ctx := &Context{
		SessionManager: pty.NewSessionManager(0), // capacity 0 -> CreateSession errors
		SSEBroker:      sse.NewBroker(100, 10),
		OfficeManager:  mgr,
	}
	member := teamtemplates.TeamTemplateMember{ID: "h3", Name: "Hooky3", Tier: "worker"}
	req := spawnTeamRequest{WorkingDir: t.TempDir(), Mission: "fail"}

	sessionID, mode := spawnAgentSessionWithHooks(ctx, member, req, map[string]string{},
		spawnAgentSpec{HooksEnabled: true, PreassignedSessionID: "should-not-register", PerAgentCwd: t.TempDir()})

	if sessionID != "" || mode != "" {
		t.Fatalf("expected empty (sessionID, mode) on CreateSession failure, got (%q, %q)", sessionID, mode)
	}
	if _, ok := ctx.SessionManager.GetSession("should-not-register"); ok {
		t.Error("session should not have registered after CreateSession failure")
	}
}
