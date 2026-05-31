package handler

// Round-2 batch: more reachable handler branches via fixtures + fault injection.
//   - GetKnowledgeStats: promoted/revoked ledger counting loop.
//   - AuthSetup: setup-lock contention (409) + SetPassword IO failure (500).
//   - UpdateCase: os.WriteFile failure -> 500 (read-only case dir).
//   - listPersistedStandups: dir/non-json skip + corrupt-json skip, via OfficeTimeline.
//   - CheckpointSessionPlan: no-plan 404 + versioned-write 500.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kshaham/jarvis/internal/cases"
	"github.com/kshaham/jarvis/internal/library"
	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/state"
	"github.com/kshaham/jarvis/internal/web/auth"
	"github.com/kshaham/jarvis/internal/web/sse"
)

func TestGetKnowledgeStats_LedgerCounts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	libDir := t.TempDir()
	restore := library.SetDirForTest(libDir)
	t.Cleanup(restore)

	ledger, err := library.OpenLedger()
	if err != nil {
		t.Fatalf("OpenLedger: %v", err)
	}
	if ledger == nil {
		t.Fatal("expected a non-nil ledger with a valid dir")
	}
	// One promoted, one revoked entry so both counting branches run.
	if err := ledger.Increment("workflows/auth.md", 1.0); err != nil {
		t.Fatalf("Increment: %v", err)
	}
	if err := ledger.MarkPromoted("workflows/auth.md"); err != nil {
		t.Fatalf("MarkPromoted: %v", err)
	}
	if err := ledger.MarkRevoked("workflows/stale.md", "outdated"); err != nil {
		t.Fatalf("MarkRevoked: %v", err)
	}

	ctx := newTimelineContext(t)
	rec := httptest.NewRecorder()
	ctx.GetKnowledgeStats(rec, httptest.NewRequest("GET", "/api/knowledge/stats", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["ledgerPromoted"] != float64(1) {
		t.Errorf("ledgerPromoted=%v, want 1", resp["ledgerPromoted"])
	}
	if resp["ledgerRevoked"] != float64(1) {
		t.Errorf("ledgerRevoked=%v, want 1", resp["ledgerRevoked"])
	}
}

func newAuthCtxAt(t *testing.T, dir string) *Context {
	t.Helper()
	am, err := auth.NewManager(dir, false)
	if err != nil {
		t.Fatalf("auth manager: %v", err)
	}
	return &Context{
		AuthManager: am,
		StateStore:  state.NewStore(filepath.Join(t.TempDir(), "state.json")),
	}
}

func TestAuthSetup_SetupLockHeld409(t *testing.T) {
	ctx := newAuthCtxAt(t, t.TempDir())
	// Hold the setup lock so AuthSetup's AcquireSetupLock returns false -> 409.
	if !ctx.AuthManager.AcquireSetupLock() {
		t.Fatal("could not acquire setup lock for the precondition")
	}
	t.Cleanup(ctx.AuthManager.ReleaseSetupLock)

	rec := postJSON(t, ctx.AuthSetup, "/api/auth/setup", map[string]interface{}{"password": "strongpassword1"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 when setup lock is held, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAuthSetup_SetPasswordError500(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory write permissions")
	}
	dir := t.TempDir()
	ctx := newAuthCtxAt(t, dir)
	// Make the auth config dir read-only so SetPassword's atomic write fails.
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	rec := postJSON(t, ctx.AuthSetup, "/api/auth/setup", map[string]interface{}{"password": "strongpassword1"})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when SetPassword fails, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateCase_WriteFileError500(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory write permissions")
	}
	cm := cases.NewCaseManager(t.TempDir())
	info, err := cm.Create("bug-ro")
	if err != nil {
		t.Fatalf("create case: %v", err)
	}
	// Make the case dir read-only so writing config.json fails.
	if err := os.Chmod(info.Path, 0o555); err != nil {
		t.Fatalf("chmod case dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(info.Path, 0o755) })

	ctx := &Context{CaseManager: cm}
	req := withURLParam(httptest.NewRequest("PUT", "/api/cases/bug-ro",
		strings.NewReader(`{"title":"updated"}`)), "name", "bug-ro")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.UpdateCase(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when config.json write fails, got %d: %s", rec.Code, rec.Body.String())
	}
}

// listPersistedStandups: seed a dir entry, a non-json file, a corrupt json, and
// a valid standup. Drives the IsDir/non-json skip + Unmarshal-skip branches via
// OfficeTimeline (which calls collectTimelineEntries -> listPersistedStandups).
func TestListPersistedStandups_SkipsViaTimeline(t *testing.T) {
	ctx := newTimelineContext(t)
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".jarvis", "office-memory", "standups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir standups: %v", err)
	}
	// A subdirectory -> IsDir skip.
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}
	// A non-json file -> suffix skip.
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignore me"), 0o644); err != nil {
		t.Fatalf("write notes: %v", err)
	}
	// A corrupt json -> Unmarshal skip.
	if err := os.WriteFile(filepath.Join(dir, "corrupt.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}
	// A valid standup.
	valid := office.PersistedStandup{ID: "good", Participants: []string{"a"}, CompletedAt: 1000}
	vb, _ := json.Marshal(valid)
	if err := os.WriteFile(filepath.Join(dir, "good.json"), vb, 0o644); err != nil {
		t.Fatalf("write good: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/office/timeline?type=standup&limit=50", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeTimeline(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	// The valid standup should surface; the dir/non-json/corrupt entries skipped.
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	entries, _ := resp["entries"].([]interface{})
	if len(entries) == 0 {
		t.Error("expected the one valid standup to appear in the timeline")
	}
}

func checkpointCtx(t *testing.T) *Context {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	return &Context{
		SessionManager: pty.NewSessionManager(20),
		SSEBroker:      sse.NewBroker(100, 10),
		StateStore:     state.NewStore(filepath.Join(t.TempDir(), "state.json")),
		OfficeManager:  office.NewManager(stubSessions{}, func(string, interface{}) {}, nil),
	}
}

func TestCheckpointSessionPlan_NoPlan404(t *testing.T) {
	skipIfNoStubShell(t)
	ctx := checkpointCtx(t)
	sess, err := ctx.SessionManager.CreateSession(context.Background(), pty.SessionConfig{
		WorkingDir: t.TempDir(), Mode: pty.ModeShell, Name: "noplan",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	t.Cleanup(func() { _ = ctx.SessionManager.StopSession(context.Background(), sess.ID) })

	req := withURLParam(httptest.NewRequest("POST", "/api/sessions/"+sess.ID+"/plan/checkpoint", nil), "id", sess.ID)
	rec := httptest.NewRecorder()
	ctx.CheckpointSessionPlan(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when there is no @fix_plan.md, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCheckpointSessionPlan_WriteError500(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory write permissions")
	}
	skipIfNoStubShell(t)
	ctx := checkpointCtx(t)
	workdir := t.TempDir()
	sess, err := ctx.SessionManager.CreateSession(context.Background(), pty.SessionConfig{
		WorkingDir: workdir, Mode: pty.ModeShell, Name: "planwrite",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	t.Cleanup(func() { _ = ctx.SessionManager.StopSession(context.Background(), sess.ID) })

	// The plan exists so we pass the ReadFile gate...
	if err := os.WriteFile(filepath.Join(workdir, "@fix_plan.md"), []byte("plan body"), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	// ...but the dir is read-only so the versioned atomicWriteFile fails.
	if err := os.Chmod(workdir, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(workdir, 0o755) })

	req := withURLParam(httptest.NewRequest("POST", "/api/sessions/"+sess.ID+"/plan/checkpoint", nil), "id", sess.ID)
	rec := httptest.NewRecorder()
	ctx.CheckpointSessionPlan(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when the versioned checkpoint write fails, got %d: %s", rec.Code, rec.Body.String())
	}
}
