package static

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// serveGet drives the given handler with a GET for path and returns the
// recorder so callers can assert status, headers and body.
func serveGet(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// writeFile creates parent dirs and writes content, failing the test on error.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestHandler_DevMode_ServesFromDisk verifies that when publicDir points at a
// real directory the handler serves files off disk (http.Dir).
func TestHandler_DevMode_ServesFromDisk(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "hello.txt"), "from disk")

	h := Handler(dir)
	rec := serveGet(t, h, "/hello.txt")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	if string(body) != "from disk" {
		t.Fatalf("body = %q, want %q", body, "from disk")
	}
}

// TestHandler_DevMode_ContentTypeByExtension checks that the disk file server
// sets a sensible Content-Type derived from the file extension.
func TestHandler_DevMode_ContentTypeByExtension(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "page.html"), "<!DOCTYPE html><html><body>hi</body></html>")
	writeFile(t, filepath.Join(dir, "app.js"), "console.log('ok');")
	writeFile(t, filepath.Join(dir, "site.css"), "body{color:red}")

	h := Handler(dir)

	cases := []struct {
		path       string
		wantPrefix string
	}{
		{"/page.html", "text/html"},
		{"/app.js", ""}, // js content-type varies by platform; checked below
		{"/site.css", "text/css"},
	}
	for _, c := range cases {
		rec := serveGet(t, h, c.path)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", c.path, rec.Code)
		}
		ct := rec.Header().Get("Content-Type")
		if c.path == "/app.js" {
			// Accept either text/javascript or application/javascript family.
			if !strings.Contains(ct, "javascript") && !strings.Contains(ct, "ecmascript") {
				t.Fatalf("%s: content-type = %q, want a javascript type", c.path, ct)
			}
			continue
		}
		if !strings.HasPrefix(ct, c.wantPrefix) {
			t.Fatalf("%s: content-type = %q, want prefix %q", c.path, ct, c.wantPrefix)
		}
	}
}

// TestHandler_DevMode_NotFound verifies a missing file yields 404 from disk.
func TestHandler_DevMode_NotFound(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "exists.txt"), "ok")

	h := Handler(dir)
	rec := serveGet(t, h, "/missing-file.txt")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestHandler_DevMode_IndexFallback verifies http.FileServer serves index.html
// for a directory request ("/").
func TestHandler_DevMode_IndexFallback(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "index.html"), "<html>root index</html>")

	h := Handler(dir)
	rec := serveGet(t, h, "/")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	if !strings.Contains(string(body), "root index") {
		t.Fatalf("expected index.html to be served, got %q", body)
	}
}

// TestHandler_DevMode_SubdirIndexFallback verifies the directory-with-index
// fallback also works for nested directories.
func TestHandler_DevMode_SubdirIndexFallback(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "sub", "index.html"), "<html>sub index</html>")

	h := Handler(dir)
	rec := serveGet(t, h, "/sub/")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	if !strings.Contains(string(body), "sub index") {
		t.Fatalf("expected sub/index.html, got %q", body)
	}
}

// TestHandler_EmptyDir_FallsToEmbed verifies that an empty publicDir string
// skips the disk branch entirely and serves from the embedded FS.
func TestHandler_EmptyDir_FallsToEmbed(t *testing.T) {
	h := Handler("")
	rec := serveGet(t, h, "/office.html")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for embedded office.html", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	if len(body) == 0 {
		t.Fatal("expected non-empty embedded office.html body")
	}
	if !strings.Contains(strings.ToLower(string(body)), "<!doctype html") {
		n := len(body)
		if n > 64 {
			n = 64
		}
		t.Fatalf("embedded office.html missing doctype, got first bytes: %q", body[:n])
	}
}

// TestHandler_NonexistentDir_FallsToEmbed verifies that a publicDir pointing at
// a path that does not exist falls through to the embedded FS (the os.Stat err
// branch of Handler), rather than serving a broken disk root.
func TestHandler_NonexistentDir_FallsToEmbed(t *testing.T) {
	h := Handler("/no/such/dir/anywhere/please")
	rec := serveGet(t, h, "/office.html")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 from embed fallback", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	if len(body) == 0 {
		t.Fatal("expected embedded office.html via fallback")
	}
}

// TestHandler_PublicDirIsFile_FallsToEmbed verifies that when publicDir exists
// but is a regular file (not a directory), the IsDir() guard rejects it and the
// handler falls through to the embedded FS.
func TestHandler_PublicDirIsFile_FallsToEmbed(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "iamfile")
	writeFile(t, filePath, "not a directory")

	h := Handler(filePath)
	rec := serveGet(t, h, "/office.html")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 from embed fallback (publicDir was a file)", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	if len(body) == 0 {
		t.Fatal("expected embedded office.html when publicDir is a file")
	}
}

// TestHandler_Embed_NotFound verifies the embedded file server returns 404 for
// an unknown path.
func TestHandler_Embed_NotFound(t *testing.T) {
	h := Handler("")
	rec := serveGet(t, h, "/this-asset-does-not-exist-12345.bin")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for unknown embedded asset", rec.Code)
	}
}

// TestHandler_Embed_ContentType verifies content-type negotiation through the
// embedded FS for a known CSS asset.
func TestHandler_Embed_ContentType(t *testing.T) {
	h := Handler("")
	rec := serveGet(t, h, "/office.css")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for embedded office.css", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/css") {
		t.Fatalf("content-type = %q, want text/css prefix", ct)
	}
}

// TestHandler_Embed_NestedAsset verifies nested embedded assets (css/ subdir)
// are reachable through the embedded file server.
func TestHandler_Embed_NestedAsset(t *testing.T) {
	h := Handler("")
	rec := serveGet(t, h, "/css/design-tokens.css")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for nested embedded css asset", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/css") {
		t.Fatalf("nested asset content-type = %q, want text/css prefix", ct)
	}
}

// TestHandler_Embed_IndexFallback verifies the embedded file server serves the
// embedded index.html for a "/" request.
func TestHandler_Embed_IndexFallback(t *testing.T) {
	h := Handler("")
	rec := serveGet(t, h, "/")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for embedded index fallback", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	if len(body) == 0 {
		t.Fatal("expected embedded index.html body for root request")
	}
}

// TestFileExists_PublicDirIsFile_FallsToEmbed verifies that when publicDir is a
// regular file (not a directory) the IsDir guard fails and FileExists consults
// the embedded FS instead. office.html ships in the embed, so it resolves true.
func TestFileExists_PublicDirIsFile_FallsToEmbed(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "regularfile")
	writeFile(t, filePath, "i am a file")

	if !FileExists(filePath, "office.html") {
		t.Fatal("expected embed fallback to find office.html when publicDir is a file")
	}
	if FileExists(filePath, "does-not-exist-anywhere-xyz.html") {
		t.Fatal("expected false for unknown embed asset when publicDir is a file")
	}
}
