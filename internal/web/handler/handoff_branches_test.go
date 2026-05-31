package handler

// Branch fills for the handoff reject/cancel handlers: agent-not-found (404)
// and malformed-body (400) guards that the happy-path tests don't reach.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOfficeAgentHandoffReject_AgentNotFound(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	rec := postJSON(t, ctx.OfficeAgentHandoffReject, "/api/office/agent/handoff/reject", map[string]interface{}{
		"sessionId": "ghost", "taskId": "t1",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown agent, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficeAgentHandoffReject_BadJSON(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	req := httptest.NewRequest("POST", "/api/office/agent/handoff/reject", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.OfficeAgentHandoffReject(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed body, got %d", rec.Code)
	}
}

func TestOfficeAgentHandoffCancel_BadJSON(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	req := httptest.NewRequest("POST", "/api/office/agent/handoff/cancel", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.OfficeAgentHandoffCancel(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed body, got %d", rec.Code)
	}
}
