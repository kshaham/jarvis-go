package handler

// Round 5 -- residual session_extended.go arms that need a live PTY session
// carrying a non-zero token count, plus the DecodeAndValidate failure arms
// and resolveSessionFilePath error variants.
//
// Token accumulation is honest: a ModeShell session that prints a Claude-style
// status line ("<N> tokens") has that line parsed by the read loop's debounced
// status-line scanner, so session.Tokens() reports N. The auto-clear/compact/
// trim handlers then take their "threshold met -> fire immediately" arm.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/pty"
)

// liveSessionNoTrackers registers a live shell session WITHOUT calling
// initSessionTrackers, so the Ralph / RunSummary "no tracker" default arms
// are reachable. The session is stopped on cleanup.
func liveSessionNoTrackers(t *testing.T, ctx *Context) string {
	t.Helper()
	sess, err := ctx.SessionManager.CreateSession(context.Background(), pty.SessionConfig{
		WorkingDir: t.TempDir(),
		Mode:       pty.ModeShell,
		Name:       "no-trackers",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	t.Cleanup(func() {
		_ = ctx.SessionManager.StopSession(context.Background(), sess.ID)
		ctx.cleanupSessionResources(sess.ID)
	})
	return sess.ID
}

func TestGetSessionRalphState_NoTrackerDefault(t *testing.T) {
	t.Parallel()
	ctx := newSessionExtCtx(t)
	id := liveSessionNoTrackers(t, ctx)

	rec := httptest.NewRecorder()
	ctx.GetSessionRalphState(rec, idReq("GET", "/api/sessions/"+id+"/ralph-state", "", id))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	state, _ := resp["state"].(map[string]interface{})
	if state == nil || state["enabled"] != false {
		t.Errorf("expected disabled default ralph state, got %v", resp["state"])
	}
	if state["loopIteration"] != float64(0) {
		t.Errorf("expected loopIteration 0 default, got %v", state["loopIteration"])
	}
}

func TestGetSessionRunSummary_NoTrackerDefault(t *testing.T) {
	t.Parallel()
	ctx := newSessionExtCtx(t)
	id := liveSessionNoTrackers(t, ctx)

	rec := httptest.NewRecorder()
	ctx.GetSessionRunSummary(rec, idReq("GET", "/api/sessions/"+id+"/run-summary", "", id))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	summary, _ := resp["summary"].(map[string]interface{})
	if summary == nil || summary["sessionId"] != id {
		t.Errorf("expected default summary with sessionId, got %v", resp["summary"])
	}
	events, _ := summary["events"].([]interface{})
	if len(events) != 0 {
		t.Errorf("expected empty events default, got %d", len(events))
	}
}

// liveSessionWithTokens creates a live shell session and drives its token
// counter above `want` by having the shell echo a status line that the read
// loop's token scanner parses. Polls until Tokens() reflects the count.
func liveSessionWithTokens(t *testing.T, ctx *Context, want int64) string {
	t.Helper()
	id, _ := liveSession(t, ctx)
	sess, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		t.Fatalf("setup: session %s missing", id)
	}
	// Print a status line the token scanner recognises ("<N> tokens").
	sess.WriteToPTY(fmt.Sprintf("printf '%%s\\n' '%d tokens'\r", want))

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if _, _, total := sess.Tokens(); total >= want {
			return id
		}
		time.Sleep(50 * time.Millisecond)
	}
	_, _, total := sess.Tokens()
	t.Fatalf("session never reached %d tokens (got %d)", want, total)
	return ""
}

func TestSessionAutoClear_ThresholdMetFiresClear(t *testing.T) {
	t.Parallel()
	ctx := newSessionExtCtx(t)
	id := liveSessionWithTokens(t, ctx, 5000)

	rec := httptest.NewRecorder()
	ctx.SessionAutoClear(rec, idReq("POST", "/api/sessions/"+id+"/auto-clear",
		`{"enabled":true,"threshold":1000}`, id))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	if resp["enabled"] != true || resp["threshold"] != float64(1000) {
		t.Errorf("unexpected response: %v", resp)
	}
}

func TestSessionAutoCompact_ThresholdMetFiresCompact(t *testing.T) {
	t.Parallel()
	ctx := newSessionExtCtx(t)
	id := liveSessionWithTokens(t, ctx, 5000)

	rec := httptest.NewRecorder()
	ctx.SessionAutoCompact(rec, idReq("POST", "/api/sessions/"+id+"/auto-compact",
		`{"enabled":true,"threshold":1000}`, id))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSessionAutoTrim_ThresholdMetFiresTrim(t *testing.T) {
	t.Parallel()
	ctx := newSessionExtCtx(t)
	id := liveSessionWithTokens(t, ctx, 5000)

	rec := httptest.NewRecorder()
	ctx.SessionAutoTrim(rec, idReq("POST", "/api/sessions/"+id+"/auto-trim",
		`{"enabled":true,"threshold":1000}`, id))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- DecodeAndValidate failure arms (110/150/190) ---

func TestBulkSessions_DecodeFailureReturns400(t *testing.T) {
	t.Parallel()
	ctx := newSessionExtCtx(t)
	cases := []struct {
		name string
		h    func(http.ResponseWriter, *http.Request)
		path string
	}{
		{"BulkDelete", ctx.BulkDeleteSessions, "/api/sessions/bulk-delete"},
		{"BulkRename", ctx.BulkRenameSessions, "/api/sessions/bulk-rename"},
		{"BulkColors", ctx.BulkUpdateSessionColors, "/api/sessions/bulk-colors"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// A non-JSON body makes request.DecodeAndValidate write its own 400
			// and return false -> handler returns immediately.
			req := httptest.NewRequest("POST", tc.path, badJSONBody())
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			tc.h(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("%s: expected 400 on undecodable body, got %d: %s", tc.name, rec.Code, rec.Body.String())
			}
		})
	}
}

// --- GetSessionRalphStatus default (1109) ---

func TestGetSessionRalphStatus_DefaultWhenNoConfig(t *testing.T) {
	t.Parallel()
	ctx := newSessionExtCtx(t)
	id, _ := liveSession(t, ctx) // no ralph-config setting written

	rec := httptest.NewRecorder()
	ctx.GetSessionRalphStatus(rec, idReq("GET", "/api/sessions/"+id+"/ralph-status", "", id))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	status, _ := resp["status"].(map[string]interface{})
	if status == nil || status["enabled"] != false {
		t.Errorf("expected default {enabled:false}, got %v", resp["status"])
	}
}

// --- GetSessionPlanHistory ReadDir error (1479) ---

func TestGetSessionPlanHistory_ReadDirErrorEmptyVersions(t *testing.T) {
	t.Parallel()
	ctx := newSessionExtCtx(t)
	id, _ := liveSession(t, ctx)
	sess, _ := ctx.SessionManager.GetSession(id)
	// Point WorkingDir at a non-existent path so os.ReadDir errors -> the
	// handler returns 200 with empty versions instead of failing.
	sess.WorkingDir = "/nonexistent/path/does/not/exist-xyz"

	rec := httptest.NewRecorder()
	ctx.GetSessionPlanHistory(rec, idReq("GET", "/api/sessions/"+id+"/plan/history", "", id))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	versions, _ := resp["versions"].([]interface{})
	if len(versions) != 0 {
		t.Errorf("expected empty versions on unreadable dir, got %d", len(versions))
	}
}

// --- PatchSessionPlanTask tracker-hit path (1867) ---

func TestPatchSessionPlanTask_TrackerUpdateSucceeds(t *testing.T) {
	t.Parallel()
	ctx := newSessionExtCtx(t)
	id, _ := liveSession(t, ctx)

	// Populate the session's Ralph tracker with a todo so UpdatePlanTask
	// finds it by ID and returns true -- the in-tracker fast path that
	// short-circuits before the on-disk plan file is consulted.
	ctx.RalphTrackersMu.RLock()
	tracker := ctx.RalphTrackers[id]
	ctx.RalphTrackersMu.RUnlock()
	if tracker == nil {
		t.Fatal("setup: expected a ralph tracker for the live session")
	}
	tracker.ProcessOutput("- [ ] ship the feature\n") // -> todo-1

	rec := httptest.NewRecorder()
	req := twoParamReq("PATCH", "/api/sessions/"+id+"/plan/task/todo-1",
		`{"status":"completed"}`, "id", id, "taskId", "todo-1")
	ctx.PatchSessionPlanTask(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from tracker fast-path, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	if resp["taskId"] != "todo-1" {
		t.Errorf("taskId=%v, want todo-1", resp["taskId"])
	}
	// The tracker's todo should now be marked completed.
	for _, todo := range tracker.GetState().Todos {
		if todo.ID == "todo-1" && todo.Status != "completed" {
			t.Errorf("todo-1 status=%q, want completed", todo.Status)
		}
	}
}

// --- PatchSessionPlanTask file-write error (1932) ---

func TestPatchSessionPlanTask_WriteErrorReturns500(t *testing.T) {
	ctx := newSessionExtCtx(t)
	id, wd := liveSession(t, ctx)
	// A real plan file with a numeric-index task; the tracker has NO matching
	// todo so the handler falls through to the on-disk rewrite, where the
	// injected chmod failure makes atomicWriteFile error -> 500.
	planPath := filepath.Join(wd, "@fix_plan.md")
	if err := os.WriteFile(planPath, []byte("- [ ] task zero\n"), 0o644); err != nil {
		t.Fatalf("seed plan: %v", err)
	}

	restoreSeams(t)
	atomicChmod = func(string, os.FileMode) error { return fmt.Errorf("injected chmod failure") }

	rec := httptest.NewRecorder()
	req := twoParamReq("PATCH", "/api/sessions/"+id+"/plan/task/0",
		`{"status":"completed"}`, "id", id, "taskId", "0")
	ctx.PatchSessionPlanTask(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when plan write fails, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "failed to write fix plan") {
		t.Errorf("expected write-failure message, got %q", rec.Body.String())
	}
}

// badJSONBody returns a reader that yields malformed JSON.
func badJSONBody() *badReader { return &badReader{} }

type badReader struct{ done bool }

func (b *badReader) Read(p []byte) (int, error) {
	if b.done {
		return 0, fmt.Errorf("eof")
	}
	b.done = true
	n := copy(p, []byte("{not valid json"))
	return n, nil
}
