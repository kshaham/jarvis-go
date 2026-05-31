package handler

// Coverage for the validation paths of OneShotSession (oneshot.go) and the
// CodePrinter / GetCaseFixPlan / LinkCase / GeneratePlanDetailed handlers in
// misc.go that reject or render before any subprocess is spawned.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kshaham/jarvis/internal/cases"
	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/state"
	"github.com/kshaham/jarvis/internal/web/middleware"
	"github.com/kshaham/jarvis/internal/web/sse"
)

func newOneShotCtx(t *testing.T) *Context {
	t.Helper()
	return &Context{
		SessionManager: pty.NewSessionManager(10),
		SSEBroker:      sse.NewBroker(100, 10),
		CSRFStore:      middleware.NewCSRFStore(context.Background()),
		StateStore:     state.NewStore(filepath.Join(t.TempDir(), "state.json")),
	}
}

// --- OneShotSession validation (no subprocess) ---

func TestOneShotSession_MissingPrompt400(t *testing.T) {
	t.Parallel()
	ctx := newOneShotCtx(t)
	rec := postJSON(t, ctx.OneShotSession, "/api/sessions/one-shot", map[string]interface{}{
		"workingDir": "/tmp",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing prompt, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOneShotSession_MissingWorkingDir400(t *testing.T) {
	t.Parallel()
	ctx := newOneShotCtx(t)
	// No workingDir on the request and none in settings -> 400.
	rec := postJSON(t, ctx.OneShotSession, "/api/sessions/one-shot", map[string]interface{}{
		"prompt": "ls",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing workingDir, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOneShotSession_InvalidWorkingDir400(t *testing.T) {
	t.Parallel()
	ctx := newOneShotCtx(t)
	rec := postJSON(t, ctx.OneShotSession, "/api/sessions/one-shot", map[string]interface{}{
		"prompt":     "ls",
		"workingDir": "/definitely/not/a/real/dir/anywhere",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid workingDir, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOneShotSession_BadJSON(t *testing.T) {
	t.Parallel()
	ctx := newOneShotCtx(t)
	req := httptest.NewRequest("POST", "/api/sessions/one-shot", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.OneShotSession(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// --- CodePrinter ---

func TestCodePrinterPrint_CountsLines(t *testing.T) {
	t.Parallel()
	ctx := &Context{}
	rec := postJSON(t, ctx.CodePrinterPrint, "/api/code-printer/print", map[string]interface{}{
		"code": "line1\nline2\nline3", "language": "go",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	resp := decodeMap(t, rec)
	if resp["lines"] != float64(3) {
		t.Errorf("lines=%v, want 3", resp["lines"])
	}
	if resp["language"] != "go" {
		t.Errorf("language=%v, want go", resp["language"])
	}
}

func TestCodePrinterPrint_BadJSON(t *testing.T) {
	t.Parallel()
	ctx := &Context{}
	req := httptest.NewRequest("POST", "/api/code-printer/print", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.CodePrinterPrint(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCodePrinterPreview_CountsLines(t *testing.T) {
	t.Parallel()
	ctx := &Context{}
	rec := postJSON(t, ctx.CodePrinterPreview, "/api/code-printer/preview", map[string]interface{}{
		"code": "only one line", "language": "python",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	resp := decodeMap(t, rec)
	if resp["lines"] != float64(1) {
		t.Errorf("lines=%v, want 1", resp["lines"])
	}
}

func TestCodePrinterConfig_DefaultsAndOverride(t *testing.T) {
	t.Parallel()
	// Defaults when no codePrinter setting.
	ctx := &Context{StateStore: state.NewStore(filepath.Join(t.TempDir(), "s.json"))}
	resp := getJSON(t, ctx.CodePrinterConfig, "/api/code-printer/config")
	langs, _ := resp["languages"].([]interface{})
	if len(langs) == 0 {
		t.Error("expected default languages")
	}
	themes, _ := resp["themes"].([]interface{})
	if len(themes) == 0 {
		t.Error("expected default themes")
	}

	// Override with a custom languages/themes list.
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"codePrinter": map[string]interface{}{
			"languages": []interface{}{"go"},
			"themes":    []interface{}{"dark"},
		},
	})
	resp2 := getJSON(t, ctx.CodePrinterConfig, "/api/code-printer/config")
	langs2, _ := resp2["languages"].([]interface{})
	if len(langs2) != 1 || langs2[0] != "go" {
		t.Errorf("override languages=%v, want [go]", langs2)
	}
	themes2, _ := resp2["themes"].([]interface{})
	if len(themes2) != 1 || themes2[0] != "dark" {
		t.Errorf("override themes=%v, want [dark]", themes2)
	}
}

// --- GetCaseFixPlan ---

func TestGetCaseFixPlan_CaseNotFound(t *testing.T) {
	t.Parallel()
	ctx := &Context{CaseManager: cases.NewCaseManager(t.TempDir())}
	req := withURLParam(httptest.NewRequest("GET", "/api/cases/ghost/fix-plan", nil), "name", "ghost")
	rec := httptest.NewRecorder()
	ctx.GetCaseFixPlan(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (exists:false), got %d", rec.Code)
	}
	resp := decodeMap(t, rec)
	if resp["exists"] != false {
		t.Errorf("exists=%v, want false for unknown case", resp["exists"])
	}
}

func TestGetCaseFixPlan_ParsesTodos(t *testing.T) {
	t.Parallel()
	cm := cases.NewCaseManager(t.TempDir())
	info, err := cm.Create("fixme")
	if err != nil {
		t.Fatalf("create case: %v", err)
	}
	plan := "# Fix Plan\n\n- [ ] pending task\n- [x] done task\n- [-] in progress task\n"
	if err := os.WriteFile(filepath.Join(info.Path, "@fix_plan.md"), []byte(plan), 0o644); err != nil {
		t.Fatalf("write fix plan: %v", err)
	}

	ctx := &Context{CaseManager: cm}
	req := withURLParam(httptest.NewRequest("GET", "/api/cases/fixme/fix-plan", nil), "name", "fixme")
	rec := httptest.NewRecorder()
	ctx.GetCaseFixPlan(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	resp := decodeMap(t, rec)
	if resp["exists"] != true {
		t.Fatalf("exists=%v, want true", resp["exists"])
	}
	todos, _ := resp["todos"].([]interface{})
	if len(todos) != 3 {
		t.Fatalf("expected 3 todos, got %d", len(todos))
	}
	// Verify status parsing across the three checkbox markers.
	statuses := map[string]bool{}
	for _, raw := range todos {
		td, _ := raw.(map[string]interface{})
		statuses[td["status"].(string)] = true
	}
	for _, want := range []string{"pending", "completed", "in_progress"} {
		if !statuses[want] {
			t.Errorf("missing todo status %q (got %v)", want, statuses)
		}
	}
}

// --- LinkCase ---

func TestLinkCase_Validation(t *testing.T) {
	t.Parallel()
	ctx := &Context{
		CaseManager: cases.NewCaseManager(t.TempDir()),
		SSEBroker:   sse.NewBroker(100, 10),
	}

	// Missing name.
	rec := postJSON(t, ctx.LinkCase, "/api/cases/link", map[string]interface{}{"path": "/tmp"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing name: expected 400, got %d", rec.Code)
	}

	// Invalid name (traversal).
	rec2 := postJSON(t, ctx.LinkCase, "/api/cases/link", map[string]interface{}{"name": "../evil", "path": "/tmp"})
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("bad name: expected 400, got %d", rec2.Code)
	}

	// Missing path.
	rec3 := postJSON(t, ctx.LinkCase, "/api/cases/link", map[string]interface{}{"name": "good"})
	if rec3.Code != http.StatusBadRequest {
		t.Fatalf("missing path: expected 400, got %d", rec3.Code)
	}

	// Bad JSON.
	reqBad := httptest.NewRequest("POST", "/api/cases/link", strings.NewReader("{bad"))
	reqBad.Header.Set("Content-Type", "application/json")
	recBad := httptest.NewRecorder()
	ctx.LinkCase(recBad, reqBad)
	if recBad.Code != http.StatusBadRequest {
		t.Fatalf("bad json: expected 400, got %d", recBad.Code)
	}
}

func TestLinkCase_HappyLinksExistingDir(t *testing.T) {
	t.Parallel()
	ctx := &Context{
		CaseManager: cases.NewCaseManager(t.TempDir()),
		SSEBroker:   sse.NewBroker(100, 10),
	}
	target := t.TempDir()
	rec := postJSON(t, ctx.LinkCase, "/api/cases/link", map[string]interface{}{
		"name": "linked-case", "path": target,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	c, _ := resp["case"].(map[string]interface{})
	if c["name"] != "linked-case" {
		t.Errorf("case.name=%v, want linked-case", c["name"])
	}
}

// --- GeneratePlanDetailed validation (no subprocess) ---

func TestGeneratePlanDetailed_MissingDescription400(t *testing.T) {
	t.Parallel()
	ctx := newOneShotCtx(t)
	rec := postJSON(t, ctx.GeneratePlanDetailed, "/api/generate-plan-detailed", map[string]interface{}{
		"taskDescription": "   ",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for blank description, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGeneratePlanDetailed_TooLong400(t *testing.T) {
	t.Parallel()
	ctx := newOneShotCtx(t)
	rec := postJSON(t, ctx.GeneratePlanDetailed, "/api/generate-plan-detailed", map[string]interface{}{
		"taskDescription": strings.Repeat("a", 10001),
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for over-long description, got %d", rec.Code)
	}
}

func TestGeneratePlanDetailed_BadJSON(t *testing.T) {
	t.Parallel()
	ctx := newOneShotCtx(t)
	req := httptest.NewRequest("POST", "/api/generate-plan-detailed", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.GeneratePlanDetailed(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}
