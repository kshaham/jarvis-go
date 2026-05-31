package office

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFailingCriteriaBinary writes a script that exits non-zero so
// cmd.Output() returns an *exec.ExitError.
func writeFailingCriteriaBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "fail-claude")
	if err := os.WriteFile(p, []byte("#!/bin/sh\necho boom 1>&2\nexit 7\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestEvaluateAcceptanceCriteria_ExecFails drives the cmd.Output error
// branch (criteria_review.go ~48-50): a non-zero-exit evaluator binary
// makes cmd.Output return an error, and the function fails open with the
// EvaluatorError set and a nil error.
func TestEvaluateAcceptanceCriteria_ExecFails(t *testing.T) {
	fake := writeFailingCriteriaBinary(t)
	prev := SetCriteriaBinaryForTest(fake)
	defer SetCriteriaBinaryForTest(prev)

	v, err := EvaluateAcceptanceCriteria(context.Background(), t.TempDir(), "done", []string{"x"})
	if err != nil {
		t.Fatalf("must fail open (nil error) on exec failure, got %v", err)
	}
	if v.EvaluatorError == "" {
		t.Fatal("expected EvaluatorError set when the evaluator exec fails")
	}
	if !strings.Contains(v.EvaluatorError, "evaluator exec failed") {
		t.Errorf("EvaluatorError %q should mention exec failure", v.EvaluatorError)
	}
}

// TestEvaluateAcceptanceCriteria_UnparseableEnvelopeResult drives the
// inner json.Unmarshal failure branch (criteria_review.go ~63-65): the
// envelope parses and has a non-empty Result, but the Result string
// itself is NOT valid JSON, so the inner unmarshal fails and the
// function reports "unparseable evaluator result".
func TestEvaluateAcceptanceCriteria_UnparseableEnvelopeResult(t *testing.T) {
	// Envelope with result = a non-JSON string.
	fake := writeFakeClaude(t, `{"result":"this is not json at all"}`)
	prev := SetCriteriaBinaryForTest(fake)
	defer SetCriteriaBinaryForTest(prev)

	v, err := EvaluateAcceptanceCriteria(context.Background(), t.TempDir(), "done", []string{"x"})
	if err != nil {
		t.Fatalf("must fail open on unparseable envelope result, got %v", err)
	}
	if v.EvaluatorError != "unparseable evaluator result" {
		t.Errorf("expected 'unparseable evaluator result', got %q", v.EvaluatorError)
	}
}
