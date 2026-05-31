package handler

// Branch fills for the agent-scoped office POST handlers that share the
// decodeBody-then-GetAgent shape: the bad-JSON 400 and agent-not-found 404
// arms. These are the two most commonly-missed branches across this family.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// postBadJSON drives a handler with a malformed JSON body and asserts 400.
func postBadJSON(t *testing.T, h func(http.ResponseWriter, *http.Request), path string) {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("%s: expected 400 on bad JSON, got %d: %s", path, rec.Code, rec.Body.String())
	}
}

func TestOfficeAgentMemoryHandlers_BadJSONAndNotFound(t *testing.T) {
	ctx := newTimelineContext(t)

	t.Run("PostMemory_BadJSON", func(t *testing.T) {
		postBadJSON(t, ctx.OfficeAgentPostMemory, "/api/office/agent/memory")
	})
	t.Run("PostMemoryAppend_BadJSON", func(t *testing.T) {
		postBadJSON(t, ctx.OfficeAgentPostMemoryAppend, "/api/office/agent/memory/append")
	})

	t.Run("PostMemory_AgentNotFound", func(t *testing.T) {
		rec := postJSON(t, ctx.OfficeAgentPostMemory, "/api/office/agent/memory", map[string]interface{}{
			"sessionId": "s-ghost", "content": "x",
		})
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for unknown agent, got %d", rec.Code)
		}
	})
	t.Run("PostMemoryAppend_AgentNotFound", func(t *testing.T) {
		rec := postJSON(t, ctx.OfficeAgentPostMemoryAppend, "/api/office/agent/memory/append", map[string]interface{}{
			"sessionId": "s-ghost", "entry": "x",
		})
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for unknown agent, got %d", rec.Code)
		}
	})
}

func TestOfficeBrainstorm_BadJSON(t *testing.T) {
	ctx := newTimelineContext(t)
	postBadJSON(t, ctx.OfficeBrainstorm, "/api/office/brainstorm")
}

func TestOfficeAgentTaskSelfAdd_BadJSON(t *testing.T) {
	ctx := newTimelineContext(t)
	postBadJSON(t, ctx.OfficeAgentTaskSelfAdd, "/api/office/agent/task/self-add")
}

func TestOfficePostCodeGraphImpact_BadJSON(t *testing.T) {
	ctx := newTimelineContext(t)
	postBadJSON(t, ctx.OfficePostCodeGraphImpact, "/api/office/codegraph/impact")
}
