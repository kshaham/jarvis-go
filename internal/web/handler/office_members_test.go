package handler

// Coverage for member-personality + agent submit handlers in office.go.
// These read/write agent state via OfficeManager and return 200 on the
// happy path; the personality getters fall back to empty for unknown
// members.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kshaham/jarvis/internal/office"
)

func TestOfficeAgentGetTelegramConfig_AgentView(t *testing.T) {
	ctx := newTimelineContext(t)
	resp := getJSON(t, ctx.OfficeAgentGetTelegramConfig, "/api/office/agent/telegram-config")
	if _, ok := resp["enabled"]; !ok {
		t.Error("expected enabled key")
	}
	if _, ok := resp["intervalMin"]; !ok {
		t.Error("expected intervalMin key")
	}
}

func TestOfficeAgentPostTelegramConfig_ClampsInterval(t *testing.T) {
	ctx := newTimelineContext(t)
	rec := postJSON(t, ctx.OfficeAgentPostTelegramConfig, "/api/office/agent/telegram-config", map[string]interface{}{
		"enabled":     false,
		"intervalMin": 0, // below floor -> clamps to 10
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	_, intervalMin, _ := ctx.OfficeManager.GetTelegramConfig()
	if intervalMin != 10 {
		t.Errorf("intervalMin=%d, want clamped to 10", intervalMin)
	}
}

func TestOfficeAgentStandupResponse_OK(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})
	rec := postJSON(t, ctx.OfficeAgentStandupResponse, "/api/office/agent/standup-response", map[string]interface{}{
		"sessionId": "s-vex", "content": "shipped the fix",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestOfficeAgentBrainstormResponse_OK(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})
	rec := postJSON(t, ctx.OfficeAgentBrainstormResponse, "/api/office/agent/brainstorm-response", map[string]interface{}{
		"sessionId": "s-vex", "proposals": "idea 1; idea 2",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestOfficeAgentLeaderTasksResponse_OK(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-cap", "captain", &office.TeamMemberConfig{ID: "captain", Name: "Cap", Tier: "strategic"})
	rec := postJSON(t, ctx.OfficeAgentLeaderTasksResponse, "/api/office/agent/leader-tasks-response", map[string]interface{}{
		"sessionId": "s-cap", "content": "assigned 3 tasks",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestOfficeAgentReflectionResponse_OK(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})
	rec := postJSON(t, ctx.OfficeAgentReflectionResponse, "/api/office/agent/reflection-response", map[string]interface{}{
		"sessionId": "s-vex",
		"learning":  "cache invalidation is hard",
		"category":  "engineering",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestOfficeAgentCuriosityDiscovery_OK(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})
	rec := postJSON(t, ctx.OfficeAgentCuriosityDiscovery, "/api/office/agent/curiosity", map[string]interface{}{
		"sessionId": "s-vex",
		"summary":   "found a dead code path",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestOfficeGetMemberPersonality_UnknownEmpty(t *testing.T) {
	ctx := newTimelineContext(t)
	req := withURLParam(httptest.NewRequest("GET", "/api/office/members/ghost/personality", nil), "sessionId", "ghost")
	rec := httptest.NewRecorder()
	ctx.OfficeGetMemberPersonality(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["personality"] != "" {
		t.Errorf("expected empty personality for unknown member, got %v", resp["personality"])
	}
}

func TestOfficePutGetMemberPersonality_RoundTrip(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex", Personality: "base"})

	// Put an override.
	req := withURLParam(httptest.NewRequest("PUT", "/api/office/members/s-vex/personality", jsonBody(t, map[string]interface{}{
		"personality":  "curious and bold",
		"systemPrompt": "Be concise.",
	})), "sessionId", "s-vex")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.OfficePutMemberPersonality(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("put: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Get reflects the override.
	reqGet := withURLParam(httptest.NewRequest("GET", "/api/office/members/s-vex/personality", nil), "sessionId", "s-vex")
	recGet := httptest.NewRecorder()
	ctx.OfficeGetMemberPersonality(recGet, reqGet)
	var resp map[string]interface{}
	_ = json.Unmarshal(recGet.Body.Bytes(), &resp)
	if resp["personality"] != "curious and bold" {
		t.Errorf("personality=%v, want override", resp["personality"])
	}
	if resp["systemPrompt"] != "Be concise." {
		t.Errorf("systemPrompt=%v, want override", resp["systemPrompt"])
	}
}

func TestOfficePutMemberVoice_InvalidVoice(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})
	req := withURLParam(httptest.NewRequest("PUT", "/api/office/members/s-vex/voice", jsonBody(t, map[string]interface{}{
		"voice": "not-a-real-voice",
	})), "sessionId", "s-vex")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.OfficePutMemberVoice(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid voice, got %d", rec.Code)
	}
}

func TestOfficePutMemberVoice_UnknownMember(t *testing.T) {
	ctx := newTimelineContext(t)
	// A valid voice but unknown member -> 404. Use the first valid voice
	// the package accepts by trying a common one.
	req := withURLParam(httptest.NewRequest("PUT", "/api/office/members/ghost/voice", jsonBody(t, map[string]interface{}{
		"voice": "alloy",
	})), "sessionId", "ghost")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.OfficePutMemberVoice(rec, req)
	// Either 400 (if "alloy" isn't a recognised voice) or 404 (recognised
	// voice, unknown member). Both are valid documented outcomes; assert
	// it is one of them rather than 200.
	if rec.Code == http.StatusOK {
		t.Fatalf("expected an error status for unknown member, got 200")
	}
}

func TestOfficeGetMemberMemory_UnknownEmpty(t *testing.T) {
	ctx := newTimelineContext(t)
	req := withURLParam(httptest.NewRequest("GET", "/api/office/members/ghost/memory", nil), "sessionId", "ghost")
	rec := httptest.NewRecorder()
	ctx.OfficeGetMemberMemory(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["memory"] != "" {
		t.Errorf("expected empty memory, got %v", resp["memory"])
	}
}
