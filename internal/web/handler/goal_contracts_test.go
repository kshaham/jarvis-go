package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/testutil/stubbin"
	"github.com/kshaham/jarvis/internal/web/middleware"
	"github.com/kshaham/jarvis/internal/web/sse"
)

// captureSessionStore extends stubSessions to capture WriteInkPrompt calls.
type captureSessionStore struct {
	stubSessions
	prompts []string
}

func (c *captureSessionStore) WriteInkPrompt(sessionID, prompt string) bool {
	c.prompts = append(c.prompts, prompt)
	return true
}

// newCriteriaGateCtx builds a Context with a captureSessionStore so tests can
// observe WriteInkPrompt calls delivered back to the agent.
func newCriteriaGateCtx(t *testing.T, sess office.SessionStore) *Context {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Cleanup(func() { time.Sleep(100 * time.Millisecond) })
	mgr := office.NewManager(sess, func(string, interface{}) {}, nil)
	return &Context{
		SessionManager: pty.NewSessionManager(10),
		SSEBroker:      sse.NewBroker(100, 10),
		CSRFStore:      middleware.NewCSRFStore(context.Background()),
		OfficeManager:  mgr,
	}
}

// makePassVerdict returns bare-JSON for a criteria verdict with allPass=true.
func makePassVerdict(criteria []string) string {
	type cr struct {
		Criterion string `json:"criterion"`
		Pass      bool   `json:"pass"`
		Reason    string `json:"reason"`
	}
	type verdict struct {
		Results []cr `json:"results"`
		AllPass bool `json:"allPass"`
	}
	v := verdict{AllPass: true}
	for _, c := range criteria {
		v.Results = append(v.Results, cr{Criterion: c, Pass: true, Reason: "ok"})
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// makeFailVerdict returns bare-JSON for a criteria verdict with allPass=false.
func makeFailVerdict(criteria []string) string {
	type cr struct {
		Criterion string `json:"criterion"`
		Pass      bool   `json:"pass"`
		Reason    string `json:"reason"`
	}
	type verdict struct {
		Results []cr `json:"results"`
		AllPass bool `json:"allPass"`
	}
	v := verdict{AllPass: false}
	for _, c := range criteria {
		v.Results = append(v.Results, cr{Criterion: c, Pass: false, Reason: "not done"})
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// postTaskComplete fires OfficeAgentTaskComplete and returns the recorder.
func postTaskComplete(t *testing.T, ctx *Context, sessionID, taskID, result string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"sessionId": sessionID,
		"taskId":    taskID,
		"result":    result,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/office/agent/task/complete", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.OfficeAgentTaskComplete(rec, req)
	return rec
}

// seedGateAgent registers an agent with a worktree path and returns it.
func seedGateAgent(t *testing.T, ctx *Context, sessionID string) *office.AgentState {
	t.Helper()
	ctx.OfficeManager.RegisterAgent(sessionID, "cfg-"+sessionID, &office.TeamMemberConfig{
		ID: "cfg-" + sessionID, Name: sessionID, Role: "worker",
	})
	a := ctx.OfficeManager.GetAgent(sessionID)
	a.SetWorktreePath(t.TempDir())
	return a
}

// newInvariantsCtx builds an isolated Context for invariants handler tests.
func newInvariantsCtx(t *testing.T) *Context {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	mgr := office.NewManager(stubSessions{}, func(string, interface{}) {}, nil)
	return &Context{
		SessionManager: pty.NewSessionManager(10),
		SSEBroker:      sse.NewBroker(100, 10),
		CSRFStore:      middleware.NewCSRFStore(context.Background()),
		OfficeManager:  mgr,
	}
}

// jsonBody serialises v into a *bytes.Reader for use in httptest requests.
func jsonBody(t *testing.T, v interface{}) *bytes.Reader {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("jsonBody marshal: %v", err)
	}
	return bytes.NewReader(b)
}

// decodeMap decodes the JSON response body into a map.
func decodeMap(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var out map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v (%s)", err, rec.Body.String())
	}
	return out
}

// invariantsFromResp extracts the "invariants" slice from a decoded response map.
func invariantsFromResp(t *testing.T, m map[string]interface{}) []string {
	t.Helper()
	raw, ok := m["invariants"]
	if !ok {
		t.Fatalf("response missing 'invariants' key: %v", m)
	}
	arr, ok := raw.([]interface{})
	if !ok {
		t.Fatalf("'invariants' is not an array: %T %v", raw, raw)
	}
	out := make([]string, len(arr))
	for i, v := range arr {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("invariant[%d] is not a string: %T", i, v)
		}
		out[i] = s
	}
	return out
}

// TestInvariantsHandlersRoundTrip exercises the four invariants handlers.
func TestInvariantsHandlersRoundTrip(t *testing.T) {
	ctx := newInvariantsCtx(t)

	// --- PUT replaces the full list ---
	putReq := httptest.NewRequest(http.MethodPut, "/api/office/project/invariants",
		jsonBody(t, map[string]interface{}{"invariants": []string{"r1", "r2"}}))
	putReq.Header.Set("Content-Type", "application/json")
	putRec := httptest.NewRecorder()
	ctx.OfficePutProjectInvariants(putRec, putReq)
	if putRec.Code != http.StatusOK {
		t.Fatalf("PUT invariants: want 200, got %d: %s", putRec.Code, putRec.Body.String())
	}
	invs := invariantsFromResp(t, decodeMap(t, putRec))
	if len(invs) != 2 || invs[0] != "r1" || invs[1] != "r2" {
		t.Fatalf("PUT response invariants = %v, want [r1 r2]", invs)
	}

	// --- GET returns the same list ---
	getRec := httptest.NewRecorder()
	ctx.OfficeGetProjectInvariants(getRec, httptest.NewRequest(http.MethodGet, "/api/office/project/invariants", nil))
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET invariants: want 200, got %d", getRec.Code)
	}
	invs = invariantsFromResp(t, decodeMap(t, getRec))
	if len(invs) != 2 || invs[0] != "r1" || invs[1] != "r2" {
		t.Fatalf("GET invariants = %v, want [r1 r2]", invs)
	}

	// --- POST adds a third entry ---
	postReq := httptest.NewRequest(http.MethodPost, "/api/office/project/invariants",
		jsonBody(t, map[string]interface{}{"text": "r3"}))
	postReq.Header.Set("Content-Type", "application/json")
	postRec := httptest.NewRecorder()
	ctx.OfficePostProjectInvariant(postRec, postReq)
	if postRec.Code != http.StatusOK {
		t.Fatalf("POST invariant: want 200, got %d: %s", postRec.Code, postRec.Body.String())
	}
	invs = invariantsFromResp(t, decodeMap(t, postRec))
	if len(invs) != 3 {
		t.Fatalf("after POST: want 3 invariants, got %d: %v", len(invs), invs)
	}
	if !strings.Contains(strings.Join(invs, ","), "r3") {
		t.Fatalf("after POST: r3 not in list: %v", invs)
	}

	// --- GET confirms 3 entries ---
	getRec2 := httptest.NewRecorder()
	ctx.OfficeGetProjectInvariants(getRec2, httptest.NewRequest(http.MethodGet, "/api/office/project/invariants", nil))
	invs = invariantsFromResp(t, decodeMap(t, getRec2))
	if len(invs) != 3 {
		t.Fatalf("GET after POST: want 3, got %d", len(invs))
	}

	// --- DELETE removes index 2 (r3), leaving r1, r2 ---
	delReq := httptest.NewRequest(http.MethodDelete, "/api/office/project/invariants/2", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("index", "2")
	delReq = delReq.WithContext(context.WithValue(delReq.Context(), chi.RouteCtxKey, rctx))
	delRec := httptest.NewRecorder()
	ctx.OfficeDeleteProjectInvariant(delRec, delReq)
	if delRec.Code != http.StatusOK {
		t.Fatalf("DELETE invariant: want 200, got %d: %s", delRec.Code, delRec.Body.String())
	}
	invs = invariantsFromResp(t, decodeMap(t, delRec))
	if len(invs) != 2 {
		t.Fatalf("after DELETE: want 2 invariants, got %d: %v", len(invs), invs)
	}
	if invs[0] != "r1" || invs[1] != "r2" {
		t.Fatalf("after DELETE: want [r1 r2], got %v", invs)
	}

	// --- Final GET confirms state ---
	getRec3 := httptest.NewRecorder()
	ctx.OfficeGetProjectInvariants(getRec3, httptest.NewRequest(http.MethodGet, "/api/office/project/invariants", nil))
	invs = invariantsFromResp(t, decodeMap(t, getRec3))
	if len(invs) != 2 || invs[0] != "r1" || invs[1] != "r2" {
		t.Fatalf("final GET: want [r1 r2], got %v", invs)
	}
}

// TestOfficeDeleteProjectInvariant_OutOfRange checks 400 for bad index.
func TestOfficeDeleteProjectInvariant_OutOfRange(t *testing.T) {
	ctx := newInvariantsCtx(t)

	// DELETE with index 5 on empty list => 400
	delReq := httptest.NewRequest(http.MethodDelete, "/api/office/project/invariants/5", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("index", "5")
	delReq = delReq.WithContext(context.WithValue(delReq.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	ctx.OfficeDeleteProjectInvariant(rec, delReq)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for out-of-range index, got %d", rec.Code)
	}
}

// TestOfficeDeleteProjectInvariant_ParseError checks 400 for non-numeric index.
func TestOfficeDeleteProjectInvariant_ParseError(t *testing.T) {
	ctx := newInvariantsCtx(t)

	delReq := httptest.NewRequest(http.MethodDelete, "/api/office/project/invariants/notanumber", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("index", "notanumber")
	delReq = delReq.WithContext(context.WithValue(delReq.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	ctx.OfficeDeleteProjectInvariant(rec, delReq)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-numeric index, got %d", rec.Code)
	}
}

// criteriaFromResp extracts the "criteria" slice from a decoded response map.
func criteriaFromResp(t *testing.T, m map[string]interface{}) []string {
	t.Helper()
	raw, ok := m["criteria"]
	if !ok {
		t.Fatalf("response missing 'criteria' key: %v", m)
	}
	arr, ok := raw.([]interface{})
	if !ok {
		t.Fatalf("'criteria' is not an array: %T %v", raw, raw)
	}
	out := make([]string, len(arr))
	for i, v := range arr {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("criteria[%d] is not a string: %T", i, v)
		}
		out[i] = s
	}
	return out
}

// TestTaskCriteriaHandler exercises the OfficeAgentTaskCriteria handler.
func TestTaskCriteriaHandler(t *testing.T) {
	ctx := newInvariantsCtx(t)

	// Seed an agent + task.
	ctx.OfficeManager.RegisterAgent("s-vex", "vex", &office.TeamMemberConfig{ID: "vex", Tier: "worker"})
	agent := ctx.OfficeManager.GetAgent("s-vex")
	agent.AddTask(&office.Task{ID: "task-1", Status: office.TaskPending})

	// --- op:set replaces the list with two criteria ---
	rec := postJSON(t, ctx.OfficeAgentTaskCriteria, "/api/office/agent/task/criteria",
		map[string]interface{}{
			"sessionId": "s-vex",
			"taskId":    "task-1",
			"op":        "set",
			"criteria":  []string{"a", "b"},
		})
	if rec.Code != http.StatusOK {
		t.Fatalf("set: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	crit := criteriaFromResp(t, decodeMap(t, rec))
	if len(crit) != 2 || crit[0] != "a" || crit[1] != "b" {
		t.Fatalf("after set: want [a b], got %v", crit)
	}

	// --- op:add appends one criterion → 3 total ---
	rec = postJSON(t, ctx.OfficeAgentTaskCriteria, "/api/office/agent/task/criteria",
		map[string]interface{}{
			"sessionId": "s-vex",
			"taskId":    "task-1",
			"op":        "add",
			"criteria":  []string{"c"},
		})
	if rec.Code != http.StatusOK {
		t.Fatalf("add: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	crit = criteriaFromResp(t, decodeMap(t, rec))
	if len(crit) != 3 {
		t.Fatalf("after add: want 3 criteria, got %d: %v", len(crit), crit)
	}

	// --- op:remove index 0 → 2 remaining ---
	rec = postJSON(t, ctx.OfficeAgentTaskCriteria, "/api/office/agent/task/criteria",
		map[string]interface{}{
			"sessionId": "s-vex",
			"taskId":    "task-1",
			"op":        "remove",
			"criteria":  []string{"0"},
		})
	if rec.Code != http.StatusOK {
		t.Fatalf("remove: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	crit = criteriaFromResp(t, decodeMap(t, rec))
	if len(crit) != 2 {
		t.Fatalf("after remove: want 2, got %d: %v", len(crit), crit)
	}

	// --- unknown taskId → 404 ---
	rec = postJSON(t, ctx.OfficeAgentTaskCriteria, "/api/office/agent/task/criteria",
		map[string]interface{}{
			"sessionId": "s-vex",
			"taskId":    "no-such-task",
			"op":        "set",
			"criteria":  []string{"x"},
		})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown task: want 404, got %d: %s", rec.Code, rec.Body.String())
	}

	// --- bad op → 400 ---
	rec = postJSON(t, ctx.OfficeAgentTaskCriteria, "/api/office/agent/task/criteria",
		map[string]interface{}{
			"sessionId": "s-vex",
			"taskId":    "task-1",
			"op":        "replace",
			"criteria":  []string{"x"},
		})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad op: want 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Criteria gate tests (Task 8)
// ---------------------------------------------------------------------------

// TestCompletionGate_NoCriteriaUnchanged: a task without criteria completes
// exactly as today (status completed, HTTP 200).
func TestCompletionGate_NoCriteriaUnchanged(t *testing.T) {
	sess := &captureSessionStore{}
	ctx := newCriteriaGateCtx(t, sess)
	recorder := newEventRecorder(ctx.SSEBroker)
	_ = recorder

	a := seedGateAgent(t, ctx, "s-plain")
	a.AddTask(&office.Task{
		ID:     "t-plain",
		Title:  "plain task",
		Status: office.TaskInProgress,
		// no AcceptanceCriteria, no labels
	})

	rec := postTaskComplete(t, ctx, "s-plain", "t-plain", "done")
	if rec.Code != http.StatusOK {
		t.Fatalf("no-criteria: want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify task is completed.
	tasks := a.GetTasks()
	if len(tasks) == 0 {
		t.Fatal("no tasks found after completion")
	}
	if tasks[0].Status != office.TaskCompleted {
		t.Errorf("no-criteria: want status completed, got %s", tasks[0].Status)
	}
}

// TestCompletionGate_GatedFailKeepsInProgress: gated task with allPass=false
// and rejectCount 0 → HTTP 202, task stays in_progress, CriteriaRejectCount==1,
// office:taskCompletionRejected broadcast seen, no re-task.
func TestCompletionGate_GatedFailKeepsInProgress(t *testing.T) {
	stub := stubbin.EchoStub(t, makeFailVerdict([]string{"feature works"}))
	prev := office.SetCriteriaBinaryForTest(stub)
	t.Cleanup(func() { office.SetCriteriaBinaryForTest(prev) })

	sess := &captureSessionStore{}
	ctx := newCriteriaGateCtx(t, sess)
	recorder := newEventRecorder(ctx.SSEBroker)

	a := seedGateAgent(t, ctx, "s-gfail")
	a.AddTask(&office.Task{
		ID:                  "t-gfail",
		Title:               "gated task",
		Status:              office.TaskInProgress,
		AcceptanceCriteria:  []string{"feature works"},
		Labels:              []string{"gated"},
		CriteriaRejectCount: 0,
	})

	rec := postTaskComplete(t, ctx, "s-gfail", "t-gfail", "finished the feature")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("gated-fail: want 202, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["completed"] != false {
		t.Errorf("gated-fail: want completed=false, got %v", resp["completed"])
	}

	// Task must still be in_progress.
	tasks := a.GetTasks()
	if len(tasks) == 0 {
		t.Fatal("no tasks after gated-fail")
	}
	if tasks[0].Status != office.TaskInProgress {
		t.Errorf("gated-fail: want status in_progress, got %s", tasks[0].Status)
	}

	// CriteriaRejectCount should be 1.
	if tasks[0].CriteriaRejectCount != 1 {
		t.Errorf("gated-fail: want CriteriaRejectCount=1, got %d", tasks[0].CriteriaRejectCount)
	}

	// Broadcast must have fired.
	if !recorder.waitFor("office:taskCompletionRejected", 2*time.Second) {
		t.Fatal("office:taskCompletionRejected not broadcast")
	}

	// Reject prompt must have been delivered.
	if len(sess.prompts) == 0 {
		t.Error("gated-fail: want WriteInkPrompt called with reject prompt, got none")
	}
}

// TestCompletionGate_AdvisoryFailStillCompletes: advisory criteria (no gated
// label), allPass=false → task still completes (HTTP 200), and eventually
// office:taskCriteriaReviewed fires.
func TestCompletionGate_AdvisoryFailStillCompletes(t *testing.T) {
	stub := stubbin.EchoStub(t, makeFailVerdict([]string{"tests pass"}))
	prev := office.SetCriteriaBinaryForTest(stub)
	t.Cleanup(func() { office.SetCriteriaBinaryForTest(prev) })

	sess := &captureSessionStore{}
	ctx := newCriteriaGateCtx(t, sess)
	recorder := newEventRecorder(ctx.SSEBroker)

	a := seedGateAgent(t, ctx, "s-advisory")
	a.AddTask(&office.Task{
		ID:                 "t-advisory",
		Title:              "advisory task",
		Status:             office.TaskInProgress,
		AcceptanceCriteria: []string{"tests pass"},
		// no "gated" label
	})

	rec := postTaskComplete(t, ctx, "s-advisory", "t-advisory", "work done")
	if rec.Code != http.StatusOK {
		t.Fatalf("advisory-fail: want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Task must be completed.
	tasks := a.GetTasks()
	if len(tasks) == 0 {
		t.Fatal("no tasks after advisory completion")
	}
	if tasks[0].Status != office.TaskCompleted {
		t.Errorf("advisory-fail: want completed, got %s", tasks[0].Status)
	}

	// Async broadcast should fire eventually.
	if !recorder.waitFor("office:taskCriteriaReviewed", 3*time.Second) {
		t.Fatal("office:taskCriteriaReviewed not broadcast for advisory path")
	}
}

// TestCompletionGate_FailOpenCompletes: gated criteria + evaluator exits
// non-zero (EvaluatorError set) → fail-open → task completes (HTTP 200).
func TestCompletionGate_FailOpenCompletes(t *testing.T) {
	stub := stubbin.FailStub(t, 1, "internal error")
	prev := office.SetCriteriaBinaryForTest(stub)
	t.Cleanup(func() { office.SetCriteriaBinaryForTest(prev) })

	sess := &captureSessionStore{}
	ctx := newCriteriaGateCtx(t, sess)

	a := seedGateAgent(t, ctx, "s-failopen")
	a.AddTask(&office.Task{
		ID:                 "t-failopen",
		Title:              "fail-open task",
		Status:             office.TaskInProgress,
		AcceptanceCriteria: []string{"something"},
		Labels:             []string{"gated"},
	})

	rec := postTaskComplete(t, ctx, "s-failopen", "t-failopen", "done")
	if rec.Code != http.StatusOK {
		t.Fatalf("fail-open: want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	tasks := a.GetTasks()
	if len(tasks) == 0 {
		t.Fatal("no tasks after fail-open completion")
	}
	if tasks[0].Status != office.TaskCompleted {
		t.Errorf("fail-open: want completed, got %s", tasks[0].Status)
	}
}

// TestCompletionGate_CapEscalates: gated + allPass=false + CriteriaRejectCount==2
// → status becomes blocked, office:taskCompletionEscalated broadcast, no re-task.
func TestCompletionGate_CapEscalates(t *testing.T) {
	stub := stubbin.EchoStub(t, makeFailVerdict([]string{"feature works", "tests pass"}))
	prev := office.SetCriteriaBinaryForTest(stub)
	t.Cleanup(func() { office.SetCriteriaBinaryForTest(prev) })

	sess := &captureSessionStore{}
	ctx := newCriteriaGateCtx(t, sess)
	recorder := newEventRecorder(ctx.SSEBroker)

	a := seedGateAgent(t, ctx, "s-escalate")
	a.AddTask(&office.Task{
		ID:                  "t-escalate",
		Title:               "should escalate",
		Status:              office.TaskInProgress,
		AcceptanceCriteria:  []string{"feature works", "tests pass"},
		Labels:              []string{"gated"},
		CriteriaRejectCount: 2, // already at cap
	})

	rec := postTaskComplete(t, ctx, "s-escalate", "t-escalate", "tried again")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("escalate: want 202, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["escalated"] != true {
		t.Errorf("escalate: want escalated=true, got %v", resp["escalated"])
	}

	// Task must be blocked.
	tasks := a.GetTasks()
	if len(tasks) == 0 {
		t.Fatal("no tasks after escalation")
	}
	if tasks[0].Status != office.TaskBlocked {
		t.Errorf("escalate: want status blocked, got %s", tasks[0].Status)
	}

	// Escalated broadcast must fire.
	if !recorder.waitFor("office:taskCompletionEscalated", 2*time.Second) {
		t.Fatal("office:taskCompletionEscalated not broadcast")
	}

	// No reject prompt on escalation path.
	if len(sess.prompts) != 0 {
		t.Errorf("escalate: want no WriteInkPrompt, got %d prompt(s)", len(sess.prompts))
	}
}
