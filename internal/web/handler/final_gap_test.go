package handler

// Final gap-fill coverage: GetTokenStats per-session loop (live session),
// OfficeGetMemberMemory agent-found branch, Case create/delete SSE-broadcast
// branches, and GeneratePlanDetailed's session-spawn happy path.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/kshaham/jarvis/internal/cases"
	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/state"
	"github.com/kshaham/jarvis/internal/web/sse"
)

func TestGetTokenStats_WithLiveSession(t *testing.T) {
	ctx := newSessionLiveCtx(t)
	id, _ := liveSession(t, ctx)

	resp := getJSON(t, ctx.GetTokenStats, "/api/token-stats")
	stats, _ := resp["stats"].(map[string]interface{})
	if stats["sessionCount"] != float64(1) {
		t.Errorf("sessionCount=%v, want 1", stats["sessionCount"])
	}
	perSession, _ := stats["sessions"].([]interface{})
	if len(perSession) != 1 {
		t.Fatalf("expected 1 per-session entry, got %d", len(perSession))
	}
	entry, _ := perSession[0].(map[string]interface{})
	if entry["sessionId"] != id {
		t.Errorf("per-session id=%v, want %s", entry["sessionId"], id)
	}
}

func TestOfficeGetMemberMemory_AgentFound(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	ctx.OfficeManager.RegisterAgent("sess-m", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})

	req := withURLParam(httptest.NewRequest("GET", "/api/office/members/sess-m/memory", nil), "sessionId", "sess-m")
	rec := httptest.NewRecorder()
	ctx.OfficeGetMemberMemory(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	resp := decodeMap(t, rec)
	if _, ok := resp["memory"]; !ok {
		t.Error("expected memory key for a registered agent")
	}
}

func TestCreateAndDeleteCase_BroadcastBranch(t *testing.T) {
	ctx := &Context{
		CaseManager: cases.NewCaseManager(t.TempDir()),
		SSEBroker:   sse.NewBroker(100, 10),
	}
	// Create -> 200 (and the SSE broadcast branch runs because the broker is set).
	recC := postJSON(t, ctx.CreateCase, "/api/cases", map[string]interface{}{"name": "bcast"})
	if recC.Code != http.StatusOK {
		t.Fatalf("create case: expected 200, got %d: %s", recC.Code, recC.Body.String())
	}
	// Delete -> 200.
	reqD := withURLParam(httptest.NewRequest("DELETE", "/api/cases/bcast", nil), "name", "bcast")
	recD := httptest.NewRecorder()
	ctx.DeleteCase(recD, reqD)
	if recD.Code != http.StatusOK {
		t.Fatalf("delete case: expected 200, got %d", recD.Code)
	}
}

func TestGeneratePlanDetailed_HappySpawn(t *testing.T) {
	ctx := newSessionLiveCtx(t)
	// Attach a case so the working dir resolves to the case path.
	cm := cases.NewCaseManager(t.TempDir())
	ctx.CaseManager = cm
	caseInfo, err := cm.Create("plan-case")
	if err != nil {
		t.Fatalf("create case: %v", err)
	}
	_ = caseInfo
	ctx.ActivePlanOrchestrators = map[string]string{}
	ctx.StateStore = state.NewStore(filepath.Join(t.TempDir(), "s.json"))

	rec := postJSON(t, ctx.GeneratePlanDetailed, "/api/generate-plan-detailed", map[string]interface{}{
		"taskDescription": "Add a health endpoint", "caseName": "plan-case",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("generate plan detailed: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	data, _ := resp["data"].(map[string]interface{})
	sessionID, _ := data["sessionId"].(string)
	orchID, _ := data["orchestratorId"].(string)
	if sessionID == "" || orchID == "" {
		t.Fatalf("expected sessionId + orchestratorId, got %v", data)
	}
	// Immediately stop the spawned (Claude) session so the deferred 2s prompt
	// injection no-ops and the subprocess is torn down promptly.
	t.Cleanup(func() {
		_ = ctx.SessionManager.StopSession(context.Background(), sessionID)
		ctx.cleanupSessionResources(sessionID)
	})
	if _, ok := ctx.ActivePlanOrchestrators[orchID]; !ok {
		t.Error("expected the orchestrator to be tracked")
	}
}
