package handler

// Coverage for the remaining easy office.go read handlers: knowledge
// search/tail, brainstorm start, and the wrap-up status getter.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOfficeAgentKnowledgeSearch_MissingQuery(t *testing.T) {
	ctx := newTimelineContext(t)
	req := httptest.NewRequest("GET", "/api/office/agent/knowledge/search", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeAgentKnowledgeSearch(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing q, got %d", rec.Code)
	}
}

func TestOfficeAgentKnowledgeSearch_EmptyRoster(t *testing.T) {
	ctx := newTimelineContext(t)
	// No agents registered and isolated HOME -> no knowledge files; search
	// returns an empty hit set with 200.
	req := httptest.NewRequest("GET", "/api/office/agent/knowledge/search?q=anything", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeAgentKnowledgeSearch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if _, ok := resp["hits"]; !ok {
		t.Error("expected hits key")
	}
}

func TestOfficeAgentKnowledgeTail_Empty(t *testing.T) {
	ctx := newTimelineContext(t)
	req := httptest.NewRequest("GET", "/api/office/agent/knowledge/tail?limit=5", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeAgentKnowledgeTail(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if _, ok := resp["hits"]; !ok {
		t.Error("expected hits key")
	}
}

func TestOfficeBrainstorm_InsufficientParticipants(t *testing.T) {
	ctx := newTimelineContext(t)
	// With no registered agents, StartBrainstorm returns nil -> 400.
	rec := postJSON(t, ctx.OfficeBrainstorm, "/api/office/brainstorm", map[string]interface{}{
		"participantIds": []string{"s-nobody"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for insufficient participants, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficeGetWrapUp_Inactive(t *testing.T) {
	ctx := newTimelineContext(t)
	req := httptest.NewRequest("GET", "/api/office/wrap-up", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeGetWrapUp(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["active"] != false {
		t.Errorf("expected active=false on a fresh team, got %v", resp["active"])
	}
}

func TestOfficePostWrapUp_NoTeamSpawned(t *testing.T) {
	ctx := newTimelineContext(t)
	req := httptest.NewRequest("POST", "/api/office/wrap-up", nil)
	rec := httptest.NewRecorder()
	ctx.OfficePostWrapUp(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when no team spawned, got %d", rec.Code)
	}
}
