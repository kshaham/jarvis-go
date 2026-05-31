package handler

// Round-3 branch fills for the remaining session_extended.go handler arms:
//   - RenameSession: bad-JSON + empty-name
//   - ImportSession: claude-mode selection + CreateSession-error (bad workingDir)
//   - GetSessionFileContent: read-error (path is a directory)
//   - ImportSessionFixPlan: bad-JSON
//   - WriteSessionRalphPrompt: bad-JSON + atomicWriteFile-error (workingDir gone)
//   - DeleteSessionTailFile: the found-stream cancel arm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// withURLParams attaches multiple chi URL params in a single route context so
// none clobber the others (withURLParam replaces the whole route context).
func withURLParams(req *http.Request, params map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestRenameSession_BadJSONAndEmptyName(t *testing.T) {
	t.Parallel()
	ctx := newSessionLiveCtx(t)
	id, _ := liveSession(t, ctx)

	rec := httptest.NewRecorder()
	ctx.RenameSession(rec, sessionJSONReq(t, id, "{bad"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad JSON: expected 400, got %d", rec.Code)
	}

	rec2 := httptest.NewRecorder()
	ctx.RenameSession(rec2, sessionJSONReq(t, id, `{"name":"   "}`))
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("empty name: expected 400, got %d: %s", rec2.Code, rec2.Body.String())
	}
}

func TestImportSession_ClaudeModeAndCreateError(t *testing.T) {
	skipIfNoStubShell(t)
	installClaudeOnPATH(t, "")
	ctx := newSessionLiveCtx(t)

	// claude mode + a valid dir -> the ModeClaude selection arm.
	wd := t.TempDir()
	recOK := postJSON(t, ctx.ImportSession, "/api/sessions/import", map[string]interface{}{
		"workingDir": wd, "mode": "claude", "name": "imp-claude",
	})
	if recOK.Code != http.StatusOK {
		t.Fatalf("claude import: expected 200, got %d: %s", recOK.Code, recOK.Body.String())
	}
	resp := decodeMap(t, recOK)
	if sess, _ := resp["session"].(map[string]interface{}); sess != nil {
		if sid, _ := sess["id"].(string); sid != "" {
			t.Cleanup(func() { _ = ctx.SessionManager.StopSession(t.Context(), sid) })
		}
	}

	// A non-existent workingDir -> CreateSession fails -> 500.
	recErr := postJSON(t, ctx.ImportSession, "/api/sessions/import", map[string]interface{}{
		"workingDir": "/definitely/not/a/dir/ever-xyz", "mode": "shell",
	})
	if recErr.Code != http.StatusInternalServerError {
		t.Fatalf("bad dir import: expected 500, got %d: %s", recErr.Code, recErr.Body.String())
	}
}

func TestGetSessionFileContent_ReadError(t *testing.T) {
	t.Parallel()
	ctx := newSessionLiveCtx(t)
	id, wd := liveSession(t, ctx)
	// A subdirectory: ReadFile on a directory returns a non-IsNotExist error -> 500.
	if err := os.Mkdir(filepath.Join(wd, "adir"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	req := withURLParam(httptest.NewRequest("GET", "/api/sessions/"+id+"/file-content?path=adir", nil), "id", id)
	rec := httptest.NewRecorder()
	ctx.GetSessionFileContent(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 reading a directory, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestImportSessionFixPlan_WriteError(t *testing.T) {
	t.Parallel()
	ctx := newSessionLiveCtx(t)
	id, wd := liveSession(t, ctx)
	// Remove the workingDir so atomicWriteFile's CreateTemp fails -> 500.
	_ = os.RemoveAll(wd)
	rec := httptest.NewRecorder()
	ctx.ImportSessionFixPlan(rec, sessionJSONReq(t, id, `{"content":"# plan"}`))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when workingDir is gone, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestWriteSessionRalphPrompt_BadJSONAndWriteError(t *testing.T) {
	t.Parallel()
	ctx := newSessionLiveCtx(t)
	id, wd := liveSession(t, ctx)

	recBad := httptest.NewRecorder()
	ctx.WriteSessionRalphPrompt(recBad, sessionJSONReq(t, id, "{bad"))
	if recBad.Code != http.StatusBadRequest {
		t.Fatalf("bad JSON: expected 400, got %d", recBad.Code)
	}

	// Remove the session's workingDir so atomicWriteFile's CreateTemp fails -> 500.
	_ = os.RemoveAll(wd)
	recErr := httptest.NewRecorder()
	ctx.WriteSessionRalphPrompt(recErr, sessionJSONReq(t, id, `{"prompt":"loop forever"}`))
	if recErr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when workingDir is gone, got %d: %s", recErr.Code, recErr.Body.String())
	}
}

func TestDeleteSessionTailFile_CancelsRegisteredStream(t *testing.T) {
	t.Parallel()
	ctx := newSessionLiveCtx(t)
	id, _ := liveSession(t, ctx)

	cancelled := false
	ctx.TailFileStreamsMu.Lock()
	if ctx.TailFileStreams == nil {
		ctx.TailFileStreams = map[string]func(){}
	}
	ctx.TailFileStreams["stream-1"] = func() { cancelled = true }
	ctx.TailFileStreamsMu.Unlock()

	req := httptest.NewRequest("DELETE", "/api/sessions/"+id+"/tail-file/stream-1", nil)
	req = withURLParams(req, map[string]string{"id": id, "streamId": "stream-1"})
	rec := httptest.NewRecorder()
	ctx.DeleteSessionTailFile(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	resp := decodeMap(t, rec)
	if resp["success"] != true {
		t.Errorf("expected success=true for a found stream, got %v", resp["success"])
	}
	if !cancelled {
		t.Error("expected the registered stream's cancel func to be called")
	}
}

var _ = strings.TrimSpace
