package handler

// Coverage for the OfficeBrowserHealth non-200 (error-status) branch, which the
// existing OK/unreachable tests don't reach. Points the probe at an httptest
// server returning a 500 via the HEADLESS_BROWSER_HOST/PORT env vars.

import (
	"net/url"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOfficeBrowserHealth_BadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	t.Setenv("HEADLESS_BROWSER_HOST", u.Hostname())
	t.Setenv("HEADLESS_BROWSER_PORT", u.Port())

	ctx := &Context{}
	resp := getJSON(t, ctx.OfficeBrowserHealth, "/api/office/browser/health")
	if resp["ok"] != false {
		t.Errorf("ok=%v, want false for a 500 probe", resp["ok"])
	}
	if errStr, _ := resp["error"].(string); !strings.Contains(errStr, "500") {
		t.Errorf("error should mention HTTP 500, got %q", errStr)
	}
}
