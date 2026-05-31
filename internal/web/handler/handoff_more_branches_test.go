package handler

// Round-3 branch fills for the remaining handoff handler arms:
//   - OfficeAgentHandoffAccept malformed-body 400
//   - OfficeAgentHandoffCancel agent-not-found 404 + success 200
//   - OfficeHandoffForceAccept / OfficeHandoffForceReject malformed-body 400
//   - statusForHandoffError too-many-pending mapping

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kshaham/jarvis/internal/office"
)

func TestOfficeAgentHandoffAccept_BadJSON(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	req := httptest.NewRequest("POST", "/api/office/task/handoff/accept", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.OfficeAgentHandoffAccept(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed accept body, got %d", rec.Code)
	}
}

func TestOfficeAgentHandoffCancel_AgentNotFound(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	rec := postJSON(t, ctx.OfficeAgentHandoffCancel, "/api/office/task/handoff/cancel", map[string]interface{}{
		"sessionId": "ghost", "taskId": "t1",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown agent on cancel, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficeAgentHandoffCancel_Success(t *testing.T) {
	ctx, vex, bolt, task := seedHandoffTeam(t)
	if _, err := ctx.OfficeManager.InitiateHandoff(vex.SessionID(), task.ID, bolt.ConfigID, "ctx"); err != nil {
		t.Fatalf("seed handoff: %v", err)
	}
	// The SENDER (vex) cancels their own pending handoff -> 200.
	rec := postJSON(t, ctx.OfficeAgentHandoffCancel, "/api/office/task/handoff/cancel", map[string]interface{}{
		"sessionId": vex.SessionID(), "taskId": task.ID,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 cancelling own handoff, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficeHandoffForce_BadJSON(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	for name, h := range map[string]func(http.ResponseWriter, *http.Request){
		"ForceAccept": ctx.OfficeHandoffForceAccept,
		"ForceReject": ctx.OfficeHandoffForceReject,
	} {
		req := httptest.NewRequest("POST", "/api/office/handoffs/force", strings.NewReader("{bad"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400 for malformed body, got %d", name, rec.Code)
		}
	}
}

func TestStatusForHandoffError_TooManyPending(t *testing.T) {
	t.Parallel()
	msg := fmt.Sprintf("too many pending handoffs (limit %d)", office.HandoffMaxPendingPerSender)
	if got := statusForHandoffError(errors.New(msg)); got != http.StatusTooManyRequests {
		t.Errorf("too-many-pending should map to 429, got %d", got)
	}
}
