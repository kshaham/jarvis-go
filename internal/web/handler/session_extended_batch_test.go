package handler

// Round-3 branch fills for session_extended.go handlers, driven against a real
// shell session via liveSession():
//   - GetSessionFileRaw: missing-path 400, path-resolution failure, file-not-found
//     404, binary-extension base64 branch, and the textual JSON branch.
//   - RunSession / SessionInteractive / SessionShell: malformed-JSON 400 arms.
//   - BulkRenameSessions / BulkUpdateSessionColors: the >maxBulkItems guard.

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kshaham/jarvis/internal/web/sse"
)

func rawFileReq(t *testing.T, id, query string) *http.Request {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/sessions/"+id+"/file-raw?"+query, nil)
	return withURLParam(req, "id", id)
}

func TestGetSessionFileRaw_MissingPath400(t *testing.T) {
	ctx := newSessionLiveCtx(t)
	id, _ := liveSession(t, ctx)
	rec := httptest.NewRecorder()
	ctx.GetSessionFileRaw(rec, rawFileReq(t, id, ""))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing path, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetSessionFileRaw_PathTraversalRejected(t *testing.T) {
	ctx := newSessionLiveCtx(t)
	id, _ := liveSession(t, ctx)
	rec := httptest.NewRecorder()
	// An escaping path -> resolveSessionFilePath writes the error, returns false.
	ctx.GetSessionFileRaw(rec, rawFileReq(t, id, "path=../../../../etc/passwd"))
	if rec.Code == http.StatusOK {
		t.Fatalf("expected a rejection for a traversal path, got 200: %s", rec.Body.String())
	}
}

func TestGetSessionFileRaw_FileNotFound404(t *testing.T) {
	ctx := newSessionLiveCtx(t)
	id, _ := liveSession(t, ctx)
	rec := httptest.NewRecorder()
	ctx.GetSessionFileRaw(rec, rawFileReq(t, id, "path=nope.txt"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for a missing file, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetSessionFileRaw_TextAndBinary(t *testing.T) {
	ctx := newSessionLiveCtx(t)
	id, wd := liveSession(t, ctx)

	// Textual file -> isText branch -> utf-8 content.
	if err := os.WriteFile(filepath.Join(wd, "notes.md"), []byte("# hello"), 0o644); err != nil {
		t.Fatalf("write text file: %v", err)
	}
	recT := httptest.NewRecorder()
	ctx.GetSessionFileRaw(recT, rawFileReq(t, id, "path=notes.md"))
	if recT.Code != http.StatusOK {
		t.Fatalf("text file: expected 200, got %d: %s", recT.Code, recT.Body.String())
	}
	respT := decodeMap(t, recT)
	if respT["encoding"] != "utf-8" {
		t.Errorf("text file should be utf-8, got %v", respT["encoding"])
	}
	if respT["content"] != "# hello" {
		t.Errorf("text content mismatch: %v", respT["content"])
	}

	// Binary extension (.png) -> base64 branch.
	pngBytes := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x01}
	if err := os.WriteFile(filepath.Join(wd, "pixel.png"), pngBytes, 0o644); err != nil {
		t.Fatalf("write png: %v", err)
	}
	recB := httptest.NewRecorder()
	ctx.GetSessionFileRaw(recB, rawFileReq(t, id, "path=pixel.png"))
	if recB.Code != http.StatusOK {
		t.Fatalf("png: expected 200, got %d: %s", recB.Code, recB.Body.String())
	}
	respB := decodeMap(t, recB)
	if respB["encoding"] != "base64" {
		t.Errorf("png should be base64, got %v", respB["encoding"])
	}
	if respB["mimeType"] != "image/png" {
		t.Errorf("png mime should be image/png, got %v", respB["mimeType"])
	}
	want := base64.StdEncoding.EncodeToString(pngBytes)
	if respB["content"] != want {
		t.Errorf("png base64 content mismatch")
	}
}

func sessionInputBadJSON(t *testing.T, h func(http.ResponseWriter, *http.Request), id string) int {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/sessions/"+id+"/x", strings.NewReader("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	req = withURLParam(req, "id", id)
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec.Code
}

func TestSessionInputHandlers_BadJSON400(t *testing.T) {
	ctx := newSessionLiveCtx(t)
	id, _ := liveSession(t, ctx)
	for name, h := range map[string]func(http.ResponseWriter, *http.Request){
		"RunSession":        ctx.RunSession,
		"SessionInteractive": ctx.SessionInteractive,
		"SessionShell":       ctx.SessionShell,
	} {
		if code := sessionInputBadJSON(t, h, id); code != http.StatusBadRequest {
			t.Errorf("%s: expected 400 on bad JSON, got %d", name, code)
		}
	}
}

func TestSessionInputHandlers_NotFound404(t *testing.T) {
	ctx := newSessionLiveCtx(t)
	for name, h := range map[string]func(http.ResponseWriter, *http.Request){
		"RunSession":        ctx.RunSession,
		"SessionInteractive": ctx.SessionInteractive,
		"SessionShell":       ctx.SessionShell,
	} {
		req := withURLParam(httptest.NewRequest("POST", "/api/sessions/ghost/x", strings.NewReader("{}")), "id", "ghost")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: expected 404 for unknown session, got %d", name, rec.Code)
		}
	}
}

func TestBulkRenameSessions_TooMany(t *testing.T) {
	ctx := &Context{SSEBroker: sse.NewBroker(10, 1)}
	renames := map[string]string{}
	for i := 0; i < maxBulkItems+1; i++ {
		renames[strings.Repeat("a", 3)+itoaMillisInt(i)] = "n"
	}
	rec := postJSON(t, ctx.BulkRenameSessions, "/api/sessions/bulk-rename", map[string]interface{}{
		"renames": renames,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for too many renames, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBulkUpdateSessionColors_TooMany(t *testing.T) {
	ctx := &Context{SSEBroker: sse.NewBroker(10, 1)}
	colors := map[string]string{}
	for i := 0; i < maxBulkItems+1; i++ {
		colors["s"+itoaMillisInt(i)] = "#fff"
	}
	rec := postJSON(t, ctx.BulkUpdateSessionColors, "/api/sessions/bulk-color", map[string]interface{}{
		"colors": colors,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for too many colors, got %d: %s", rec.Code, rec.Body.String())
	}
}

// itoaMillisInt renders an int as decimal (small helper to make unique keys).
func itoaMillisInt(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
