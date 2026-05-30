package office

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFakeClaude writes a shell script that echoes the given stdout and exits 0.
func writeFakeClaude(t *testing.T, stdout string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "fake-claude")
	script := "#!/bin/sh\ncat <<'EOF'\n" + stdout + "\nEOF\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestEvaluateAcceptanceCriteriaAllPass(t *testing.T) {
	fake := writeFakeClaude(t, `{"results":[{"criterion":"tests pass","pass":true,"reason":"ok"}],"allPass":true}`)
	prev := SetCriteriaBinaryForTest(fake)
	defer SetCriteriaBinaryForTest(prev)
	v, err := EvaluateAcceptanceCriteria(context.Background(), t.TempDir(), "done", []string{"tests pass"})
	if err != nil {
		t.Fatal(err)
	}
	if !v.AllPass || len(v.Results) != 1 || !v.Results[0].Pass {
		t.Fatalf("unexpected verdict: %#v", v)
	}
}

// The REAL `claude -p --output-format json` wraps output in an envelope
// {"result":"<json string>"}. This test asserts the evaluator unwraps it.
func TestEvaluateAcceptanceCriteriaEnvelope(t *testing.T) {
	fake := writeFakeClaude(t, `{"result":"{\"results\":[{\"criterion\":\"x\",\"pass\":true,\"reason\":\"ok\"}],\"allPass\":true}"}`)
	prev := SetCriteriaBinaryForTest(fake)
	defer SetCriteriaBinaryForTest(prev)
	v, err := EvaluateAcceptanceCriteria(context.Background(), t.TempDir(), "done", []string{"x"})
	if err != nil {
		t.Fatal(err)
	}
	if !v.AllPass || len(v.Results) != 1 {
		t.Fatalf("envelope not unwrapped: %#v", v)
	}
}

func TestEvaluateAcceptanceCriteriaFailOpenOnBadJSON(t *testing.T) {
	fake := writeFakeClaude(t, `not json`)
	prev := SetCriteriaBinaryForTest(fake)
	defer SetCriteriaBinaryForTest(prev)
	v, err := EvaluateAcceptanceCriteria(context.Background(), t.TempDir(), "done", []string{"x"})
	if err != nil {
		t.Fatalf("must not hard-error on bad JSON: %v", err)
	}
	if v.EvaluatorError == "" {
		t.Fatalf("expected EvaluatorError set on unparseable output")
	}
}

// TestEvaluateAcceptanceCriteriaErrorEnvelope verifies that an envelope with
// is_error:true produces a diagnosable EvaluatorError (fail-open, nil error).
func TestEvaluateAcceptanceCriteriaErrorEnvelope(t *testing.T) {
	fake := writeFakeClaude(t, `{"is_error":true,"result":"rate limit exceeded"}`)
	prev := SetCriteriaBinaryForTest(fake)
	defer SetCriteriaBinaryForTest(prev)
	v, err := EvaluateAcceptanceCriteria(context.Background(), t.TempDir(), "done", []string{"x"})
	if err != nil {
		t.Fatalf("must not hard-error on error envelope: %v", err)
	}
	if v.EvaluatorError == "" {
		t.Fatal("EvaluatorError must be set when envelope has is_error:true")
	}
	if !strings.Contains(v.EvaluatorError, "rate limit") {
		t.Errorf("EvaluatorError %q does not contain 'rate limit'", v.EvaluatorError)
	}
}
