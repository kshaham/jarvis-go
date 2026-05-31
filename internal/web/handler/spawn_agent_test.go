package handler

// Coverage for the pure + file-backed helpers in spawn_agent.go: the office
// MCP map builder, the Codex MCP allowlist filter, the Claude->Codex MCP map
// translation, and the Codex subscription auth staging. The full spawn paths
// exec real subprocesses (claude/codex), so only these seams are unit-testable.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kshaham/jarvis/internal/office"
)

func TestBuildOfficeMCPServers_AlwaysIncludesCoreServers(t *testing.T) {
	t.Parallel()
	ctx := &Context{
		OfficeManager: office.NewManager(stubSessions{}, func(string, interface{}) {}, nil),
	}
	req := spawnTeamRequest{WorkingDir: "/srv/app"}
	servers := ctx.buildOfficeMCPServers(req, "sess-1", "tok-abc")

	// jarvis + codegraphcontext are unconditional.
	jarvis, ok := servers["jarvis"].(map[string]any)
	if !ok {
		t.Fatalf("expected jarvis MCP server, got %T", servers["jarvis"])
	}
	env, _ := jarvis["env"].(map[string]string)
	if env["JARVIS_SESSION_ID"] != "sess-1" {
		t.Errorf("JARVIS_SESSION_ID=%v, want sess-1", env["JARVIS_SESSION_ID"])
	}
	if env["JARVIS_AGENT_TOKEN"] != "tok-abc" {
		t.Errorf("JARVIS_AGENT_TOKEN=%v, want tok-abc", env["JARVIS_AGENT_TOKEN"])
	}
	if _, ok := servers["codegraphcontext"]; !ok {
		t.Error("expected codegraphcontext MCP server")
	}
}

func TestBuildOfficeMCPServers_OmitsAgentTokenWhenEmpty(t *testing.T) {
	t.Parallel()
	ctx := &Context{
		OfficeManager: office.NewManager(stubSessions{}, func(string, interface{}) {}, nil),
	}
	req := spawnTeamRequest{WorkingDir: "/srv/app"}
	servers := ctx.buildOfficeMCPServers(req, "sess-2", "")
	jarvis, _ := servers["jarvis"].(map[string]any)
	env, _ := jarvis["env"].(map[string]string)
	if _, present := env["JARVIS_AGENT_TOKEN"]; present {
		t.Error("JARVIS_AGENT_TOKEN should be omitted when token is empty")
	}
}

func TestLeanCodexMCPServers_FiltersToAllowlist(t *testing.T) {
	t.Parallel()
	full := map[string]any{
		"jarvis":              map[string]any{"command": "jarvis-mcp"},
		"codegraph":           map[string]any{"command": "codegraph"},
		"codegraphcontext":    map[string]any{"command": "cgc"},
		"context7":            map[string]any{"command": "npx"},
		"sequential-thinking": map[string]any{"command": "npx"},
		"shadcn":              map[string]any{"command": "npx"},
	}
	lean := leanCodexMCPServers(full)
	if len(lean) != 3 {
		t.Fatalf("expected 3 allowlisted servers, got %d: %v", len(lean), lean)
	}
	for _, want := range []string{"jarvis", "codegraph", "codegraphcontext"} {
		if _, ok := lean[want]; !ok {
			t.Errorf("expected %q in lean set", want)
		}
	}
	// Situational npx servers dropped.
	for _, dropped := range []string{"context7", "sequential-thinking", "shadcn"} {
		if _, ok := lean[dropped]; ok {
			t.Errorf("%q should have been dropped from Codex set", dropped)
		}
	}
}

func TestCodexMCPFromMap_TranslatesStdioOnly(t *testing.T) {
	t.Parallel()
	in := map[string]any{
		"jarvis": map[string]any{
			"command": "jarvis-mcp",
			"args":    []string{"--flag"},
			"env":     map[string]string{"K": "V"},
		},
		"noCommand": map[string]any{"type": "stdio"}, // dropped (no command)
		"badShape":  "not-a-map",                     // dropped (not a map)
	}
	out := codexMCPFromMap(in)
	if len(out) != 1 {
		t.Fatalf("expected 1 translated server, got %d: %v", len(out), out)
	}
	srv, ok := out["jarvis"]
	if !ok {
		t.Fatal("expected jarvis in output")
	}
	if srv.Command != "jarvis-mcp" {
		t.Errorf("command=%q, want jarvis-mcp", srv.Command)
	}
	if len(srv.Args) != 1 || srv.Args[0] != "--flag" {
		t.Errorf("args=%v, want [--flag]", srv.Args)
	}
	if srv.Env["K"] != "V" {
		t.Errorf("env[K]=%q, want V", srv.Env["K"])
	}
}

func TestCodexSubscriptionAuthDir(t *testing.T) {
	t.Parallel()
	got := codexSubscriptionAuthDir("/home/op")
	want := filepath.Join("/home/op", ".jarvis", "codex-auth")
	if got != want {
		t.Errorf("codexSubscriptionAuthDir=%q, want %q", got, want)
	}
}

func TestStageCodexAuth_FromPersistedDir(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	codexHome := t.TempDir()

	// Seed the persisted ~/.jarvis/codex-auth/auth.json.
	authDir := codexSubscriptionAuthDir(home)
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(authDir, "auth.json"), []byte(`{"token":"x"}`), 0o600); err != nil {
		t.Fatalf("seed auth: %v", err)
	}

	if err := stageCodexAuth(home, codexHome); err != nil {
		t.Fatalf("stageCodexAuth: %v", err)
	}
	// auth.json should now exist in codexHome with 0600 perms.
	dst := filepath.Join(codexHome, "auth.json")
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("staged auth missing: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("staged auth perms=%v, want 0600", info.Mode().Perm())
	}
	data, _ := os.ReadFile(dst)
	if string(data) != `{"token":"x"}` {
		t.Errorf("staged content=%q", string(data))
	}
}

func TestStageCodexAuth_NoCredentialsErrors(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	codexHome := t.TempDir()
	// Neither ~/.jarvis/codex-auth/auth.json nor ~/.codex/auth.json exists.
	err := stageCodexAuth(home, codexHome)
	if err == nil {
		t.Fatal("expected error when no codex auth.json exists")
	}
}
