package handler

// Branch fills for OfficeAgentTaskScreenshot: agent-not-found (404),
// taskId-required (400), and dataBase64-required (400). The happy path +
// size/base64 validation already live in task_screenshot_test.go.

import (
	"net/http"
	"testing"

	"github.com/kshaham/jarvis/internal/office"
)

func TestTaskScreenshot_AgentNotFound(t *testing.T) {
	ctx := newTimelineContext(t)
	rec := postJSON(t, ctx.OfficeAgentTaskScreenshot, "/api/office/agent/task/screenshot", map[string]interface{}{
		"sessionId": "s-ghost", "taskId": "t1", "dataBase64": "AAAA",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown agent, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTaskScreenshot_MissingTaskID(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})
	rec := postJSON(t, ctx.OfficeAgentTaskScreenshot, "/api/office/agent/task/screenshot", map[string]interface{}{
		"sessionId": "s-vex", "dataBase64": "AAAA",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing taskId, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTaskScreenshot_MissingData(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})
	rec := postJSON(t, ctx.OfficeAgentTaskScreenshot, "/api/office/agent/task/screenshot", map[string]interface{}{
		"sessionId": "s-vex", "taskId": "t1",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing dataBase64, got %d: %s", rec.Code, rec.Body.String())
	}
}
