package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

var (
	apiURL     = os.Getenv("JARVIS_API_URL")
	httpClient = &http.Client{Timeout: 30 * time.Second}
)

func init() {
	if apiURL == "" {
		apiURL = "http://localhost:3555"
	}
}

// attachAuth adds the agent token header (when set via env) so /api/office/agent/*
// and any other auth-protected endpoint will accept the request. Matches the
// behavior of cmd/cm's addAgentAuth helper.
func attachAuth(req *http.Request) {
	if tok := os.Getenv("JARVIS_AGENT_TOKEN"); tok != "" {
		req.Header.Set("X-Jarvis-Agent-Token", tok)
	}
}

// doRequest is the single code path all apiGet/apiPost/apiDelete helpers
// funnel through so the auth header, body limit, and status handling stay
// consistent.
func doRequest(method, path string, body io.Reader, contentType string) (interface{}, error) {
	req, err := http.NewRequest(method, apiURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create %s %s request: %w", method, path, err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	attachAuth(req)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP %s %s failed: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	if len(respBody) == 0 {
		return map[string]interface{}{"success": true}, nil
	}

	var result interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return string(respBody), nil
	}
	return result, nil
}

func apiGet(path string) (interface{}, error) {
	return doRequest(http.MethodGet, path, nil, "")
}

func apiPost(path string, payload interface{}) (interface{}, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	return doRequest(http.MethodPost, path, bytes.NewReader(data), "application/json")
}

func apiDelete(path string) (interface{}, error) {
	return doRequest(http.MethodDelete, path, nil, "")
}

func apiPut(path string, payload interface{}) (interface{}, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	return doRequest(http.MethodPut, path, bytes.NewReader(data), "application/json")
}
