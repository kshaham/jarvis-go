package handler

// Round-4 fills for context.go provider/credential helper arms:
//   - getProviderEnvVars: AgentTokenMgr token injection (184-187) and the
//     openai-codex env-var key fallback (222)
//   - getProviderEnvVarsFor: openai-subscription drops the Opus pin (288-292)
//     and the zai-glm sonnet-model fallback chain (315-319)
//   - readClaudeOAuthToken: os.UserHomeDir error (344-346) and the
//     oauth-block-missing return (369-371)

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/kshaham/jarvis/internal/state"
	"github.com/kshaham/jarvis/internal/web/auth"
)

func TestGetProviderEnvVars_AgentTokenAndCodexEnvKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "env-codex-key")
	ctx := &Context{
		StateStore:    state.NewStore(filepath.Join(t.TempDir(), "s.json")),
		AgentTokenMgr: auth.NewAgentTokenManager(t.TempDir()),
	}
	// openai-codex with no operator key -> the env OPENAI_API_KEY fallback fires.
	ctx.StateStore.UpdateSettings(map[string]interface{}{"provider": "openai-codex"})

	env := ctx.getProviderEnvVars()
	if env["JARVIS_AGENT_TOKEN"] == "" {
		t.Error("expected JARVIS_AGENT_TOKEN injected from AgentTokenMgr")
	}
	if env["OPENAI_API_KEY"] != "env-codex-key" {
		t.Errorf("OPENAI_API_KEY=%q, want env-codex-key (env fallback)", env["OPENAI_API_KEY"])
	}
	if _, present := env["ANTHROPIC_DEFAULT_OPUS_MODEL"]; present {
		t.Error("codex provider must drop the Anthropic Opus pin")
	}
}

func TestGetProviderEnvVarsFor_OpenAISubscriptionDropsOpus(t *testing.T) {
	t.Parallel()
	ctx := &Context{StateStore: state.NewStore(filepath.Join(t.TempDir(), "s.json"))}
	env := ctx.getProviderEnvVarsFor("openai-subscription", "", "")
	if _, present := env["ANTHROPIC_DEFAULT_OPUS_MODEL"]; present {
		t.Error("openai-subscription must drop the Anthropic Opus pin")
	}
	// No API key vars for the subscription path.
	if _, present := env["OPENAI_API_KEY"]; present {
		t.Error("openai-subscription must not emit OPENAI_API_KEY")
	}
}

func TestGetProviderEnvVarsFor_ZaiSonnetFallbackDefault(t *testing.T) {
	t.Parallel()
	ctx := &Context{StateStore: state.NewStore(filepath.Join(t.TempDir(), "s.json"))}
	// zai key set but NO zaiSonnetModel and an empty sonnetOverride -> the
	// default "glm-4.7" fallback chain fires.
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"provider": "zai-glm", "zaiApiKey": "zai-key",
	})
	env := ctx.getProviderEnvVarsFor("zai-glm", "", "")
	if env["ANTHROPIC_DEFAULT_SONNET_MODEL"] != "glm-4.7" {
		t.Errorf("sonnet default=%q, want glm-4.7 fallback", env["ANTHROPIC_DEFAULT_SONNET_MODEL"])
	}
	// model "" -> default glm-5.2 mapped onto the opus alias.
	if env["ANTHROPIC_DEFAULT_OPUS_MODEL"] != "glm-5.2" {
		t.Errorf("opus default=%q, want glm-5.2", env["ANTHROPIC_DEFAULT_OPUS_MODEL"])
	}
}

func TestReadClaudeOAuthToken_NoHomeReturnsEmpty(t *testing.T) {
	t.Setenv("HOME", "") // os.UserHomeDir errors on an empty $HOME
	if tok := readClaudeOAuthToken(); tok != "" {
		t.Errorf("expected empty token when HOME is unset, got %q", tok)
	}
}

func TestReadClaudeOAuthToken_MissingOAuthBlockReturnsEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	credDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(credDir, 0o755); err != nil {
		t.Fatalf("mkdir cred dir: %v", err)
	}
	// Valid JSON but with neither claudeAiOauth nor oauthAccount -> oauth == nil.
	body, _ := json.Marshal(map[string]interface{}{"somethingElse": map[string]interface{}{"x": 1}})
	if err := os.WriteFile(filepath.Join(credDir, ".credentials.json"), body, 0o600); err != nil {
		t.Fatalf("write creds: %v", err)
	}
	if tok := readClaudeOAuthToken(); tok != "" {
		t.Errorf("expected empty token when the oauth block is missing, got %q", tok)
	}
}
