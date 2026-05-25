package office

// Outcome scoring for the knowledge-library feedback loop.
//
// Every completed task gets a composite outcome score in [0, 1]
// that captures "how well did this go?". The score lands in the
// experience markdown's YAML frontmatter and feeds:
//   - the auto-promotion threshold (avg_outcome >= 0.75 to promote)
//   - the dashboard's outcome-score trend ("is the team getting
//     better?")
//   - the eager-retrieval ranking at spawn (top-K by score)
//
// Formula weights (chosen so each lever is meaningful but no single
// signal can dominate):
//
//   0.40 * completion_signal       (did the task finish at all?)
//   0.25 * (1 - severity_weight)   (did review find blockers?)
//   0.20 * quality_score           (existing AgentScore quality)
//   0.15 * (1 - norm(recovery))    (did the agent wedge?)
//
// Pure function. No side effects. Caller (CompleteTask hook)
// computes once and writes into the library record.

// CompletionSignal maps a task's terminal status into a [0, 1]
// completion weight. Completed=1.0, failed=0.3 (the agent at least
// reported back), abandoned=0.0.
//
// Exported separately so callers can debug "why is my outcome low?"
// by inspecting each component.
func CompletionSignal(status TaskStatus) float64 {
	switch status {
	case TaskCompleted:
		return 1.0
	case TaskFailed:
		return 0.3
	default:
		// Pending / in-progress shouldn't reach the scorer at all,
		// but if they do, treat as abandoned so the data point is
		// clearly low instead of silently 0.5.
		return 0.0
	}
}

// SeverityWeight maps an ultrareview severity to a [0, 1] penalty.
// Used as `(1 - SeverityWeight)` in the composite — so a blocker
// (weight 1.0) zeroes out the review component, a nit (weight 0.2)
// barely dents it.
//
// "none" covers tasks where ultrareview didn't run (the common case
// for tasks without the needs-review label).
func SeverityWeight(severity string) float64 {
	switch severity {
	case "blocker":
		return 1.0
	case "warning":
		return 0.6
	case "nit":
		return 0.2
	case "info", "":
		return 0.0
	case "none":
		return 0.0
	default:
		// Unknown severity — treat conservatively (mild penalty) so
		// a future severity tier doesn't silently produce a
		// suspiciously perfect score.
		return 0.3
	}
}

// normalizeRecoveryAttempts squashes the unbounded recovery count
// into [0, 1]. 0 attempts → 0; 3+ attempts → 1.0 (max penalty).
// Threshold matches the default RecoveryMaxAttempts so the score
// component saturates at the operational kill point.
func normalizeRecoveryAttempts(attempts int) float64 {
	if attempts <= 0 {
		return 0
	}
	if attempts >= 3 {
		return 1.0
	}
	return float64(attempts) / 3.0
}

// ComputeOutcomeScore returns the composite outcome score in [0, 1]
// for a completed task. Inputs:
//
//   - status: terminal TaskStatus (TaskCompleted / TaskFailed / etc.)
//   - ultrareviewSeverity: highest severity from the ultrareview
//     findings, or "none" if ultrareview didn't run. The library
//     write site collapses the per-finding severities to the max
//     before calling this.
//   - qualityScore: AgentScore.QualityScore from the existing
//     scoring.go path. Reuses the same proxy (completion rate based)
//     so we don't introduce a separate quality model.
//   - recoveryAttempts: how many times the recovery system fired
//     against this task's agent during the task's lifetime. Higher
//     means the agent wedged repeatedly.
//
// Output is clamped to [0, 1] defensively, though the formula
// already produces values in that range for valid inputs.
func ComputeOutcomeScore(status TaskStatus, ultrareviewSeverity string, qualityScore float64, recoveryAttempts int) float64 {
	completion := CompletionSignal(status)
	severityWeight := SeverityWeight(ultrareviewSeverity)
	if qualityScore < 0 {
		qualityScore = 0
	}
	if qualityScore > 1 {
		qualityScore = 1
	}
	recoveryPenalty := normalizeRecoveryAttempts(recoveryAttempts)

	score := 0.40*completion +
		0.25*(1.0-severityWeight) +
		0.20*qualityScore +
		0.15*(1.0-recoveryPenalty)

	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}

// SeverityFromFindings collapses a slice of ultrareview findings
// into the single max-severity string the library record stores.
// Empty findings → "none". Exported so the ultrareview ingest hook
// can call it directly without re-implementing the comparison.
func SeverityFromFindings(findings []UltrareviewFinding) string {
	if len(findings) == 0 {
		return "none"
	}
	// Rank: blocker > warning > nit > info > other.
	max := "info"
	for _, f := range findings {
		if rankSeverity(f.Severity) > rankSeverity(max) {
			max = f.Severity
		}
	}
	return max
}

func rankSeverity(s string) int {
	switch s {
	case "blocker":
		return 4
	case "warning":
		return 3
	case "nit":
		return 2
	case "info":
		return 1
	default:
		return 0
	}
}
