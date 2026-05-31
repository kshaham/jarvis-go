package handler

// Coverage for getProviderEnvVars (all provider branches), OfficePutSettings
// happy path, and OfficePostWrapUp (no-team 400 + spawned happy path).

import (
	"net/http"
	"testing"
)

func TestGetProviderEnvVars_AllProviders(t *testing.T) {
	t.Parallel()
	ctx := newConfigCtx(t)

	// Subscription (default): no auth vars, but the Opus pin is present.
	ctx.StateStore.UpdateSettings(map[string]interface{}{"provider": "claude-subscription"})
	env := ctx.getProviderEnvVars()
	if env["ANTHROPIC_DEFAULT_OPUS_MODEL"] != "claude-opus-4-8" {
		t.Errorf("subscription opus pin=%q, want claude-opus-4-8", env["ANTHROPIC_DEFAULT_OPUS_MODEL"])
	}
	if _, present := env["ANTHROPIC_API_KEY"]; present {
		t.Error("subscription mode must not set ANTHROPIC_API_KEY")
	}

	// claude-api.
	ctx.StateStore.UpdateSettings(map[string]interface{}{"provider": "claude-api", "claudeApiKey": "sk-ant-key"})
	envAPI := ctx.getProviderEnvVars()
	if envAPI["ANTHROPIC_API_KEY"] != "sk-ant-key" {
		t.Errorf("claude-api key=%q, want sk-ant-key", envAPI["ANTHROPIC_API_KEY"])
	}

	// openai-subscription drops the Opus pin.
	ctx.StateStore.UpdateSettings(map[string]interface{}{"provider": "openai-subscription"})
	envSub := ctx.getProviderEnvVars()
	if _, present := envSub["ANTHROPIC_DEFAULT_OPUS_MODEL"]; present {
		t.Error("openai-subscription must drop the Opus pin")
	}

	// openai-codex with an operator key.
	ctx.StateStore.UpdateSettings(map[string]interface{}{"provider": "openai-codex", "openaiApiKey": "sk-openai"})
	envCodex := ctx.getProviderEnvVars()
	if envCodex["OPENAI_API_KEY"] != "sk-openai" {
		t.Errorf("codex OPENAI_API_KEY=%q, want sk-openai", envCodex["OPENAI_API_KEY"])
	}
	if envCodex["CODEX_API_KEY"] != "sk-openai" {
		t.Errorf("codex CODEX_API_KEY=%q, want sk-openai", envCodex["CODEX_API_KEY"])
	}

	// zai-glm with defaults filled in.
	ctx.StateStore.UpdateSettings(map[string]interface{}{"provider": "zai-glm", "zaiApiKey": "zai-key"})
	envZ := ctx.getProviderEnvVars()
	if envZ["ANTHROPIC_AUTH_TOKEN"] != "zai-key" {
		t.Errorf("zai auth token=%q, want zai-key", envZ["ANTHROPIC_AUTH_TOKEN"])
	}
	if envZ["ANTHROPIC_DEFAULT_OPUS_MODEL"] != "glm-5.1" {
		t.Errorf("zai default opus=%q, want glm-5.1", envZ["ANTHROPIC_DEFAULT_OPUS_MODEL"])
	}
	if envZ["ANTHROPIC_BASE_URL"] == "" {
		t.Error("zai-glm must set a base URL")
	}
}

func TestOfficePutSettings_HappyWithAutoStandup(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	rec := postJSON(t, ctx.OfficePutSettings, "/api/office/settings", map[string]interface{}{
		"autoStandup": map[string]interface{}{
			"enabled":    true,
			"intervalMs": float64(120000),
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("put settings: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	// The handler echoes the updated settings.
	resp := decodeMap(t, rec)
	if resp["success"] != true {
		t.Errorf("success=%v, want true", resp["success"])
	}
}

func TestOfficePostWrapUp_NoTeam400(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	rec := postJSON(t, ctx.OfficePostWrapUp, "/api/office/wrap-up", map[string]interface{}{})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when no team spawned, got %d", rec.Code)
	}
}

func TestOfficePostWrapUp_SpawnedHappyAndIdempotent(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	// Mark a team spawned so the wrap-up flow runs.
	ctx.OfficeManager.SetTeamSpawned(true)

	rec := postJSON(t, ctx.OfficePostWrapUp, "/api/office/wrap-up", map[string]interface{}{})
	if rec.Code != http.StatusOK {
		t.Fatalf("first wrap-up: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	if resp["active"] != true {
		t.Errorf("active=%v, want true after wrap-up", resp["active"])
	}

	// A second call while already wrapping up returns alreadyActive.
	rec2 := postJSON(t, ctx.OfficePostWrapUp, "/api/office/wrap-up", map[string]interface{}{})
	if rec2.Code != http.StatusOK {
		t.Fatalf("second wrap-up: expected 200, got %d", rec2.Code)
	}
	resp2 := decodeMap(t, rec2)
	if resp2["alreadyActive"] != true {
		t.Errorf("alreadyActive=%v, want true on repeat wrap-up", resp2["alreadyActive"])
	}
}
