package library

// LLM-driven workflow extraction.
//
// Memify's heuristic path (clusterExperiences + buildWorkflowFromCluster)
// already produces a serviceable workflow doc: it groups recent
// high-outcome experiences by (role, labels) and pulls top-3
// approach summaries into the Steps field. The LLM extractor here
// is the enrichment layer: it takes the same cluster and synthesises
// a more cohesive workflow — numbered steps written as prose, named
// pitfalls drawn from outcome variance across the cluster, a
// human-readable summary.
//
// Cost discipline (the user runs subscription billing so per-call
// cost is effectively zero, but throughput still matters):
//   - One LLM call per qualifying cluster, hard-capped per memify
//     tick (default 5 — see MemifyOptions.LLMBudget).
//   - Timeout-bounded (`llmExtractTimeout`) so a hung `claude` can't
//     wedge the memify goroutine.
//   - Any error (timeout, parse failure, missing binary) falls
//     back silently to the heuristic Workflow that memify would
//     have written anyway. LLM is enrichment, never required.
//
// Test seam: llmBinary + llmExtractTimeout are vars so stubbin tests
// can swap them; matches the ultrareview pattern.

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// llmBinary is the CLI invoked for one-shot prompts. Same `claude`
// the rest of jarvis uses so the operator's existing auth + provider
// routing (anthropic / z.ai) flows through automatically.
var llmBinary = "claude"

// SetLLMBinaryForTest swaps the binary path. Returns the previous
// value for t.Cleanup restoration. Production code must not call.
func SetLLMBinaryForTest(path string) (previous string) {
	previous = llmBinary
	llmBinary = path
	return previous
}

// llmExtractTimeout caps a single `claude -p` call. 90s is generous
// for the small prompts we send + leaves headroom for slow upstream
// providers. Variable so tests can shorten without sleeping.
var llmExtractTimeout = 90 * time.Second

// SetLLMTimeoutForTest swaps the timeout for fast tests. Returns
// the previous value for t.Cleanup restoration.
func SetLLMTimeoutForTest(d time.Duration) (previous time.Duration) {
	previous = llmExtractTimeout
	llmExtractTimeout = d
	return previous
}

// ExtractedWorkflow is the shape the LLM produces. JSON-tagged so
// claude -p --output-format json deserialises cleanly.
type ExtractedWorkflow struct {
	Title         string   `json:"title"`
	Summary       string   `json:"summary"`
	Steps         []string `json:"steps"`
	KnownPitfalls []string `json:"known_pitfalls"`
}

// llmExtractError is returned for any failure mode. Callers check
// errors.Is against the sentinel variants below to decide whether
// to count the call against the per-tick budget (network/parse =
// no, hard cap = yes).
type llmExtractError struct {
	stage string
	err   error
}

func (e *llmExtractError) Error() string {
	if e.err == nil {
		return "llm extract: " + e.stage
	}
	return "llm extract: " + e.stage + ": " + e.err.Error()
}
func (e *llmExtractError) Unwrap() error { return e.err }

// extractWorkflowWithLLM synthesises a richer workflow from a
// cluster of experiences via `claude -p`. Returns nil + error on
// any failure; callers fall back to the heuristic workflow.
//
// The prompt asks for a fixed JSON shape (ExtractedWorkflow). We
// trust the model to follow the schema — modern claude respects
// `--output-format json` instructions reliably. Any deviation
// shows up as a json decode error and triggers the heuristic
// fallback at the memify call site.
func extractWorkflowWithLLM(ctx context.Context, cluster *experienceCluster, model string) (*ExtractedWorkflow, error) {
	if cluster == nil || len(cluster.entries) == 0 {
		return nil, &llmExtractError{stage: "empty cluster"}
	}

	prompt := buildExtractPrompt(cluster)

	callCtx, cancel := context.WithTimeout(ctx, llmExtractTimeout)
	defer cancel()

	args := []string{"-p", prompt, "--output-format", "json"}
	if model != "" {
		args = append(args, "--model", model)
	}
	cmd := exec.CommandContext(callCtx, llmBinary, args...)
	out, err := cmd.Output()
	if err != nil {
		if callCtx.Err() == context.DeadlineExceeded {
			return nil, &llmExtractError{stage: "timeout", err: callCtx.Err()}
		}
		return nil, &llmExtractError{stage: "exec", err: err}
	}

	// Two response shapes accepted:
	//   (a) `claude -p --output-format json` envelope:
	//       {"result": "<json string>", ...}
	//   (b) bare JSON object matching ExtractedWorkflow directly
	//       (older / debug-mode claude builds).
	//
	// We probe envelope first. If the envelope decodes AND has a
	// non-empty Result, parse that as the schema. Otherwise fall
	// through to the bare-direct parse so callers don't need to
	// know which CLI flavour produced the output.
	var envelope struct {
		Result string `json:"result"`
	}
	if jerr := json.Unmarshal(out, &envelope); jerr == nil && envelope.Result != "" {
		body := stripCodeFence(envelope.Result)
		var ext ExtractedWorkflow
		if derr := json.Unmarshal([]byte(body), &ext); derr != nil {
			return nil, &llmExtractError{stage: "result decode", err: derr}
		}
		if ext.Title == "" && len(ext.Steps) == 0 {
			return nil, &llmExtractError{stage: "empty response"}
		}
		return &ext, nil
	}
	// Bare-direct fallback.
	var direct ExtractedWorkflow
	if derr := json.Unmarshal(out, &direct); derr != nil {
		return nil, &llmExtractError{stage: "decode", err: derr}
	}
	if direct.Title == "" && len(direct.Steps) == 0 {
		return nil, &llmExtractError{stage: "empty response"}
	}
	return &direct, nil
}

// buildExtractPrompt renders the cluster into the prompt body. Kept
// pure + exported-via-tests so prompt evolution can be regression-
// tested separately from the exec path.
func buildExtractPrompt(c *experienceCluster) string {
	var b strings.Builder
	b.WriteString("You are consolidating a cluster of past task experiences from an AI engineering team into a reusable workflow doc.\n\n")
	b.WriteString("Cluster shape:\n")
	if c.role != "" {
		fmt.Fprintf(&b, "- Agent role: %s\n", c.role)
	}
	if len(c.labels) > 0 {
		fmt.Fprintf(&b, "- Task labels: %s\n", strings.Join(c.labels, ", "))
	}
	b.WriteString("\nExperiences (highest-outcome first):\n\n")
	limit := 8 // cap the prompt size; pull most-recent N.
	if len(c.entries) < limit {
		limit = len(c.entries)
	}
	for i := 0; i < limit; i++ {
		e := c.entries[i]
		fmt.Fprintf(&b, "%d. **%s** (outcome %.2f)\n", i+1, e.TaskTitle, e.OutcomeScore)
		if e.ApproachSummary != "" {
			fmt.Fprintf(&b, "   Approach: %s\n", e.ApproachSummary)
		}
		if e.WorkingDir != "" {
			fmt.Fprintf(&b, "   Dir: %s\n", e.WorkingDir)
		}
		b.WriteString("\n")
	}
	b.WriteString(`Produce a reusable workflow that future agents can follow when they hit a similar task. Return ONLY a JSON object with this shape:

{
  "title": "short, action-oriented workflow title",
  "summary": "1-2 sentence summary of when this applies + the core insight",
  "steps": ["concrete numbered step 1", "step 2", "..."],
  "known_pitfalls": ["pitfall an agent should avoid", "..."]
}

Rules:
- Steps must be concrete + actionable (not "think about X"). 3-7 steps is ideal.
- Known pitfalls should reflect mistakes visible in lower-outcome experiences. If the cluster has uniformly high outcomes, return an empty list — do not invent pitfalls.
- No markdown, no commentary, no explanations outside the JSON object.`)
	return b.String()
}

// stripCodeFence strips a single leading ```json ... ``` (or ```)
// wrapper. Handles both with and without language tag. No-op if
// no fence present.
func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	// Drop the opening fence + optional language line.
	idx := strings.Index(s, "\n")
	if idx < 0 {
		return s
	}
	s = s[idx+1:]
	// Drop the trailing fence.
	if end := strings.LastIndex(s, "```"); end >= 0 {
		s = s[:end]
	}
	return strings.TrimSpace(s)
}

// ExtractedAntiPattern is the parallel-to-ExtractedWorkflow shape
// the LLM produces for low-outcome clusters.
type ExtractedAntiPattern struct {
	Title         string   `json:"title"`
	WhyItFails    string   `json:"why_it_fails"`
	WhatToAvoid   []string `json:"what_to_avoid"`
	WhatWentWrong []string `json:"what_went_wrong"`
}

// extractAntiPatternWithLLM is the failure-case analog of
// extractWorkflowWithLLM. Same shell-out shape (claude -p
// --output-format json) but the prompt asks for what to avoid
// rather than what to do.
func extractAntiPatternWithLLM(ctx context.Context, cluster *experienceCluster, model string) (*ExtractedAntiPattern, error) {
	if cluster == nil || len(cluster.entries) == 0 {
		return nil, &llmExtractError{stage: "empty cluster"}
	}

	prompt := buildAntiPatternPrompt(cluster)

	callCtx, cancel := context.WithTimeout(ctx, llmExtractTimeout)
	defer cancel()

	args := []string{"-p", prompt, "--output-format", "json"}
	if model != "" {
		args = append(args, "--model", model)
	}
	cmd := exec.CommandContext(callCtx, llmBinary, args...)
	out, err := cmd.Output()
	if err != nil {
		if callCtx.Err() == context.DeadlineExceeded {
			return nil, &llmExtractError{stage: "timeout", err: callCtx.Err()}
		}
		return nil, &llmExtractError{stage: "exec", err: err}
	}

	// Same two-shape acceptance as the workflow path.
	var envelope struct {
		Result string `json:"result"`
	}
	if jerr := json.Unmarshal(out, &envelope); jerr == nil && envelope.Result != "" {
		body := stripCodeFence(envelope.Result)
		var ext ExtractedAntiPattern
		if derr := json.Unmarshal([]byte(body), &ext); derr != nil {
			return nil, &llmExtractError{stage: "result decode", err: derr}
		}
		if ext.Title == "" && len(ext.WhatToAvoid) == 0 {
			return nil, &llmExtractError{stage: "empty response"}
		}
		return &ext, nil
	}
	var direct ExtractedAntiPattern
	if derr := json.Unmarshal(out, &direct); derr != nil {
		return nil, &llmExtractError{stage: "decode", err: derr}
	}
	if direct.Title == "" && len(direct.WhatToAvoid) == 0 {
		return nil, &llmExtractError{stage: "empty response"}
	}
	return &direct, nil
}

// buildAntiPatternPrompt renders the low-outcome cluster into the
// prompt body. Asks for a fixed JSON shape matching
// ExtractedAntiPattern.
func buildAntiPatternPrompt(c *experienceCluster) string {
	var b strings.Builder
	b.WriteString("You are consolidating a cluster of FAILED or LOW-OUTCOME task experiences from an AI engineering team into a reusable anti-pattern lesson.\n\n")
	b.WriteString("Cluster shape:\n")
	if c.role != "" {
		fmt.Fprintf(&b, "- Agent role: %s\n", c.role)
	}
	if len(c.labels) > 0 {
		fmt.Fprintf(&b, "- Task labels: %s\n", strings.Join(c.labels, ", "))
	}
	b.WriteString("\nFailed experiences (most recent first):\n\n")
	limit := 8
	if len(c.entries) < limit {
		limit = len(c.entries)
	}
	for i := 0; i < limit; i++ {
		e := c.entries[i]
		fmt.Fprintf(&b, "%d. **%s** (outcome %.2f, status %s)\n",
			i+1, e.TaskTitle, e.OutcomeScore, e.CompletionStatus)
		if e.ApproachSummary != "" {
			fmt.Fprintf(&b, "   What was tried: %s\n", e.ApproachSummary)
		}
		if e.WorkingDir != "" {
			fmt.Fprintf(&b, "   Dir: %s\n", e.WorkingDir)
		}
		b.WriteString("\n")
	}
	b.WriteString(`Synthesise an anti-pattern lesson future agents can read to avoid the same mistakes. Return ONLY a JSON object with this shape:

{
  "title": "short, descriptive anti-pattern title (e.g. 'Closing the refresh cookie too early')",
  "why_it_fails": "1-2 sentence root-cause explanation",
  "what_went_wrong": ["concrete observation from the cluster", "..."],
  "what_to_avoid": ["actionable guidance an agent can follow", "..."]
}

Rules:
- what_to_avoid must be actionable ("don't X" or "before Y, do Z") — not vague.
- what_went_wrong should reflect specific signals visible in the experiences.
- 2-5 items per list is ideal.
- If the cluster's failure mode is unclear (mixed signals), return empty lists.
- No markdown, no commentary outside the JSON object.`)
	return b.String()
}

// mergeIntoAntiPattern overlays LLM fields onto the heuristic
// AntiPattern. Identity/provenance fields (Slug, DerivedFromTaskIDs,
// AvgOutcome, ObservationCount, AgentRole, TaskLabels,
// WorkingDirHints, CreatedAt) are authoritative.
func mergeIntoAntiPattern(base AntiPattern, ext *ExtractedAntiPattern) AntiPattern {
	if ext == nil {
		return base
	}
	if ext.Title != "" {
		base.Title = ext.Title
	}
	if ext.WhyItFails != "" {
		base.WhyItFails = ext.WhyItFails
	}
	if len(ext.WhatWentWrong) > 0 {
		base.WhatWentWrong = ext.WhatWentWrong
	}
	if len(ext.WhatToAvoid) > 0 {
		base.WhatToAvoid = ext.WhatToAvoid
	}
	return base
}

// mergeIntoWorkflow takes the heuristic Workflow and overlays the
// LLM-extracted fields where they're non-empty. Heuristic fields
// (Slug, DerivedFromTaskIDs, AvgOutcome, ReuseCountAtCreation,
// AgentRole, TaskLabels, WorkingDirHints, CreatedAt) are
// authoritative — the LLM never overrides identity/provenance.
// Title, Steps, KnownPitfalls fall through from the LLM when
// non-empty.
func mergeIntoWorkflow(base Workflow, ext *ExtractedWorkflow) Workflow {
	if ext == nil {
		return base
	}
	if ext.Title != "" {
		base.Title = ext.Title
	}
	if len(ext.Steps) > 0 {
		base.Steps = ext.Steps
	}
	if len(ext.KnownPitfalls) > 0 {
		base.KnownPitfalls = ext.KnownPitfalls
	}
	// Summary lands as the first "Steps" line so it shows up in the
	// rendered markdown even though Workflow doesn't have a Summary
	// field. Only if it adds context the steps don't already carry.
	if ext.Summary != "" && len(base.Steps) > 0 {
		base.Steps = append([]string{"_Summary_: " + ext.Summary}, base.Steps...)
	}
	return base
}
