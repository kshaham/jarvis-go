package office

// R3.4 · `claude ultrareview` integration. When a task tagged
// `needs-review` is completed, jarvis shells out to
//
//   claude ultrareview --json --timeout 10
//
// inside the agent's worktree to produce a structured review of the
// diff. Results land as a new SSE event (office:taskReviewed) +
// append to the task's progress log so operators see the findings in
// the task detail modal.
//
// Why not the SDK: ultrareview is a CLI-only subcommand at the time
// of writing. The SDK doesn't expose it as an option. Shelling out is
// the only path. We keep the call short (10-minute timeout, max ~$0
// because subscription plans don't bill per turn, sane stderr cap)
// so a hung review never wedges the task-complete handler.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"time"

	"github.com/kshaham/jarvis/internal/library"
	"github.com/kshaham/jarvis/internal/logscrub"
	"github.com/kshaham/jarvis/internal/safego"
)

// NeedsReviewLabel is the label that triggers ultrareview. Tasks
// without it skip review entirely.
const NeedsReviewLabel = "needs-review"

// ultrareviewBinary is the executable invoked. Variable (not const)
// so tests can swap in a stub that emits a scripted JSON response
// without needing a real Claude install. Same pattern as oneshot.go.
var ultrareviewBinary = "claude"

// SetUltrareviewBinaryForTest exposes the binary swap to consumers
// in other packages (handler tests that exercise the full
// task-complete → ultrareview goroutine integration). Returns the
// previous value so callers can restore in t.Cleanup. Production
// code must not call this.
func SetUltrareviewBinaryForTest(path string) (previous string) {
	previous = ultrareviewBinary
	ultrareviewBinary = path
	return previous
}

// ultrareviewTimeout caps the entire shellout. ultrareview itself
// defaults to 30 minutes; we shorten because a stuck review past 10
// minutes is more likely a hung process than a long review. Variable
// (not const) so tests can shorten it without sleeping for minutes.
var ultrareviewTimeout = 10 * time.Minute

// UltrareviewFinding mirrors the documented shape Claude's
// ultrareview emits in --json mode. We pull only the fields the
// dashboard renders today; extra fields pass through untouched in
// `Raw` so future UI work doesn't need a backend round-trip.
type UltrareviewFinding struct {
	Severity    string `json:"severity"`           // "blocker" | "warning" | "nit" | "info"
	File        string `json:"file,omitempty"`
	Line        int    `json:"line,omitempty"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Category    string `json:"category,omitempty"`
}

// UltrareviewResult is the parsed top-level ultrareview payload.
// Findings comes from the documented `findings` array; everything
// else passes through in Raw so we don't lose detail on schema
// extensions.
type UltrareviewResult struct {
	Findings []UltrareviewFinding `json:"findings"`
	Summary  string               `json:"summary,omitempty"`
	Took     time.Duration        `json:"-"`
	Raw      json.RawMessage      `json:"-"`
}

// RunUltrareviewAndBroadcast runs ultrareview, then emits a
// single office:taskReviewed SSE event with either the failure
// shape (ok:false + error message) or the success shape (findings,
// summary, severity counts, took-ms).
//
// Wraps the foreground RunUltrareview + the two distinct broadcast
// payloads that the handler used to inline. Caller still owns the
// goroutine + panic recovery — the broadcast is synchronous within
// this call.
//
// broadcast may be nil for tests that only want to exercise the
// RunUltrareview path without an SSE broker.
func RunUltrareviewAndBroadcast(ctx context.Context, cwd, sessionID, taskID string, broadcast SSEBroadcaster) {
	RunUltrareviewAndBroadcastForAgent(ctx, cwd, "", sessionID, taskID, broadcast)
}

// RunUltrareviewAndBroadcastForAgent is the configID-aware variant.
// configID enables a library.AppendUltrareview side effect — the
// findings stack as a "## Ultrareview" section on the agent's
// experience markdown so PageIndex can surface review history when
// future agents query the same task pattern.
//
// configID == "" preserves legacy callers; the library write is
// skipped in that case (no harm — broadcast still fires).
//
// The library write is wrapped in safego.Go so any I/O failure can't
// affect the broadcast or panic the daemon.
func RunUltrareviewAndBroadcastForAgent(ctx context.Context, cwd, configID, sessionID, taskID string, broadcast SSEBroadcaster) {
	result, err := RunUltrareview(ctx, cwd)
	if err != nil {
		if broadcast != nil {
			broadcast("office:taskReviewed", map[string]interface{}{
				"sessionId": sessionID,
				"taskId":    taskID,
				"ok":        false,
				"error":     err.Error(),
			})
		}
		return
	}
	if broadcast != nil {
		broadcast("office:taskReviewed", map[string]interface{}{
			"sessionId":   sessionID,
			"taskId":      taskID,
			"ok":          true,
			"findings":    result.Findings,
			"summary":     result.Summary,
			"counts":      result.CountBySeverity(),
			"hasBlockers": result.HasBlockers(),
			"tookMs":      result.Took.Milliseconds(),
		})
	}
	if configID != "" {
		appendUltrareviewToLibrary(configID, taskID, result)
	}
}

// appendUltrareviewToLibrary translates the office-package result
// into the library schema and writes it asynchronously. Failure to
// find the experience file is expected when the task was completed
// before the library was wired in (legacy runs) — logged at debug.
func appendUltrareviewToLibrary(configID, taskID string, result *UltrareviewResult) {
	libFindings := make([]library.Finding, 0, len(result.Findings))
	for _, f := range result.Findings {
		libFindings = append(libFindings, library.Finding{
			Severity:    f.Severity,
			File:        f.File,
			Line:        f.Line,
			Title:       f.Title,
			Description: f.Description,
			Category:    f.Category,
		})
	}
	rec := library.UltrareviewAppendRecord{
		AgentConfigID: configID,
		TaskID:        taskID,
		Findings:      libFindings,
		Summary:       result.Summary,
		Took:          result.Took,
		Timestamp:     time.Now(),
	}
	safego.Go("library.appendUltrareview", func() {
		if err := library.AppendUltrareview(rec); err != nil {
			slog.Debug("library ultrareview append skipped",
				"configId", configID, "taskId", taskID, "err", err)
		}
	})
}

// ErrUltrareviewTimeout is returned when the call exceeds
// ultrareviewTimeout. Callers can downgrade to a soft warning rather
// than a task-complete failure.
var ErrUltrareviewTimeout = errors.New("office: ultrareview timed out")

// RunUltrareview executes ultrareview inside cwd and parses the
// result. Pure function — does not mutate office state. The caller
// is responsible for persisting findings into the task / broadcasting
// the office:taskReviewed event.
func RunUltrareview(ctx context.Context, cwd string) (*UltrareviewResult, error) {
	if cwd == "" {
		return nil, errors.New("office: RunUltrareview requires a cwd")
	}
	callCtx, cancel := context.WithTimeout(ctx, ultrareviewTimeout)
	defer cancel()

	// `--json` flips ultrareview into structured-output mode (the
	// documented machine-readable format). `--timeout` is passed in
	// minutes because that's what the CLI accepts.
	args := []string{
		"ultrareview",
		"--json",
		"--timeout", strconv.Itoa(int(ultrareviewTimeout.Minutes())),
	}
	start := time.Now()
	cmd := exec.CommandContext(callCtx, ultrareviewBinary, args...)
	cmd.Dir = cwd
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	took := time.Since(start)
	if err != nil {
		if callCtx.Err() == context.DeadlineExceeded {
			return nil, ErrUltrareviewTimeout
		}
		return nil, fmt.Errorf("office: ultrareview failed (took %s): %w; stderr=%s", took, err, logscrub.All(truncateUltrareviewLog(stderr.String())))
	}
	out := stdout.Bytes()
	var parsed UltrareviewResult
	if jerr := json.Unmarshal(out, &parsed); jerr != nil {
		return nil, fmt.Errorf("office: ultrareview JSON decode failed: %w; raw=%s", jerr, truncateUltrareviewLog(string(out)))
	}
	parsed.Took = took
	parsed.Raw = json.RawMessage(out)
	slog.Info("ultrareview",
		"cwd", cwd,
		"findings", len(parsed.Findings),
		"took", took,
	)
	return &parsed, nil
}

// CountBySeverity returns a map of severity → finding count. Useful
// for the dashboard rollup ("3 blockers, 2 warnings, 1 nit").
func (r *UltrareviewResult) CountBySeverity() map[string]int {
	out := make(map[string]int)
	for _, f := range r.Findings {
		key := f.Severity
		if key == "" {
			key = "info"
		}
		out[key]++
	}
	return out
}

// HasBlockers reports whether any finding has severity "blocker".
// Drives the operator-facing "must fix before merge" indicator.
func (r *UltrareviewResult) HasBlockers() bool {
	for _, f := range r.Findings {
		if f.Severity == "blocker" {
			return true
		}
	}
	return false
}

// TaskHasNeedsReviewLabel returns true when the task carries the
// `needs-review` tag (case-insensitive match against any label).
// Public so the handler can decide whether to fire ultrareview
// without re-implementing the comparison.
func TaskHasNeedsReviewLabel(t *Task) bool {
	if t == nil {
		return false
	}
	for _, l := range t.Labels {
		if NormalizeTaskLabel(l) == NeedsReviewLabel {
			return true
		}
	}
	return false
}

func truncateUltrareviewLog(s string) string {
	const max = 4096
	if len(s) <= max {
		return s
	}
	return s[:max] + "…(truncated)"
}
