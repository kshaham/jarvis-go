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
