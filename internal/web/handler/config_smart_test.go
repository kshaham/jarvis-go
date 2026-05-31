package handler

// Coverage for the smart-kickstart / smart-update preview generators and the
// remaining respawn + prompt-processor config getters/setters in config.go.
// All are thin settings-store reads/writes, so they run hermetically against
// the temp-dir-backed StateStore from newConfigCtx.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPreviewSmartKickstart_SourcesFromInputsAndSettings(t *testing.T) {
	ctx := newConfigCtx(t)
	// Seed a kickstart template + learning data so all four source branches fire.
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"smartKickstart":  map[string]interface{}{"template": "do the thing"},
		"respawnLearning": []interface{}{map[string]interface{}{"note": "x"}},
	})

	rec := postJSON(t, ctx.PreviewSmartKickstart, "/api/respawn/smart-kickstart/preview", map[string]interface{}{
		"workingDir":  "/srv/app",
		"sessionName": "alpha",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	if resp["success"] != true {
		t.Errorf("success=%v, want true", resp["success"])
	}
	prompt, _ := resp["prompt"].(string)
	if !strings.Contains(prompt, "alpha") || !strings.Contains(prompt, "/srv/app") || !strings.Contains(prompt, "do the thing") {
		t.Errorf("prompt missing expected parts: %q", prompt)
	}
	sources, _ := resp["sources"].([]interface{})
	// session_name, working_dir, kickstart_config, learning_data
	if len(sources) != 4 {
		t.Errorf("expected 4 sources, got %d: %v", len(sources), sources)
	}
}

func TestPreviewSmartKickstart_EmptyBodyNoSources(t *testing.T) {
	ctx := newConfigCtx(t)
	rec := postJSON(t, ctx.PreviewSmartKickstart, "/api/respawn/smart-kickstart/preview", map[string]interface{}{})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	resp := decodeMap(t, rec)
	sources, _ := resp["sources"].([]interface{})
	if len(sources) != 0 {
		t.Errorf("expected 0 sources with empty inputs/settings, got %v", sources)
	}
	// Prompt still carries the header line.
	if p, _ := resp["prompt"].(string); !strings.Contains(p, "Smart Kickstart Prompt") {
		t.Errorf("prompt should keep header, got %q", p)
	}
}

func TestPreviewSmartKickstart_BadJSON(t *testing.T) {
	ctx := newConfigCtx(t)
	req := httptest.NewRequest("POST", "/api/respawn/smart-kickstart/preview", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.PreviewSmartKickstart(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestPreviewSmartUpdate_Sources(t *testing.T) {
	ctx := newConfigCtx(t)
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"respawnLearning": []interface{}{map[string]interface{}{"note": "y"}},
	})
	rec := postJSON(t, ctx.PreviewSmartUpdate, "/api/respawn/smart-update/preview", map[string]interface{}{
		"workingDir":  "/srv/beta",
		"sessionName": "bravo",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	resp := decodeMap(t, rec)
	prompt, _ := resp["prompt"].(string)
	if !strings.Contains(prompt, "Smart Update Prompt") || !strings.Contains(prompt, "bravo") || !strings.Contains(prompt, "/srv/beta") {
		t.Errorf("unexpected prompt: %q", prompt)
	}
	sources, _ := resp["sources"].([]interface{})
	// session_name, working_dir, learning_data
	if len(sources) != 3 {
		t.Errorf("expected 3 sources, got %d: %v", len(sources), sources)
	}
}

func TestPreviewSmartUpdate_BadJSON(t *testing.T) {
	ctx := newConfigCtx(t)
	req := httptest.NewRequest("POST", "/api/respawn/smart-update/preview", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.PreviewSmartUpdate(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestGetRespawnSmartKickstart_RoundTrip(t *testing.T) {
	ctx := newConfigCtx(t)
	// Empty default.
	resp := getJSON(t, ctx.GetRespawnSmartKickstart, "/api/respawn/smart-kickstart")
	if resp["success"] != true {
		t.Errorf("success=%v, want true", resp["success"])
	}

	// Write via the updater, then re-read.
	upd := postJSON(t, ctx.UpdateRespawnSmartKickstart, "/api/respawn/smart-kickstart", map[string]interface{}{
		"template": "kick it",
	})
	if upd.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d", upd.Code)
	}
	resp2 := getJSON(t, ctx.GetRespawnSmartKickstart, "/api/respawn/smart-kickstart")
	sk, _ := resp2["smartKickstart"].(map[string]interface{})
	if sk["template"] != "kick it" {
		t.Errorf("smartKickstart.template=%v, want 'kick it'", sk["template"])
	}
}

func TestUpdateRespawnSmartKickstart_BadJSON(t *testing.T) {
	ctx := newConfigCtx(t)
	req := httptest.NewRequest("PUT", "/api/respawn/smart-kickstart", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.UpdateRespawnSmartKickstart(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestGetRespawnSmartKickstartStats_DefaultsToEmptyMap(t *testing.T) {
	ctx := newConfigCtx(t)
	resp := getJSON(t, ctx.GetRespawnSmartKickstartStats, "/api/respawn/smart-kickstart/stats")
	stats, ok := resp["stats"].(map[string]interface{})
	if !ok {
		t.Fatalf("stats should be an object, got %T", resp["stats"])
	}
	if len(stats) != 0 {
		t.Errorf("expected empty stats, got %v", stats)
	}
}

func TestGetRespawnSmartKickstartStats_SeededValue(t *testing.T) {
	ctx := newConfigCtx(t)
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"smartKickstartStats": map[string]interface{}{"runs": float64(3)},
	})
	resp := getJSON(t, ctx.GetRespawnSmartKickstartStats, "/api/respawn/smart-kickstart/stats")
	stats, _ := resp["stats"].(map[string]interface{})
	if stats["runs"] != float64(3) {
		t.Errorf("stats.runs=%v, want 3", stats["runs"])
	}
}

func TestUpdatePromptProcessorContextInjector_NestsUnderPromptProcessor(t *testing.T) {
	ctx := newConfigCtx(t)
	// Seed an existing promptProcessor map so the merge-copy branch runs.
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"promptProcessor": map[string]interface{}{"enabled": true},
	})
	rec := postJSON(t, ctx.UpdatePromptProcessorContextInjector, "/api/prompt-processor/context-injector", map[string]interface{}{
		"config": map[string]interface{}{"maxChars": float64(500)},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	// The contextInjector must be nested INSIDE promptProcessor (regression
	// guard from the comment in the handler), and the pre-existing key kept.
	pp, _ := ctx.StateStore.GetSettings()["promptProcessor"].(map[string]interface{})
	if pp == nil {
		t.Fatal("promptProcessor map missing after update")
	}
	if pp["enabled"] != true {
		t.Errorf("pre-existing enabled flag should be preserved, got %v", pp["enabled"])
	}
	ci, _ := pp["contextInjector"].(map[string]interface{})
	if ci == nil || ci["maxChars"] != float64(500) {
		t.Errorf("contextInjector not nested correctly: %v", pp["contextInjector"])
	}
	// And no dotted top-level key was created.
	if _, dotted := ctx.StateStore.GetSettings()["promptProcessor.contextInjector"]; dotted {
		t.Error("handler created a dotted top-level key (regression)")
	}
}

func TestUpdatePromptProcessorContextInjector_BadJSON(t *testing.T) {
	ctx := newConfigCtx(t)
	req := httptest.NewRequest("PUT", "/api/prompt-processor/context-injector", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.UpdatePromptProcessorContextInjector(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestResetPromptProcessorStats_ClearsToEmptyMap(t *testing.T) {
	ctx := newConfigCtx(t)
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"promptProcessorStats": map[string]interface{}{"applied": float64(9)},
	})
	rec := postJSON(t, ctx.ResetPromptProcessorStats, "/api/prompt-processor/reset-stats", map[string]interface{}{})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	stats, _ := ctx.StateStore.GetSettings()["promptProcessorStats"].(map[string]interface{})
	if len(stats) != 0 {
		t.Errorf("expected stats cleared, got %v", stats)
	}
}

func TestPreviewPromptProcessor_PassthroughCountsRules(t *testing.T) {
	ctx := newConfigCtx(t)
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"promptProcessorRules": []interface{}{
			map[string]interface{}{"pattern": "a"},
			map[string]interface{}{"pattern": "b"},
		},
	})
	rec := postJSON(t, ctx.PreviewPromptProcessor, "/api/prompt-processor/preview", map[string]interface{}{
		"prompt":     "hello world",
		"workingDir": "/srv",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	resp := decodeMap(t, rec)
	if resp["result"] != "hello world" {
		t.Errorf("result should pass through unchanged, got %v", resp["result"])
	}
	if resp["rulesApplied"] != float64(2) {
		t.Errorf("rulesApplied=%v, want 2", resp["rulesApplied"])
	}
}

func TestPreviewPromptProcessor_BadJSON(t *testing.T) {
	ctx := newConfigCtx(t)
	req := httptest.NewRequest("POST", "/api/prompt-processor/preview", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.PreviewPromptProcessor(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestGetRespawnLearningCategories_CountsAndGeneralFallback(t *testing.T) {
	ctx := newConfigCtx(t)
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"respawnLearning": []interface{}{
			map[string]interface{}{"category": "build"},
			map[string]interface{}{"category": "build"},
			map[string]interface{}{}, // no category -> "general"
		},
	})
	resp := getJSON(t, ctx.GetRespawnLearningCategories, "/api/respawn/learning/categories")
	if resp["totalLearnings"] != float64(3) {
		t.Errorf("totalLearnings=%v, want 3", resp["totalLearnings"])
	}
	cats, _ := resp["categories"].(map[string]interface{})
	if cats["build"] != float64(2) {
		t.Errorf("categories.build=%v, want 2", cats["build"])
	}
	if cats["general"] != float64(1) {
		t.Errorf("categories.general=%v, want 1", cats["general"])
	}
}
