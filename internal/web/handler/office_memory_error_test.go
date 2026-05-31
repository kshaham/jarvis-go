package handler

// Round-3 branch fills for the memory-section error arms in office.go:
//   - OfficeAgentPostMemorySection: the write-failure 500 (3114-3125)
//   - OfficeAgentGetMemorySection: the read-failure 500 (3145-3148)
//   - OfficeAgentGetMemoryQuery: the query-failure 500 (3169-3172)
// Forced by making the agent's memory FILE path a directory, so os.ReadFile /
// the temp-file write fails with a non-IsNotExist error.

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/kshaham/jarvis/internal/office"
)

// getRaw invokes a GET handler and returns the raw recorder (no status assert).
func getRaw(t *testing.T, h func(http.ResponseWriter, *http.Request), path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func blockAgentMemoryPath(t *testing.T, configID string) {
	t.Helper()
	// The handler resolves workingDir to "" for an agent with no live session.
	memPath := office.GetAgentMemoryPath(configID, "")
	if err := os.MkdirAll(memPath, 0o755); err != nil {
		t.Fatalf("mkdir memory path as dir: %v", err)
	}
}

func TestOfficeAgentPostMemorySection_WriteError(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-mem", "cfg-memerr", &office.TeamMemberConfig{ID: "cfg-memerr", Name: "M"})
	blockAgentMemoryPath(t, "cfg-memerr")

	rec := postJSON(t, ctx.OfficeAgentPostMemorySection, "/api/office/agent/memory/section", map[string]interface{}{
		"sessionId": "s-mem", "name": "learnings", "content": "x", "append": false,
	})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when the memory file path is a directory, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficeAgentGetMemorySection_ReadError(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-mem", "cfg-memrd", &office.TeamMemberConfig{ID: "cfg-memrd", Name: "M"})
	blockAgentMemoryPath(t, "cfg-memrd")

	req := "/api/office/agent/memory/section?sessionId=s-mem&name=learnings"
	rec := getRaw(t, ctx.OfficeAgentGetMemorySection, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 reading a directory as memory, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficeAgentGetMemoryQuery_ReadError(t *testing.T) {
	ctx := newTimelineContext(t)
	ctx.OfficeManager.RegisterAgent("s-mem", "cfg-memq", &office.TeamMemberConfig{ID: "cfg-memq", Name: "M"})
	blockAgentMemoryPath(t, "cfg-memq")

	req := "/api/office/agent/memory/query?sessionId=s-mem&q=lesson"
	rec := getRaw(t, ctx.OfficeAgentGetMemoryQuery, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 querying a directory as memory, got %d: %s", rec.Code, rec.Body.String())
	}
}
