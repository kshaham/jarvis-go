package handler

// Round-3 branch fills for the session_extended.go file + toggle handlers,
// driven against a real shell session via liveSession():
//   - GetSessionFiles: ReadDir-error arm (workingDir removed) + >100 truncation
//   - GetSessionPlanHistory: bad version-number skip + empty-versions arm + the
//     versions list path
//   - SessionImageWatcher / SessionFlickerFilter / SetSessionCPULimit /
//     UpdateSessionColor: malformed-JSON 400 arms

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func sessionJSONReq(t *testing.T, id, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/sessions/"+id+"/x", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return withURLParam(req, "id", id)
}

func TestGetSessionFiles_ReadDirErrorAndTruncation(t *testing.T) {
	t.Parallel()
	// >100 files -> truncation arm.
	ctx := newSessionLiveCtx(t)
	id, wd := liveSession(t, ctx)
	for i := 0; i < 105; i++ {
		if err := os.WriteFile(filepath.Join(wd, "f"+strconv.Itoa(i)+".txt"), []byte("x"), 0o644); err != nil {
			t.Fatalf("seed file: %v", err)
		}
	}
	rec := httptest.NewRecorder()
	ctx.GetSessionFiles(rec, withURLParam(httptest.NewRequest("GET", "/api/sessions/"+id+"/files", nil), "id", id))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	resp := decodeMap(t, rec)
	files, _ := resp["files"].([]interface{})
	if len(files) != 100 {
		t.Errorf("expected truncation to 100 files, got %d", len(files))
	}

	// ReadDir error: a session whose workingDir was removed.
	ctx2 := newSessionLiveCtx(t)
	id2, wd2 := liveSession(t, ctx2)
	_ = os.RemoveAll(wd2)
	rec2 := httptest.NewRecorder()
	ctx2.GetSessionFiles(rec2, withURLParam(httptest.NewRequest("GET", "/api/sessions/"+id2+"/files", nil), "id", id2))
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 even on ReadDir error, got %d", rec2.Code)
	}
	resp2 := decodeMap(t, rec2)
	if _, hasErr := resp2["error"]; !hasErr {
		t.Errorf("expected an error field when workingDir is gone, got %v", resp2)
	}
}

func TestGetSessionPlanHistory_VersionsAndBadName(t *testing.T) {
	t.Parallel()
	ctx := newSessionLiveCtx(t)
	id, wd := liveSession(t, ctx)
	// A valid versioned plan + a malformed one (non-numeric version -> skipped).
	if err := os.WriteFile(filepath.Join(wd, "@fix_plan.v2.md"), []byte("v2"), 0o644); err != nil {
		t.Fatalf("write v2: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wd, "@fix_plan.vXYZ.md"), []byte("bad"), 0o644); err != nil {
		t.Fatalf("write bad: %v", err)
	}
	rec := httptest.NewRecorder()
	ctx.GetSessionPlanHistory(rec, withURLParam(httptest.NewRequest("GET", "/api/sessions/"+id+"/plan-history", nil), "id", id))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	resp := decodeMap(t, rec)
	versions, _ := resp["versions"].([]interface{})
	if len(versions) != 1 {
		t.Errorf("expected exactly the one valid version (bad one skipped), got %d", len(versions))
	}

	// A session with no plan files -> empty-versions arm.
	ctx2 := newSessionLiveCtx(t)
	id2, _ := liveSession(t, ctx2)
	rec2 := httptest.NewRecorder()
	ctx2.GetSessionPlanHistory(rec2, withURLParam(httptest.NewRequest("GET", "/api/sessions/"+id2+"/plan-history", nil), "id", id2))
	resp2 := decodeMap(t, rec2)
	versions2, _ := resp2["versions"].([]interface{})
	if len(versions2) != 0 {
		t.Errorf("expected empty versions for a fresh session, got %d", len(versions2))
	}
}

func TestSessionToggleHandlers_BadJSON400(t *testing.T) {
	t.Parallel()
	ctx := newSessionLiveCtx(t)
	id, _ := liveSession(t, ctx)
	for name, h := range map[string]func(http.ResponseWriter, *http.Request){
		"SessionImageWatcher":  ctx.SessionImageWatcher,
		"SessionFlickerFilter": ctx.SessionFlickerFilter,
		"SetSessionCPULimit":   ctx.SetSessionCPULimit,
		"UpdateSessionColor":   ctx.UpdateSessionColor,
		"SetSessionRalphConfig": ctx.SetSessionRalphConfig,
	} {
		rec := httptest.NewRecorder()
		h(rec, sessionJSONReq(t, id, "{bad json"))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400 on malformed body, got %d", name, rec.Code)
		}
	}
}
