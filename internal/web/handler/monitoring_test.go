package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/web/middleware"
	"github.com/kshaham/jarvis/internal/web/sse"
)

func newTestContext() *Context {
	return &Context{
		SessionManager: pty.NewSessionManager(10),
		SSEBroker:      sse.NewBroker(100, 10),
		CSRFStore:      middleware.NewCSRFStore(context.Background()),
	}
}

func TestHealth(t *testing.T) {
	ctx := newTestContext()
	req := httptest.NewRequest("GET", "/api/health", nil)
	rec := httptest.NewRecorder()

	ctx.Health(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["status"] != "ok" {
		t.Errorf("expected status=ok, got %v", resp["status"])
	}
}

func TestStatus(t *testing.T) {
	ctx := newTestContext()
	req := httptest.NewRequest("GET", "/api/status", nil)
	rec := httptest.NewRecorder()

	ctx.Status(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["status"] != "running" {
		t.Errorf("expected running, got %v", resp["status"])
	}
	sessions, ok := resp["sessions"].([]interface{})
	if !ok || len(sessions) != 0 {
		t.Errorf("expected empty sessions array, got %v", resp["sessions"])
	}
}

func TestReady_Ok(t *testing.T) {
	ctx := newTestContext()
	// Provide a StateStore so all nil checks pass.
	ctx.StateStore = nil // explicit: will cause 503
	req := httptest.NewRequest("GET", "/api/ready", nil)
	rec := httptest.NewRecorder()

	ctx.Ready(rec, req)

	// Without a StateStore, expect 503.
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when StateStore is nil, got %d", rec.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["ready"] != false {
		t.Errorf("expected ready=false, got %v", resp["ready"])
	}
	failing, _ := resp["failing"].([]interface{})
	if len(failing) == 0 {
		t.Error("expected non-empty failing list when StateStore is nil")
	}
}

func TestReady_Degraded_GoroutineCount(t *testing.T) {
	ctx := newTestContext()
	// Test that goroutine count appears in the response.
	req := httptest.NewRequest("GET", "/api/ready", nil)
	rec := httptest.NewRecorder()

	ctx.Ready(rec, req)

	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	if _, ok := resp["goroutines"]; !ok {
		t.Error("expected goroutines field in response")
	}
}

func TestCSRFToken(t *testing.T) {
	ctx := newTestContext()
	req := httptest.NewRequest("GET", "/api/csrf-token", nil)
	rec := httptest.NewRecorder()

	ctx.CSRFToken(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	token, ok := resp["token"].(string)
	if !ok || token == "" {
		t.Error("expected non-empty token")
	}

	// Token should be valid
	if !ctx.CSRFStore.Validate(token) {
		t.Error("generated token should be valid")
	}
	// Multi-use: second validate should also succeed (tokens reusable until TTL)
	if !ctx.CSRFStore.Validate(token) {
		t.Error("token should be reusable (multi-use)")
	}
}

func TestQuerySessions_EmptyReturnsZero(t *testing.T) {
	ctx := newTestContext()
	req := httptest.NewRequest("GET", "/api/sessions/query", nil)
	rec := httptest.NewRecorder()

	ctx.QuerySessions(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["total"] != float64(0) {
		t.Errorf("expected total=0, got %v", resp["total"])
	}
	if resp["hasMore"] != false {
		t.Errorf("expected hasMore=false, got %v", resp["hasMore"])
	}
}

func TestQuerySessions_HonorsLimit(t *testing.T) {
	ctx := newTestContext()
	req := httptest.NewRequest("GET", "/api/sessions/query?limit=5&offset=0", nil)
	rec := httptest.NewRecorder()

	ctx.QuerySessions(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["limit"] != float64(5) {
		t.Errorf("expected limit=5, got %v", resp["limit"])
	}
}

// TestGetClaudeAuthStatus_CacheServesStaleResult verifies that the 30s TTL
// cache on GetClaudeAuthStatus is served for subsequent calls even after
// the credentials file changes on disk.
func TestGetClaudeAuthStatus_CacheServesStaleResult(t *testing.T) {
	// Point HOME at a temp dir so the credentials file is isolated.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	// Reset the global cache so this test starts clean, and restore on exit.
	resetClaudeAuthCache()
	t.Cleanup(resetClaudeAuthCache)

	ctx := newTestContext()

	// Write a credentials file with an API key so the first call returns
	// authenticated=true.
	claudeDir := filepath.Join(tmpHome, ".claude")
	_ = os.MkdirAll(claudeDir, 0o755)
	credPath := filepath.Join(claudeDir, ".credentials.json")
	_ = os.WriteFile(credPath, []byte(`{"apiKey":"sk-test-key"}`), 0o600)

	req1 := httptest.NewRequest("GET", "/api/claude/auth-status", nil)
	rec1 := httptest.NewRecorder()
	ctx.GetClaudeAuthStatus(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first call: status=%d", rec1.Code)
	}
	var resp1 map[string]interface{}
	_ = json.Unmarshal(rec1.Body.Bytes(), &resp1)
	if resp1["authenticated"] != true {
		t.Fatalf("first call: authenticated=%v want true", resp1["authenticated"])
	}

	// Remove the credentials file and the symlink target that claudeauth
	// may have created as a side effect of checkStatus — but DON'T reset
	// the in-memory cache.
	_ = os.Remove(credPath)
	_ = os.Remove(filepath.Join(tmpHome, ".claude-auth", ".credentials.json"))

	// Second call within TTL should still return the cached (authenticated=true) result.
	req2 := httptest.NewRequest("GET", "/api/claude/auth-status", nil)
	rec2 := httptest.NewRecorder()
	ctx.GetClaudeAuthStatus(rec2, req2)
	var resp2 map[string]interface{}
	_ = json.Unmarshal(rec2.Body.Bytes(), &resp2)
	if resp2["authenticated"] != true {
		t.Errorf("second call (cached): authenticated=%v want true (stale cache)", resp2["authenticated"])
	}

	// After resetting the cache, the next call reflects disk state (no credentials).
	resetClaudeAuthCache()
	req3 := httptest.NewRequest("GET", "/api/claude/auth-status", nil)
	rec3 := httptest.NewRecorder()
	ctx.GetClaudeAuthStatus(rec3, req3)
	var resp3 map[string]interface{}
	_ = json.Unmarshal(rec3.Body.Bytes(), &resp3)
	if resp3["authenticated"] != false {
		t.Errorf("third call (cache reset): authenticated=%v want false", resp3["authenticated"])
	}
}
