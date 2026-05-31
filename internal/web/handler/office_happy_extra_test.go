package handler

// Final office happy-path coverage: template-from-team (spawned + roster
// snapshot), learnings-by-config (seeded file), and get-standup-by-id (seeded
// file) -- all file-backed so they run hermetically under a temp HOME.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/office"
)

func TestOfficeTemplateFromTeam_Happy(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	ctx.OfficeManager.RegisterAgent("sess-a", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex", Role: "lead"})
	ctx.OfficeManager.SetTeamSpawned(true)

	rec := postJSON(t, ctx.OfficeTemplateFromTeam, "/api/office/template-from-team", map[string]interface{}{
		"name": "Saved Team", "description": "captured", "category": "custom",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("template-from-team: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	// The new template should be persisted in the store.
	if len(ctx.TeamTemplateStore.List()) != 1 {
		t.Errorf("expected 1 saved template, got %d", len(ctx.TeamTemplateStore.List()))
	}
}

func TestOfficeTemplateFromTeam_NoTeam400(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	rec := postJSON(t, ctx.OfficeTemplateFromTeam, "/api/office/template-from-team", map[string]interface{}{
		"name": "x",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no team: expected 400, got %d", rec.Code)
	}
}

func TestOfficeGetLearningsByConfig_Happy(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	// Seed a learning file for config "vex".
	ld := &office.AgentLearningData{
		ConfigID: "vex",
		Name:     "Vex",
		Learnings: []office.AgentLearning{
			{Category: "build", Content: "always run go vet", CreatedAt: time.Now()},
		},
	}
	if err := office.SaveLearningData("vex", ld); err != nil {
		t.Fatalf("save learning: %v", err)
	}

	req := withURLParam(httptest.NewRequest("GET", "/api/office/learnings/vex", nil), "configId", "vex")
	rec := httptest.NewRecorder()
	ctx.OfficeGetLearningsByConfig(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("learnings by config: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	if resp["configId"] != "vex" {
		t.Errorf("configId=%v, want vex", resp["configId"])
	}
	if resp["name"] != "Vex" {
		t.Errorf("name=%v, want Vex", resp["name"])
	}
	learnings, _ := resp["learnings"].([]interface{})
	if len(learnings) == 0 {
		t.Error("expected at least one learning entry")
	}
}

func TestOfficeGetStandup_ActiveWithResponseSerializes(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	a := "sess-a"
	b := "sess-b"
	ctx.OfficeManager.RegisterAgent(a, "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})
	ctx.OfficeManager.RegisterAgent(b, "bolt", &office.TeamMemberConfig{ID: "bolt", Name: "Bolt"})

	// Start a standup and submit a response so the transcript + responses maps
	// are populated and the currentSpeakerID branch resolves to a participant.
	ctx.OfficeManager.StartStandup([]string{a, b})
	ctx.OfficeManager.SubmitStandupResponse(a, "shipped the parser")

	resp := getJSON(t, ctx.OfficeGetStandup, "/api/office/standup")
	su, _ := resp["standup"].(map[string]interface{})
	if su == nil {
		t.Fatal("expected a non-nil active standup")
	}
	if su["active"] != true {
		t.Errorf("active=%v, want true", su["active"])
	}
	// currentSpeakerId should be one of the participants (non-empty).
	if speaker, _ := su["currentSpeakerId"].(string); speaker == "" {
		t.Error("expected a non-empty currentSpeakerId for an active standup")
	}
	ctx.OfficeManager.StopStandup()
}

func TestOfficeGetStandupByID_Happy(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	// Seed a standup JSON in the standup dir (HOME is already a temp dir).
	dir := office.StandupDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	payload := map[string]interface{}{"id": "sdone", "participants": []string{"a"}}
	data, _ := json.Marshal(payload)
	if err := os.WriteFile(filepath.Join(dir, "sdone.json"), data, 0o644); err != nil {
		t.Fatalf("write standup: %v", err)
	}

	req := withURLParam(httptest.NewRequest("GET", "/api/office/standups/sdone", nil), "standupId", "sdone")
	rec := httptest.NewRecorder()
	ctx.OfficeGetStandupByID(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get standup by id: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	if resp["id"] != "sdone" {
		t.Errorf("id=%v, want sdone", resp["id"])
	}
}
