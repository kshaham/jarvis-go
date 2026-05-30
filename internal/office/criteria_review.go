package office

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"
)

var criteriaBinary = "claude"

// SetCriteriaBinaryForTest swaps the evaluator binary; returns the previous value.
func SetCriteriaBinaryForTest(path string) (previous string) {
	previous = criteriaBinary
	criteriaBinary = path
	return previous
}

// criteriaTimeout is intentionally much shorter than ultrareviewTimeout (10m):
// the gated path runs this synchronously on the completion request.
var criteriaTimeout = 90 * time.Second

// CriterionResult holds the evaluator's verdict for a single criterion.
type CriterionResult struct {
	Criterion string `json:"criterion"`
	Pass      bool   `json:"pass"`
	Reason    string `json:"reason"`
}

// CriteriaVerdict is the full evaluator response.
type CriteriaVerdict struct {
	Results        []CriterionResult `json:"results"`
	AllPass        bool              `json:"allPass"`
	EvaluatorError string            `json:"-"` // set when evaluator infra failed (fail-open)
}

// EvaluateAcceptanceCriteria runs the evaluator binary in worktreeDir and returns a
// verdict. It NEVER returns a hard error for evaluator/parse failures — those set
// EvaluatorError so callers can fail-open. A nil error always accompanies a usable verdict.
func EvaluateAcceptanceCriteria(ctx context.Context, worktreeDir, summary string, criteria []string) (CriteriaVerdict, error) {
	cctx, cancel := context.WithTimeout(ctx, criteriaTimeout)
	defer cancel()
	prompt := buildCriteriaPrompt(summary, criteria)
	cmd := exec.CommandContext(cctx, criteriaBinary, "-p", prompt, "--output-format", "json")
	cmd.Dir = worktreeDir
	out, err := cmd.Output()
	if err != nil {
		return CriteriaVerdict{EvaluatorError: "evaluator exec failed: " + err.Error()}, nil
	}
	// `claude -p --output-format json` wraps output in {"result":"<json string>"}.
	// Probe envelope first (mirroring internal/library/llm_extract.go); fall back to
	// bare JSON. Any parse failure is fail-open (EvaluatorError set, nil error).
	var envelope struct {
		Result string `json:"result"`
	}
	if jErr := json.Unmarshal(out, &envelope); jErr == nil && envelope.Result != "" {
		var v CriteriaVerdict
		if derr := json.Unmarshal([]byte(stripCriteriaFence(envelope.Result)), &v); derr != nil {
			return CriteriaVerdict{EvaluatorError: "unparseable evaluator result"}, nil
		}
		return v, nil
	}
	var v CriteriaVerdict
	if jErr := json.Unmarshal(out, &v); jErr != nil {
		return CriteriaVerdict{EvaluatorError: "unparseable evaluator output"}, nil
	}
	return v, nil
}

// stripCriteriaFence removes a leading/trailing ```json ... ``` fence if present.
func stripCriteriaFence(s string) string {
	t := strings.TrimSpace(s)
	if strings.HasPrefix(t, "```") {
		if i := strings.IndexByte(t, '\n'); i >= 0 {
			t = t[i+1:]
		}
		t = strings.TrimSuffix(strings.TrimSpace(t), "```")
	}
	return strings.TrimSpace(t)
}

// buildCriteriaPrompt constructs the evaluator prompt for the given summary and criteria.
func buildCriteriaPrompt(summary string, criteria []string) string {
	const schema = `{"results":[{"criterion":string,"pass":bool,"reason":string}],"allPass":bool}`
	var b strings.Builder
	b.WriteString("You are reviewing whether completed work satisfies its acceptance criteria. ")
	b.WriteString("Inspect the working tree (diff, tests) as needed. Return ONLY JSON: ")
	b.WriteString(schema)
	b.WriteString("\n\nCompletion summary:\n")
	b.WriteString(summary)
	b.WriteString("\n\nAcceptance criteria:\n")
	for _, c := range criteria {
		b.WriteString("- ")
		b.WriteString(c)
		b.WriteString("\n")
	}
	return b.String()
}
