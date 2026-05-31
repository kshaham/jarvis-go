package handler

// Drives spawnAgentSession end-to-end through its SDK and PTY-fallback paths
// against a stub `claude` binary placed on PATH (the office spawner doesn't
// thread a ClaudeBinaryPath, so both backends resolve `claude` via PATH). The
// SDK path is the dominant one (trySDK=true); the PTY fallback covers the
// trySDK=false branch. Both register a real session so the SSE "session:created"
// broadcast + forwarder goroutine launch run.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/state"
	"github.com/kshaham/jarvis/internal/teamtemplates"
	"github.com/kshaham/jarvis/internal/web/sse"
)

// installClaudeOnPATH writes a stub binary literally named `claude` (emitting
// the given stream-json payload) into a fresh dir and prepends that dir to
// PATH for the test. The SDK resolves `claude` via exec.LookPath when no
// explicit ClaudeBinaryPath is set, so this redirects it to the stub.
func installClaudeOnPATH(t *testing.T, payload string) {
	t.Helper()
	skipIfNoStubShell(t)
	dir := t.TempDir()
	escaped := strings.ReplaceAll(payload, "'", "'\\''")
	body := "#!/bin/sh\ncat >/dev/null &\nprintf '%s' '" + escaped + "'\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(body), 0o755); err != nil {
		t.Fatalf("install claude stub: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func spawnAgentCtx(t *testing.T) *Context {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	mgr := office.NewManager(stubSessions{}, func(string, interface{}) {}, nil)
	return &Context{
		SessionManager: pty.NewSessionManager(20),
		SSEBroker:      sse.NewBroker(100, 10),
		StateStore:     state.NewStore(filepath.Join(t.TempDir(), "state.json")),
		OfficeManager:  mgr,
	}
}

func TestSpawnAgentSession_SDKPath(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"system","subtype":"init","session_id":"c-1","model":"claude-opus-4-8"}`,
		`{"type":"result","subtype":"success","num_turns":1,"total_cost_usd":0.01,"session_id":"c-1","usage":{"input_tokens":5,"output_tokens":2}}`,
		"",
	}, "\n")
	installClaudeOnPATH(t, stream)

	ctx := spawnAgentCtx(t)
	member := teamtemplates.TeamTemplateMember{ID: "vex", Name: "Vex", Role: "Engineer", Tier: "worker"}
	req := spawnTeamRequest{WorkingDir: t.TempDir(), Mission: "ship it"}
	teamConfigs := []*office.TeamMemberConfig{{ID: "vex", Name: "Vex", Role: "Engineer", Tier: "worker"}}

	sessionID, mode := spawnAgentSession(ctx, member, req, map[string]string{}, teamConfigs,
		true /*perpetual*/, true /*trySDK*/, nil, "claude-subscription", "claude-opus-4-8", spawnAgentSpec{})

	if mode != "sdk" {
		t.Fatalf("expected sdk mode, got %q (sessionID=%q)", mode, sessionID)
	}
	if sessionID == "" {
		t.Fatal("expected non-empty sessionID from SDK spawn")
	}
	t.Cleanup(func() {
		_ = ctx.SessionManager.StopSDKSession(sessionID)
		time.Sleep(50 * time.Millisecond)
	})
	// The SDK session must be registered + live in the manager.
	if _, ok := ctx.SessionManager.GetSDKSession(sessionID); !ok {
		t.Errorf("SDK session %s not registered in manager", sessionID)
	}
}

func TestSpawnAgentSession_PTYFallback(t *testing.T) {
	// trySDK=false routes straight to the PTY fallback, which spawns the
	// interactive `claude` CLI in a PTY. The stub stays alive on stdin so the
	// PTY read loop has a live process.
	skipIfNoStubShell(t)
	dir := t.TempDir()
	// A PTY claude stub: just read stdin forever (keeps the process alive).
	body := "#!/bin/sh\ncat >/dev/null\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(body), 0o755); err != nil {
		t.Fatalf("install pty claude stub: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	ctx := spawnAgentCtx(t)
	member := teamtemplates.TeamTemplateMember{ID: "ana", Name: "Ana", Role: "Reviewer", Tier: "lead"}
	req := spawnTeamRequest{WorkingDir: t.TempDir(), Mission: "review"}
	teamConfigs := []*office.TeamMemberConfig{{ID: "ana", Name: "Ana"}}

	sessionID, mode := spawnAgentSession(ctx, member, req, map[string]string{}, teamConfigs,
		false /*perpetual*/, false /*trySDK*/, nil, "claude-subscription", "claude-opus-4-8", spawnAgentSpec{})

	if mode != "pty" {
		t.Fatalf("expected pty mode, got %q (sessionID=%q)", mode, sessionID)
	}
	if sessionID == "" {
		t.Fatal("expected non-empty sessionID from PTY spawn")
	}
	t.Cleanup(func() {
		_ = ctx.SessionManager.StopSession(context.Background(), sessionID)
		time.Sleep(50 * time.Millisecond)
	})
	if _, ok := ctx.SessionManager.GetSession(sessionID); !ok {
		t.Errorf("PTY session %s not registered", sessionID)
	}
}

func TestSpawnAgentSession_HooksPath(t *testing.T) {
	skipIfNoStubShell(t)
	dir := t.TempDir()
	body := "#!/bin/sh\ncat >/dev/null\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(body), 0o755); err != nil {
		t.Fatalf("install hooks claude stub: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	ctx := spawnAgentCtx(t)
	member := teamtemplates.TeamTemplateMember{ID: "h1", Name: "Hooky", Tier: "worker"}
	req := spawnTeamRequest{WorkingDir: t.TempDir(), Mission: "hooks"}
	teamConfigs := []*office.TeamMemberConfig{{ID: "h1", Name: "Hooky"}}

	preassigned := "preassigned-hooks-id"
	sessionID, mode := spawnAgentSession(ctx, member, req, map[string]string{}, teamConfigs,
		true, false /*trySDK*/, nil, "claude-subscription", "claude-opus-4-8",
		spawnAgentSpec{HooksEnabled: true, PreassignedSessionID: preassigned, PerAgentCwd: t.TempDir()})

	if mode != "hooks" {
		t.Fatalf("expected hooks mode, got %q (sessionID=%q)", mode, sessionID)
	}
	if sessionID != preassigned {
		t.Errorf("hooks session should use the preassigned ID %q, got %q", preassigned, sessionID)
	}
	t.Cleanup(func() {
		_ = ctx.SessionManager.StopSession(context.Background(), sessionID)
		time.Sleep(50 * time.Millisecond)
	})
}
