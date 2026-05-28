package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/kshaham/jarvis/internal/claudehooks"
	"github.com/kshaham/jarvis/internal/web/middleware"
	"github.com/kshaham/jarvis/internal/web/sse"
)

func newToolPolicyCtx(t *testing.T) *Context {
	t.Helper()
	store := claudehooks.NewToolPolicyStore(filepath.Join(t.TempDir(), "tool-policy.json"))
	return &Context{
		ToolPolicy: store,
		SSEBroker:  sse.NewBroker(100, 10),
		CSRFStore:  middleware.NewCSRFStore(context.Background()),
	}
}

func TestGetToolPolicy_ReturnsDefaultWhenNoStoreConfigured(t *testing.T) {
	ctx := &Context{} // ToolPolicy nil
	req := httptest.NewRequest(http.MethodGet, "/api/office/tool-policy", nil)
	rec := httptest.NewRecorder()
	ctx.GetToolPolicy(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var got claudehooks.ToolPolicy
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.DenyRules) == 0 {
		t.Errorf("expected default policy with deny patterns, got %+v", got)
	}
}

func TestGetToolPolicy_ReturnsActivePolicy(t *testing.T) {
	ctx := newToolPolicyCtx(t)
	req := httptest.NewRequest(http.MethodGet, "/api/office/tool-policy", nil)
	rec := httptest.NewRecorder()
	ctx.GetToolPolicy(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var got claudehooks.ToolPolicy
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Fresh store seeds with DefaultToolPolicy; should be non-empty.
	if len(got.DenyRules) == 0 {
		t.Errorf("expected default deny patterns on fresh store, got %+v", got)
	}
}

func TestPutToolPolicy_NoStore503(t *testing.T) {
	ctx := &Context{}
	body := `{"denyPatterns":[],"allowOutsideWorktree":false}`
	req := httptest.NewRequest(http.MethodPut, "/api/office/tool-policy", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	ctx.PutToolPolicy(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
}

func TestPutToolPolicy_InvalidJSON400(t *testing.T) {
	ctx := newToolPolicyCtx(t)
	req := httptest.NewRequest(http.MethodPut, "/api/office/tool-policy",
		bytes.NewReader([]byte("not json {")))
	rec := httptest.NewRecorder()
	ctx.PutToolPolicy(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPutToolPolicy_HappyPathRoundTrips(t *testing.T) {
	ctx := newToolPolicyCtx(t)
	custom := claudehooks.ToolPolicy{
		DenyRules: []claudehooks.ToolPolicyRule{
			{Tool: "Bash", Pattern: `rm\s+-rf\s+/`},
		},
		AllowOutsideWorktree: true,
	}
	buf, _ := json.Marshal(custom)
	req := httptest.NewRequest(http.MethodPut, "/api/office/tool-policy", bytes.NewReader(buf))
	rec := httptest.NewRecorder()
	ctx.PutToolPolicy(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Success bool                  `json:"success"`
		Policy  claudehooks.ToolPolicy `json:"policy"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Success {
		t.Error("success=false")
	}
	if !resp.Policy.AllowOutsideWorktree {
		t.Error("AllowOutsideWorktree not applied")
	}
	if len(resp.Policy.DenyRules) != 1 {
		t.Errorf("expected 1 deny pattern, got %d", len(resp.Policy.DenyRules))
	}

	// Round-trip via a second GET
	getReq := httptest.NewRequest(http.MethodGet, "/api/office/tool-policy", nil)
	getRec := httptest.NewRecorder()
	ctx.GetToolPolicy(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET expected 200, got %d", getRec.Code)
	}
	var roundtripped claudehooks.ToolPolicy
	if err := json.Unmarshal(getRec.Body.Bytes(), &roundtripped); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !roundtripped.AllowOutsideWorktree {
		t.Error("round-trip: AllowOutsideWorktree lost")
	}
	if len(roundtripped.DenyRules) != 1 || roundtripped.DenyRules[0].Tool != "Bash" {
		t.Errorf("round-trip: deny patterns mismatch: %+v", roundtripped.DenyRules)
	}
}

func TestPutToolPolicy_InvalidRegex400(t *testing.T) {
	ctx := newToolPolicyCtx(t)
	bad := claudehooks.ToolPolicy{
		DenyRules: []claudehooks.ToolPolicyRule{
			{Tool: "Bash", Pattern: `[unclosed`}, // invalid regex
		},
	}
	buf, _ := json.Marshal(bad)
	req := httptest.NewRequest(http.MethodPut, "/api/office/tool-policy", bytes.NewReader(buf))
	rec := httptest.NewRecorder()
	ctx.PutToolPolicy(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid regex, got %d", rec.Code)
	}
}
