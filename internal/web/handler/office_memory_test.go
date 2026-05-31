package handler

// Coverage for the agent memory endpoints in office.go. newTimelineContext
// isolates HOME, so office.SaveAgentMemory et al. write under the temp dir.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kshaham/jarvis/internal/office"
)

func seedMemoryAgent(t *testing.T) *Context {
	t.Helper()
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})
	return ctx
}

func TestOfficeAgentMemory_WriteThenRead(t *testing.T) {
	ctx := seedMemoryAgent(t)
	rec := postJSON(t, ctx.OfficeAgentPostMemory, "/api/office/agent/memory", map[string]interface{}{
		"sessionId": "s-vex",
		"content":   "# Vex memory\nremember the auth flow",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("write: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	reqGet := httptest.NewRequest("GET", "/api/office/agent/memory?sessionId=s-vex", nil)
	recGet := httptest.NewRecorder()
	ctx.OfficeAgentGetMemory(recGet, reqGet)
	if recGet.Code != http.StatusOK {
		t.Fatalf("read: expected 200, got %d", recGet.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(recGet.Body.Bytes(), &resp)
	mem, _ := resp["memory"].(string)
	if !strings.Contains(mem, "remember the auth flow") {
		t.Errorf("memory roundtrip lost content: %q", mem)
	}
}

func TestOfficeAgentGetMemory_UnknownAgentEmpty(t *testing.T) {
	ctx := newTimelineContext(t)
	req := httptest.NewRequest("GET", "/api/office/agent/memory?sessionId=s-nobody", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeAgentGetMemory(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["memory"] != "" {
		t.Errorf("expected empty memory for unknown agent, got %v", resp["memory"])
	}
}

func TestOfficeAgentPostMemory_UnknownAgent404(t *testing.T) {
	ctx := newTimelineContext(t)
	rec := postJSON(t, ctx.OfficeAgentPostMemory, "/api/office/agent/memory", map[string]interface{}{
		"sessionId": "s-nobody", "content": "x",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestOfficeAgentPostMemoryAppend(t *testing.T) {
	ctx := seedMemoryAgent(t)
	rec := postJSON(t, ctx.OfficeAgentPostMemoryAppend, "/api/office/agent/memory/append", map[string]interface{}{
		"sessionId": "s-vex",
		"entry":     "learned: caches expire",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficeAgentPostMemoryAppend_UnknownAgent404(t *testing.T) {
	ctx := newTimelineContext(t)
	rec := postJSON(t, ctx.OfficeAgentPostMemoryAppend, "/api/office/agent/memory/append", map[string]interface{}{
		"sessionId": "s-nobody", "entry": "x",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestOfficeAgentMemorySection_WriteAndRead(t *testing.T) {
	ctx := seedMemoryAgent(t)
	rec := postJSON(t, ctx.OfficeAgentPostMemorySection, "/api/office/agent/memory/section", map[string]interface{}{
		"sessionId": "s-vex",
		"name":      "decisions",
		"content":   "chose JWT over sessions",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("write section: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["section"] != "decisions" {
		t.Errorf("section=%v, want decisions", resp["section"])
	}

	reqGet := httptest.NewRequest("GET", "/api/office/agent/memory/section?sessionId=s-vex&name=decisions", nil)
	recGet := httptest.NewRecorder()
	ctx.OfficeAgentGetMemorySection(recGet, reqGet)
	if recGet.Code != http.StatusOK {
		t.Fatalf("read section: expected 200, got %d", recGet.Code)
	}
	var got map[string]interface{}
	_ = json.Unmarshal(recGet.Body.Bytes(), &got)
	if !strings.Contains(got["content"].(string), "chose JWT") {
		t.Errorf("section content lost: %v", got["content"])
	}
}

func TestOfficeAgentPostMemorySection_InvalidName(t *testing.T) {
	ctx := seedMemoryAgent(t)
	rec := postJSON(t, ctx.OfficeAgentPostMemorySection, "/api/office/agent/memory/section", map[string]interface{}{
		"sessionId": "s-vex",
		"name":      "bad name!!", // spaces + punctuation rejected
		"content":   "x",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid section name, got %d", rec.Code)
	}
}

func TestOfficeAgentGetMemorySection_InvalidName(t *testing.T) {
	ctx := seedMemoryAgent(t)
	req := httptest.NewRequest("GET", "/api/office/agent/memory/section?sessionId=s-vex&name=has%20space", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeAgentGetMemorySection(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid section name, got %d", rec.Code)
	}
}

func TestOfficeAgentGetMemoryQuery_MissingQ(t *testing.T) {
	ctx := seedMemoryAgent(t)
	req := httptest.NewRequest("GET", "/api/office/agent/memory/query?sessionId=s-vex", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeAgentGetMemoryQuery(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing q, got %d", rec.Code)
	}
}

func TestOfficeAgentGetMemoryQuery_FindsHit(t *testing.T) {
	ctx := seedMemoryAgent(t)
	// Seed memory, then query.
	postJSON(t, ctx.OfficeAgentPostMemory, "/api/office/agent/memory", map[string]interface{}{
		"sessionId": "s-vex",
		"content":   "line one\nthe magic token is XYZ\nline three",
	})
	req := httptest.NewRequest("GET", "/api/office/agent/memory/query?sessionId=s-vex&q=magic", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeAgentGetMemoryQuery(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	hits, _ := resp["hits"].([]interface{})
	if len(hits) == 0 {
		t.Error("expected at least one hit for 'magic'")
	}
}

func TestOfficeAgentGetMemoryQuery_UnknownAgentEmpty(t *testing.T) {
	ctx := newTimelineContext(t)
	req := httptest.NewRequest("GET", "/api/office/agent/memory/query?sessionId=s-nobody&q=x", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeAgentGetMemoryQuery(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	hits, _ := resp["hits"].([]interface{})
	if len(hits) != 0 {
		t.Errorf("expected no hits for unknown agent, got %d", len(hits))
	}
}
