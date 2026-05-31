package main

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

// withServer spins up an httptest.Server, points the package-level apiURL at it,
// and restores the previous value when the test finishes. The handler receives
// every request so individual tests can assert on method/path/body and craft a
// response. It returns the server so tests may also inspect server.URL.
func withServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	prev := apiURL
	apiURL = srv.URL
	t.Cleanup(func() {
		apiURL = prev
		srv.Close()
	})
	return srv
}

// captureRequest is a recording struct populated by recordingHandler.
type captureRequest struct {
	mu      sync.Mutex
	Method  string
	Path    string
	RawPath string
	Query   string
	Body    string
	Headers http.Header
	Count   int
}

func (c *captureRequest) get() captureRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return captureRequest{
		Method:  c.Method,
		Path:    c.Path,
		RawPath: c.RawPath,
		Query:   c.Query,
		Body:    c.Body,
		Headers: c.Headers,
		Count:   c.Count,
	}
}

// recordingHandler captures the last request into cap and replies with the
// provided status and body. If body is JSON it is sent verbatim.
func recordingHandler(cap *captureRequest, status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		cap.mu.Lock()
		cap.Method = r.Method
		cap.Path = r.URL.Path
		cap.RawPath = r.URL.EscapedPath()
		cap.Query = r.URL.RawQuery
		cap.Body = string(b)
		cap.Headers = r.Header.Clone()
		cap.Count++
		cap.mu.Unlock()
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

// resultText returns the concatenated text of all content blocks in a ToolResult.
func resultText(res ToolResult) string {
	var sb strings.Builder
	for _, c := range res.Content {
		sb.WriteString(c.Text)
	}
	return sb.String()
}

// captureStdout redirects os.Stdout for the duration of fn and returns whatever
// was written. writeResponse/writeError call os.Stdout.Sync(); a pipe supports
// Sync as a no-op on most platforms, and on darwin it returns an error which the
// production code ignores, so this is safe.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w

	done := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(r)
		done <- string(data)
	}()

	fn()

	// Restore and close the writer so the reader goroutine sees EOF.
	w.Close()
	os.Stdout = orig
	out := <-done
	r.Close()
	return out
}

// decodeResponse parses a single JSON-RPC response line.
func decodeResponse(t *testing.T, line string) JSONRPCResponse {
	t.Helper()
	var resp JSONRPCResponse
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &resp); err != nil {
		t.Fatalf("decode response %q: %v", line, err)
	}
	return resp
}

// firstLine returns the first non-empty newline-delimited line in s.
func firstLine(s string) string {
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		if t := strings.TrimSpace(sc.Text()); t != "" {
			return t
		}
	}
	return ""
}

// setEnv sets an env var for the duration of the test and restores it after.
func setEnv(t *testing.T, key, val string) {
	t.Helper()
	prev, had := os.LookupEnv(key)
	if err := os.Setenv(key, val); err != nil {
		t.Fatalf("setenv %s: %v", key, err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, prev)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}
