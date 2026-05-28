package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kshaham/jarvis/internal/office"
)

func searchReq(t *testing.T, ctx *Context, body map[string]interface{}) *httptest.ResponseRecorder {
	t.Helper()
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/office/search", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.OfficeSearch(rec, req)
	return rec
}

func TestOfficeSearch_RequiresQuery(t *testing.T) {
	ctx := newSnapshotCtx(t)
	rec := searchReq(t, ctx, map[string]interface{}{"query": ""})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty query, got %d", rec.Code)
	}
}

func TestOfficeSearch_NoManager(t *testing.T) {
	ctx := &Context{}
	rec := searchReq(t, ctx, map[string]interface{}{"query": "x"})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when manager nil, got %d", rec.Code)
	}
}

func TestOfficeSearch_FindsTaskAndMessage(t *testing.T) {
	ctx := newSnapshotCtx(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{
		ID: "vex", Name: "Vex", Role: "scout",
	})
	ctx.OfficeManager.RegisterAgent("s-bolt", "bolt", &office.TeamMemberConfig{
		ID: "bolt", Name: "Bolt", Role: "builder",
	})
	vex := ctx.OfficeManager.GetAgent("s-vex")
	vex.AddTask(&office.Task{
		ID:          "t-1",
		Title:       "Refactor the dropdown widget",
		Description: "The dropdown jumps when clicked twice",
	})
	ctx.OfficeManager.SendMessage("s-vex", "s-bolt", "dropdown bug surfaces in safari")

	rec := searchReq(t, ctx, map[string]interface{}{"query": "dropdown"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Query string                   `json:"query"`
		Hits  []map[string]interface{} `json:"hits"`
		Total int                      `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total < 2 {
		t.Errorf("expected ≥2 hits (task + message), got %d (hits=%+v)", resp.Total, resp.Hits)
	}
	seenKinds := map[string]bool{}
	for _, h := range resp.Hits {
		if k, ok := h["kind"].(string); ok {
			seenKinds[k] = true
		}
	}
	if !seenKinds["task"] {
		t.Error("expected at least one task hit")
	}
	if !seenKinds["message"] {
		t.Error("expected at least one message hit")
	}
}

func TestOfficeSearch_TypeFilter(t *testing.T) {
	ctx := newSnapshotCtx(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})
	ctx.OfficeManager.GetAgent("s-vex").AddTask(&office.Task{ID: "t-1", Title: "alpha widget"})
	ctx.OfficeManager.SendMessage("s-vex", "", "alpha bravo")

	rec := searchReq(t, ctx, map[string]interface{}{
		"query": "alpha",
		"types": []string{"task"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp struct {
		Hits []map[string]interface{} `json:"hits"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	for _, h := range resp.Hits {
		if h["kind"] != "task" {
			t.Errorf("type filter leaked non-task hit: %+v", h)
		}
	}
}

func TestOfficeSearch_LimitClamp(t *testing.T) {
	ctx := newSnapshotCtx(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})
	agent := ctx.OfficeManager.GetAgent("s-vex")
	for i := 0; i < 250; i++ {
		agent.AddTask(&office.Task{ID: "t", Title: "needle " + strings.Repeat("x", i)})
	}
	rec := searchReq(t, ctx, map[string]interface{}{
		"query": "needle",
		"limit": 99999, // ask for huge; expect clamp at searchMaxLimit
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp struct {
		Hits  []map[string]interface{} `json:"hits"`
		Total int                      `json:"total"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Hits) > searchMaxLimit {
		t.Errorf("limit not clamped: got %d hits, expected ≤ %d", len(resp.Hits), searchMaxLimit)
	}
	if resp.Total < 250 {
		t.Errorf("Total should reflect pre-clamp count, got %d", resp.Total)
	}
}

func TestScoreSubstring_Basics(t *testing.T) {
	if s, _ := scoreSubstring("foo", ""); s != 0 {
		t.Error("empty text should score 0")
	}
	if s, _ := scoreSubstring("foo", "bar baz"); s != 0 {
		t.Error("no-match should score 0")
	}
	// Earlier-position matches score higher.
	earlyScore, _ := scoreSubstring("foo", "foo bar baz")
	lateScore, _ := scoreSubstring("foo", strings.Repeat("x", 60)+"foo")
	if earlyScore <= lateScore {
		t.Errorf("early match (%g) should outrank late match (%g)", earlyScore, lateScore)
	}
}
