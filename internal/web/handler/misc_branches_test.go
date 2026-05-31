package handler

// Small branch fills for misc.go case + code-printer handlers.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kshaham/jarvis/internal/cases"
	"github.com/kshaham/jarvis/internal/web/sse"
)

func TestUpdateCase_BadJSON(t *testing.T) {
	ctx := &Context{CaseManager: cases.NewCaseManager(t.TempDir())}
	if _, err := ctx.CaseManager.Create("upd"); err != nil {
		t.Fatalf("seed case: %v", err)
	}
	req := withURLParam(httptest.NewRequest("PUT", "/api/cases/upd", strings.NewReader("{bad")), "name", "upd")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.UpdateCase(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed body, got %d", rec.Code)
	}
}

func TestCodePrinterPreview_BadJSON(t *testing.T) {
	ctx := &Context{}
	req := httptest.NewRequest("POST", "/api/code-printer/preview", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.CodePrinterPreview(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestLinkCase_HomeExpansion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ctx := &Context{
		CaseManager: cases.NewCaseManager(t.TempDir()),
		SSEBroker:   sse.NewBroker(100, 10),
	}
	// Path with a ~ prefix gets expanded against HOME; point it at a real
	// subdir so the link succeeds.
	rec := postJSON(t, ctx.LinkCase, "/api/cases/link", map[string]interface{}{
		"name": "home-linked", "path": "~",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("link with ~ path: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}
