package handler

// Coverage for the dev-instance + dev-credentials agent endpoints in
// office.go. These track per-slot dev-server URLs and login blobs in the
// OfficeManager, so they exercise cleanly with newTimelineContext.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOfficeAgentPostDevInstance_RejectsLocalhost(t *testing.T) {
	ctx := newTimelineContext(t)
	rec := postJSON(t, ctx.OfficeAgentPostDevInstance, "/api/office/agent/dev-instance", map[string]interface{}{
		"url": "http://localhost:3000",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for localhost URL, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficeAgentPostDevInstance_AcceptsReachableURL(t *testing.T) {
	ctx := newTimelineContext(t)
	rec := postJSON(t, ctx.OfficeAgentPostDevInstance, "/api/office/agent/dev-instance", map[string]interface{}{
		"name": "app",
		"url":  "http://jarvis:5173",
		"pid":  4242,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["url"] != "http://jarvis:5173" {
		t.Errorf("url=%v, want http://jarvis:5173", resp["url"])
	}
	// It should now appear in the listing.
	inst, ok := ctx.OfficeManager.GetDevInstance("app")
	if !ok || inst.URL != "http://jarvis:5173" {
		t.Errorf("manager missing the registered dev instance: %+v", inst)
	}
}

func TestOfficeAgentPostDevInstance_LocalhostForced(t *testing.T) {
	ctx := newTimelineContext(t)
	req := httptest.NewRequest("POST", "/api/office/agent/dev-instance?force=true", jsonBody(t, map[string]interface{}{
		"url": "http://localhost:3000",
	}))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.OfficeAgentPostDevInstance(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with force=true, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficeAgentGetDevInstanceAll_Listing(t *testing.T) {
	ctx := newTimelineContext(t)
	// Register one instance, then list.
	postJSON(t, ctx.OfficeAgentPostDevInstance, "/api/office/agent/dev-instance", map[string]interface{}{
		"name": "api", "url": "http://jarvis:8080",
	})
	req := httptest.NewRequest("GET", "/api/office/agent/dev-instances", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeAgentGetDevInstanceAll(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	slots, _ := resp["slots"].(map[string]interface{})
	if _, ok := slots["api"]; !ok {
		t.Errorf("expected 'api' slot in listing, got %v", slots)
	}
}

func TestOfficeGetDevInstances_BrowserMirror(t *testing.T) {
	ctx := newTimelineContext(t)
	req := httptest.NewRequest("GET", "/api/office/dev-instances", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeGetDevInstances(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if _, ok := resp["slots"]; !ok {
		t.Error("expected slots key")
	}
}

func TestOfficeAgentDeleteDevInstance_SingleAndAll(t *testing.T) {
	ctx := newTimelineContext(t)
	postJSON(t, ctx.OfficeAgentPostDevInstance, "/api/office/agent/dev-instance", map[string]interface{}{
		"name": "app", "url": "http://jarvis:1",
	})

	// Delete single slot.
	req := httptest.NewRequest("DELETE", "/api/office/agent/dev-instance?name=app", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeAgentDeleteDevInstance(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["cleared"] != "app" {
		t.Errorf("cleared=%v, want app", resp["cleared"])
	}

	// Delete all.
	reqAll := httptest.NewRequest("DELETE", "/api/office/agent/dev-instance?all=true", nil)
	recAll := httptest.NewRecorder()
	ctx.OfficeAgentDeleteDevInstance(recAll, reqAll)
	if recAll.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recAll.Code)
	}
	var respAll map[string]interface{}
	_ = json.Unmarshal(recAll.Body.Bytes(), &respAll)
	if respAll["cleared"] != "all" {
		t.Errorf("cleared=%v, want all", respAll["cleared"])
	}
}

func TestOfficeAgentDevCredentials_RoundTrip(t *testing.T) {
	ctx := newTimelineContext(t)
	// A dev instance must exist before credentials can attach.
	postJSON(t, ctx.OfficeAgentPostDevInstance, "/api/office/agent/dev-instance", map[string]interface{}{
		"name": "app", "url": "http://jarvis:9",
	})

	// Post credentials.
	rec := postJSON(t, ctx.OfficeAgentPostDevCredentials, "/api/office/agent/dev-credentials", map[string]interface{}{
		"name":        "app",
		"credentials": "user:pass",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["set"] != true {
		t.Errorf("set=%v, want true", resp["set"])
	}

	// Get credentials -- value reflected back to agent.
	reqGet := httptest.NewRequest("GET", "/api/office/agent/dev-credentials?name=app", nil)
	recGet := httptest.NewRecorder()
	ctx.OfficeAgentGetDevCredentials(recGet, reqGet)
	if recGet.Code != http.StatusOK {
		t.Fatalf("get creds: expected 200, got %d", recGet.Code)
	}
	var got map[string]interface{}
	_ = json.Unmarshal(recGet.Body.Bytes(), &got)
	if got["credentials"] != "user:pass" {
		t.Errorf("credentials=%v, want user:pass", got["credentials"])
	}

	// Delete credentials.
	reqDel := httptest.NewRequest("DELETE", "/api/office/agent/dev-credentials?name=app", nil)
	recDel := httptest.NewRecorder()
	ctx.OfficeAgentDeleteDevCredentials(recDel, reqDel)
	if recDel.Code != http.StatusOK {
		t.Fatalf("delete creds: expected 200, got %d", recDel.Code)
	}
	var del map[string]interface{}
	_ = json.Unmarshal(recDel.Body.Bytes(), &del)
	if del["set"] != false {
		t.Errorf("after delete set=%v, want false", del["set"])
	}
}

func TestOfficeAgentGetDevCredentials_UnknownSlot(t *testing.T) {
	ctx := newTimelineContext(t)
	req := httptest.NewRequest("GET", "/api/office/agent/dev-credentials?name=ghost", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeAgentGetDevCredentials(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["credentials"] != "" {
		t.Errorf("expected empty credentials for unknown slot, got %v", resp["credentials"])
	}
}
