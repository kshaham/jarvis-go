package office

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/library"
	"github.com/kshaham/jarvis/internal/testutil/stubbin"
)

// TestSetUltrareviewBinaryForTest covers the cross-package test helper that
// swaps the binary and returns the previous value.
func TestSetUltrareviewBinaryForTest(t *testing.T) {
	orig := ultrareviewBinary
	prev := SetUltrareviewBinaryForTest("/fake/claude")
	if prev != orig {
		t.Errorf("expected previous=%q, got %q", orig, prev)
	}
	if ultrareviewBinary != "/fake/claude" {
		t.Errorf("expected binary swapped, got %q", ultrareviewBinary)
	}
	// Restore.
	SetUltrareviewBinaryForTest(orig)
	if ultrareviewBinary != orig {
		t.Error("expected binary restored")
	}
}

// TestRunUltrareview_CommandFails drives the non-zero-exit error branch.
func TestRunUltrareview_CommandFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script stub not supported on Windows")
	}
	orig := ultrareviewBinary
	t.Cleanup(func() { ultrareviewBinary = orig })
	ultrareviewBinary = stubbin.FailStub(t, 3, "review crashed")

	if _, err := RunUltrareview(context.Background(), t.TempDir()); err == nil {
		t.Fatal("expected error when ultrareview binary exits non-zero")
	}
}

// TestRunUltrareview_BadJSON drives the JSON-decode error branch.
func TestRunUltrareview_BadJSON(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script stub not supported on Windows")
	}
	orig := ultrareviewBinary
	t.Cleanup(func() { ultrareviewBinary = orig })
	ultrareviewBinary = stubbin.EchoStub(t, "this is not json")

	if _, err := RunUltrareview(context.Background(), t.TempDir()); err == nil {
		t.Fatal("expected JSON decode error")
	}
}

// TestCountBySeverity_EmptySeverityDefaultsToInfo drives the key=="" → info
// default.
func TestCountBySeverity_EmptySeverityDefaultsToInfo(t *testing.T) {
	r := &UltrareviewResult{Findings: []UltrareviewFinding{
		{Severity: ""},        // → "info"
		{Severity: "blocker"}, // explicit
		{Severity: ""},        // → "info"
	}}
	counts := r.CountBySeverity()
	if counts["info"] != 2 {
		t.Errorf("expected 2 info (empty severities), got %d", counts["info"])
	}
	if counts["blocker"] != 1 {
		t.Errorf("expected 1 blocker, got %d", counts["blocker"])
	}
}

// TestTaskHasGatedLabel_Nil covers the nil-task guard.
func TestTaskHasGatedLabel_Nil(t *testing.T) {
	if TaskHasGatedLabel(nil) {
		t.Error("expected false for nil task")
	}
	if !TaskHasGatedLabel(&Task{Labels: []string{"gated"}}) {
		t.Error("expected true for gated-labeled task")
	}
}

// TestRunUltrareviewAndBroadcastForAgent_AppendsToLibrary drives the
// configID != "" branch (calls appendUltrareviewToLibrary) on the happy
// path. The async library append (safego goroutine) is drained
// deterministically by seeding the experience file and polling until the
// goroutine has appended its ultrareview section — this both exercises the
// library-write path AND prevents the goroutine from racing the test's
// TempDir teardown ("directory not empty").
func TestRunUltrareviewAndBroadcastForAgent_AppendsToLibrary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script stub not supported on Windows")
	}
	t.Setenv("HOME", t.TempDir())
	orig := ultrareviewBinary
	t.Cleanup(func() { ultrareviewBinary = orig })
	ultrareviewBinary = stubbin.EchoStubReadStdin(t, `{"findings":[{"severity":"warning","title":"x"}],"summary":"ok"}`)

	// Resolve the library root (creates its subdirs) and seed an experience
	// file so the async AppendUltrareview write actually lands somewhere we
	// can poll on.
	root := library.Dir()
	if root == "" {
		t.Skip("library dir unavailable")
	}
	expDir := filepath.Join(root, "experiences", "cfg-1")
	if err := os.MkdirAll(expDir, 0o755); err != nil {
		t.Fatal(err)
	}
	expPath := filepath.Join(expDir, "task-1.md")
	seed := "# Experience\n"
	if err := os.WriteFile(expPath, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	var gotOK bool
	RunUltrareviewAndBroadcastForAgent(context.Background(), t.TempDir(), "cfg-1", "sess-1", "task-1",
		func(event string, data interface{}) {
			if m, ok := data.(map[string]interface{}); ok {
				if okv, _ := m["ok"].(bool); okv {
					gotOK = true
				}
			}
		})
	if !gotOK {
		t.Error("expected a successful office:taskReviewed broadcast")
	}

	// Drain the async library-append goroutine deterministically: poll until
	// the experience file has grown past the seed (the goroutine appended its
	// ultrareview section). This guarantees the goroutine has finished its
	// disk write before the test (and its TempDir cleanup) returns.
	deadline := time.Now().Add(2 * time.Second)
	appended := false
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(expPath); err == nil && len(data) > len(seed) {
			appended = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !appended {
		t.Error("expected the async library append to grow the experience file")
	}
}
