package handler

// Round-3 branch fills for misc.go:
//   - verifyHookSignature: no-prefix, bad-hex, and empty-key arms
//   - LinkCase: the CaseManager.Link error arm (target path doesn't exist)
//   - CreateCase: malformed-JSON 400 arm
//   - GetCaseFixPlan: the read-error / no-plan arm

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kshaham/jarvis/internal/cases"
	"github.com/kshaham/jarvis/internal/web/auth"
	"github.com/kshaham/jarvis/internal/web/sse"
)

func TestVerifyHookSignature_RejectArms(t *testing.T) {
	t.Parallel()
	am, err := auth.NewManager(t.TempDir(), false)
	if err != nil {
		t.Fatalf("auth manager: %v", err)
	}
	ctx := &Context{AuthManager: am}
	body := []byte("payload")

	// No "sha256=" prefix -> false.
	if ctx.verifyHookSignature(body, "deadbeef") {
		t.Error("expected false for a header without the sha256= prefix")
	}
	// Prefix present but the hex is malformed -> DecodeString error -> false.
	if ctx.verifyHookSignature(body, "sha256=nothex!!") {
		t.Error("expected false for non-hex signature digits")
	}
	// Well-formed but wrong signature -> mismatch -> false (also exercises the
	// non-empty-key check path).
	if ctx.verifyHookSignature(body, "sha256=00ff00ff") {
		t.Error("expected false for a mismatched signature")
	}
}

func TestLinkCase_LinkError(t *testing.T) {
	t.Parallel()
	ctx := &Context{
		CaseManager: cases.NewCaseManager(t.TempDir()),
		SSEBroker:   sse.NewBroker(100, 10),
	}
	// A target path that doesn't exist -> CaseManager.Link returns a 400-shaped
	// error.
	rec := postJSON(t, ctx.LinkCase, "/api/cases/link", map[string]interface{}{
		"name": "broken-link", "path": "/definitely/not/a/real/dir/anywhere-xyz",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 linking a missing path, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateCase_BadJSON400(t *testing.T) {
	t.Parallel()
	ctx := &Context{CaseManager: cases.NewCaseManager(t.TempDir())}
	req := httptest.NewRequest("POST", "/api/cases", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.CreateCase(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on malformed CreateCase body, got %d", rec.Code)
	}
}

func TestGetCaseFixPlan_NoPlanFile(t *testing.T) {
	t.Parallel()
	cm := cases.NewCaseManager(t.TempDir())
	info, err := cm.Create("plan-case")
	if err != nil {
		t.Fatalf("create case: %v", err)
	}
	// No @fix_plan.md written -> the ReadFile error arm (exists:false).
	_ = filepath.Join(info.Path, "@fix_plan.md")
	ctx := &Context{CaseManager: cm}
	rec := httptest.NewRecorder()
	ctx.GetCaseFixPlan(rec, withURLParam(httptest.NewRequest("GET", "/api/cases/plan-case/fix-plan", nil), "name", "plan-case"))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	resp := decodeMap(t, rec)
	if resp["exists"] != false {
		t.Errorf("expected exists=false when no @fix_plan.md, got %v", resp["exists"])
	}
}
