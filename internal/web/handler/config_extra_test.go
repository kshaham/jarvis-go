package handler

// Coverage for the prompt-processor + respawn-learning config getters and
// setters in config.go. All are thin settings-store reads/writes.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPromptProcessor_GetUpdate(t *testing.T) {
	ctx := newConfigCtx(t)

	// Empty get.
	resp := getJSON(t, ctx.GetPromptProcessor, "/api/prompt-processor")
	if resp["success"] != true {
		t.Errorf("expected success=true, got %v", resp["success"])
	}

	// Update, then re-get.
	rec := postJSON(t, ctx.UpdatePromptProcessor, "/api/prompt-processor", map[string]interface{}{"enabled": true})
	if rec.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d", rec.Code)
	}
	resp2 := getJSON(t, ctx.GetPromptProcessor, "/api/prompt-processor")
	pp, _ := resp2["promptProcessor"].(map[string]interface{})
	if pp["enabled"] != true {
		t.Errorf("promptProcessor.enabled=%v, want true", pp["enabled"])
	}
}

func TestUpdatePromptProcessor_BadJSON(t *testing.T) {
	ctx := newConfigCtx(t)
	req := httptest.NewRequest("PUT", "/api/prompt-processor", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.UpdatePromptProcessor(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestPromptProcessorRules_GetUpdate(t *testing.T) {
	ctx := newConfigCtx(t)

	// Empty -> [].
	resp := getJSON(t, ctx.GetPromptProcessorRules, "/api/prompt-processor/rules")
	rules, _ := resp["rules"].([]interface{})
	if len(rules) != 0 {
		t.Errorf("expected empty rules, got %d", len(rules))
	}

	// Update with a list.
	rec := postJSON(t, ctx.UpdatePromptProcessorRules, "/api/prompt-processor/rules", []map[string]interface{}{
		{"pattern": "foo", "replace": "bar"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update rules: expected 200, got %d", rec.Code)
	}
	resp2 := getJSON(t, ctx.GetPromptProcessorRules, "/api/prompt-processor/rules")
	rules2, _ := resp2["rules"].([]interface{})
	if len(rules2) != 1 {
		t.Errorf("expected 1 rule, got %d", len(rules2))
	}
}

func TestUpdatePromptProcessorRules_BadJSON(t *testing.T) {
	ctx := newConfigCtx(t)
	req := httptest.NewRequest("PUT", "/api/prompt-processor/rules", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.UpdatePromptProcessorRules(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestGetRespawnLearning_Empty(t *testing.T) {
	ctx := newConfigCtx(t)
	resp := getJSON(t, ctx.GetRespawnLearning, "/api/respawn/learning")
	if resp["success"] != true {
		t.Errorf("expected success=true, got %v", resp["success"])
	}
}

func TestGetRespawnLearningStats_DefaultsToEmptyMap(t *testing.T) {
	ctx := newConfigCtx(t)
	resp := getJSON(t, ctx.GetRespawnLearningStats, "/api/respawn/learning/stats")
	stats, ok := resp["stats"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected stats to be an object, got %T", resp["stats"])
	}
	if len(stats) != 0 {
		t.Errorf("expected empty stats, got %v", stats)
	}
}

func TestGetRespawnLearningRecommendations_FiltersByWorkingDir(t *testing.T) {
	ctx := newConfigCtx(t)
	// Seed two learning entries with different workingDirs.
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"respawnLearning": []interface{}{
			map[string]interface{}{"workingDir": "/srv/alpha", "note": "a"},
			map[string]interface{}{"workingDir": "/srv/beta", "note": "b"},
		},
	})

	// Filter for alpha.
	req := httptest.NewRequest("GET", "/api/respawn/learning/recommendations?workingDir=alpha", nil)
	rec := httptest.NewRecorder()
	ctx.GetRespawnLearningRecommendations(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	resp := decodeMap(t, rec)
	recs, _ := resp["recommendations"].([]interface{})
	if len(recs) != 1 {
		t.Fatalf("expected 1 recommendation matching alpha, got %d", len(recs))
	}

	// No filter -> all entries.
	reqAll := httptest.NewRequest("GET", "/api/respawn/learning/recommendations", nil)
	recAll := httptest.NewRecorder()
	ctx.GetRespawnLearningRecommendations(recAll, reqAll)
	respAll := decodeMap(t, recAll)
	recsAll, _ := respAll["recommendations"].([]interface{})
	if len(recsAll) != 2 {
		t.Errorf("expected 2 recommendations with no filter, got %d", len(recsAll))
	}
}

func TestGetRespawnLearningCategories_CountsByCategory(t *testing.T) {
	ctx := newConfigCtx(t)
	resp := getJSON(t, ctx.GetRespawnLearningCategories, "/api/respawn/learning/categories")
	if resp["success"] != true {
		t.Errorf("expected success=true, got %v", resp["success"])
	}
}
