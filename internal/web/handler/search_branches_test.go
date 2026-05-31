package handler

// Round-3 branch fills for OfficeSearch:
//   - malformed JSON body -> decode-fail 400
//   - a description-stronger-than-title task match (descScore > titleScore)
//   - non-matching message + non-matching task present (the score==0 continues)

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kshaham/jarvis/internal/office"
)

func TestOfficeSearch_BadJSON400(t *testing.T) {
	ctx := newSnapshotCtx(t)
	req := httptest.NewRequest(http.MethodPost, "/api/office/search", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.OfficeSearch(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on malformed search body, got %d", rec.Code)
	}
}

func TestOfficeSearch_DescriptionStrongerThanTitle(t *testing.T) {
	ctx := newSnapshotCtx(t)
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})
	vex := ctx.OfficeManager.GetAgent("s-vex")

	// A matching task whose DESCRIPTION carries the query but whose TITLE does
	// not -> the descScore>score branch wins.
	vex.AddTask(&office.Task{
		ID:          "t-desc",
		Title:       "unrelated heading",
		Description: "the kanban migration needs a kanban schema bump",
	})
	// A non-matching task -> the both-scores-zero continue.
	vex.AddTask(&office.Task{ID: "t-skip", Title: "nothing here", Description: "irrelevant"})
	// A non-matching message -> the message score==0 continue.
	ctx.OfficeManager.SendMessage("s-vex", "", "completely unrelated chatter")

	rec := searchReq(t, ctx, map[string]interface{}{"query": "kanban"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	hits, _ := resp["hits"].([]interface{})
	if len(hits) != 1 {
		t.Fatalf("expected exactly the description-matching task, got %d hits: %v", len(hits), hits)
	}
	h := hits[0].(map[string]interface{})
	if h["id"] != "t-desc" {
		t.Errorf("expected the description-match task t-desc, got %v", h["id"])
	}
	if snip, _ := h["snippet"].(string); !strings.Contains(strings.ToLower(snip), "kanban") {
		t.Errorf("expected the description snippet to carry the query, got %q", snip)
	}
}
