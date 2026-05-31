package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/office/spawnpayload"
	"github.com/kshaham/jarvis/internal/projects"
	"github.com/kshaham/jarvis/internal/state"
)

func newProjectsCtx(t *testing.T) *Context {
	t.Helper()
	store := state.NewStore(filepath.Join(t.TempDir(), "state.json"))
	return &Context{StateStore: store, Projects: projects.New(store)}
}

func TestListProjectsEmpty(t *testing.T) {
	t.Parallel()
	ctx := newProjectsCtx(t)
	req := httptest.NewRequest("GET", "/api/projects", nil)
	rec := httptest.NewRecorder()
	ctx.ListProjects(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestDeleteProject(t *testing.T) {
	t.Parallel()
	ctx := newProjectsCtx(t)
	p := ctx.Projects.Upsert(projects.Project{Kind: projects.KindLocal, Path: "/p/a", LastUsedAt: "2026-05-31T10:00:00Z"})
	req := httptest.NewRequest("DELETE", "/api/projects/"+p.ID, nil)
	rec := httptest.NewRecorder()
	ctx.deleteProjectWithID(rec, req, p.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if len(ctx.Projects.List()) != 0 {
		t.Fatal("expected project deleted")
	}
}

func TestResolveGithubRepoDir_InvalidRepo(t *testing.T) {
	t.Parallel()
	ctx := newProjectsCtx(t) // only StateStore + Projects are set
	_, err := ctx.resolveGithubRepoDir(spawnpayload.SpawnTeamRequest{GithubRepo: "../evil"})
	if !errors.Is(err, office.ErrInvalidRepo) {
		t.Fatalf("expected ErrInvalidRepo, got %v", err)
	}
}
