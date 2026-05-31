package handler

// Branch fills for the dev-instance / dev-credentials POST handlers:
//   - bad-JSON 400 (decodeBody failure)
//   - default-slot ("" -> "app") path
//   - SetDevInstanceCredentials invalid-slot 400
//   - SetDevInstance port-conflict 409

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOfficeAgentPostDevInstance_BadJSON(t *testing.T) {
	ctx := newTimelineContext(t)
	req := httptest.NewRequest("POST", "/api/office/agent/dev-instance", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.OfficeAgentPostDevInstance(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on bad JSON, got %d", rec.Code)
	}
}

func TestOfficeAgentPostDevInstance_DefaultSlot(t *testing.T) {
	ctx := newTimelineContext(t)
	// No "name" -> defaults to "app".
	rec := postJSON(t, ctx.OfficeAgentPostDevInstance, "/api/office/agent/dev-instance", map[string]interface{}{
		"url": "http://jarvis:6001", "pid": 11,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	if resp["name"] != "app" {
		t.Errorf("default slot name=%v, want app", resp["name"])
	}
}

func TestOfficeAgentPostDevInstance_PortConflict409(t *testing.T) {
	ctx := newTimelineContext(t)
	// Register slot "app" on a port.
	rec1 := postJSON(t, ctx.OfficeAgentPostDevInstance, "/api/office/agent/dev-instance", map[string]interface{}{
		"name": "app", "url": "http://jarvis:7777", "pid": 1,
	})
	if rec1.Code != http.StatusOK {
		t.Fatalf("first register: expected 200, got %d: %s", rec1.Code, rec1.Body.String())
	}
	// A different slot grabbing the SAME port conflicts -> 409.
	rec2 := postJSON(t, ctx.OfficeAgentPostDevInstance, "/api/office/agent/dev-instance", map[string]interface{}{
		"name": "api", "url": "http://jarvis:7777", "pid": 2,
	})
	if rec2.Code != http.StatusConflict {
		t.Fatalf("expected 409 on port conflict, got %d: %s", rec2.Code, rec2.Body.String())
	}
}

func TestOfficeAgentPostDevCredentials_BadJSON(t *testing.T) {
	ctx := newTimelineContext(t)
	req := httptest.NewRequest("POST", "/api/office/agent/dev-credentials", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.OfficeAgentPostDevCredentials(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on bad JSON, got %d", rec.Code)
	}
}

func TestOfficeAgentPostDevCredentials_DefaultSlotAndSet(t *testing.T) {
	ctx := newTimelineContext(t)
	// Empty name -> slot defaults to "app"; non-empty creds -> set:true.
	rec := postJSON(t, ctx.OfficeAgentPostDevCredentials, "/api/office/agent/dev-credentials", map[string]interface{}{
		"credentials": "user:pass",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	if resp["name"] != "app" {
		t.Errorf("default slot=%v, want app", resp["name"])
	}
	if resp["set"] != true {
		t.Errorf("set=%v, want true for non-empty creds", resp["set"])
	}
}

func TestOfficeAgentPostDevCredentials_InvalidSlot400(t *testing.T) {
	ctx := newTimelineContext(t)
	rec := postJSON(t, ctx.OfficeAgentPostDevCredentials, "/api/office/agent/dev-credentials", map[string]interface{}{
		"name": "bad slot!!", "credentials": "x",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid slot name, got %d: %s", rec.Code, rec.Body.String())
	}
}
