package handler

// Round-2 batch: remaining clearly-reachable handler error/edge arms reached
// HONESTLY via fault injection (corrupt files, read-only dirs, malformed JSON,
// seeded fixtures, real stub-codex spawn).

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/library"
	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/peers"
	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/ralph"
	"github.com/kshaham/jarvis/internal/respawn"
	"github.com/kshaham/jarvis/internal/runsummary"
	"github.com/kshaham/jarvis/internal/state"
	"github.com/kshaham/jarvis/internal/teamtemplates"
	"github.com/kshaham/jarvis/internal/templates"
	"github.com/kshaham/jarvis/internal/web/sse"
)

// GetKnowledgePromotions: ledger.List() error arm via a corrupt ledger file.
func TestGetKnowledgePromotions_ListError500(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	libDir := t.TempDir()
	restore := library.SetDirForTest(libDir)
	t.Cleanup(restore)
	// Write a corrupt ledger file so Ledger.load()/List() fails to parse.
	if err := os.WriteFile(filepath.Join(libDir, ".promotion-ledger.json"), []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("seed corrupt ledger: %v", err)
	}
	ctx := newTimelineContext(t)
	rec := httptest.NewRecorder()
	ctx.GetKnowledgePromotions(rec, httptest.NewRequest("GET", "/api/knowledge/promotions", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when ledger List fails, got %d: %s", rec.Code, rec.Body.String())
	}
}

// OfficeAgentSkillCreate: malformed JSON -> 400.
func TestOfficeAgentSkillCreate_BadJSON400(t *testing.T) {
	ctx := newTimelineContext(t)
	req := httptest.NewRequest("POST", "/api/office/agent/skills", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.OfficeAgentSkillCreate(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on malformed JSON, got %d: %s", rec.Code, rec.Body.String())
	}
}

// OfficeAgentSkillCreate: WriteSharedSkill IO failure -> 500. Force it by making
// the skills root read-only so MkdirAll of the per-skill dir fails.
func TestOfficeAgentSkillCreate_WriteError500(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory write permissions")
	}
	ctx := newTimelineContext(t)
	home, _ := os.UserHomeDir()
	skillsRoot := filepath.Join(home, ".claude", "skills")
	if err := os.MkdirAll(skillsRoot, 0o755); err != nil {
		t.Fatalf("mkdir skills root: %v", err)
	}
	if err := os.Chmod(skillsRoot, 0o555); err != nil {
		t.Fatalf("chmod skills root: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(skillsRoot, 0o755) })

	rec := postJSON(t, ctx.OfficeAgentSkillCreate, "/api/office/agent/skills", map[string]interface{}{
		"skillName": "newskill",
		"content":   "body",
	})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when WriteSharedSkill fails, got %d: %s", rec.Code, rec.Body.String())
	}
}

// OfficeAgentSearchSkills: entries exist but none match -> skills stays nil ->
// normalized to []. Drives the nil-normalization branch.
func TestOfficeAgentSearchSkills_NilNormalizedToEmpty(t *testing.T) {
	ctx := newTimelineContext(t)
	home, _ := os.UserHomeDir()
	skillsRoot := filepath.Join(home, ".claude", "skills")
	// One real skill dir, but the query won't match it.
	if err := os.MkdirAll(filepath.Join(skillsRoot, "alpha-skill"), 0o755); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	// Also a non-dir entry to exercise the !IsDir skip.
	if err := os.WriteFile(filepath.Join(skillsRoot, "loose.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write loose file: %v", err)
	}
	req := httptest.NewRequest("GET", "/api/office/agent/skills/search?q=zzz-no-match", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeAgentSearchSkills(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"skills":[]`) {
		t.Errorf("expected empty skills array, got %s", rec.Body.String())
	}
}

// OfficeGetTaskScreenshot: valid filename but missing file -> 404.
func TestOfficeGetTaskScreenshot_NotFound404(t *testing.T) {
	ctx := newTimelineContext(t)
	req := withURLParam(httptest.NewRequest("GET", "/api/office/task-screenshots/task1-1700000000.png", nil),
		"filename", "task1-1700000000.png")
	rec := httptest.NewRecorder()
	ctx.OfficeGetTaskScreenshot(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing screenshot, got %d: %s", rec.Code, rec.Body.String())
	}
}

// OfficeGetTaskScreenshot: serves jpg/jpeg/webp with the right MIME types.
func TestOfficeGetTaskScreenshot_MimeTypes(t *testing.T) {
	ctx := newTimelineContext(t)
	dir := office.TaskScreenshotsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir screenshots: %v", err)
	}
	cases := []struct {
		filename string
		wantMime string
	}{
		{"shotj-1700000001.jpg", "image/jpeg"},
		{"shote-1700000002.jpeg", "image/jpeg"},
		{"shotw-1700000003.webp", "image/webp"},
	}
	for _, c := range cases {
		if err := os.WriteFile(filepath.Join(dir, c.filename), []byte("imgbytes"), 0o644); err != nil {
			t.Fatalf("seed %s: %v", c.filename, err)
		}
		req := withURLParam(httptest.NewRequest("GET", "/api/office/task-screenshots/"+c.filename, nil),
			"filename", c.filename)
		rec := httptest.NewRecorder()
		ctx.OfficeGetTaskScreenshot(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d: %s", c.filename, rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Content-Type"); got != c.wantMime {
			t.Errorf("%s: Content-Type=%q, want %q", c.filename, got, c.wantMime)
		}
	}
}

// ImportTemplate: malformed JSON -> 400.
func TestImportTemplate_BadJSON400(t *testing.T) {
	t.Parallel()
	ctx := &Context{TemplateStore: templates.NewStore(t.TempDir())}
	req := httptest.NewRequest("POST", "/api/templates/import", strings.NewReader("{not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.ImportTemplate(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on malformed JSON, got %d: %s", rec.Code, rec.Body.String())
	}
}

// PeersDelete: Registry.Remove persist failure -> 500. Parent dir of the
// registry file is read-only so saveLocked's MkdirAll fails.
func TestPeersDelete_RegistryRemoveError500(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory write permissions")
	}
	dir := t.TempDir()
	roParent := filepath.Join(dir, "ro")
	if err := os.MkdirAll(roParent, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	regPath := filepath.Join(roParent, "sub", "peers.json")
	reg, err := peers.NewRegistry(regPath)
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	// Make the parent read-only AFTER construction so saveLocked's MkdirAll of
	// the missing "sub" dir fails.
	if err := os.Chmod(roParent, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(roParent, 0o755) })
	p := &Peers{Registry: reg}

	req := withURLParam(httptest.NewRequest("DELETE", "/api/peers/p1", nil), "peerID", "p1")
	rec := httptest.NewRecorder()
	p.PeersDelete(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when registry persist fails, got %d: %s", rec.Code, rec.Body.String())
	}
}

// OfficeShutdownTeam: the Codex-session teardown branch. Register a live Codex
// session (real stub `codex` binary on PATH) under an agent, then shut down.
func TestOfficeShutdownTeam_StopsLiveCodexAgent(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"thread.started","thread_id":"th-shut"}`,
		`{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}`,
		"",
	}, "\n")
	installCodexOnPATH(t, stream)

	t.Setenv("HOME", t.TempDir())
	ctx := &Context{
		SessionManager:          pty.NewSessionManager(20),
		SSEBroker:               sse.NewBroker(100, 10),
		StateStore:              state.NewStore(filepath.Join(t.TempDir(), "state.json")),
		OfficeManager:           office.NewManager(stubSessions{}, func(string, interface{}) {}, nil),
		RalphTrackers:           map[string]*ralph.Tracker{},
		RunSummaries:            map[string]*runsummary.Tracker{},
		RespawnControllers:      map[string]*respawn.Controller{},
		SessionColors:           map[string]string{},
		ActivePlanOrchestrators: map[string]string{},
	}

	member := teamtemplates.TeamTemplateMember{ID: "cod", Name: "Codey", Tier: "worker"}
	req := spawnTeamRequest{WorkingDir: t.TempDir(), Mission: "ship"}
	sessionID, mode := ctx.spawnCodexAgentSession(member, req, map[string]string{"OPENAI_API_KEY": "sk-test"},
		nil, false, nil, "openai-custom", "gpt-5.5")
	if mode != "codex" || sessionID == "" {
		t.Fatalf("expected a codex session, got mode=%q id=%q", mode, sessionID)
	}
	ctx.initSessionTrackers(sessionID)
	ctx.OfficeManager.RegisterAgent(sessionID, "cod", &office.TeamMemberConfig{ID: "cod", Name: "Codey"})
	ctx.OfficeManager.SetTeamSpawned(true)

	rec := postJSON(t, ctx.OfficeShutdownTeam, "/api/office/shutdown", map[string]interface{}{})
	if rec.Code != http.StatusOK {
		t.Fatalf("shutdown: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	// The codex session should have been stopped by the teardown callback.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := ctx.SessionManager.GetCodexSession(sessionID); !ok {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, ok := ctx.SessionManager.GetCodexSession(sessionID); ok {
		_ = ctx.SessionManager.StopCodexSession(sessionID)
		t.Error("expected the codex session to be stopped by shutdown teardown")
	}
	if ctx.OfficeManager.IsTeamSpawned() {
		t.Error("team should be torn down after shutdown")
	}
}

// Guard against unused-import churn if any helper above is trimmed.
var _ = context.Background
