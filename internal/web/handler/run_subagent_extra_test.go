package handler

// Coverage for the Run handler happy path (a real allowlisted subprocess) and
// the monitoring_extended subagent handlers' non-nil-watcher branches (built
// with a real subagent.Watcher rooted at an empty temp dir).

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kshaham/jarvis/internal/state"
	"github.com/kshaham/jarvis/internal/subagent"
)

func TestRun_HappyEchoCommand(t *testing.T) {
	t.Parallel()
	ctx := &Context{StateStore: state.NewStore(t.TempDir() + "/s.json")}
	rec := postJSON(t, ctx.Run, "/api/run", map[string]interface{}{
		"command": "echo hello-from-run",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("run: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	if resp["exitCode"] != float64(0) {
		t.Errorf("exitCode=%v, want 0", resp["exitCode"])
	}
	out, _ := resp["output"].(string)
	if !strings.Contains(out, "hello-from-run") {
		t.Errorf("output should contain the echoed text, got %q", out)
	}
}

func TestRun_NonZeroExitCaptured(t *testing.T) {
	t.Parallel()
	ctx := &Context{StateStore: state.NewStore(t.TempDir() + "/s.json")}
	// `ls` of a non-existent path exits non-zero; the handler still 200s and
	// reports the exit code rather than erroring.
	rec := postJSON(t, ctx.Run, "/api/run", map[string]interface{}{
		"command": "ls /definitely/not/here/at/all",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("run: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	if resp["exitCode"] == float64(0) {
		t.Error("expected a non-zero exit code for ls of a missing path")
	}
}

// subagentCtx builds a Context with a real (empty) SubagentWatcher so the
// non-nil branches of the subagent handlers run.
func subagentCtx(t *testing.T) *Context {
	t.Helper()
	w := subagent.NewWatcher(context.Background(), t.TempDir())
	return &Context{
		SubagentWatcher: w,
		StateStore:      state.NewStore(t.TempDir() + "/s.json"),
	}
}

func TestGetSubagents_RealWatcherEmpty(t *testing.T) {
	t.Parallel()
	ctx := subagentCtx(t)
	resp := getJSON(t, ctx.GetSubagents, "/api/subagents")
	agents, _ := resp["agents"].([]interface{})
	if len(agents) != 0 {
		t.Errorf("expected 0 agents from empty watcher, got %d", len(agents))
	}
}

func TestGetSubagent_RealWatcherNotFound(t *testing.T) {
	t.Parallel()
	ctx := subagentCtx(t)
	req := withURLParam(httptest.NewRequest("GET", "/api/subagents/a1", nil), "agentId", "a1")
	rec := httptest.NewRecorder()
	ctx.GetSubagent(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown agent, got %d", rec.Code)
	}
}

func TestDeleteSubagent_RealWatcher(t *testing.T) {
	t.Parallel()
	ctx := subagentCtx(t)
	req := withURLParam(httptest.NewRequest("DELETE", "/api/subagents/a1", nil), "agentId", "a1")
	rec := httptest.NewRecorder()
	ctx.DeleteSubagent(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	resp := decodeMap(t, rec)
	// Nothing to remove -> removed=false.
	if resp["removed"] != false {
		t.Errorf("removed=%v, want false for unknown agent", resp["removed"])
	}
}

func TestGetSubagentTranscript_RealWatcherNotFound(t *testing.T) {
	t.Parallel()
	ctx := subagentCtx(t)
	req := withURLParam(httptest.NewRequest("GET", "/api/subagents/a1/transcript", nil), "agentId", "a1")
	rec := httptest.NewRecorder()
	ctx.GetSubagentTranscript(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestCleanupAndClearSubagents_RealWatcher(t *testing.T) {
	t.Parallel()
	ctx := subagentCtx(t)
	resp := getJSON(t, ctx.CleanupSubagents, "/api/subagents/cleanup")
	if resp["cleaned"] != float64(0) {
		t.Errorf("cleaned=%v, want 0", resp["cleaned"])
	}

	resp2 := getJSON(t, ctx.ClearAllSubagents, "/api/subagents")
	if resp2["success"] != true {
		t.Errorf("success=%v, want true", resp2["success"])
	}
}

func TestGetSessionSubagents_NoWatcher_EmptyButOK(t *testing.T) {
	t.Parallel()
	// SubagentWatcher present, session found, no matching agents -> empty list.
	ctx := newSessionExtCtx(t)
	ctx.SubagentWatcher = subagent.NewWatcher(context.Background(), t.TempDir())
	id, _ := liveSession(t, ctx)
	req := withURLParam(httptest.NewRequest("GET", "/api/sessions/"+id+"/subagents", nil), "id", id)
	rec := httptest.NewRecorder()
	ctx.GetSessionSubagents(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	resp := decodeMap(t, rec)
	subs, _ := resp["subagents"].([]interface{})
	if len(subs) != 0 {
		t.Errorf("expected 0 matched subagents, got %d", len(subs))
	}
}
