package codexcfg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMapReasoningEffort(t *testing.T) {
	cases := map[string]string{
		"low": "low", "medium": "medium", "high": "high",
		"max": "xhigh", "xhigh": "xhigh", "minimal": "minimal",
		"": "", "bogus": "",
	}
	for in, want := range cases {
		if got := MapReasoningEffort(in); got != want {
			t.Errorf("MapReasoningEffort(%q)=%q want %q", in, got, want)
		}
	}
}

func TestRender_FeatureKnobs(t *testing.T) {
	out := Render(Config{
		Model:            "gpt-5.4",
		SandboxMode:      "danger-full-access",
		ApprovalPolicy:   "never",
		WebSearch:        "live",
		Personality:      "none",
		AgentsMaxThreads: 4,
		AgentsMaxDepth:   1,
	})
	for _, want := range []string{
		"web_search = \"live\"",
		"personality = \"none\"",
		"[agents]",
		"max_threads = 4",
		"max_depth = 1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Render missing %q\n---\n%s", want, out)
		}
	}
	// All top-level keys must precede the [agents] table (valid TOML).
	if idx := strings.Index(out, "[agents]"); idx >= 0 {
		if strings.Index(out, "web_search") > idx || strings.Index(out, "personality") > idx {
			t.Errorf("top-level keys must come before [agents] table:\n%s", out)
		}
	}
	// Empty knobs omit their keys entirely.
	bare := Render(Config{Model: "gpt-5.4"})
	for _, absent := range []string{"web_search", "personality", "[agents]"} {
		if strings.Contains(bare, absent) {
			t.Errorf("bare config should omit %q, got:\n%s", absent, bare)
		}
	}
}

func TestRender_WorkspaceWriteSandbox(t *testing.T) {
	out := Render(Config{
		Model:                 "gpt-5.4",
		SandboxMode:           "workspace-write",
		WorkspaceWriteNetwork: true,
		ApprovalPolicy:        "never",
	})
	for _, want := range []string{
		"sandbox_mode = \"workspace-write\"",
		"[sandbox_workspace_write]",
		"network_access = true",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Render missing %q\n---\n%s", want, out)
		}
	}
	// The table is only meaningful under workspace-write; danger-full-access omits it.
	full := Render(Config{Model: "gpt-5.4", SandboxMode: "danger-full-access"})
	if strings.Contains(full, "[sandbox_workspace_write]") {
		t.Errorf("danger-full-access must not render [sandbox_workspace_write]:\n%s", full)
	}
}

func TestRender_CustomProvider(t *testing.T) {
	out := Render(Config{
		Model:           "gpt-5.5",
		ReasoningEffort: "high",
		SandboxMode:     "danger-full-access",
		ApprovalPolicy:  "never",
		Provider: &Provider{
			ID:                 "jarvis-openai",
			Name:               "OpenAI (jarvis)",
			BaseURL:            "https://proxy.internal.example.com/v1",
			EnvKey:             "OPENAI_API_KEY",
			WireAPI:            "responses",
			RequiresOpenAIAuth: false,
		},
		MCPServers: map[string]MCPServer{
			"jarvis":    {Command: "jarvis-mcp", Env: map[string]string{"JARVIS_API_URL": "http://localhost:3555"}},
			"codegraph": {Command: "codegraph", Args: []string{"serve", "--mcp", "--path", "/w"}},
		},
	})
	// Provider block + the auth shape proven in the spike.
	for _, want := range []string{
		`model = "gpt-5.5"`,
		`model_provider = "jarvis-openai"`,
		`model_reasoning_effort = "high"`,
		`approval_policy = "never"`,
		`sandbox_mode = "danger-full-access"`,
		`[model_providers.jarvis-openai]`,
		`base_url = "https://proxy.internal.example.com/v1"`,
		`env_key = "OPENAI_API_KEY"`,
		`wire_api = "responses"`,
		`requires_openai_auth = false`,
		`[mcp_servers.codegraph]`,
		`command = "codegraph"`,
		`args = ["serve", "--mcp", "--path", "/w"]`,
		`[mcp_servers.jarvis]`,
		`env = { "JARVIS_API_URL" = "http://localhost:3555" }`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered config missing %q\n---\n%s", want, out)
		}
	}
}

func TestRender_Subscription_NoProviderBlock(t *testing.T) {
	out := Render(Config{Model: "gpt-5.5", ApprovalPolicy: "never", SandboxMode: "danger-full-access"})
	if strings.Contains(out, "[model_providers.") {
		t.Errorf("subscription config must not emit a provider block:\n%s", out)
	}
	if strings.Contains(out, "model_provider =") {
		t.Errorf("subscription config must not set model_provider (uses built-in openai):\n%s", out)
	}
	if !strings.Contains(out, `model = "gpt-5.5"`) {
		t.Errorf("missing model line:\n%s", out)
	}
}

func TestWrite_AtomicAndPerms(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "codex-home")
	path, err := Write(home, Config{Model: "gpt-5.5", ApprovalPolicy: "never"})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if path != filepath.Join(home, "config.toml") {
		t.Errorf("path=%q", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(b), `model = "gpt-5.5"`) {
		t.Errorf("written config missing model:\n%s", b)
	}
	if _, err := os.Stat(filepath.Join(home, "config.toml.tmp")); !os.IsNotExist(err) {
		t.Errorf("temp file should be gone after atomic rename")
	}
}
