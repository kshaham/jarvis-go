package main

import (
	"net/http"
	"strings"
	"testing"
)

// ---- skill ----

func TestHandleSkill_NoArgs_Fatal(t *testing.T) {
	res := runCLI(t, func() { handleSkill(nil) })
	if !res.exited {
		t.Fatalf("expected fatal, got %+v", res)
	}
}

func TestHandleSkill_UnknownSub_Fatal(t *testing.T) {
	res := runCLI(t, func() { handleSkill([]string{"frobnicate"}) })
	if !res.exited || !strings.Contains(res.stderr, "Unknown skill subcommand") {
		t.Fatalf("got %+v", res)
	}
}

func TestHandleSkill_List(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{"skills":[]}`)
	res := runCLI(t, func() { handleSkill([]string{"list"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.path != "/api/office/agent/skills" {
		t.Errorf("path=%q", rec.path)
	}
}

func TestHandleSkill_Read(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{"content":"x"}`)
	res := runCLI(t, func() { handleSkill([]string{"read", "office-ask-for-help"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if !strings.Contains(rec.path, "/api/office/agent/skills/office-ask-for-help") {
		t.Errorf("path=%q", rec.path)
	}
}

func TestHandleSkill_ReadMissingName_Fatal(t *testing.T) {
	res := runCLI(t, func() { handleSkill([]string{"read"}) })
	if !res.exited {
		t.Fatalf("expected fatal, got %+v", res)
	}
}

func TestHandleSkill_Create(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{}`)
	res := runCLI(t, func() { handleSkill([]string{"create", "my-skill", "body", "text"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.path != "/api/office/agent/skill-create" || rec.body["skillName"] != "my-skill" {
		t.Errorf("path=%q body=%#v", rec.path, rec.body)
	}
}

func TestHandleSkill_Search(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{"hits":[]}`)
	res := runCLI(t, func() { handleSkill([]string{"search", "browser"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if !strings.Contains(rec.path, "/api/office/agent/skills/search?q=browser") {
		t.Errorf("path=%q", rec.path)
	}
}

// ---- project invariants ----

func TestHandleProject_NoArgs_Fatal(t *testing.T) {
	res := runCLI(t, func() { handleProject(nil) })
	if !res.exited {
		t.Fatalf("expected fatal, got %+v", res)
	}
}

func TestHandleProject_UnknownSub_Fatal(t *testing.T) {
	res := runCLI(t, func() { handleProject([]string{"frobnicate"}) })
	if !res.exited || !strings.Contains(res.stderr, "Unknown project subcommand") {
		t.Fatalf("got %+v", res)
	}
}

func TestHandleProjectInvariants_ListDefault(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{"invariants":["never force push","tests must pass"]}`)
	res := runCLI(t, func() { handleProject([]string{"invariants"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.method != http.MethodGet || rec.path != "/api/office/project/invariants" {
		t.Errorf("method=%q path=%q", rec.method, rec.path)
	}
	if !strings.Contains(res.stdout, "0. never force push") {
		t.Errorf("stdout=%q", res.stdout)
	}
}

func TestHandleProjectInvariants_EmptyList(t *testing.T) {
	newServer(t, http.StatusOK, `{"invariants":[]}`)
	res := runCLI(t, func() { handleProject([]string{"invariants", "list"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if !strings.Contains(res.stdout, "no invariants") {
		t.Errorf("stdout=%q", res.stdout)
	}
}

func TestHandleProjectInvariants_Add(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{"invariants":["r1"]}`)
	res := runCLI(t, func() { handleProject([]string{"invariants", "add", "no", "secrets", "in", "code"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.method != http.MethodPost || rec.body["text"] != "no secrets in code" {
		t.Errorf("method=%q body=%#v", rec.method, rec.body)
	}
}

func TestHandleProjectInvariants_AddMissing_Fatal(t *testing.T) {
	res := runCLI(t, func() { handleProject([]string{"invariants", "add"}) })
	if !res.exited {
		t.Fatalf("expected fatal, got %+v", res)
	}
}

func TestHandleProjectInvariants_Remove(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{"invariants":[]}`)
	res := runCLI(t, func() { handleProject([]string{"invariants", "remove", "2"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.method != http.MethodDelete || !strings.HasSuffix(rec.path, "/invariants/2") {
		t.Errorf("method=%q path=%q", rec.method, rec.path)
	}
}

func TestHandleProjectInvariants_RemoveBadIndex_Fatal(t *testing.T) {
	res := runCLI(t, func() { handleProject([]string{"invariants", "remove", "-1"}) })
	if !res.exited {
		t.Fatalf("expected fatal, got %+v", res)
	}
}

func TestHandleProjectInvariants_Set(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{"invariants":["a","b"]}`)
	res := runCLI(t, func() { handleProject([]string{"invariants", "set", "a", "b"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.method != http.MethodPut {
		t.Errorf("method=%q", rec.method)
	}
}

func TestHandleProjectInvariants_BadSub_Fatal(t *testing.T) {
	res := runCLI(t, func() { handleProject([]string{"invariants", "frobnicate"}) })
	if !res.exited {
		t.Fatalf("expected fatal, got %+v", res)
	}
}
