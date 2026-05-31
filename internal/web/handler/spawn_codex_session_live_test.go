package handler

// Drives spawnCodexAgentSession end-to-end. The "openai-custom" provider path
// emits a custom [model_providers] block (prov != nil) so no auth.json staging
// is required, letting the happy path reach CreateCodexSession against a stub
// `codex` binary on PATH. A second test drives the auth-failure path
// (prov == nil + no staged auth.json -> early "",""). A third drives the
// openai-custom branch through spawnAgentSession's harness short-circuit.

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

// installCodexOnPATH writes a stub `codex` binary emitting the given JSONL
// stream and prepends its dir to PATH.
func installCodexOnPATH(t *testing.T, stream string) {
	t.Helper()
	skipIfNoStubShell(t)
	dir := t.TempDir()
	escaped := strings.ReplaceAll(stream, "'", "'\\''")
	body := "#!/bin/sh\nprintf '%s' '" + escaped + "'\n"
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte(body), 0o755); err != nil {
		t.Fatalf("install codex stub: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func codexSpawnCtx(t *testing.T) *Context {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	return &Context{
		SessionManager: pty.NewSessionManager(20),
		SSEBroker:      sse.NewBroker(100, 10),
		StateStore:     state.NewStore(filepath.Join(t.TempDir(), "state.json")),
		OfficeManager:  office.NewManager(stubSessions{}, func(string, interface{}) {}, nil),
	}
}

func TestSpawnCodexAgentSession_CustomProviderHappy(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"thread.started","thread_id":"th-1"}`,
		`{"type":"turn.started"}`,
		`{"type":"turn.completed","usage":{"input_tokens":4,"output_tokens":1}}`,
		"",
	}, "\n")
	installCodexOnPATH(t, stream)

	ctx := codexSpawnCtx(t)
	member := teamtemplates.TeamTemplateMember{ID: "cod", Name: "Codey", Tier: "worker"}
	req := spawnTeamRequest{WorkingDir: t.TempDir(), Mission: "build"}
	teamConfigs := []*office.TeamMemberConfig{{ID: "cod", Name: "Codey"}}

	// openai-custom => prov != nil => no auth.json staging required.
	envVars := map[string]string{"OPENAI_API_KEY": "sk-test-key"}
	sessionID, mode := ctx.spawnCodexAgentSession(member, req, envVars, teamConfigs, true, nil, "openai-custom", "gpt-5.5")

	if mode != "codex" {
		t.Fatalf("expected codex mode, got %q (sessionID=%q)", mode, sessionID)
	}
	if sessionID == "" {
		t.Fatal("expected non-empty sessionID")
	}
	t.Cleanup(func() {
		_ = ctx.SessionManager.StopCodexSession(sessionID)
		time.Sleep(50 * time.Millisecond)
	})
	if _, ok := ctx.SessionManager.GetCodexSession(sessionID); !ok {
		t.Errorf("codex session %s not registered", sessionID)
	}
}

func TestSpawnCodexAgentSession_SubscriptionNoAuthFails(t *testing.T) {
	skipIfNoStubShell(t)
	// HOME has no ~/.jarvis/codex-auth/auth.json and no ~/.codex/auth.json, so
	// the prov==nil (openai-subscription) path fails to stage auth -> "","".
	ctx := codexSpawnCtx(t)
	member := teamtemplates.TeamTemplateMember{ID: "sub", Name: "Sub", Tier: "lead"}
	req := spawnTeamRequest{WorkingDir: t.TempDir(), Mission: "x"}
	teamConfigs := []*office.TeamMemberConfig{{ID: "sub", Name: "Sub"}}

	sessionID, mode := ctx.spawnCodexAgentSession(member, req, map[string]string{}, teamConfigs, true, nil, "openai-subscription", "")
	if sessionID != "" || mode != "" {
		t.Fatalf("expected empty result when codex auth.json is missing, got (%q,%q)", sessionID, mode)
	}
}

func TestSpawnAgentSession_RoutesToCodexHarness(t *testing.T) {
	// HarnessFor("openai-custom") == "codex" so spawnAgentSession short-circuits
	// into spawnCodexAgentSession. Verifying the routing covers that branch.
	stream := strings.Join([]string{
		`{"type":"thread.started","thread_id":"th-2"}`,
		`{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}`,
		"",
	}, "\n")
	installCodexOnPATH(t, stream)

	ctx := codexSpawnCtx(t)
	member := teamtemplates.TeamTemplateMember{ID: "r1", Name: "Router", Tier: "worker"}
	req := spawnTeamRequest{WorkingDir: t.TempDir(), Mission: "route"}
	teamConfigs := []*office.TeamMemberConfig{{ID: "r1", Name: "Router"}}

	sessionID, mode := spawnAgentSession(ctx, member, req, map[string]string{"OPENAI_API_KEY": "sk-x"},
		teamConfigs, true, true, nil, "openai-custom", "gpt-5.5", spawnAgentSpec{})
	if mode != "codex" {
		t.Fatalf("expected routing into codex harness, got mode %q", mode)
	}
	t.Cleanup(func() {
		if sessionID != "" {
			_ = ctx.SessionManager.StopCodexSession(sessionID)
			time.Sleep(50 * time.Millisecond)
		}
	})
	_ = context.Background()
}
