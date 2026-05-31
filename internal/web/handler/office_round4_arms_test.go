package handler

// Round-4 fills for reachable office.go arms:
//   - OfficeGetLearningsByConfig: corrupt learnings file -> 500 (1577-1578)
//   - OfficeAgentPostDevInstance: invalid slot name -> 400 (2616-2617)
//   - OfficePutBugTask / OfficePutMemberPersonality / OfficePostProjectInvariant:
//     decodeBody bad-JSON 400 (1467, 3268, 3320)

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOfficeGetLearningsByConfig_CorruptFile500(t *testing.T) {
	ctx := newTimelineContext(t) // HOME is a temp dir
	// Seed a corrupt learnings file so LoadLearningsForUI's json.Unmarshal fails
	// with a non-ErrLearningsNotFound error -> 500.
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".jarvis", "learnings")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir learnings: %v", err)
	}
	configID := "corruptcfg"
	if err := os.WriteFile(filepath.Join(dir, configID+".json"), []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("write corrupt learnings: %v", err)
	}

	req := withURLParam(httptest.NewRequest("GET", "/api/office/learnings/"+configID, nil), "configId", configID)
	rec := httptest.NewRecorder()
	ctx.OfficeGetLearningsByConfig(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for a corrupt learnings file, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficeAgentPostDevInstance_InvalidSlotName400(t *testing.T) {
	ctx := newTimelineContext(t)
	// An invalid slot name (contains a space) is rejected by SetDevInstance with a
	// non-conflict error -> generic 400 arm.
	rec := postJSON(t, ctx.OfficeAgentPostDevInstance, "/api/office/agent/dev-instance?force=true", map[string]interface{}{
		"name": "bad slot name!", "url": "http://jarvis:3000", "pid": 123,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an invalid slot name, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid slot name") {
		t.Errorf("expected 'invalid slot name' in body, got %s", rec.Body.String())
	}
}

// NOTE: OfficeBrainstorm's success serialization (office.go:3190) is NOT
// covered here. StartBrainstorm launches an async runBrainstormFlow goroutine
// that mutates the returned *Meeting under the manager lock, while the handler
// JSON-encodes the SAME live pointer without the lock — a genuine production
// data race (see bugsFound). Any test that reaches line 3190 trips -race; the
// fix lives in internal/office (a locked snapshot accessor), which is out of
// scope for this package's test pass.

func TestOfficePutBugTask_BadJSON400(t *testing.T) {
	ctx := newTimelineContext(t)
	req := withURLParam(httptest.NewRequest("PUT", "/api/office/bug-tasks/b1", strings.NewReader("{bad")), "id", "b1")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.OfficePutBugTask(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on bad JSON, got %d", rec.Code)
	}
}

func TestOfficePutMemberPersonality_BadJSON400(t *testing.T) {
	ctx := newTimelineContext(t)
	req := withURLParam(httptest.NewRequest("PUT", "/api/office/members/s1/personality", strings.NewReader("{bad")), "sessionId", "s1")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.OfficePutMemberPersonality(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on bad JSON, got %d", rec.Code)
	}
}

func TestOfficePostProjectInvariant_BadJSON400(t *testing.T) {
	ctx := newTimelineContext(t)
	req := httptest.NewRequest("POST", "/api/office/project-invariants", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.OfficePostProjectInvariant(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on bad JSON, got %d", rec.Code)
	}
}
