package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/office"
)

func seedHandoffTeam(t *testing.T) (*Context, *office.AgentState, *office.AgentState, *office.Task) {
	t.Helper()
	ctx := newTimelineContext(t)
	mgr := ctx.OfficeManager
	mgr.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex", Tier: "worker"})
	mgr.RegisterAgent("s-bolt", "bolt", &office.TeamMemberConfig{ID: "bolt", Name: "Bolt", Tier: "worker"})
	task := &office.Task{
		ID:         "t-hx",
		Title:      "audit session cookies",
		Status:     office.TaskInProgress,
		Priority:   "medium",
		AssignedAt: time.Now(),
	}
	mgr.GetAgent("s-vex").AddTask(task)
	return ctx, mgr.GetAgent("s-vex"), mgr.GetAgent("s-bolt"), task
}

func TestHandoffInitiate_Happy(t *testing.T) {
	ctx, vex, bolt, task := seedHandoffTeam(t)
	rec := postJSON(t, ctx.OfficeAgentHandoffInitiate, "/api/office/task/handoff", map[string]interface{}{
		"sessionId":  vex.SessionID,
		"taskId":     task.ID,
		"toConfigId": bolt.ConfigID,
		"context":    "API contract Qs",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["handoff"] == nil {
		t.Fatal("expected handoff in response")
	}
}

func TestHandoffAccept_WrongReceiver(t *testing.T) {
	ctx, vex, bolt, task := seedHandoffTeam(t)
	// Seed the handoff via the manager directly so we can test accept in isolation.
	if _, err := ctx.OfficeManager.InitiateHandoff(vex.SessionID, task.ID, bolt.ConfigID, "ctx"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// A third party ("stranger") tries to accept.
	rec := postJSON(t, ctx.OfficeAgentHandoffAccept, "/api/office/task/handoff/accept", map[string]interface{}{
		"sessionId": "s-stranger",
		"taskId":    task.ID,
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 (stranger not a registered agent), got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandoffAccept_ReceiverOK(t *testing.T) {
	ctx, vex, bolt, task := seedHandoffTeam(t)
	if _, err := ctx.OfficeManager.InitiateHandoff(vex.SessionID, task.ID, bolt.ConfigID, "ctx"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	rec := postJSON(t, ctx.OfficeAgentHandoffAccept, "/api/office/task/handoff/accept", map[string]interface{}{
		"sessionId": bolt.SessionID,
		"taskId":    task.ID,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandoffReject_ReasonOptional(t *testing.T) {
	ctx, vex, bolt, task := seedHandoffTeam(t)
	if _, err := ctx.OfficeManager.InitiateHandoff(vex.SessionID, task.ID, bolt.ConfigID, "ctx"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	rec := postJSON(t, ctx.OfficeAgentHandoffReject, "/api/office/task/handoff/reject", map[string]interface{}{
		"sessionId": bolt.SessionID,
		"taskId":    task.ID,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandoffInitiate_OversizeContext(t *testing.T) {
	ctx, vex, bolt, task := seedHandoffTeam(t)
	big := strings.Repeat("x", office.HandoffContextLimit+1)
	rec := postJSON(t, ctx.OfficeAgentHandoffInitiate, "/api/office/task/handoff", map[string]interface{}{
		"sessionId":  vex.SessionID,
		"taskId":     task.ID,
		"toConfigId": bolt.ConfigID,
		"context":    big,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for oversize context, got %d", rec.Code)
	}
}

func TestHandoffInitiate_SelfHandoff(t *testing.T) {
	ctx, vex, _, task := seedHandoffTeam(t)
	rec := postJSON(t, ctx.OfficeAgentHandoffInitiate, "/api/office/task/handoff", map[string]interface{}{
		"sessionId":  vex.SessionID,
		"taskId":     task.ID,
		"toConfigId": vex.ConfigID,
		"context":    "ctx",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for self handoff, got %d", rec.Code)
	}
}

func TestOfficeListHandoffs_PendingAndHistory(t *testing.T) {
	ctx, vex, bolt, task := seedHandoffTeam(t)
	if _, err := ctx.OfficeManager.InitiateHandoff(vex.SessionID, task.ID, bolt.ConfigID, "ctx"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	req := httptest.NewRequest("GET", "/api/office/handoffs", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeListHandoffs(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	pending, _ := resp["pending"].([]interface{})
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(pending))
	}
}

func TestHandoffCancel_HappyPath(t *testing.T) {
	ctx, vex, bolt, task := seedHandoffTeam(t)
	if _, err := ctx.OfficeManager.InitiateHandoff(vex.SessionID, task.ID, bolt.ConfigID, "ctx"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	rec := postJSON(t, ctx.OfficeAgentHandoffCancel, "/api/office/task/handoff/cancel", map[string]interface{}{
		"sessionId": vex.SessionID,
		"taskId":    task.ID,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if task.Handoff != nil {
		t.Error("handoff should be cleared after cancel")
	}
}

func TestHandoffCancel_WrongSender(t *testing.T) {
	ctx, vex, bolt, task := seedHandoffTeam(t)
	if _, err := ctx.OfficeManager.InitiateHandoff(vex.SessionID, task.ID, bolt.ConfigID, "ctx"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// bolt (the receiver) tries to cancel -- should fail with 403.
	rec := postJSON(t, ctx.OfficeAgentHandoffCancel, "/api/office/task/handoff/cancel", map[string]interface{}{
		"sessionId": bolt.SessionID,
		"taskId":    task.ID,
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandoffForceReject_Operator(t *testing.T) {
	ctx, vex, bolt, task := seedHandoffTeam(t)
	if _, err := ctx.OfficeManager.InitiateHandoff(vex.SessionID, task.ID, bolt.ConfigID, "ctx"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	rec := postJSON(t, ctx.OfficeHandoffForceReject, "/api/office/handoffs/force-reject", map[string]interface{}{
		"taskId": task.ID,
		"reason": "operator stepping in",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if task.Handoff != nil {
		t.Error("task handoff should be cleared")
	}
}
