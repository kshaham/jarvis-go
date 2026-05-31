package handler

// Branch fills for the plan/fix-plan write handlers in session_extended.go:
//   - PatchSessionPlanTask: bad JSON, no-plan-404, non-numeric taskId,
//     task-not-found, the completed/in_progress/pending/failed status cases,
//     and the write-error path (read-only working dir).
//   - WriteSessionFixPlan / ImportSessionFixPlan / WriteSessionRalphPrompt:
//     bad-JSON 400 + write-error 500.

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPatchSessionPlanTask_AllStatusTransitions(t *testing.T) {
	ctx := newSessionExtCtx(t)
	id, wd := liveSession(t, ctx)
	planPath := filepath.Join(wd, "@fix_plan.md")
	// Four tasks so each status case has its own index. Task three starts
	// unchecked ("- [ ]") so the "failed" transition (which only rewrites
	// "- [ ]" -> "- [x]") flips it AND appends the FAILED annotation.
	seed := "- [ ] task zero\n- [x] task one\n- [x] task two\n- [ ] task three\n"
	if err := os.WriteFile(planPath, []byte(seed), 0o644); err != nil {
		t.Fatalf("seed plan: %v", err)
	}

	cases := []struct {
		taskID string
		status string
		want   string // expected checkbox prefix after patch
	}{
		{"0", "completed", "- [x]"},
		{"1", "in_progress", "- [-]"},
		{"2", "pending", "- [ ]"},
		{"3", "failed", "- [x]"},
	}
	for _, c := range cases {
		body := `{"status":"` + c.status + `"}`
		if c.status == "failed" {
			body = `{"status":"failed","error":"boom"}`
		}
		req := twoParamReq("PATCH", "/api/sessions/"+id+"/plan/task/"+c.taskID, body, "id", id, "taskId", c.taskID)
		rec := httptest.NewRecorder()
		ctx.PatchSessionPlanTask(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("patch task %s %s: expected 200, got %d: %s", c.taskID, c.status, rec.Code, rec.Body.String())
		}
	}
	got, _ := os.ReadFile(planPath)
	lines := strings.Split(string(got), "\n")
	for i, c := range cases {
		if !strings.HasPrefix(strings.TrimSpace(lines[i]), c.want) {
			t.Errorf("task %d: line=%q, want prefix %q", i, lines[i], c.want)
		}
	}
	// The failed task carries the FAILED annotation.
	if !strings.Contains(lines[3], "FAILED: boom") {
		t.Errorf("failed task should carry error annotation, got %q", lines[3])
	}
}

func TestPatchSessionPlanTask_BadJSON(t *testing.T) {
	ctx := newSessionExtCtx(t)
	id, _ := liveSession(t, ctx)
	req := twoParamReq("PATCH", "/api/sessions/"+id+"/plan/task/0", "{bad", "id", id, "taskId", "0")
	rec := httptest.NewRecorder()
	ctx.PatchSessionPlanTask(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on bad JSON, got %d", rec.Code)
	}
}

func TestPatchSessionPlanTask_NoPlanFile(t *testing.T) {
	ctx := newSessionExtCtx(t)
	id, _ := liveSession(t, ctx)
	req := twoParamReq("PATCH", "/api/sessions/"+id+"/plan/task/0", `{"status":"completed"}`, "id", id, "taskId", "0")
	rec := httptest.NewRecorder()
	ctx.PatchSessionPlanTask(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when no @fix_plan.md exists, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPatchSessionPlanTask_NonNumericTaskID(t *testing.T) {
	ctx := newSessionExtCtx(t)
	id, wd := liveSession(t, ctx)
	_ = os.WriteFile(filepath.Join(wd, "@fix_plan.md"), []byte("- [ ] task\n"), 0o644)
	req := twoParamReq("PATCH", "/api/sessions/"+id+"/plan/task/abc", `{"status":"completed"}`, "id", id, "taskId", "abc")
	rec := httptest.NewRecorder()
	ctx.PatchSessionPlanTask(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-numeric taskId, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPatchSessionPlanTask_TaskIndexOutOfRange(t *testing.T) {
	ctx := newSessionExtCtx(t)
	id, wd := liveSession(t, ctx)
	_ = os.WriteFile(filepath.Join(wd, "@fix_plan.md"), []byte("- [ ] only one task\n"), 0o644)
	req := twoParamReq("PATCH", "/api/sessions/"+id+"/plan/task/9", `{"status":"completed"}`, "id", id, "taskId", "9")
	rec := httptest.NewRecorder()
	ctx.PatchSessionPlanTask(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for out-of-range task index, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestWriteSessionFixPlan_BadJSON(t *testing.T) {
	ctx := newSessionExtCtx(t)
	id, _ := liveSession(t, ctx)
	rec := httptest.NewRecorder()
	ctx.WriteSessionFixPlan(rec, idReq("POST", "/api/sessions/"+id+"/fix-plan/write", "{bad", id))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on bad JSON, got %d", rec.Code)
	}
}

func TestImportSessionFixPlan_BadJSON(t *testing.T) {
	ctx := newSessionExtCtx(t)
	id, _ := liveSession(t, ctx)
	rec := httptest.NewRecorder()
	ctx.ImportSessionFixPlan(rec, idReq("POST", "/api/sessions/"+id+"/fix-plan/import", "{bad", id))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on bad JSON, got %d", rec.Code)
	}
}

func TestWriteSessionRalphPrompt_BadJSON(t *testing.T) {
	ctx := newSessionExtCtx(t)
	id, _ := liveSession(t, ctx)
	rec := httptest.NewRecorder()
	ctx.WriteSessionRalphPrompt(rec, idReq("POST", "/api/sessions/"+id+"/ralph-prompt/write", "{bad", id))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on bad JSON, got %d", rec.Code)
	}
}

// TestWriteSessionFixPlan_WriteError drives the atomicWriteFile 500 path by
// making the session's working dir read-only so the temp-file create fails.
func TestWriteSessionFixPlan_WriteError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file-mode semantics")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory write permissions")
	}
	ctx := newSessionExtCtx(t)
	id, wd := liveSession(t, ctx)
	if err := os.Chmod(wd, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(wd, 0o755) })

	rec := httptest.NewRecorder()
	ctx.WriteSessionFixPlan(rec, idReq("POST", "/api/sessions/"+id+"/fix-plan/write", `{"content":"x"}`, id))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 writing into a read-only dir, got %d: %s", rec.Code, rec.Body.String())
	}
}
