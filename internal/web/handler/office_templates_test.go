package handler

// Coverage for the team-template CRUD handlers in office.go. These need a
// real TeamTemplateStore (temp-dir backed) plus the OfficeManager from
// newTimelineContext. Built-in templates are immutable, so we assert the
// 403 guards as well as the custom-template happy paths.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kshaham/jarvis/internal/teamtemplates"
)

func newTemplateCtx(t *testing.T) *Context {
	t.Helper()
	ctx := newTimelineContext(t)
	ctx.TeamTemplateStore = teamtemplates.NewStore(t.TempDir())
	return ctx
}

func sampleTemplate(name string) teamtemplates.TeamTemplate {
	return teamtemplates.TeamTemplate{
		Name:        name,
		Description: "desc",
		Category:    "custom",
		Members: []teamtemplates.TeamTemplateMember{
			{ID: "m1", Name: "Vex", Role: "scout", Tier: "worker"},
		},
	}
}

func TestOfficeTemplates_ListsBuiltInsWithMemberCount(t *testing.T) {
	ctx := newTemplateCtx(t)
	resp := getJSON(t, ctx.OfficeTemplates, "/api/office/templates")
	templates, _ := resp["templates"].([]interface{})
	if len(templates) == 0 {
		t.Fatal("expected at least the built-in templates")
	}
	first := templates[0].(map[string]interface{})
	if _, ok := first["memberCount"]; !ok {
		t.Error("expected memberCount field on each template")
	}
}

func TestOfficeCreateTeamTemplate_Happy(t *testing.T) {
	ctx := newTemplateCtx(t)
	rec := postJSON(t, ctx.OfficeCreateTeamTemplate, "/api/office/templates", sampleTemplate("squad-a"))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var created teamtemplates.TeamTemplate
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.ID == "" || created.Name != "squad-a" {
		t.Errorf("unexpected created template: %+v", created)
	}
	if len(ctx.TeamTemplateStore.List()) != 1 {
		t.Errorf("store should hold 1 custom template, has %d", len(ctx.TeamTemplateStore.List()))
	}
}

func TestOfficeCreateTeamTemplate_MissingName(t *testing.T) {
	ctx := newTemplateCtx(t)
	tpl := sampleTemplate("")
	rec := postJSON(t, ctx.OfficeCreateTeamTemplate, "/api/office/templates", tpl)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing name, got %d", rec.Code)
	}
}

func TestOfficeCreateTeamTemplate_NoMembers(t *testing.T) {
	ctx := newTemplateCtx(t)
	tpl := sampleTemplate("squad-b")
	tpl.Members = nil
	rec := postJSON(t, ctx.OfficeCreateTeamTemplate, "/api/office/templates", tpl)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for no members, got %d", rec.Code)
	}
}

func TestOfficeUpdateTeamTemplate_BuiltInForbidden(t *testing.T) {
	ctx := newTemplateCtx(t)
	builtInID := teamtemplates.BuiltInTemplates()[0].ID
	req := withURLParam(httptest.NewRequest("PUT", "/api/office/templates/"+builtInID, jsonBody(t, sampleTemplate("x"))), "id", builtInID)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.OfficeUpdateTeamTemplate(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 editing built-in, got %d", rec.Code)
	}
}

func TestOfficeUpdateTeamTemplate_HappyAndNotFound(t *testing.T) {
	ctx := newTemplateCtx(t)
	created := ctx.TeamTemplateStore.Create(sampleTemplate("squad-c"))

	// Happy update.
	upd := sampleTemplate("squad-c-renamed")
	req := withURLParam(httptest.NewRequest("PUT", "/api/office/templates/"+created.ID, jsonBody(t, upd)), "id", created.ID)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.OfficeUpdateTeamTemplate(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Not found (a fresh UUID that isn't built-in and isn't stored).
	req2 := withURLParam(httptest.NewRequest("PUT", "/api/office/templates/ghost-id", jsonBody(t, upd)), "id", "ghost-id")
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	ctx.OfficeUpdateTeamTemplate(rec2, req2)
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec2.Code)
	}
}

func TestOfficeDeleteTeamTemplate_BuiltInForbidden(t *testing.T) {
	ctx := newTemplateCtx(t)
	builtInID := teamtemplates.BuiltInTemplates()[0].ID
	req := withURLParam(httptest.NewRequest("DELETE", "/api/office/templates/"+builtInID, nil), "id", builtInID)
	rec := httptest.NewRecorder()
	ctx.OfficeDeleteTeamTemplate(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 deleting built-in, got %d", rec.Code)
	}
}

func TestOfficeDeleteTeamTemplate_HappyAndNotFound(t *testing.T) {
	ctx := newTemplateCtx(t)
	created := ctx.TeamTemplateStore.Create(sampleTemplate("squad-d"))

	req := withURLParam(httptest.NewRequest("DELETE", "/api/office/templates/"+created.ID, nil), "id", created.ID)
	rec := httptest.NewRecorder()
	ctx.OfficeDeleteTeamTemplate(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Delete again -> 404.
	req2 := withURLParam(httptest.NewRequest("DELETE", "/api/office/templates/"+created.ID, nil), "id", created.ID)
	rec2 := httptest.NewRecorder()
	ctx.OfficeDeleteTeamTemplate(rec2, req2)
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on second delete, got %d", rec2.Code)
	}
}

func TestOfficeDuplicateTeamTemplate_BuiltIn(t *testing.T) {
	ctx := newTemplateCtx(t)
	builtInID := teamtemplates.BuiltInTemplates()[0].ID
	req := withURLParam(httptest.NewRequest("POST", "/api/office/templates/"+builtInID+"/duplicate", nil), "id", builtInID)
	rec := httptest.NewRecorder()
	ctx.OfficeDuplicateTeamTemplate(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 duplicating built-in, got %d: %s", rec.Code, rec.Body.String())
	}
	var dup teamtemplates.TeamTemplate
	_ = json.Unmarshal(rec.Body.Bytes(), &dup)
	if dup.ID == "" || dup.ID == builtInID {
		t.Errorf("duplicate should have a fresh ID, got %q", dup.ID)
	}
}

func TestOfficeDuplicateTeamTemplate_NotFound(t *testing.T) {
	ctx := newTemplateCtx(t)
	req := withURLParam(httptest.NewRequest("POST", "/api/office/templates/ghost/duplicate", nil), "id", "ghost")
	rec := httptest.NewRecorder()
	ctx.OfficeDuplicateTeamTemplate(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestOfficeTemplateFromTeam_NoTeamSpawned(t *testing.T) {
	ctx := newTemplateCtx(t)
	// teamSpawned defaults false -> 400.
	rec := postJSON(t, ctx.OfficeTemplateFromTeam, "/api/office/templates/from-team", map[string]interface{}{
		"name": "snapshot-team",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when no team spawned, got %d", rec.Code)
	}
}

func TestOfficeTemplateFromTeam_MissingName(t *testing.T) {
	ctx := newTemplateCtx(t)
	ctx.OfficeManager.SetTeamSpawned(true)
	rec := postJSON(t, ctx.OfficeTemplateFromTeam, "/api/office/templates/from-team", map[string]interface{}{})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing name, got %d", rec.Code)
	}
}
