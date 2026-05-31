package handler

// Branch fills for the office agent knowledge search/tail handlers (query
// validation, limit parsing, the all=true vs team-scoped paths) and the
// GeneratePlanDetailed no-case home-dir fallback.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// agentResponseBadJSON asserts a decode-body handler 400s on malformed input.
func agentResponseBadJSON(t *testing.T, h func(http.ResponseWriter, *http.Request)) {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/office/agent/x", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed body, got %d", rec.Code)
	}
}

func TestAgentResponseHandlers_BadJSON(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	agentResponseBadJSON(t, ctx.OfficeAgentBrainstormResponse)
	agentResponseBadJSON(t, ctx.OfficeAgentReflectionResponse)
	agentResponseBadJSON(t, ctx.OfficeAgentLeaderTasksResponse)
	agentResponseBadJSON(t, ctx.OfficeAgentCuriosityDiscovery)
}

func TestOfficeAgentKnowledgeTail_AllAndScoped(t *testing.T) {
	ctx := newOfficeExtCtx(t)

	// all=true skips the team-scope filter; limit parses.
	reqAll := httptest.NewRequest("GET", "/api/office/agent/knowledge/tail?all=true&limit=5", nil)
	recAll := httptest.NewRecorder()
	ctx.OfficeAgentKnowledgeTail(recAll, reqAll)
	if recAll.Code != http.StatusOK {
		t.Fatalf("tail (all): expected 200, got %d: %s", recAll.Code, recAll.Body.String())
	}

	// Default scoped path.
	reqScoped := httptest.NewRequest("GET", "/api/office/agent/knowledge/tail", nil)
	recScoped := httptest.NewRecorder()
	ctx.OfficeAgentKnowledgeTail(recScoped, reqScoped)
	if recScoped.Code != http.StatusOK {
		t.Fatalf("tail (scoped): expected 200, got %d", recScoped.Code)
	}
}

func TestOfficeAgentKnowledgeSearch_MissingQuery400(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	req := httptest.NewRequest("GET", "/api/office/agent/knowledge/search", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeAgentKnowledgeSearch(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing q: expected 400, got %d", rec.Code)
	}
}

func TestOfficeAgentKnowledgeSearch_AllScopeWithLimit(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	// all=true skips the team-scoped allowed-agents filter; limit parses.
	req := httptest.NewRequest("GET", "/api/office/agent/knowledge/search?q=auth&limit=5&all=true", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeAgentKnowledgeSearch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("knowledge search (all): expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if _, ok := resp["hits"]; !ok {
		t.Error("expected a hits field in the search response")
	}
}

func TestOfficeAgentKnowledgeSearch_TeamScoped(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	// Default (no all=true) takes the team-scoped branch -> liveTeamAgentSet +
	// liveTeamWorkingDir. With no spawned team these return empty, but the
	// branch still executes.
	req := httptest.NewRequest("GET", "/api/office/agent/knowledge/search?q=cache", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeAgentKnowledgeSearch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("knowledge search (scoped): expected 200, got %d", rec.Code)
	}
}

func TestGeneratePlanDetailed_NoCaseHomeFallback(t *testing.T) {
	ctx := newSessionLiveCtx(t)
	ctx.ActivePlanOrchestrators = map[string]string{}
	// No caseName -> the handler falls back to the home directory for the
	// plan session's working dir.
	rec := postJSON(t, ctx.GeneratePlanDetailed, "/api/generate-plan-detailed", map[string]interface{}{
		"taskDescription": "wire up CI",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("generate plan (no case): expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	data, _ := resp["data"].(map[string]interface{})
	sessionID, _ := data["sessionId"].(string)
	if sessionID == "" {
		t.Fatal("expected a session id")
	}
	t.Cleanup(func() {
		_ = ctx.SessionManager.StopSession(context.Background(), sessionID)
		ctx.cleanupSessionResources(sessionID)
	})
}
