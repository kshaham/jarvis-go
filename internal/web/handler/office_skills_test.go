package handler

// Coverage for the agent skills endpoints in office.go. newTimelineContext
// isolates HOME, so ~/.claude/skills lands under the temp dir and the
// create/list/get/delete cycle stays hermetic.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOfficeAgentGetSkills_EmptyWhenNoDir(t *testing.T) {
	ctx := newTimelineContext(t)
	req := httptest.NewRequest("GET", "/api/office/agent/skills", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeAgentGetSkills(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	skills, _ := resp["skills"].([]interface{})
	if len(skills) != 0 {
		t.Errorf("expected empty skills, got %d", len(skills))
	}
}

func TestOfficeAgentSkill_CreateListGetDelete(t *testing.T) {
	ctx := newTimelineContext(t)

	// Create.
	rec := postJSON(t, ctx.OfficeAgentSkillCreate, "/api/office/agent/skills", map[string]interface{}{
		"skillName": "my-skill",
		"content":   "---\ndescription: test skill\n---\nbody",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var created map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created["action"] != "created" {
		t.Errorf("action=%v, want created", created["action"])
	}

	// Re-create the same name -> "updated".
	rec2 := postJSON(t, ctx.OfficeAgentSkillCreate, "/api/office/agent/skills", map[string]interface{}{
		"skillName": "my-skill",
		"content":   "---\ndescription: v2\n---\nbody2",
	})
	var updated map[string]interface{}
	_ = json.Unmarshal(rec2.Body.Bytes(), &updated)
	if updated["action"] != "updated" {
		t.Errorf("action=%v, want updated", updated["action"])
	}

	// List now shows it.
	reqList := httptest.NewRequest("GET", "/api/office/agent/skills", nil)
	recList := httptest.NewRecorder()
	ctx.OfficeAgentGetSkills(recList, reqList)
	var listed map[string]interface{}
	_ = json.Unmarshal(recList.Body.Bytes(), &listed)
	skills, _ := listed["skills"].([]interface{})
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}

	// Get by name.
	reqGet := withURLParam(httptest.NewRequest("GET", "/api/office/agent/skills/my-skill", nil), "name", "my-skill")
	recGet := httptest.NewRecorder()
	ctx.OfficeAgentGetSkillByName(recGet, reqGet)
	if recGet.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d", recGet.Code)
	}
	var got map[string]interface{}
	_ = json.Unmarshal(recGet.Body.Bytes(), &got)
	if !strings.Contains(got["content"].(string), "v2") {
		t.Errorf("expected updated content, got %v", got["content"])
	}

	// Delete.
	reqDel := withURLParam(httptest.NewRequest("DELETE", "/api/office/agent/skills/my-skill", nil), "name", "my-skill")
	recDel := httptest.NewRecorder()
	ctx.OfficeAgentDeleteSkill(recDel, reqDel)
	if recDel.Code != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d", recDel.Code)
	}
}

func TestOfficeAgentSkillCreate_MissingFields(t *testing.T) {
	ctx := newTimelineContext(t)
	rec := postJSON(t, ctx.OfficeAgentSkillCreate, "/api/office/agent/skills", map[string]interface{}{
		"skillName": "x",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing content, got %d", rec.Code)
	}
}

func TestOfficeAgentSkillCreate_InvalidName(t *testing.T) {
	ctx := newTimelineContext(t)
	rec := postJSON(t, ctx.OfficeAgentSkillCreate, "/api/office/agent/skills", map[string]interface{}{
		"skillName": "bad/../name",
		"content":   "x",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid name, got %d", rec.Code)
	}
}

func TestOfficeAgentGetSkillByName_InvalidName(t *testing.T) {
	ctx := newTimelineContext(t)
	req := withURLParam(httptest.NewRequest("GET", "/api/office/agent/skills/bad..name", nil), "name", "bad..name")
	rec := httptest.NewRecorder()
	ctx.OfficeAgentGetSkillByName(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid name, got %d", rec.Code)
	}
}

func TestOfficeAgentGetSkillByName_NotFound(t *testing.T) {
	ctx := newTimelineContext(t)
	req := withURLParam(httptest.NewRequest("GET", "/api/office/agent/skills/ghost", nil), "name", "ghost")
	rec := httptest.NewRecorder()
	ctx.OfficeAgentGetSkillByName(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing skill, got %d", rec.Code)
	}
}

func TestOfficeAgentSearchSkills_FiltersByQuery(t *testing.T) {
	ctx := newTimelineContext(t)
	// Seed two skills.
	postJSON(t, ctx.OfficeAgentSkillCreate, "/api/office/agent/skills", map[string]interface{}{
		"skillName": "alpha-tool", "content": "---\ndescription: a\n---\n",
	})
	postJSON(t, ctx.OfficeAgentSkillCreate, "/api/office/agent/skills", map[string]interface{}{
		"skillName": "beta-tool", "content": "---\ndescription: b\n---\n",
	})

	req := httptest.NewRequest("GET", "/api/office/agent/skills/search?q=alpha", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeAgentSearchSkills(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	skills, _ := resp["skills"].([]interface{})
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill matching 'alpha', got %d", len(skills))
	}
	first := skills[0].(map[string]interface{})
	if first["name"] != "alpha-tool" {
		t.Errorf("name=%v, want alpha-tool", first["name"])
	}
}

func TestOfficeAgentDeleteSkill_InvalidName(t *testing.T) {
	ctx := newTimelineContext(t)
	req := withURLParam(httptest.NewRequest("DELETE", "/api/office/agent/skills/bad..name", nil), "name", "bad..name")
	rec := httptest.NewRecorder()
	ctx.OfficeAgentDeleteSkill(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid name, got %d", rec.Code)
	}
}
