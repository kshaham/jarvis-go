package handler

// Round-2 final batch of reachable branch fills: scheduler bad-JSON + duplicate-
// ID, agent-memory write/append IO failures, RollbackSessionPlan read/write
// errors, GetSessionFixPlan no-plan, SessionInput bad-JSON.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/scheduler"
	"github.com/kshaham/jarvis/internal/web/sse"
)

// --- Scheduler ---

func TestCreateSchedulerJob_BadJSON400(t *testing.T) {
	t.Parallel()
	ctx := newSchedulerCtx(t)
	req := httptest.NewRequest("POST", "/api/scheduler/jobs", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.CreateSchedulerJob(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on malformed JSON, got %d", rec.Code)
	}
}

func TestUpdateSchedulerJob_BadJSON400(t *testing.T) {
	t.Parallel()
	ctx := newSchedulerCtx(t)
	req := withURLParam(httptest.NewRequest("PUT", "/api/scheduler/jobs/x", strings.NewReader("{bad")), "id", "x")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.UpdateSchedulerJob(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on malformed JSON, got %d", rec.Code)
	}
}

func TestCreateSchedulerJob_DuplicateID500(t *testing.T) {
	t.Parallel()
	ctx := newSchedulerCtx(t)
	// Pre-create a job with a fixed ID directly in the store.
	if _, err := ctx.Scheduler.Create(scheduler.Job{
		ID: "dup-id", Name: "first", Cron: "* * * * *", Action: "one_shot", Enabled: false,
	}); err != nil {
		t.Fatalf("seed first job: %v", err)
	}
	// POST a second job that reuses the same ID -> Store.Create returns
	// ErrDuplicateID -> handler 500.
	rec := postJSON(t, ctx.CreateSchedulerJob, "/api/scheduler/jobs", map[string]interface{}{
		"id": "dup-id", "name": "second", "cron": "* * * * *", "action": "one_shot",
	})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on duplicate job ID, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- Agent memory IO failures ---

// memCtx registers an agent + a session so the memory handlers reach the
// SaveAgentMemory/AppendAgentMemory call. workingDir is returned for planting.
func memCtx(t *testing.T) (*Context, string, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	ctx := &Context{
		SessionManager: pty.NewSessionManager(10),
		SSEBroker:      sse.NewBroker(100, 10),
		OfficeManager:  office.NewManager(stubSessions{}, func(string, interface{}) {}, nil),
	}
	workingDir := t.TempDir()
	sdk := pty.NewSDKSession(pty.SessionConfig{ID: "mem-sess", WorkingDir: workingDir, Mode: pty.ModeClaudeSDK})
	ctx.SessionManager.RegisterSDKSessionForTest(sdk)
	ctx.OfficeManager.RegisterAgent("mem-sess", "mem-cfg", &office.TeamMemberConfig{ID: "mem-cfg", Name: "Mem"})
	return ctx, "mem-sess", workingDir
}

// plantAgentMemoryBlocker makes the agent-memory project dir un-writable by
// planting a FILE where the per-project memory dir should be, so MkdirAll is a
// no-op and the subsequent WriteFile of the .tmp fails (parent is a file).
func plantAgentMemoryBlocker(t *testing.T, configID, workingDir string) {
	t.Helper()
	memPath := office.GetAgentMemoryPath(configID, workingDir) // .../<hash>/<cfg>.md
	projectDir := filepath.Dir(memPath)                        // .../<hash>
	if err := os.MkdirAll(filepath.Dir(projectDir), 0o755); err != nil {
		t.Fatalf("mkdir agent-memory root: %v", err)
	}
	// Plant the <hash> path as a regular file so MkdirAll(projectDir) fails and
	// WriteFile(projectDir/<cfg>.md.tmp) errors (not a directory).
	if err := os.WriteFile(projectDir, []byte("blocker"), 0o644); err != nil {
		t.Fatalf("plant blocker file: %v", err)
	}
}

func TestOfficeAgentPostMemory_WriteError500(t *testing.T) {
	ctx, sess, workingDir := memCtx(t)
	plantAgentMemoryBlocker(t, "mem-cfg", workingDir)
	rec := postJSON(t, ctx.OfficeAgentPostMemory, "/api/office/agent/memory", map[string]interface{}{
		"sessionId": sess, "content": "remember this",
	})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when SaveAgentMemory fails, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficeAgentPostMemoryAppend_AppendError500(t *testing.T) {
	ctx, sess, workingDir := memCtx(t)
	plantAgentMemoryBlocker(t, "mem-cfg", workingDir)
	rec := postJSON(t, ctx.OfficeAgentPostMemoryAppend, "/api/office/agent/memory/append", map[string]interface{}{
		"sessionId": sess, "entry": "appended line",
	})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when AppendAgentMemory fails, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- Session handlers ---

func TestSessionInput_BadJSON400(t *testing.T) {
	skipIfNoStubShell(t)
	ctx, id := autoSessionCtx(t)
	req := withURLParam(httptest.NewRequest("POST", "/api/sessions/"+id+"/input", strings.NewReader("{bad")), "id", id)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.SessionInput(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on malformed JSON, got %d", rec.Code)
	}
}

func TestGetSessionFixPlan_NoPlanReturnsNil(t *testing.T) {
	skipIfNoStubShell(t)
	ctx, id := autoSessionCtx(t)
	req := withURLParam(httptest.NewRequest("GET", "/api/sessions/"+id+"/fix-plan", nil), "id", id)
	rec := httptest.NewRecorder()
	ctx.GetSessionFixPlan(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"plan":null`) {
		t.Errorf("expected plan:null when no @fix_plan.md exists, got %s", rec.Body.String())
	}
}

func TestRollbackSessionPlan_ReadVersionError500(t *testing.T) {
	skipIfNoStubShell(t)
	ctx, id := autoSessionCtx(t)
	wd, _ := ctx.sessionWorkingDir(id)
	// The version file is a DIRECTORY -> os.ReadFile returns EISDIR (non-IsNotExist) -> 500.
	if err := os.MkdirAll(filepath.Join(wd, "@fix_plan.v1.md"), 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}
	req := rollbackReq(id, "1")
	rec := httptest.NewRecorder()
	ctx.RollbackSessionPlan(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 reading a directory as the version file, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRollbackSessionPlan_WritePlanError500(t *testing.T) {
	skipIfNoStubShell(t)
	ctx, id := autoSessionCtx(t)
	wd, _ := ctx.sessionWorkingDir(id)
	// A readable version file...
	if err := os.WriteFile(filepath.Join(wd, "@fix_plan.v2.md"), []byte("v2 plan"), 0o644); err != nil {
		t.Fatalf("write version file: %v", err)
	}
	// ...but @fix_plan.md is a non-empty directory so atomicWriteFile's rename fails.
	if err := os.MkdirAll(filepath.Join(wd, "@fix_plan.md", "child"), 0o755); err != nil {
		t.Fatalf("plant plan dir: %v", err)
	}
	req := rollbackReq(id, "2")
	rec := httptest.NewRecorder()
	ctx.RollbackSessionPlan(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when writing @fix_plan.md fails, got %d: %s", rec.Code, rec.Body.String())
	}
}

var _ = context.Background
