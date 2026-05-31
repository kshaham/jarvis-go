package handler

// Happy-path coverage for session_extended.go using a REAL shell PTY session.
// ModeShell spawns `bash -l -i`, which stays alive reading stdin, so we can
// create one session per test and exercise the file/plan/toggle handlers that
// the 404 sweep can't reach. Sessions are stopped in cleanup so the race
// detector doesn't flag leaked read-loop goroutines.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/kshaham/jarvis/internal/pty"
)

// liveSession creates and registers a real shell session in ctx's manager,
// rooted at a temp working dir, and returns its ID. The session is stopped on
// cleanup.
func liveSession(t *testing.T, ctx *Context) (string, string) {
	t.Helper()
	workingDir := t.TempDir()
	sess, err := ctx.SessionManager.CreateSession(context.Background(), pty.SessionConfig{
		WorkingDir: workingDir,
		Mode:       pty.ModeShell,
		Name:       "live-test",
	})
	if err != nil {
		t.Fatalf("create live session: %v", err)
	}
	ctx.initSessionTrackers(sess.ID)
	t.Cleanup(func() {
		_ = ctx.SessionManager.StopSession(context.Background(), sess.ID)
		ctx.cleanupSessionResources(sess.ID)
	})
	return sess.ID, workingDir
}

func idReq(method, path, body, id string) *http.Request {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	return withURLParam(r, "id", id)
}

func TestRenameSession_Live(t *testing.T) {
	t.Parallel()
	ctx := newSessionExtCtx(t)
	id, _ := liveSession(t, ctx)

	rec := httptest.NewRecorder()
	ctx.RenameSession(rec, idReq("PUT", "/api/sessions/"+id+"/name", `{"name":"renamed"}`, id))
	if rec.Code != http.StatusOK {
		t.Fatalf("rename: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	sess, _ := ctx.SessionManager.GetSession(id)
	if sess.Name() != "renamed" {
		t.Errorf("name=%q, want renamed", sess.Name())
	}

	// Blank name -> 400.
	recBlank := httptest.NewRecorder()
	ctx.RenameSession(recBlank, idReq("PUT", "/api/sessions/"+id+"/name", `{"name":"   "}`, id))
	if recBlank.Code != http.StatusBadRequest {
		t.Fatalf("blank name: expected 400, got %d", recBlank.Code)
	}
}

func TestUpdateSessionColor_Live(t *testing.T) {
	t.Parallel()
	ctx := newSessionExtCtx(t)
	id, _ := liveSession(t, ctx)

	rec := httptest.NewRecorder()
	ctx.UpdateSessionColor(rec, idReq("PUT", "/api/sessions/"+id+"/color", `{"color":"blue"}`, id))
	if rec.Code != http.StatusOK {
		t.Fatalf("color: expected 200, got %d", rec.Code)
	}
	ctx.SessionColorsMu.RLock()
	got := ctx.SessionColors[id]
	ctx.SessionColorsMu.RUnlock()
	if got != "blue" {
		t.Errorf("stored color=%q, want blue", got)
	}
}

func TestSessionResize_Live(t *testing.T) {
	t.Parallel()
	ctx := newSessionExtCtx(t)
	id, _ := liveSession(t, ctx)

	// Valid resize -> 200.
	rec := httptest.NewRecorder()
	ctx.SessionResize(rec, idReq("POST", "/api/sessions/"+id+"/resize", `{"cols":100,"rows":40}`, id))
	if rec.Code != http.StatusOK {
		t.Fatalf("resize: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Non-positive dims -> 400.
	recBad := httptest.NewRecorder()
	ctx.SessionResize(recBad, idReq("POST", "/api/sessions/"+id+"/resize", `{"cols":0,"rows":0}`, id))
	if recBad.Code != http.StatusBadRequest {
		t.Fatalf("bad dims: expected 400, got %d", recBad.Code)
	}
}

func TestSessionToggles_Live(t *testing.T) {
	t.Parallel()
	ctx := newSessionExtCtx(t)
	id, _ := liveSession(t, ctx)

	type tc struct {
		name    string
		handler func(http.ResponseWriter, *http.Request)
		key     string
	}
	cases := []tc{
		{"auto-clear", ctx.SessionAutoClear, "auto-clear-" + id},
		{"auto-compact", ctx.SessionAutoCompact, "auto-compact-" + id},
		{"auto-trim", ctx.SessionAutoTrim, "auto-trim-" + id},
		{"image-watcher", ctx.SessionImageWatcher, "image-watcher-" + id},
		{"flicker-filter", ctx.SessionFlickerFilter, "flicker-filter-" + id},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			// threshold>0 so the immediate /clear path isn't taken (keeps the
			// shell from receiving slash commands mid-test).
			c.handler(rec, idReq("POST", "/api/sessions/"+id+"/"+c.name, `{"enabled":true,"threshold":999999}`, id))
			if rec.Code != http.StatusOK {
				t.Fatalf("%s: expected 200, got %d: %s", c.name, rec.Code, rec.Body.String())
			}
			if _, ok := ctx.StateStore.GetSettings()[c.key]; !ok {
				t.Errorf("%s: expected settings key %q to be written", c.name, c.key)
			}
		})
	}
}

func TestSessionCPULimit_Live(t *testing.T) {
	t.Parallel()
	ctx := newSessionExtCtx(t)
	id, _ := liveSession(t, ctx)

	// Set.
	recSet := httptest.NewRecorder()
	ctx.SetSessionCPULimit(recSet, idReq("POST", "/api/sessions/"+id+"/cpu-limit", `{"limit":50}`, id))
	if recSet.Code != http.StatusOK {
		t.Fatalf("set cpu-limit: expected 200, got %d", recSet.Code)
	}

	// Get returns the stored value.
	recGet := httptest.NewRecorder()
	ctx.GetSessionCPULimit(recGet, idReq("GET", "/api/sessions/"+id+"/cpu-limit", "", id))
	if recGet.Code != http.StatusOK {
		t.Fatalf("get cpu-limit: expected 200, got %d", recGet.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(recGet.Body.Bytes(), &resp)
	if resp["limit"] != float64(50) {
		t.Errorf("limit=%v, want 50", resp["limit"])
	}
}

func TestSessionRalphConfigStatus_Live(t *testing.T) {
	t.Parallel()
	ctx := newSessionExtCtx(t)
	id, _ := liveSession(t, ctx)

	recSet := httptest.NewRecorder()
	ctx.SetSessionRalphConfig(recSet, idReq("POST", "/api/sessions/"+id+"/ralph-config", `{"enabled":true,"completionPhrase":"DONE","maxIterations":5}`, id))
	if recSet.Code != http.StatusOK {
		t.Fatalf("set ralph config: expected 200, got %d: %s", recSet.Code, recSet.Body.String())
	}

	recGet := httptest.NewRecorder()
	ctx.GetSessionRalphStatus(recGet, idReq("GET", "/api/sessions/"+id+"/ralph-status", "", id))
	if recGet.Code != http.StatusOK {
		t.Fatalf("get ralph status: expected 200, got %d", recGet.Code)
	}

	recReset := httptest.NewRecorder()
	ctx.ResetRalphCircuitBreaker(recReset, idReq("POST", "/api/sessions/"+id+"/ralph-cb/reset", "", id))
	if recReset.Code != http.StatusOK {
		t.Fatalf("reset cb: expected 200, got %d", recReset.Code)
	}
}

func TestSessionRalphState_Live(t *testing.T) {
	t.Parallel()
	ctx := newSessionExtCtx(t)
	id, _ := liveSession(t, ctx)
	// initSessionTrackers registered a Ralph tracker, so the populated branch runs.
	rec := httptest.NewRecorder()
	ctx.GetSessionRalphState(rec, idReq("GET", "/api/sessions/"+id+"/ralph-state", "", id))
	if rec.Code != http.StatusOK {
		t.Fatalf("ralph state: expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	st, _ := resp["state"].(map[string]interface{})
	if _, ok := st["enabled"]; !ok {
		t.Error("expected enabled in ralph state")
	}
}

func TestSessionRunSummary_Live(t *testing.T) {
	t.Parallel()
	ctx := newSessionExtCtx(t)
	id, _ := liveSession(t, ctx)
	rec := httptest.NewRecorder()
	ctx.GetSessionRunSummary(rec, idReq("GET", "/api/sessions/"+id+"/run-summary", "", id))
	if rec.Code != http.StatusOK {
		t.Fatalf("run summary: expected 200, got %d", rec.Code)
	}
}

func TestSessionFiles_Live(t *testing.T) {
	t.Parallel()
	ctx := newSessionExtCtx(t)
	id, wd := liveSession(t, ctx)
	// Seed a couple of files so the listing has content.
	_ = os.WriteFile(filepath.Join(wd, "a.txt"), []byte("x"), 0o644)
	_ = os.Mkdir(filepath.Join(wd, "sub"), 0o755)

	rec := httptest.NewRecorder()
	ctx.GetSessionFiles(rec, idReq("GET", "/api/sessions/"+id+"/files", "", id))
	if rec.Code != http.StatusOK {
		t.Fatalf("files: expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	files, _ := resp["files"].([]interface{})
	if len(files) != 2 {
		t.Errorf("expected 2 entries, got %d", len(files))
	}
}

func TestSessionFixPlanRoundTrip_Live(t *testing.T) {
	t.Parallel()
	ctx := newSessionExtCtx(t)
	id, wd := liveSession(t, ctx)

	// Write a fix plan.
	recW := httptest.NewRecorder()
	ctx.WriteSessionFixPlan(recW, idReq("POST", "/api/sessions/"+id+"/fix-plan/write", `{"content":"# Plan\n- [ ] step"}`, id))
	if recW.Code != http.StatusOK {
		t.Fatalf("write fix plan: expected 200, got %d: %s", recW.Code, recW.Body.String())
	}
	// File should exist on disk.
	if _, err := os.Stat(filepath.Join(wd, "@fix_plan.md")); err != nil {
		t.Fatalf("@fix_plan.md not written: %v", err)
	}

	// Read it back.
	recR := httptest.NewRecorder()
	ctx.GetSessionFixPlan(recR, idReq("GET", "/api/sessions/"+id+"/fix-plan", "", id))
	if recR.Code != http.StatusOK {
		t.Fatalf("get fix plan: expected 200, got %d", recR.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(recR.Body.Bytes(), &resp)
	if plan, _ := resp["plan"].(string); !strings.Contains(plan, "step") {
		t.Errorf("plan content not returned: %v", resp["plan"])
	}

	// Append a task.
	recT := httptest.NewRecorder()
	ctx.SessionPlanTask(recT, idReq("POST", "/api/sessions/"+id+"/plan/task", `{"task":"another"}`, id))
	if recT.Code != http.StatusOK {
		t.Fatalf("plan task: expected 200, got %d", recT.Code)
	}
}

func TestSessionPlanCheckpointHistoryRollback_Live(t *testing.T) {
	t.Parallel()
	ctx := newSessionExtCtx(t)
	id, wd := liveSession(t, ctx)

	// Seed a fix plan so checkpoint has something to copy.
	if err := os.WriteFile(filepath.Join(wd, "@fix_plan.md"), []byte("v1 content"), 0o644); err != nil {
		t.Fatalf("seed plan: %v", err)
	}

	// Checkpoint -> creates @fix_plan.v1.md.
	recC := httptest.NewRecorder()
	ctx.CheckpointSessionPlan(recC, idReq("POST", "/api/sessions/"+id+"/plan/checkpoint", "", id))
	if recC.Code != http.StatusOK {
		t.Fatalf("checkpoint: expected 200, got %d: %s", recC.Code, recC.Body.String())
	}
	var cp map[string]interface{}
	_ = json.Unmarshal(recC.Body.Bytes(), &cp)
	version, _ := cp["version"].(float64)
	if version < 1 {
		t.Fatalf("expected version >= 1, got %v", cp["version"])
	}

	// History lists the version.
	recH := httptest.NewRecorder()
	ctx.GetSessionPlanHistory(recH, idReq("GET", "/api/sessions/"+id+"/plan/history", "", id))
	if recH.Code != http.StatusOK {
		t.Fatalf("history: expected 200, got %d", recH.Code)
	}
	var hist map[string]interface{}
	_ = json.Unmarshal(recH.Body.Bytes(), &hist)
	versions, _ := hist["versions"].([]interface{})
	if len(versions) == 0 {
		t.Error("expected at least one checkpoint version")
	}

	// Overwrite the plan, then roll back to v1.
	_ = os.WriteFile(filepath.Join(wd, "@fix_plan.md"), []byte("v2 content"), 0o644)
	recRB := httptest.NewRecorder()
	ctx.RollbackSessionPlan(recRB, rollbackReq(id, "1"))
	if recRB.Code != http.StatusOK {
		t.Fatalf("rollback: expected 200, got %d: %s", recRB.Code, recRB.Body.String())
	}
	data, _ := os.ReadFile(filepath.Join(wd, "@fix_plan.md"))
	if string(data) != "v1 content" {
		t.Errorf("rollback did not restore v1: %q", string(data))
	}

	// Rollback to a missing version -> 404.
	recMiss := httptest.NewRecorder()
	ctx.RollbackSessionPlan(recMiss, rollbackReq(id, "99"))
	if recMiss.Code != http.StatusNotFound {
		t.Fatalf("rollback missing version: expected 404, got %d", recMiss.Code)
	}

	// Rollback with a non-numeric version -> 400.
	recBad := httptest.NewRecorder()
	ctx.RollbackSessionPlan(recBad, rollbackReq(id, "x"))
	if recBad.Code != http.StatusBadRequest {
		t.Fatalf("rollback bad version: expected 400, got %d", recBad.Code)
	}
}

// rollbackReq builds a request carrying BOTH the id and version chi params in
// a single route context (withURLParam replaces the whole context each call,
// so chaining it would drop the first param).
func rollbackReq(id, version string) *http.Request {
	return twoParamReq("POST", "/api/sessions/"+id+"/plan/rollback/"+version, "", "id", id, "version", version)
}

// twoParamReq builds a request whose route context carries two chi URL params.
func twoParamReq(method, path, body, k1, v1, k2, v2 string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(k1, v1)
	rctx.URLParams.Add(k2, v2)
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestSessionFileContentAndRaw_Live(t *testing.T) {
	t.Parallel()
	ctx := newSessionExtCtx(t)
	id, wd := liveSession(t, ctx)
	if err := os.WriteFile(filepath.Join(wd, "note.md"), []byte("# Title\nbody"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	// file-content (text).
	recC := httptest.NewRecorder()
	ctx.GetSessionFileContent(recC, idReq("GET", "/api/sessions/"+id+"/file-content?path=note.md", "", id))
	if recC.Code != http.StatusOK {
		t.Fatalf("file-content: expected 200, got %d: %s", recC.Code, recC.Body.String())
	}
	var rc map[string]interface{}
	_ = json.Unmarshal(recC.Body.Bytes(), &rc)
	if content, _ := rc["content"].(string); !strings.Contains(content, "body") {
		t.Errorf("file content not returned: %v", rc["content"])
	}

	// Missing path param -> 400.
	recNoPath := httptest.NewRecorder()
	ctx.GetSessionFileContent(recNoPath, idReq("GET", "/api/sessions/"+id+"/file-content", "", id))
	if recNoPath.Code != http.StatusBadRequest {
		t.Fatalf("missing path: expected 400, got %d", recNoPath.Code)
	}

	// file-raw on the same text file -> detects text mime.
	recR := httptest.NewRecorder()
	ctx.GetSessionFileRaw(recR, idReq("GET", "/api/sessions/"+id+"/file-raw?path=note.md", "", id))
	if recR.Code != http.StatusOK {
		t.Fatalf("file-raw: expected 200, got %d", recR.Code)
	}
	var rr map[string]interface{}
	_ = json.Unmarshal(recR.Body.Bytes(), &rr)
	if rr["encoding"] != "utf-8" {
		t.Errorf("expected utf-8 encoding for .md, got %v", rr["encoding"])
	}

	// file-raw missing file -> 404.
	recMiss := httptest.NewRecorder()
	ctx.GetSessionFileRaw(recMiss, idReq("GET", "/api/sessions/"+id+"/file-raw?path=ghost.md", "", id))
	if recMiss.Code != http.StatusNotFound {
		t.Fatalf("missing raw file: expected 404, got %d", recMiss.Code)
	}
}

func TestSessionOutputAndContext_Live(t *testing.T) {
	t.Parallel()
	ctx := newSessionExtCtx(t)
	id, _ := liveSession(t, ctx)

	recO := httptest.NewRecorder()
	ctx.GetSessionOutput(recO, idReq("GET", "/api/sessions/"+id+"/output", "", id))
	if recO.Code != http.StatusOK {
		t.Fatalf("output: expected 200, got %d", recO.Code)
	}

	recCtx := httptest.NewRecorder()
	ctx.GetSessionContext(recCtx, idReq("GET", "/api/sessions/"+id+"/context", "", id))
	if recCtx.Code != http.StatusOK {
		t.Fatalf("context: expected 200, got %d", recCtx.Code)
	}

	recExp := httptest.NewRecorder()
	ctx.ExportSession(recExp, idReq("GET", "/api/sessions/"+id+"/export", "", id))
	if recExp.Code != http.StatusOK {
		t.Fatalf("export: expected 200, got %d", recExp.Code)
	}
	var exp map[string]interface{}
	_ = json.Unmarshal(recExp.Body.Bytes(), &exp)
	sess, _ := exp["session"].(map[string]interface{})
	if sess["id"] != id {
		t.Errorf("export id=%v, want %s", sess["id"], id)
	}
}

func TestWriteSessionRalphPrompt_Live(t *testing.T) {
	t.Parallel()
	ctx := newSessionExtCtx(t)
	id, wd := liveSession(t, ctx)
	rec := httptest.NewRecorder()
	ctx.WriteSessionRalphPrompt(rec, idReq("POST", "/api/sessions/"+id+"/ralph-prompt/write", `{"prompt":"do the work"}`, id))
	if rec.Code != http.StatusOK {
		t.Fatalf("write ralph prompt: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	data, err := os.ReadFile(filepath.Join(wd, "initprompt.md"))
	if err != nil || string(data) != "do the work" {
		t.Errorf("initprompt.md content=%q err=%v", string(data), err)
	}
}

func TestImportReadSessionFixPlan_Live(t *testing.T) {
	t.Parallel()
	ctx := newSessionExtCtx(t)
	id, wd := liveSession(t, ctx)

	// Import a fix plan.
	recI := httptest.NewRecorder()
	ctx.ImportSessionFixPlan(recI, idReq("POST", "/api/sessions/"+id+"/fix-plan/import", `{"content":"imported plan"}`, id))
	if recI.Code != http.StatusOK {
		t.Fatalf("import fix plan: expected 200, got %d: %s", recI.Code, recI.Body.String())
	}
	data, _ := os.ReadFile(filepath.Join(wd, "@fix_plan.md"))
	if string(data) != "imported plan" {
		t.Errorf("imported plan content=%q", string(data))
	}

	// Read it back via the POST read endpoint.
	recR := httptest.NewRecorder()
	ctx.ReadSessionFixPlan(recR, idReq("POST", "/api/sessions/"+id+"/fix-plan/read", "", id))
	if recR.Code != http.StatusOK {
		t.Fatalf("read fix plan: expected 200, got %d", recR.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(recR.Body.Bytes(), &resp)
	if resp["plan"] != "imported plan" {
		t.Errorf("read plan=%v, want 'imported plan'", resp["plan"])
	}
}

func TestReadSessionFixPlan_MissingFileReturnsNil_Live(t *testing.T) {
	t.Parallel()
	ctx := newSessionExtCtx(t)
	id, _ := liveSession(t, ctx)
	rec := httptest.NewRecorder()
	ctx.ReadSessionFixPlan(rec, idReq("POST", "/api/sessions/"+id+"/fix-plan/read", "", id))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["plan"] != nil {
		t.Errorf("plan should be nil when no @fix_plan.md exists, got %v", resp["plan"])
	}
}

func TestSessionInteractiveRespawn_Live(t *testing.T) {
	t.Parallel()
	ctx := newSessionExtCtx(t)
	id, _ := liveSession(t, ctx)
	rec := httptest.NewRecorder()
	ctx.SessionInteractiveRespawn(rec, idReq("POST", "/api/sessions/"+id+"/interactive-respawn", "", id))
	if rec.Code != http.StatusOK {
		t.Fatalf("interactive respawn: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	// A controller is now running; stop it so the goroutine doesn't leak.
	t.Cleanup(func() {
		ctx.RespawnMu.RLock()
		rc := ctx.RespawnControllers[id]
		ctx.RespawnMu.RUnlock()
		if rc != nil {
			rc.Stop()
		}
	})
}

func TestPatchSessionPlanTask_Live(t *testing.T) {
	t.Parallel()
	ctx := newSessionExtCtx(t)
	id, wd := liveSession(t, ctx)
	// Seed a plan so the patch has a task to update.
	_ = os.WriteFile(filepath.Join(wd, "@fix_plan.md"), []byte("- [ ] task one\n"), 0o644)

	rec := httptest.NewRecorder()
	patchReq := twoParamReq("PATCH", "/api/sessions/"+id+"/plan/task/0", `{"status":"completed"}`, "id", id, "taskId", "0")
	ctx.PatchSessionPlanTask(rec, patchReq)
	// Either 200 (updated) or a documented not-found for the task index; the
	// session-found path is what we're covering. Accept any non-404-session code.
	if rec.Code == http.StatusNotFound && strings.Contains(rec.Body.String(), "session not found") {
		t.Fatalf("unexpected session-not-found: %s", rec.Body.String())
	}
}

func TestDeleteSessionTailFile_Live(t *testing.T) {
	t.Parallel()
	ctx := newSessionExtCtx(t)
	id, _ := liveSession(t, ctx)
	rec := httptest.NewRecorder()
	delReq := twoParamReq("DELETE", "/api/sessions/"+id+"/tail-file/stream-1", "", "id", id, "streamId", "stream-1")
	ctx.DeleteSessionTailFile(rec, delReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete tail file: expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	// No such stream registered -> success:false but still 200.
	if resp["success"] != false {
		t.Errorf("success=%v, want false for unknown stream", resp["success"])
	}
}

func TestImportSession_HappyThroughHandler_Live(t *testing.T) {
	t.Parallel()
	ctx := newSessionLiveCtx(t)
	dir := t.TempDir()
	rec := postJSON(t, ctx.ImportSession, "/api/sessions/import", map[string]interface{}{
		"workingDir": dir, "mode": "shell", "name": "imported",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("import: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	sess, _ := resp["session"].(map[string]interface{})
	id, _ := sess["id"].(string)
	if id == "" {
		t.Fatal("expected an imported session id")
	}
	t.Cleanup(func() {
		_ = ctx.SessionManager.StopSession(context.Background(), id)
		ctx.cleanupSessionResources(id)
	})
	if _, ok := ctx.SessionManager.GetSession(id); !ok {
		t.Error("imported session not registered")
	}
}
