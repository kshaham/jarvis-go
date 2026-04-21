package handler

// R34 · Headless-shell (sibling browser container) health endpoint.
//
// Agents run `browser-screenshot <url>` which drives a
// chromedp/headless-shell container over CDP. When that container
// crashes or is misconfigured the screenshot fails with a cryptic
// Protocol error. This endpoint lets the dashboard (and agents) see
// the state of the browser itself, not just the app under test.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/claudeman/jarvis-go/internal/web/request"
)

type browserHealthResponse struct {
	OK             bool   `json:"ok"`
	Host           string `json:"host"`
	Port           string `json:"port"`
	LatencyMs      int64  `json:"latencyMs"`
	BrowserVersion string `json:"browserVersion,omitempty"`
	Error          string `json:"error,omitempty"`
}

// OfficeBrowserHealth returns the reachability + version of the
// sibling headless-shell container. Reads HEADLESS_BROWSER_HOST /
// HEADLESS_BROWSER_PORT from env with safe defaults.
// GET /api/office/browser/health
func (ctx *Context) OfficeBrowserHealth(w http.ResponseWriter, r *http.Request) {
	host := os.Getenv("HEADLESS_BROWSER_HOST")
	if host == "" {
		host = "headless-shell"
	}
	port := os.Getenv("HEADLESS_BROWSER_PORT")
	if port == "" {
		port = "9222"
	}

	url := fmt.Sprintf("http://%s:%s/json/version", host, port)
	probeCtx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, url, nil)
	if err != nil {
		request.WriteJSON(w, http.StatusOK, browserHealthResponse{
			OK: false, Host: host, Port: port,
			Error: "build request: " + err.Error(),
		})
		return
	}
	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		request.WriteJSON(w, http.StatusOK, browserHealthResponse{
			OK: false, Host: host, Port: port, LatencyMs: latency,
			Error: err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		request.WriteJSON(w, http.StatusOK, browserHealthResponse{
			OK: false, Host: host, Port: port, LatencyMs: latency,
			Error: fmt.Sprintf("HTTP %d from headless-shell", resp.StatusCode),
		})
		return
	}

	// Parse just enough to surface the browser version for the
	// dashboard tooltip. /json/version fields are standard CDP.
	var body struct {
		Browser string `json:"Browser"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)

	request.WriteJSON(w, http.StatusOK, browserHealthResponse{
		OK: true, Host: host, Port: port, LatencyMs: latency,
		BrowserVersion: body.Browser,
	})
}
