package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kshaham/jarvis/internal/office"
)

func TestOfficeGetLabels_Empty(t *testing.T) {
	ctx := newTimelineContext(t)
	req := httptest.NewRequest("GET", "/api/office/labels", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeGetLabels(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	labels, _ := resp["labels"].([]interface{})
	if len(labels) != 0 {
		t.Errorf("expected empty labels, got %v", labels)
	}
}

func TestOfficeGetLabelRollups_Populated(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Tier: "worker"})
	agent := ctx.OfficeManager.GetAgent("s-vex")
	agent.AddTask(&office.Task{
		ID: "t-1", Status: office.TaskCompleted, Labels: []string{"auth"},
		FinalCostUSD: 1.5, FinalTurnCount: 4,
	})
	agent.AddTask(&office.Task{
		ID: "t-2", Status: office.TaskInProgress, Labels: []string{"q2"},
	})

	req := httptest.NewRequest("GET", "/api/office/labels/rollups", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeGetLabelRollups(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp struct {
		Rollups []office.LabelRollup `json:"rollups"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Rollups) != 2 {
		t.Fatalf("expected 2 rollups, got %d", len(resp.Rollups))
	}
	// auth should sort first (cost > 0).
	if resp.Rollups[0].Label != "auth" {
		t.Errorf("auth should be first, got %+v", resp.Rollups)
	}
}

func TestOfficeAgentTaskLabels_AddRemove(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Tier: "worker"})
	agent := ctx.OfficeManager.GetAgent("s-vex")
	agent.AddTask(&office.Task{ID: "t-1", Status: office.TaskPending})

	// Add two labels.
	rec := postJSON(t, ctx.OfficeAgentTaskLabels, "/api/office/agent/task/labels",
		map[string]interface{}{
			"sessionId": "s-vex",
			"taskId":    "t-1",
			"add":       []string{"auth", "Q2"},
		})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	// After add, task should carry normalized labels.
	got := agent.GetTasks()[0].Labels
	if len(got) != 2 || got[0] != "auth" || got[1] != "q2" {
		t.Errorf("labels after add: %v", got)
	}

	// Remove one.
	rec = postJSON(t, ctx.OfficeAgentTaskLabels, "/api/office/agent/task/labels",
		map[string]interface{}{
			"sessionId": "s-vex",
			"taskId":    "t-1",
			"remove":    []string{"auth"},
		})
	if rec.Code != http.StatusOK {
		t.Fatalf("remove status: %d", rec.Code)
	}
	got = agent.GetTasks()[0].Labels
	if len(got) != 1 || got[0] != "q2" {
		t.Errorf("labels after remove: %v", got)
	}
}

func TestOfficeAgentTaskLabels_RejectInvalid(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Tier: "worker"})
	ctx.OfficeManager.GetAgent("s-vex").AddTask(&office.Task{ID: "t-1", Status: office.TaskPending})

	rec := postJSON(t, ctx.OfficeAgentTaskLabels, "/api/office/agent/task/labels",
		map[string]interface{}{
			"sessionId": "s-vex",
			"taskId":    "t-1",
			"add":       []string{"has spaces"},
		})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestOfficeAgentTaskLabels_UnknownAgent(t *testing.T) {
	ctx := newTimelineContext(t)
	rec := postJSON(t, ctx.OfficeAgentTaskLabels, "/api/office/agent/task/labels",
		map[string]interface{}{
			"sessionId": "s-unknown",
			"taskId":    "t-1",
			"add":       []string{"x"},
		})
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}
