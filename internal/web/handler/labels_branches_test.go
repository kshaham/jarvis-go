package handler

// Branch fills for the labels handlers:
//   - OfficeGetLabels: the non-nil branch (labels present across the roster)
//   - OfficeAgentTaskLabels: bad-JSON 400 and missing-taskId 400

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kshaham/jarvis/internal/office"
)

func TestOfficeGetLabels_Populated(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Tier: "worker"})
	agent := ctx.OfficeManager.GetAgent("s-vex")
	agent.AddTask(&office.Task{ID: "t-1", Status: office.TaskPending, Labels: []string{"auth", "q2"}})

	rec := getJSON(t, ctx.OfficeGetLabels, "/api/office/labels")
	labels, _ := rec["labels"].([]interface{})
	if len(labels) == 0 {
		t.Fatalf("expected non-empty labels, got %v", rec["labels"])
	}
}

func TestOfficeAgentTaskLabels_BadJSON(t *testing.T) {
	ctx := newTimelineContext(t)
	req := httptest.NewRequest("POST", "/api/office/agent/task/labels", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.OfficeAgentTaskLabels(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on bad JSON, got %d", rec.Code)
	}
}

func TestOfficeAgentTaskLabels_MissingTaskID(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Tier: "worker"})
	rec := postJSON(t, ctx.OfficeAgentTaskLabels, "/api/office/agent/task/labels", map[string]interface{}{
		"sessionId": "s-vex", "add": []string{"auth"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing taskId, got %d: %s", rec.Code, rec.Body.String())
	}
}
