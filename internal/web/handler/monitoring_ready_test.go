package handler

// Extra coverage for monitoring.go Ready/Health/Status edge cases not hit
// by monitoring_test.go: the individual failing-dependency branches and
// the all-healthy 200 path.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/state"
	"github.com/kshaham/jarvis/internal/web/middleware"
	"github.com/kshaham/jarvis/internal/web/sse"
)

func TestReady_AllHealthy(t *testing.T) {
	ctx := &Context{
		StateStore:     state.NewStore(filepath.Join(t.TempDir(), "state.json")),
		SSEBroker:      sse.NewBroker(100, 10),
		SessionManager: pty.NewSessionManager(10),
		CSRFStore:      middleware.NewCSRFStore(context.Background()),
	}
	req := httptest.NewRequest("GET", "/api/ready", nil)
	rec := httptest.NewRecorder()
	ctx.Ready(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when all deps present, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["ready"] != true {
		t.Errorf("expected ready=true, got %v", resp["ready"])
	}
	failing, _ := resp["failing"].([]interface{})
	if len(failing) != 0 {
		t.Errorf("expected no failing deps, got %v", failing)
	}
}

func TestReady_SSEBrokerNil(t *testing.T) {
	ctx := &Context{
		StateStore:     state.NewStore(filepath.Join(t.TempDir(), "state.json")),
		SessionManager: pty.NewSessionManager(10),
	}
	req := httptest.NewRequest("GET", "/api/ready", nil)
	rec := httptest.NewRecorder()
	ctx.Ready(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when SSEBroker nil, got %d", rec.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	failing, _ := resp["failing"].([]interface{})
	if !containsStr(failing, "sseBroker") {
		t.Errorf("expected sseBroker in failing list, got %v", failing)
	}
}

func TestReady_SessionManagerNil(t *testing.T) {
	ctx := &Context{
		StateStore: state.NewStore(filepath.Join(t.TempDir(), "state.json")),
		SSEBroker:  sse.NewBroker(100, 10),
	}
	req := httptest.NewRequest("GET", "/api/ready", nil)
	rec := httptest.NewRecorder()
	ctx.Ready(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when SessionManager nil, got %d", rec.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	failing, _ := resp["failing"].([]interface{})
	if !containsStr(failing, "sessionManager") {
		t.Errorf("expected sessionManager in failing list, got %v", failing)
	}
}

func TestHealth_Shape(t *testing.T) {
	ctx := newTestContext()
	req := httptest.NewRequest("GET", "/api/health", nil)
	rec := httptest.NewRecorder()
	ctx.Health(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["status"] != "ok" {
		t.Errorf("status=%v, want ok", resp["status"])
	}
	if _, ok := resp["uptime"]; !ok {
		t.Error("expected uptime field")
	}
}

func TestStatus_VersionAndGoVersion(t *testing.T) {
	ctx := newTestContext()
	req := httptest.NewRequest("GET", "/api/status", nil)
	rec := httptest.NewRecorder()
	ctx.Status(rec, req)
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["version"] != "0.2.0-go" {
		t.Errorf("version=%v, want 0.2.0-go", resp["version"])
	}
	if gv, _ := resp["goVersion"].(string); gv == "" {
		t.Error("expected non-empty goVersion")
	}
	if resp["success"] != true {
		t.Errorf("expected success=true, got %v", resp["success"])
	}
}

func containsStr(list []interface{}, want string) bool {
	for _, v := range list {
		if s, _ := v.(string); s == want {
			return true
		}
	}
	return false
}
