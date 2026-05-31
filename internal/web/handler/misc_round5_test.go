package handler

// Round 5 -- residual misc.go arms:
//   - Run: the stdout >1MB truncation arm (cat a large file) and the
//     non-ExitError 500 arm (binary vanishes between resolve and run).
//   - QuickStart: the defaultWorkingDir-from-settings resolution arm.
//   - LinkCase: the ~-expansion happy path + the UserHomeDir-error 500 arm.
//   - GetCaseFixPlan: the no-todos normalisation (plan file with no checkboxes).
//   - GetRalphWizardFile: the path-traversal -> 403 arm.
//   - verifyHookSignature: the empty-key short-circuit (no signing secret).

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kshaham/jarvis/internal/cases"
	"github.com/kshaham/jarvis/internal/state"
	"github.com/kshaham/jarvis/internal/web/auth"
)

func TestRun_StdoutTruncatedOver1MB(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	wd := t.TempDir()
	// Create a >1MB file and cat it; the handler caps stdout at 1MB.
	big := filepath.Join(wd, "big.txt")
	if err := os.WriteFile(big, make([]byte, (1<<20)+4096), 0o644); err != nil {
		t.Fatalf("write big file: %v", err)
	}
	ctx := &Context{StateStore: state.NewStore(filepath.Join(t.TempDir(), "s.json"))}
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"workingDirAllowlist": []interface{}{wd},
	})
	rec := postJSON(t, ctx.Run, "/api/run", map[string]interface{}{
		"command":    "cat " + big,
		"workingDir": wd,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	out, _ := resp["output"].(string)
	if len(out) != (1 << 20) {
		t.Errorf("stdout should be truncated to exactly 1MB, got %d bytes", len(out))
	}
}

func TestQuickStart_UsesDefaultWorkingDir(t *testing.T) {
	t.Parallel()
	ctx := newSessionLiveCtx(t)
	ctx.CaseManager = cases.NewCaseManager(t.TempDir())
	if _, err := ctx.CaseManager.Create("dw-case"); err != nil {
		t.Fatalf("create case: %v", err)
	}
	// A real default working dir in settings takes priority over the case dir.
	defaultDir := t.TempDir()
	ctx.StateStore.UpdateSettings(map[string]interface{}{
		"defaultWorkingDir": defaultDir,
	})

	rec := postJSON(t, ctx.QuickStart, "/api/quick-start", map[string]interface{}{
		"caseName": "dw-case", "mode": "shell",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	sess, _ := resp["session"].(map[string]interface{})
	id, _ := sess["id"].(string)
	if id == "" {
		t.Fatal("expected a session id")
	}
	t.Cleanup(func() {
		_ = ctx.SessionManager.StopSession(context.Background(), id)
		ctx.cleanupSessionResources(id)
	})
	// The spawned session should be rooted at the default working dir.
	live, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		t.Fatal("session not registered")
	}
	if live.WorkingDir != defaultDir {
		t.Errorf("workingDir=%q, want defaultDir %q", live.WorkingDir, defaultDir)
	}
}

func TestLinkCase_TildeExpansionHappy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Create a real directory under HOME the operator can reference as ~/proj.
	proj := filepath.Join(home, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	ctx := newSessionLiveCtx(t)
	ctx.CaseManager = cases.NewCaseManager(t.TempDir())

	rec := postJSON(t, ctx.LinkCase, "/api/cases/link", map[string]interface{}{
		"name": "linked-case",
		"path": "~/proj",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 linking ~/proj, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestLinkCase_HomeDirErrorReturns500(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("UserHomeDir uses USERPROFILE on Windows")
	}
	t.Setenv("HOME", "") // os.UserHomeDir errors on unix when HOME is empty
	ctx := newSessionLiveCtx(t)
	ctx.CaseManager = cases.NewCaseManager(t.TempDir())

	rec := postJSON(t, ctx.LinkCase, "/api/cases/link", map[string]interface{}{
		"name": "linked-case",
		"path": "~/proj",
	})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when HOME is unresolvable, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetCaseFixPlan_NoCheckboxesEmptyTodos(t *testing.T) {
	t.Parallel()
	ctx := newSessionLiveCtx(t)
	cm := cases.NewCaseManager(t.TempDir())
	ctx.CaseManager = cm
	info, err := cm.Create("plan-case")
	if err != nil {
		t.Fatalf("create case: %v", err)
	}
	// A plan file with prose but NO "- [" checkbox lines -> todos stays nil
	// and is normalised to an empty array.
	if err := os.WriteFile(filepath.Join(info.Path, "@fix_plan.md"),
		[]byte("# Plan\n\nJust prose, no tasks.\n"), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	rec := withURLParam(httptest.NewRequest("GET", "/api/cases/plan-case/fix-plan", nil), "name", "plan-case")
	w := httptest.NewRecorder()
	ctx.GetCaseFixPlan(w, rec)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeMap(t, w)
	if resp["exists"] != true {
		t.Errorf("exists=%v, want true", resp["exists"])
	}
	todos, ok := resp["todos"].([]interface{})
	if !ok || len(todos) != 0 {
		t.Errorf("expected empty todos array, got %v", resp["todos"])
	}
}

func TestGetRalphWizardFile_TraversalForbidden(t *testing.T) {
	t.Parallel()
	ctx := newSessionLiveCtx(t)
	cm := cases.NewCaseManager(t.TempDir())
	ctx.CaseManager = cm
	info, err := cm.Create("wiz-case")
	if err != nil {
		t.Fatalf("create case: %v", err)
	}
	// Build the wizard dir and a sibling secret OUTSIDE it. The relPath
	// "../secret.txt" resolves (EvalSymlinks) to a real file that escapes the
	// wizard root -> ErrRalphWizardPathInvalid -> 403.
	wizardDir := filepath.Join(info.Path, "ralph-wizard")
	if err := os.MkdirAll(wizardDir, 0o755); err != nil {
		t.Fatalf("mkdir wizard: %v", err)
	}
	if err := os.WriteFile(filepath.Join(info.Path, "secret.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write secret: %v", err)
	}

	req := twoParamReq("GET", "/api/cases/wiz-case/ralph-wizard/../secret.txt", "",
		"caseName", "wiz-case", "*", "../secret.txt")
	rec := httptest.NewRecorder()
	ctx.GetRalphWizardFile(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for traversal path, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestVerifyHookSignature_EmptySigningKey(t *testing.T) {
	t.Parallel()
	// A zero-value Manager has no signing secret, so both HookSigningKey and
	// SigningKey return empty -> the check closure short-circuits to false.
	ctx := &Context{AuthManager: &auth.Manager{}}
	// A well-formed sha256= header so the closure is actually reached.
	header := "sha256=" + strings.Repeat("ab", 32)
	if ctx.verifyHookSignature([]byte("body"), header) {
		t.Fatal("expected verification to fail with an empty signing key")
	}
}
