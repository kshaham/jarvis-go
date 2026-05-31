package handler

// Round-3 branch fills for OfficeBrowserHealth: the host/port default arms
// (env unset) and the http.NewRequestWithContext build-error arm (an invalid
// host produces an unparseable URL).

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOfficeBrowserHealth_DefaultHostPort(t *testing.T) {
	// Unset both env vars so the "headless-shell:9222" defaults are assigned.
	t.Setenv("HEADLESS_BROWSER_HOST", "")
	t.Setenv("HEADLESS_BROWSER_PORT", "")

	ctx := &Context{}
	req := httptest.NewRequest("GET", "/api/office/browser/health", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeBrowserHealth(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (handler always 200), got %d", rec.Code)
	}
	resp := decodeMap(t, rec)
	// The default host "headless-shell" doesn't resolve in test, so ok=false,
	// but the default host/port must be reflected in the response.
	if resp["host"] != "headless-shell" {
		t.Errorf("expected default host headless-shell, got %v", resp["host"])
	}
	if resp["port"] != "9222" {
		t.Errorf("expected default port 9222, got %v", resp["port"])
	}
}

func TestOfficeBrowserHealth_BuildRequestError(t *testing.T) {
	// A host containing a control character makes the assembled URL unparseable,
	// so http.NewRequestWithContext returns a build error.
	t.Setenv("HEADLESS_BROWSER_HOST", "bad\x7fhost")
	t.Setenv("HEADLESS_BROWSER_PORT", "9222")

	ctx := &Context{}
	req := httptest.NewRequest("GET", "/api/office/browser/health", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeBrowserHealth(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	resp := decodeMap(t, rec)
	if resp["ok"] != false {
		t.Errorf("expected ok=false on build error, got %v", resp["ok"])
	}
	if errStr, _ := resp["error"].(string); !strings.Contains(errStr, "build request") {
		t.Errorf("expected a build-request error, got %q", errStr)
	}
}
