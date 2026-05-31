package handler

// Round-4 fills:
//   - OfficeAgentHandoffReject: RejectHandoff error arm (handoff.go:127-130),
//     reached by rejecting a non-existent handoff for a registered agent.
//   - DeleteSubagent: the removed==true arm (monitoring_extended.go:361-363),
//     reached by registering an agent via OnHookStart then deleting it.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/subagent"
)

func TestOfficeAgentHandoffReject_NoHandoffError(t *testing.T) {
	ctx := newTimelineContext(t)
	// Agent exists so the not-found guard passes, but there is no pending
	// handoff for this taskId -> RejectHandoff returns an error.
	ctx.OfficeManager.RegisterAgent("s-recv", "recv", &office.TeamMemberConfig{ID: "recv", Name: "Recv"})

	rec := postJSON(t, ctx.OfficeAgentHandoffReject, "/api/office/agent/handoff/reject", map[string]interface{}{
		"sessionId": "s-recv", "taskId": "no-such-task", "reason": "nope",
	})
	// statusForHandoffError maps the resolve failure to a 4xx (not 200).
	if rec.Code == http.StatusOK {
		t.Fatalf("expected an error status for a missing handoff, got 200: %s", rec.Body.String())
	}
	if rec.Code < 400 {
		t.Fatalf("expected a 4xx/5xx error, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDeleteSubagent_RemovedTrue(t *testing.T) {
	w := subagent.NewWatcher(context.Background(), t.TempDir())
	ctx := &Context{SubagentWatcher: w}
	// Register an agent so RemoveAgent returns true (the removed==true arm).
	w.OnHookStart("sess-del", "explorer", "agent-del")

	req := withURLParam(httptest.NewRequest("DELETE", "/api/subagents/agent-del", nil), "agentId", "agent-del")
	rec := httptest.NewRecorder()
	ctx.DeleteSubagent(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	if resp["success"] != true {
		t.Errorf("success=%v, want true", resp["success"])
	}
	// The agent should no longer be tracked after a successful removal.
	if w.GetAgent("agent-del") != nil {
		t.Error("agent should be removed from the watcher after DeleteSubagent")
	}
}
