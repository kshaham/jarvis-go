package office

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/claudeman/jarvis-go/internal/testutil/stubbin"
)

func withUltrareviewStub(t *testing.T, output string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script stub not supported on Windows")
	}
	prev := ultrareviewBinary
	ultrareviewBinary = stubbin.EchoStubReadStdin(t, output)
	t.Cleanup(func() { ultrareviewBinary = prev })
}

func TestRunUltrareview_RequiresCwd(t *testing.T) {
	_, err := RunUltrareview(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty cwd")
	}
}

func TestRunUltrareview_ParsesFindings(t *testing.T) {
	withUltrareviewStub(t, `{"findings":[{"severity":"blocker","file":"a.go","line":42,"title":"oops"},{"severity":"nit","title":"style"}],"summary":"2 issues"}`)
	result, err := RunUltrareview(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("RunUltrareview: %v", err)
	}
	if len(result.Findings) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(result.Findings))
	}
	if result.Findings[0].Severity != "blocker" || result.Findings[0].File != "a.go" || result.Findings[0].Line != 42 {
		t.Errorf("blocker finding mismatch: %+v", result.Findings[0])
	}
	if result.Summary != "2 issues" {
		t.Errorf("summary mismatch: %q", result.Summary)
	}
	if !result.HasBlockers() {
		t.Error("HasBlockers should be true")
	}
	counts := result.CountBySeverity()
	if counts["blocker"] != 1 || counts["nit"] != 1 {
		t.Errorf("CountBySeverity mismatch: %+v", counts)
	}
	if result.Took <= 0 {
		t.Errorf("Took should be positive, got %v", result.Took)
	}
}

// captureBroadcast is a tiny SSEBroadcaster stub that records every
// call so the *AndBroadcast tests can assert what was emitted.
type capturedEvent struct {
	event string
	data  map[string]interface{}
}

func captureBroadcast(events *[]capturedEvent) SSEBroadcaster {
	return func(event string, data interface{}) {
		m, _ := data.(map[string]interface{})
		*events = append(*events, capturedEvent{event, m})
	}
}

func TestRunUltrareviewAndBroadcast_HappyPathEmitsSuccessShape(t *testing.T) {
	withUltrareviewStub(t, `{"findings":[{"severity":"blocker","title":"oops"}],"summary":"1 issue"}`)
	var events []capturedEvent
	RunUltrareviewAndBroadcast(context.Background(), t.TempDir(), "sess-aaa-0001", "task-123", captureBroadcast(&events))
	if len(events) != 1 {
		t.Fatalf("expected 1 SSE event, got %d", len(events))
	}
	if events[0].event != "office:taskReviewed" {
		t.Errorf("event=%q want office:taskReviewed", events[0].event)
	}
	if ok, _ := events[0].data["ok"].(bool); !ok {
		t.Errorf("ok=%v want true", events[0].data["ok"])
	}
	if events[0].data["sessionId"] != "sess-aaa-0001" {
		t.Errorf("sessionId=%v", events[0].data["sessionId"])
	}
	if events[0].data["taskId"] != "task-123" {
		t.Errorf("taskId=%v", events[0].data["taskId"])
	}
	if events[0].data["summary"] != "1 issue" {
		t.Errorf("summary=%v", events[0].data["summary"])
	}
	if hb, _ := events[0].data["hasBlockers"].(bool); !hb {
		t.Errorf("hasBlockers=%v want true", events[0].data["hasBlockers"])
	}
}

func TestRunUltrareviewAndBroadcast_EmptyCwdEmitsFailureShape(t *testing.T) {
	var events []capturedEvent
	RunUltrareviewAndBroadcast(context.Background(), "", "sess-001", "task-1", captureBroadcast(&events))
	if len(events) != 1 {
		t.Fatalf("expected 1 SSE event, got %d", len(events))
	}
	if ok, _ := events[0].data["ok"].(bool); ok {
		t.Errorf("ok=%v want false", events[0].data["ok"])
	}
	if events[0].data["error"] == nil {
		t.Error("error field missing on failure shape")
	}
}

func TestRunUltrareviewAndBroadcast_NilBroadcasterIsNoOp(t *testing.T) {
	// Documented contract: passing nil is safe — useful for tests
	// that exercise the RunUltrareview path without an SSE broker.
	// Should not panic.
	RunUltrareviewAndBroadcast(context.Background(), "", "sess-001", "task-1", nil)
}

func TestRunUltrareview_TimeoutReturnsSentinel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script stub not supported on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "slow.sh")
	body := "#!/bin/sh\nsleep 5\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	prev := ultrareviewBinary
	prevTimeout := ultrareviewTimeout
	ultrareviewBinary = path
	ultrareviewTimeout = 50_000_000 // 50ms in ns
	t.Cleanup(func() {
		ultrareviewBinary = prev
		ultrareviewTimeout = prevTimeout
	})
	_, err := RunUltrareview(context.Background(), t.TempDir())
	if !errors.Is(err, ErrUltrareviewTimeout) {
		t.Fatalf("expected ErrUltrareviewTimeout, got %v", err)
	}
}

func TestTaskHasNeedsReviewLabel(t *testing.T) {
	if TaskHasNeedsReviewLabel(nil) {
		t.Error("nil task should not match")
	}
	if TaskHasNeedsReviewLabel(&Task{}) {
		t.Error("task without labels should not match")
	}
	if !TaskHasNeedsReviewLabel(&Task{Labels: []string{"other", "Needs-Review"}}) {
		t.Error("case-insensitive label should match")
	}
	if TaskHasNeedsReviewLabel(&Task{Labels: []string{"misc"}}) {
		t.Error("unrelated label should not match")
	}
}
