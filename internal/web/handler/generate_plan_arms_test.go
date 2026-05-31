package handler

// Round-4 branch fills for GeneratePlanDetailed's residual arms:
//   - ClearRalphWizardDir error is logged but doesn't fail the request
//     (1121-1124)
//   - CreateSession failure -> 500 (1152-1155)
//   - nil ActivePlanOrchestrators map is lazily initialised (1161-1163)
//
// The 2s prompt-injection goroutine and its panic-recovery are timing-bound
// background work, not exercised here (see notes / unreachable list).

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/cases"
	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/state"
	"github.com/kshaham/jarvis/internal/web/sse"
)

// TestGeneratePlanDetailed_CreateSessionFails500 drives the CreateSession
// error arm by capping the manager at zero sessions.
func TestGeneratePlanDetailed_CreateSessionFails500(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := &Context{
		SessionManager: pty.NewSessionManager(0), // every create rejected
		SSEBroker:      sse.NewBroker(100, 10),
		StateStore:     state.NewStore(filepath.Join(t.TempDir(), "s.json")),
		OfficeManager:  office.NewManager(stubSessions{}, func(string, interface{}) {}, nil),
	}

	rec := postJSON(t, ctx.GeneratePlanDetailed, "/api/generate-plan-detailed", map[string]interface{}{
		"taskDescription": "Add a health endpoint",
	})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when CreateSession fails, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestGeneratePlanDetailed_NilOrchestratorMapInit drives the lazy map-init arm
// (ActivePlanOrchestrators == nil) and the no-case home-dir fallback through a
// real spawned shell-less Claude session.
func TestGeneratePlanDetailed_NilOrchestratorMapInit(t *testing.T) {
	ctx := newSessionLiveCtx(t)
	ctx.ActivePlanOrchestrators = nil // force the lazy make()
	ctx.StateStore = state.NewStore(filepath.Join(t.TempDir(), "s.json"))

	rec := postJSON(t, ctx.GeneratePlanDetailed, "/api/generate-plan-detailed", map[string]interface{}{
		"taskDescription": "Document the API",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	data, _ := resp["data"].(map[string]interface{})
	orchID, _ := data["orchestratorId"].(string)
	sessionID, _ := data["sessionId"].(string)
	if orchID == "" || sessionID == "" {
		t.Fatalf("expected orchestratorId+sessionId, got %v", data)
	}
	if ctx.ActivePlanOrchestrators == nil {
		t.Fatal("expected ActivePlanOrchestrators to be lazily initialised")
	}
	if ctx.ActivePlanOrchestrators[orchID] != sessionID {
		t.Errorf("orchestrator %s not tracked -> %q (want %q)", orchID, ctx.ActivePlanOrchestrators[orchID], sessionID)
	}
	t.Cleanup(func() {
		_ = ctx.SessionManager.StopSession(t.Context(), sessionID)
		ctx.cleanupSessionResources(sessionID)
		time.Sleep(50 * time.Millisecond)
	})
}

// TestGeneratePlanDetailed_ClearWizardDirErrorLoggedNot500 drives the
// ClearRalphWizardDir error arm: the wizard dir exists but its parent (the case
// path) is read-only so RemoveAll fails. The handler logs and proceeds to a 200.
func TestGeneratePlanDetailed_ClearWizardDirErrorLoggedNot500(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod-based read-only dir semantics differ on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses directory permission checks")
	}
	ctx := newSessionLiveCtx(t)
	ctx.StateStore = state.NewStore(filepath.Join(t.TempDir(), "s.json"))
	ctx.ActivePlanOrchestrators = map[string]string{}

	cm := cases.NewCaseManager(t.TempDir())
	ctx.CaseManager = cm
	caseInfo, err := cm.Create("wiz-case")
	if err != nil {
		t.Fatalf("create case: %v", err)
	}

	// Create ralph-wizard/<file> then make the case path read-only so RemoveAll
	// of ralph-wizard can't unlink from the parent.
	wizardDir := filepath.Join(caseInfo.Path, "ralph-wizard")
	if err := os.MkdirAll(wizardDir, 0o755); err != nil {
		t.Fatalf("mkdir wizard: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wizardDir, "stale.md"), []byte("old"), 0o644); err != nil {
		t.Fatalf("write stale: %v", err)
	}
	if err := os.Chmod(caseInfo.Path, 0o500); err != nil {
		t.Fatalf("chmod case path: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(caseInfo.Path, 0o755) })

	rec := postJSON(t, ctx.GeneratePlanDetailed, "/api/generate-plan-detailed", map[string]interface{}{
		"taskDescription": "Refactor the planner", "caseName": "wiz-case",
	})
	// The clear failure is logged but must NOT fail the request.
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 despite wizard-clear failure, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	data, _ := resp["data"].(map[string]interface{})
	sessionID, _ := data["sessionId"].(string)
	t.Cleanup(func() {
		if sessionID != "" {
			_ = ctx.SessionManager.StopSession(t.Context(), sessionID)
			ctx.cleanupSessionResources(sessionID)
			time.Sleep(50 * time.Millisecond)
		}
	})
}
