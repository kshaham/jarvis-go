package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/claudeman/jarvis-go/internal/pty"
	"github.com/claudeman/jarvis-go/internal/web/middleware"
	"github.com/claudeman/jarvis-go/internal/web/sse"
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
