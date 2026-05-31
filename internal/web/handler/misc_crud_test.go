package handler

// Coverage for the store-backed CRUD handlers in misc.go: prompt
// templates and cases. Both are file-backed stores we can build against a
// temp dir, so the full create/list/get/update/delete cycle is hermetic.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kshaham/jarvis/internal/cases"
	"github.com/kshaham/jarvis/internal/templates"
)

func newMiscCtx(t *testing.T) *Context {
	t.Helper()
	return &Context{
		TemplateStore: templates.NewStore(t.TempDir()),
		CaseManager:   cases.NewCaseManager(t.TempDir()),
	}
}

// --- Templates ---

func TestListTemplates_Empty(t *testing.T) {
	ctx := newMiscCtx(t)
	resp := getJSON(t, ctx.ListTemplates, "/api/templates")
	if resp["success"] != true {
		t.Errorf("expected success=true, got %v", resp["success"])
	}
}

func TestCreateTemplate_Happy(t *testing.T) {
	ctx := newMiscCtx(t)
	rec := postJSON(t, ctx.CreateTemplate, "/api/templates", map[string]interface{}{
		"name":        "review",
		"description": "code review prompt",
		"content":     "Review this code",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(ctx.TemplateStore.List()) != 1 {
		t.Errorf("expected 1 template, got %d", len(ctx.TemplateStore.List()))
	}
}

func TestCreateTemplate_MissingName(t *testing.T) {
	ctx := newMiscCtx(t)
	rec := postJSON(t, ctx.CreateTemplate, "/api/templates", map[string]interface{}{
		"description": "no name",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestGetTemplate_FoundAndNotFound(t *testing.T) {
	ctx := newMiscCtx(t)
	created := ctx.TemplateStore.Create("t1", "d", "c")

	req := withURLParam(httptest.NewRequest("GET", "/api/templates/"+created.ID, nil), "id", created.ID)
	rec := httptest.NewRecorder()
	ctx.GetTemplate(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	req2 := withURLParam(httptest.NewRequest("GET", "/api/templates/ghost", nil), "id", "ghost")
	rec2 := httptest.NewRecorder()
	ctx.GetTemplate(rec2, req2)
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec2.Code)
	}
}

func TestUpdateTemplate_HappyAndNotFound(t *testing.T) {
	ctx := newMiscCtx(t)
	created := ctx.TemplateStore.Create("t1", "d", "c")

	req := withURLParam(httptest.NewRequest("PUT", "/api/templates/"+created.ID, jsonBody(t, map[string]string{"name": "renamed"})), "id", created.ID)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.UpdateTemplate(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	req2 := withURLParam(httptest.NewRequest("PUT", "/api/templates/ghost", jsonBody(t, map[string]string{"name": "x"})), "id", "ghost")
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	ctx.UpdateTemplate(rec2, req2)
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec2.Code)
	}
}

func TestDeleteTemplate_HappyAndNotFound(t *testing.T) {
	ctx := newMiscCtx(t)
	created := ctx.TemplateStore.Create("t1", "d", "c")

	req := withURLParam(httptest.NewRequest("DELETE", "/api/templates/"+created.ID, nil), "id", created.ID)
	rec := httptest.NewRecorder()
	ctx.DeleteTemplate(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	req2 := withURLParam(httptest.NewRequest("DELETE", "/api/templates/"+created.ID, nil), "id", created.ID)
	rec2 := httptest.NewRecorder()
	ctx.DeleteTemplate(rec2, req2)
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on second delete, got %d", rec2.Code)
	}
}

func TestDuplicateTemplate_HappyAndNotFound(t *testing.T) {
	ctx := newMiscCtx(t)
	created := ctx.TemplateStore.Create("t1", "d", "c")

	req := withURLParam(httptest.NewRequest("POST", "/api/templates/"+created.ID+"/duplicate", nil), "id", created.ID)
	rec := httptest.NewRecorder()
	ctx.DuplicateTemplate(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if len(ctx.TemplateStore.List()) != 2 {
		t.Errorf("expected 2 templates after duplicate, got %d", len(ctx.TemplateStore.List()))
	}

	req2 := withURLParam(httptest.NewRequest("POST", "/api/templates/ghost/duplicate", nil), "id", "ghost")
	rec2 := httptest.NewRecorder()
	ctx.DuplicateTemplate(rec2, req2)
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec2.Code)
	}
}

func TestExportTemplate_FoundAndNotFound(t *testing.T) {
	ctx := newMiscCtx(t)
	created := ctx.TemplateStore.Create("t1", "d", "c")

	req := withURLParam(httptest.NewRequest("GET", "/api/templates/"+created.ID+"/export", nil), "id", created.ID)
	rec := httptest.NewRecorder()
	ctx.ExportTemplate(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if cd := rec.Header().Get("Content-Disposition"); cd == "" {
		t.Error("expected Content-Disposition header on export")
	}

	req2 := withURLParam(httptest.NewRequest("GET", "/api/templates/ghost/export", nil), "id", "ghost")
	rec2 := httptest.NewRecorder()
	ctx.ExportTemplate(rec2, req2)
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec2.Code)
	}
}

func TestImportTemplate_HappyAndMissingName(t *testing.T) {
	ctx := newMiscCtx(t)
	rec := postJSON(t, ctx.ImportTemplate, "/api/templates/import", map[string]interface{}{
		"name": "imported", "content": "x",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	rec2 := postJSON(t, ctx.ImportTemplate, "/api/templates/import", map[string]interface{}{
		"content": "no name",
	})
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing name, got %d", rec2.Code)
	}
}

// --- Cases ---

func TestListCases_Empty(t *testing.T) {
	ctx := newMiscCtx(t)
	resp := getJSON(t, ctx.ListCases, "/api/cases")
	if resp["success"] != true {
		t.Errorf("expected success=true, got %v", resp["success"])
	}
}

func TestCreateCase_HappyAndListGet(t *testing.T) {
	ctx := newMiscCtx(t)
	rec := postJSON(t, ctx.CreateCase, "/api/cases", map[string]interface{}{
		"name": "bug-123",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create case: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// List now has 1.
	resp := getJSON(t, ctx.ListCases, "/api/cases")
	cs, _ := resp["cases"].([]interface{})
	if len(cs) != 1 {
		t.Fatalf("expected 1 case, got %d", len(cs))
	}

	// Get by name.
	reqGet := withURLParam(httptest.NewRequest("GET", "/api/cases/bug-123", nil), "name", "bug-123")
	recGet := httptest.NewRecorder()
	ctx.GetCase(recGet, reqGet)
	if recGet.Code != http.StatusOK {
		t.Fatalf("get case: expected 200, got %d", recGet.Code)
	}
}

func TestCreateCase_MissingName(t *testing.T) {
	ctx := newMiscCtx(t)
	rec := postJSON(t, ctx.CreateCase, "/api/cases", map[string]interface{}{})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestGetCase_NotFound(t *testing.T) {
	ctx := newMiscCtx(t)
	req := withURLParam(httptest.NewRequest("GET", "/api/cases/ghost", nil), "name", "ghost")
	rec := httptest.NewRecorder()
	ctx.GetCase(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestUpdateCase_NotFound(t *testing.T) {
	ctx := newMiscCtx(t)
	req := withURLParam(httptest.NewRequest("PUT", "/api/cases/ghost", jsonBody(t, map[string]interface{}{"x": 1})), "name", "ghost")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.UpdateCase(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 updating unknown case, got %d", rec.Code)
	}
}

func TestUpdateCase_Happy(t *testing.T) {
	ctx := newMiscCtx(t)
	if _, err := ctx.CaseManager.Create("bug-9"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	req := withURLParam(httptest.NewRequest("PUT", "/api/cases/bug-9", jsonBody(t, map[string]interface{}{"title": "fix it"})), "name", "bug-9")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.UpdateCase(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDeleteCase_Happy(t *testing.T) {
	ctx := newMiscCtx(t)
	if _, err := ctx.CaseManager.Create("bug-del"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	req := withURLParam(httptest.NewRequest("DELETE", "/api/cases/bug-del", nil), "name", "bug-del")
	rec := httptest.NewRecorder()
	ctx.DeleteCase(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	resp := getJSON(t, ctx.ListCases, "/api/cases")
	cs, _ := resp["cases"].([]interface{})
	if len(cs) != 0 {
		t.Errorf("expected 0 cases after delete, got %d", len(cs))
	}
}

func TestCreateTemplate_InvalidJSON(t *testing.T) {
	ctx := newMiscCtx(t)
	req := httptest.NewRequest("POST", "/api/templates", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.CreateTemplate(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed JSON, got %d", rec.Code)
	}
}
