package handler

// Top-up coverage for a handful of partially-covered handlers: the
// specific-orchestrator branch of CancelPlanGeneration (with a live session),
// the memory-section append branch, the delete-skill happy path, and
// getSessionWorkingDir for a live session.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/pty"
)

func TestCancelPlanGeneration_SpecificStopsLiveSession(t *testing.T) {
	ctx := newSessionLiveCtx(t)
	id, _ := liveSession(t, ctx)
	ctx.ActivePlanOrchestrators = map[string]string{"orch-1": id}

	rec := postJSON(t, ctx.CancelPlanGeneration, "/api/cancel-plan-generation", map[string]interface{}{
		"orchestratorId": "orch-1",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel specific: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	data, _ := resp["data"].(map[string]interface{})
	cancelled, _ := data["cancelled"].([]interface{})
	if len(cancelled) != 1 {
		t.Errorf("expected 1 cancelled orchestrator, got %d", len(cancelled))
	}
	// The orchestrator entry should be gone and the session stopped.
	if _, ok := ctx.ActivePlanOrchestrators["orch-1"]; ok {
		t.Error("orchestrator should be removed from the active map")
	}
	if _, ok := ctx.SessionManager.GetSession(id); ok {
		t.Error("the plan session should have been stopped")
	}
}

func TestCancelPlanGeneration_CancelAllWithLiveSession(t *testing.T) {
	ctx := newSessionLiveCtx(t)
	id, _ := liveSession(t, ctx)
	ctx.ActivePlanOrchestrators = map[string]string{"orch-a": id}

	rec := postJSON(t, ctx.CancelPlanGeneration, "/api/cancel-plan-generation", map[string]interface{}{})
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel all: expected 200, got %d", rec.Code)
	}
	if len(ctx.ActivePlanOrchestrators) != 0 {
		t.Errorf("cancel-all should empty the orchestrator map, got %d", len(ctx.ActivePlanOrchestrators))
	}
}

func TestOfficeAgentPostMemorySection_AppendBranch(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	ctx.OfficeManager.RegisterAgent("sess-mem", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})

	// Write a section.
	recW := postJSON(t, ctx.OfficeAgentPostMemorySection, "/api/office/agent/memory/section", map[string]interface{}{
		"sessionId": "sess-mem", "name": "decisions", "content": "use Postgres",
	})
	if recW.Code != http.StatusOK {
		t.Fatalf("write section: expected 200, got %d: %s", recW.Code, recW.Body.String())
	}

	// Append to the same section (the append=true branch).
	recA := postJSON(t, ctx.OfficeAgentPostMemorySection, "/api/office/agent/memory/section", map[string]interface{}{
		"sessionId": "sess-mem", "name": "decisions", "content": "cache with Redis", "append": true,
	})
	if recA.Code != http.StatusOK {
		t.Fatalf("append section: expected 200, got %d: %s", recA.Code, recA.Body.String())
	}
	resp := decodeMap(t, recA)
	if resp["append"] != true {
		t.Errorf("append=%v, want true", resp["append"])
	}
}

func TestOfficeAgentDeleteSkill_Happy(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	// Create a shared skill, then delete it via the handler.
	recC := postJSON(t, ctx.OfficeAgentSkillCreate, "/api/office/agent/skill", map[string]interface{}{
		"skillName": "refactor", "content": "how to refactor safely",
	})
	if recC.Code != http.StatusOK {
		t.Fatalf("create skill: expected 200, got %d: %s", recC.Code, recC.Body.String())
	}

	req := withURLParam(httptest.NewRequest("DELETE", "/api/office/agent/skill/refactor", nil), "name", "refactor")
	rec := httptest.NewRecorder()
	ctx.OfficeAgentDeleteSkill(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete skill: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	if resp["deleted"] != "refactor" {
		t.Errorf("deleted=%v, want refactor", resp["deleted"])
	}
}

func TestGetSessionWorkingDir_LivePTY(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	sess, err := ctx.SessionManager.CreateSession(context.Background(), pty.SessionConfig{
		WorkingDir: t.TempDir(), Mode: pty.ModeShell, Name: "wd",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = ctx.SessionManager.StopSession(context.Background(), sess.ID) })

	wd := ctx.getSessionWorkingDir(sess.ID)
	if wd != sess.WorkingDir {
		t.Errorf("getSessionWorkingDir=%q, want %q", wd, sess.WorkingDir)
	}
}
