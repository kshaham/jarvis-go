package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestOfficeBrowserHealth_OK runs a fake headless-shell HTTP server
// locally, points the handler at it via HEADLESS_BROWSER_HOST/PORT,
// and asserts the happy path.
func TestOfficeBrowserHealth_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/version" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"Browser":"HeadlessChrome/140.0.0.0"}`))
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	t.Setenv("HEADLESS_BROWSER_HOST", u.Hostname())
	t.Setenv("HEADLESS_BROWSER_PORT", u.Port())

	ctx := newTimelineContext(t)
	req := httptest.NewRequest("GET", "/api/office/browser/health", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeBrowserHealth(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body browserHealthResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if !body.OK {
		t.Errorf("expected ok=true, got %+v", body)
	}
	if !strings.Contains(body.BrowserVersion, "HeadlessChrome") {
		t.Errorf("expected HeadlessChrome in browserVersion, got %q", body.BrowserVersion)
	}
}

func TestOfficeBrowserHealth_Unreachable(t *testing.T) {
	// Reserved port range: nothing will respond.
	t.Setenv("HEADLESS_BROWSER_HOST", "127.0.0.1")
	t.Setenv("HEADLESS_BROWSER_PORT", "1") // low/reserved; should refuse fast

	ctx := newTimelineContext(t)
	req := httptest.NewRequest("GET", "/api/office/browser/health", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeBrowserHealth(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body browserHealthResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.OK {
		t.Errorf("expected ok=false for unreachable host, got %+v", body)
	}
	if body.Error == "" {
		t.Error("expected non-empty error field")
	}
}
