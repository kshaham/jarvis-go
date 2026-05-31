package handler

// Additional handoff coverage: operator force-accept (the one handler the
// existing suite skips), the wrapping-up 409 guard, decode failures, and
// the manager-error -> status-code mapping for accept/reject/initiate when
// no handoff exists.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandoffForceAccept_Operator(t *testing.T) {
	ctx, vex, bolt, task := seedHandoffTeam(t)
	if _, err := ctx.OfficeManager.InitiateHandoff(vex.SessionID(), task.ID, bolt.ConfigID, "ctx"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	rec := postJSON(t, ctx.OfficeHandoffForceAccept, "/api/office/handoffs/force-accept", map[string]interface{}{
		"taskId": task.ID,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["handoff"] == nil {
		t.Fatal("expected handoff in response")
	}
	// Force-accept MOVES the task: it leaves the sender's (vex) queue and
	// lands in the receiver's (bolt) queue as a fresh in-progress task with
	// the handoff pointer cleared on the moved copy.
	for _, vt := range vex.GetTasks() {
		if vt.ID == task.ID {
			t.Error("task should have left the sender's queue after force-accept")
		}
	}
	var moved bool
	for _, bt := range bolt.GetTasks() {
		if bt.ID == task.ID {
			moved = true
			if bt.Handoff != nil {
				t.Error("moved task should have a nil Handoff pointer")
			}
		}
	}
	if !moved {
		t.Error("task should now be in the receiver's queue")
	}
}

func TestHandoffForceAccept_NoPending(t *testing.T) {
	ctx, _, _, _ := seedHandoffTeam(t)
	// No handoff was initiated, so force-accept on this task must 404.
	rec := postJSON(t, ctx.OfficeHandoffForceAccept, "/api/office/handoffs/force-accept", map[string]interface{}{
		"taskId": "no-such-task",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing handoff, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandoffForceReject_NoPending(t *testing.T) {
	ctx, _, _, _ := seedHandoffTeam(t)
	rec := postJSON(t, ctx.OfficeHandoffForceReject, "/api/office/handoffs/force-reject", map[string]interface{}{
		"taskId": "ghost-task",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing handoff, got %d", rec.Code)
	}
}

func TestHandoffInitiate_WrappingUpConflict(t *testing.T) {
	ctx, vex, bolt, task := seedHandoffTeam(t)
	ctx.OfficeManager.SetWrappingUp(true)
	rec := postJSON(t, ctx.OfficeAgentHandoffInitiate, "/api/office/task/handoff", map[string]interface{}{
		"sessionId":  vex.SessionID(),
		"taskId":     task.ID,
		"toConfigId": bolt.ConfigID,
		"context":    "ctx",
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 while wrapping up, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandoffInitiate_UnknownAgent(t *testing.T) {
	ctx, _, bolt, task := seedHandoffTeam(t)
	rec := postJSON(t, ctx.OfficeAgentHandoffInitiate, "/api/office/task/handoff", map[string]interface{}{
		"sessionId":  "s-nobody",
		"taskId":     task.ID,
		"toConfigId": bolt.ConfigID,
		"context":    "ctx",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown sender, got %d", rec.Code)
	}
}

func TestHandoffAccept_UnknownAgent(t *testing.T) {
	ctx, _, _, task := seedHandoffTeam(t)
	rec := postJSON(t, ctx.OfficeAgentHandoffAccept, "/api/office/task/handoff/accept", map[string]interface{}{
		"sessionId": "s-nobody",
		"taskId":    task.ID,
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestHandoffAccept_NoPendingForTask(t *testing.T) {
	ctx, _, bolt, task := seedHandoffTeam(t)
	// bolt is a real agent, but no handoff exists for this task.
	rec := postJSON(t, ctx.OfficeAgentHandoffAccept, "/api/office/task/handoff/accept", map[string]interface{}{
		"sessionId": bolt.SessionID(),
		"taskId":    task.ID,
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when no handoff pending, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandoffReject_CustomReason(t *testing.T) {
	ctx, vex, bolt, task := seedHandoffTeam(t)
	if _, err := ctx.OfficeManager.InitiateHandoff(vex.SessionID(), task.ID, bolt.ConfigID, "ctx"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	rec := postJSON(t, ctx.OfficeAgentHandoffReject, "/api/office/task/handoff/reject", map[string]interface{}{
		"sessionId": bolt.SessionID(),
		"taskId":    task.ID,
		"reason":    "out of scope",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestHandoffInitiate_BadJSON(t *testing.T) {
	ctx, _, _, _ := seedHandoffTeam(t)
	req := httptest.NewRequest("POST", "/api/office/task/handoff", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.OfficeAgentHandoffInitiate(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed JSON, got %d", rec.Code)
	}
}

func TestStatusForHandoffError_Mapping(t *testing.T) {
	cases := []struct {
		msg  string
		want int
	}{
		{"task not found", http.StatusNotFound},
		{"agent not found", http.StatusNotFound},
		{"not your handoff", http.StatusForbidden},
		{"context too large for handoff", http.StatusBadRequest},
		{"cannot hand off to self", http.StatusBadRequest},
		{"task must be in_progress to hand off", http.StatusBadRequest},
		{"task already has a pending handoff", http.StatusBadRequest},
		{"some unexpected failure", http.StatusInternalServerError},
	}
	for _, c := range cases {
		got := statusForHandoffError(errString(c.msg))
		if got != c.want {
			t.Errorf("statusForHandoffError(%q)=%d, want %d", c.msg, got, c.want)
		}
	}
}

// errString is a tiny error wrapper for the mapping table test.
type errString string

func (e errString) Error() string { return string(e) }
