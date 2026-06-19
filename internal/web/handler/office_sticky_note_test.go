package handler

// Feature B · agent sticky-note feed handler coverage.
//   - OfficePostStickyNote: empty-content 400, too-long 400, and the
//     happy path that resolves the author from the agent session and
//     appends a note.
//   - OfficeGetStickyNotes / OfficeListReports: sticky notes appear in
//     the feed and merge into the report library with type:"note".

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kshaham/jarvis/internal/office"
)

func TestOfficePostStickyNote_EmptyContent400(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	rec := postJSON(t, ctx.OfficePostStickyNote, "/api/office/agent/sticky-note",
		map[string]interface{}{"sessionId": "s1", "content": "   "})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty content, got %d", rec.Code)
	}
}

func TestOfficePostStickyNote_TooLong400(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	rec := postJSON(t, ctx.OfficePostStickyNote, "/api/office/agent/sticky-note",
		map[string]interface{}{"sessionId": "s1", "content": strings.Repeat("x", 2001)})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for over-long content, got %d", rec.Code)
	}
}

func TestOfficePostStickyNote_BadJSON400(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	rec := httptest.NewRecorder()
	ctx.OfficePostStickyNote(rec, badJSONReq("/api/office/agent/sticky-note"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on bad JSON, got %d", rec.Code)
	}
}

func TestOfficePostStickyNote_ResolvesAuthorAndAppends(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	ctx.OfficeManager.RegisterAgent("sess-1", "cfg-1", &office.TeamMemberConfig{ID: "cfg-1", Name: "Builder"})

	rec := postJSON(t, ctx.OfficePostStickyNote, "/api/office/agent/sticky-note",
		map[string]interface{}{"sessionId": "sess-1", "content": "shipped the parser"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	noteAny, ok := resp["note"].(map[string]interface{})
	if !ok {
		t.Fatalf("response missing note object: %v", resp)
	}
	if noteAny["author"] != "Builder" {
		t.Errorf("author = %v, want Builder", noteAny["author"])
	}

	notes := ctx.OfficeManager.ListStickyNotes()
	if len(notes) != 1 || notes[0].Author != "Builder" || notes[0].Content != "shipped the parser" {
		t.Fatalf("manager state wrong: %+v", notes)
	}
}

func TestOfficePostStickyNote_UnknownSessionFallsBackToAgent(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	rec := postJSON(t, ctx.OfficePostStickyNote, "/api/office/agent/sticky-note",
		map[string]interface{}{"sessionId": "nobody", "content": "orphan note"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	notes := ctx.OfficeManager.ListStickyNotes()
	if len(notes) != 1 || notes[0].Author != "agent" {
		t.Fatalf("expected fallback author 'agent', got %+v", notes)
	}
}

func TestOfficeGetStickyNotes_ReturnsNewestFirst(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	ctx.OfficeManager.AddStickyNote("A", "first", "")
	ctx.OfficeManager.AddStickyNote("B", "second", "")

	rec := httptest.NewRecorder()
	ctx.OfficeGetStickyNotes(rec, httptest.NewRequest(http.MethodGet, "/api/office/sticky-notes", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	resp := decodeMap(t, rec)
	notes, ok := resp["notes"].([]interface{})
	if !ok || len(notes) != 2 {
		t.Fatalf("expected 2 notes, got %v", resp["notes"])
	}
	first := notes[0].(map[string]interface{})
	if first["content"] != "second" {
		t.Errorf("expected newest-first (second), got %v", first["content"])
	}
}

func TestOfficeListReports_MergesStickyNotes(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	ctx.OfficeManager.AddStickyNote("Builder", "decided on chi router", "")

	rec := httptest.NewRecorder()
	ctx.OfficeListReports(rec, httptest.NewRequest(http.MethodGet, "/api/office/reports", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	resp := decodeMap(t, rec)
	reports, ok := resp["reports"].([]interface{})
	if !ok {
		t.Fatalf("reports not a list: %v", resp["reports"])
	}
	var foundNote bool
	for _, r := range reports {
		rm, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		if rm["type"] == "note" {
			foundNote = true
			if rm["author"] != "Builder" {
				t.Errorf("note author = %v, want Builder", rm["author"])
			}
			fn, _ := rm["filename"].(string)
			if !strings.HasPrefix(fn, "note-") {
				t.Errorf("note filename = %q, want note-<id> prefix", fn)
			}
		}
	}
	if !foundNote {
		t.Fatal("expected a type:\"note\" entry in the merged report library")
	}
}
