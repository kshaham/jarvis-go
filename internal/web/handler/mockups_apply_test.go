package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/kshaham/jarvis/internal/mockups"
	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/projects"
	"github.com/kshaham/jarvis/internal/state"
	"github.com/kshaham/jarvis/internal/web/sse"
)

// applyPNG is a minimal PNG-signature-bearing payload mockups.Save accepts.
var applyPNG = append([]byte{0x89}, []byte("PNG\r\n\x1a\n....")...)

// newApplyCtx builds a Context with a state-backed Projects + Mockups manager.
// HOME is redirected to a temp dir so mockup images and settings stay isolated.
func newApplyCtx(t *testing.T) *Context {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	store := state.NewStore(filepath.Join(t.TempDir(), "state.json"))
	broker := sse.NewBroker(100, 10)
	return &Context{
		StateStore:    store,
		Projects:      projects.New(store),
		Mockups:       mockups.NewManager(store, broker),
		SSEBroker:     broker,
		OfficeManager: office.NewManager(stubSessions{}, func(string, interface{}) {}, nil),
	}
}

// spawnTeamOn marks a team spawned on workdir and registers one lead agent.
func spawnTeamOn(ctx *Context, workdir string) {
	ctx.OfficeManager.RegisterAgent("s-lead", "lead", &office.TeamMemberConfig{ID: "lead", Name: "Lead"})
	ctx.OfficeManager.SetSpawnContext("tmpl", workdir)
	ctx.OfficeManager.SetTeamSpawned(true)
}

func postApply(t *testing.T, ctx *Context, body map[string]interface{}) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/mockups/apply", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.MockupsApply(rec, req)
	return rec
}

func TestMockupsApply_RejectsNonUIKind(t *testing.T) {
	ctx := newApplyCtx(t)
	rec := postApply(t, ctx, map[string]interface{}{"projectId": "x", "kind": "presentation", "index": 0})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-ui kind, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMockupsApply_ConflictWithoutTeam(t *testing.T) {
	ctx := newApplyCtx(t)
	dir := t.TempDir()
	p := ctx.Projects.Upsert(projects.Project{Kind: projects.KindLocal, Path: dir, LastUsedAt: "2026-05-31T10:00:00Z"})

	// No team spawned -> 409.
	rec := postApply(t, ctx, map[string]interface{}{"projectId": p.ID, "kind": "ui", "index": 0})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 without a team, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMockupsApply_HappyPath(t *testing.T) {
	ctx := newApplyCtx(t)
	dir := t.TempDir()
	p := ctx.Projects.Upsert(projects.Project{Kind: projects.KindLocal, Path: dir, LastUsedAt: "2026-05-31T10:00:00Z"})

	// Arrange: spawn a team whose workdir == project path, seed a slot-3 mockup.
	spawnTeamOn(ctx, dir)
	if _, err := mockups.Save(p.ID, mockups.KindUI, 3, applyPNG, 1700000000); err != nil {
		t.Fatalf("seed mockup: %v", err)
	}

	// Act.
	rec := postApply(t, ctx, map[string]interface{}{"projectId": p.ID, "kind": "ui", "index": 3})

	// Assert: 200, design-reference.png copied in, AppliedDesign recorded.
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if idx, _ := resp["appliedIndex"].(float64); int(idx) != 3 {
		t.Errorf("appliedIndex = %v, want 3", resp["appliedIndex"])
	}
	if _, err := os.Stat(filepath.Join(dir, "design-reference.png")); err != nil {
		t.Errorf("expected design-reference.png in project dir: %v", err)
	}
	if got, ok := ctx.Mockups.AppliedDesign(p.ID); !ok || got != 3 {
		t.Errorf("AppliedDesign = (%d, %v), want (3, true)", got, ok)
	}
}

func TestMockupsApply_NotFoundWhenSlotEmpty(t *testing.T) {
	ctx := newApplyCtx(t)
	dir := t.TempDir()
	p := ctx.Projects.Upsert(projects.Project{Kind: projects.KindLocal, Path: dir, LastUsedAt: "2026-05-31T10:00:00Z"})
	spawnTeamOn(ctx, dir)

	// No image at slot 1 -> 404 (the slot has no image to apply).
	rec := postApply(t, ctx, map[string]interface{}{"projectId": p.ID, "kind": "ui", "index": 1})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for empty slot, got %d: %s", rec.Code, rec.Body.String())
	}
}
