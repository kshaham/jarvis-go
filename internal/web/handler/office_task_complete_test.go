package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/claudeman/jarvis-go/internal/office"
	"github.com/claudeman/jarvis-go/internal/testutil/stubbin"
	"github.com/claudeman/jarvis-go/internal/web/sse"
)

// TestTaskHasNeedsReviewLabel_OnRealTaskShape is a regression guard
// against the contract `OfficeAgentTaskComplete` relies on when
// deciding whether to fire the R3.4 ultrareview goroutine: the
// handler iterates `agent.GetTasks()` and passes each `*office.Task`
// to `office.TaskHasNeedsReviewLabel`. The full handler integration
// test would require a stubbed Context with auth + SSE + a fake
// claude binary; the goroutine is also async, making it brittle. This
// test pins the smallest invariant — the label decision itself —
// against the exact shape the handler hands in. Extends the coverage
// already in internal/office/ultrareview_test.go.
func TestTaskHasNeedsReviewLabel_OnRealTaskShape(t *testing.T) {
	cases := []struct {
		name   string
		labels []string
		want   bool
	}{
		{"single needs-review", []string{"needs-review"}, true},
		{"mixed with needs-review", []string{"perf", "needs-review", "urgent"}, true},
		{"different case", []string{"Needs-Review"}, true},
		{"unrelated only", []string{"perf"}, false},
		{"empty", []string{}, false},
		{"trailing whitespace stripped by NormalizeTaskLabel", []string{"  needs-review  "}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tk := &office.Task{ID: "t", Title: "y", Labels: tc.labels}
			got := office.TaskHasNeedsReviewLabel(tk)
			if got != tc.want {
				t.Errorf("TaskHasNeedsReviewLabel(%v) = %v, want %v", tc.labels, got, tc.want)
			}
		})
	}
}

// eventRecorder hooks an SSE broker and tracks which event names
// have fired. Goroutines write under a mutex; waitFor blocks (with
// timeout) until the named event has been observed at least once.
type eventRecorder struct {
	mu    sync.Mutex
	count map[string]int
}

func newEventRecorder(b *sse.Broker) *eventRecorder {
	r := &eventRecorder{count: map[string]int{}}
	b.AddPostBroadcastHook(func(msg sse.EventMessage) {
		r.mu.Lock()
		r.count[msg.Event]++
		r.mu.Unlock()
	})
	return r
}

func (r *eventRecorder) waitFor(event string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		seen := r.count[event] > 0
		r.mu.Unlock()
		if seen {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func (r *eventRecorder) total(event string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.count[event]
}

// TestOfficeAgentTaskComplete_UltrareviewFiresOnLabel exercises the
// async ultrareview goroutine end-to-end through the handler. Stubs
// `claude` to emit a minimal valid ultrareview payload so the
// goroutine completes without a real Claude install, then polls the
// SSE broker for the broadcast.
func TestOfficeAgentTaskComplete_UltrareviewFiresOnLabel(t *testing.T) {
	stub := stubbin.EchoStub(t, `{"findings":[{"severity":"nit","title":"x"}],"summary":"ok"}`)
	prev := office.SetUltrareviewBinaryForTest(stub)
	t.Cleanup(func() { office.SetUltrareviewBinaryForTest(prev) })

	ctx := newSnapshotCtx(t)
	recorder := newEventRecorder(ctx.SSEBroker)

	// Register an agent + give it a worktree path so the goroutine
	// has somewhere to run (it shells out `claude ultrareview` in
	// that cwd). t.TempDir is a real dir so the cwd resolves.
	wd := t.TempDir()
	ctx.OfficeManager.RegisterAgent("sess-vex", "cfg-vex", &office.TeamMemberConfig{
		ID: "cfg-vex", Name: "Vex", Role: "scout",
	})
	agent := ctx.OfficeManager.GetAgent("sess-vex")
	agent.SetWorktreePath(wd)
	agent.AddTask(&office.Task{
		ID:     "t-1",
		Title:  "do a thing",
		Status: office.TaskInProgress,
		Labels: []string{"needs-review"},
	})

	body, _ := json.Marshal(map[string]interface{}{
		"sessionId": "sess-vex",
		"taskId":    "t-1",
		"result":    "done",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/office/agent/task/complete",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.OfficeAgentTaskComplete(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from task-complete, got %d: %s", rec.Code, rec.Body.String())
	}

	if !recorder.waitFor("office:taskReviewed", 3*time.Second) {
		t.Fatal("office:taskReviewed never fired within 3s")
	}
}

// Companion: NO label, NO ultrareview broadcast.
func TestOfficeAgentTaskComplete_NoLabelSkipsUltrareview(t *testing.T) {
	// Even though we install a stub, the handler must not call it
	// when the task lacks the needs-review label.
	stub := stubbin.EchoStub(t, `{"findings":[],"summary":"ok"}`)
	prev := office.SetUltrareviewBinaryForTest(stub)
	t.Cleanup(func() { office.SetUltrareviewBinaryForTest(prev) })

	ctx := newSnapshotCtx(t)
	recorder := newEventRecorder(ctx.SSEBroker)

	ctx.OfficeManager.RegisterAgent("sess-bolt", "cfg-bolt", &office.TeamMemberConfig{
		ID: "cfg-bolt", Name: "Bolt",
	})
	agent := ctx.OfficeManager.GetAgent("sess-bolt")
	agent.SetWorktreePath(t.TempDir())
	agent.AddTask(&office.Task{
		ID:     "t-2",
		Title:  "no review here",
		Status: office.TaskInProgress,
		Labels: []string{"perf"}, // explicitly NOT needs-review
	})

	body, _ := json.Marshal(map[string]interface{}{
		"sessionId": "sess-bolt",
		"taskId":    "t-2",
		"result":    "done",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/office/agent/task/complete",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.OfficeAgentTaskComplete(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Give the goroutine a beat — should still not fire.
	time.Sleep(250 * time.Millisecond)
	if recorder.total("office:taskReviewed") != 0 {
		t.Errorf("office:taskReviewed fired %d times without label", recorder.total("office:taskReviewed"))
	}
}
