package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kshaham/jarvis/internal/office"
)

func TestOfficeGetTaskGraph_Empty(t *testing.T) {
	ctx := newTimelineContext(t)
	req := httptest.NewRequest("GET", "/api/office/task-graph", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeGetTaskGraph(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp office.TaskGraphResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(resp.Nodes))
	}
	if resp.CycleDetected {
		t.Error("empty team should not report a cycle")
	}
}

func TestOfficeGetTaskGraph_WithChain(t *testing.T) {
	ctx := newTimelineContext(t)
	mgr := ctx.OfficeManager
	mgr.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})
	agent := mgr.GetAgent("s-vex")
	agent.AddTask(&office.Task{ID: "t-a", Title: "build", Status: office.TaskInProgress, Priority: "medium"})
	agent.AddTask(&office.Task{ID: "t-b", Title: "test", Status: office.TaskPending, Priority: "medium", BlockedBy: []string{"t-a"}})

	req := httptest.NewRequest("GET", "/api/office/task-graph", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeGetTaskGraph(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp office.TaskGraphResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(resp.Nodes))
	}
	if len(resp.CriticalPath) != 2 {
		t.Errorf("expected critical path length 2, got %v", resp.CriticalPath)
	}
}
