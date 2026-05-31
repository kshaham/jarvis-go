package handler

// Round-4 fills for session_extended.go file-handler arms:
//   - GetSessionFileContent: IsNotExist -> 404 (1275-1278)
//   - GetSessionFileRaw: non-IsNotExist read error -> 500 (1630-1631)
//   - SessionPlanTask: atomicWriteFile failure -> 500 (1590-1593)

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestGetSessionFileContent_NotExist404(t *testing.T) {
	t.Parallel()
	ctx := newSessionLiveCtx(t)
	id, _ := liveSession(t, ctx)
	req := withURLParam(httptest.NewRequest("GET", "/api/sessions/"+id+"/file-content?path=missing.md", nil), "id", id)
	rec := httptest.NewRecorder()
	ctx.GetSessionFileContent(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for a non-existent file, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetSessionFileRaw_DirectoryReadError500(t *testing.T) {
	t.Parallel()
	ctx := newSessionLiveCtx(t)
	id, wd := liveSession(t, ctx)
	// Reading a directory yields a non-IsNotExist error -> 500.
	if err := os.Mkdir(filepath.Join(wd, "rawdir"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	req := withURLParam(httptest.NewRequest("GET", "/api/sessions/"+id+"/file-raw?path=rawdir", nil), "id", id)
	rec := httptest.NewRecorder()
	ctx.GetSessionFileRaw(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 reading a directory, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSessionPlanTask_WriteError500(t *testing.T) {
	t.Parallel()
	ctx := newSessionLiveCtx(t)
	id, wd := liveSession(t, ctx)
	// Remove the workingDir so atomicWriteFile's CreateTemp (in the now-missing
	// dir) fails -> 500.
	_ = os.RemoveAll(wd)
	rec := httptest.NewRecorder()
	ctx.SessionPlanTask(rec, sessionJSONReq(t, id, `{"task":"do a thing"}`))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when the workingDir is gone, got %d: %s", rec.Code, rec.Body.String())
	}
}
