package office

import (
	"strings"
	"testing"
)

// TestRankSeverity_DefaultBranch drives the default arm of rankSeverity
// (outcome.go line ~155) via SeverityFromFindings: a finding with an
// unrecognized severity string causes rankSeverity to return 0 (the
// default), which is below "info" (rank 1), so the max stays "info".
func TestRankSeverity_DefaultBranch(t *testing.T) {
	findings := []UltrareviewFinding{
		{Severity: "totally-unknown-severity"},
	}
	// rankSeverity("totally-unknown-severity") == 0 (default arm), which
	// is < rankSeverity("info") == 1, so the initial "info" wins.
	if got := SeverityFromFindings(findings); got != "info" {
		t.Errorf("expected unknown severity to fall through to info, got %q", got)
	}
}

// TestRankSeverity_AllArms exercises every labeled arm too, guarding
// against silent reorderings.
func TestRankSeverity_AllArms(t *testing.T) {
	cases := []struct {
		severities []string
		wantMax    string
	}{
		{[]string{"blocker", "warning"}, "blocker"},
		{[]string{"warning", "nit"}, "warning"},
		{[]string{"nit", "info"}, "nit"},
		{[]string{"info"}, "info"},
	}
	for _, tc := range cases {
		findings := make([]UltrareviewFinding, len(tc.severities))
		for i, s := range tc.severities {
			findings[i] = UltrareviewFinding{Severity: s}
		}
		if got := SeverityFromFindings(findings); got != tc.wantMax {
			t.Errorf("SeverityFromFindings(%v)=%q want %q", tc.severities, got, tc.wantMax)
		}
	}
}

// TestTrimExcerpt_RuneBoundaryBackup drives the multi-byte rune backup
// loop in trimExcerpt (knowledge.go lines ~414-416). We build a string
// longer than maxExcerptChars whose naive truncation point lands in the
// middle of a multi-byte rune, forcing the loop to back up to a rune
// boundary so the result remains valid UTF-8.
func TestTrimExcerpt_RuneBoundaryBackup(t *testing.T) {
	// Pad with ASCII to comfortably exceed maxExcerptChars, then a run of
	// multi-byte runes so the truncation index lands mid-rune.
	padded := strings.Repeat("a", 300) + strings.Repeat("世", 50)
	got := trimExcerpt(padded)

	if !strings.HasSuffix(got, "...") {
		t.Errorf("expected ellipsis suffix on truncated excerpt, got %q tail", got[len(got)-6:])
	}
	// Result must be valid UTF-8 (no rune was split).
	for i, r := range got {
		if r == '�' {
			t.Errorf("excerpt contains replacement char at %d => rune was split: %q", i, got)
		}
	}
	if len(got) >= len(padded) {
		t.Errorf("expected truncated output shorter than input, got len %d >= %d", len(got), len(padded))
	}
}

// TestTrimExcerpt_ShortLineUnchanged exercises the no-truncation path.
func TestTrimExcerpt_ShortLineUnchanged(t *testing.T) {
	in := "  a short line  "
	if got := trimExcerpt(in); got != "a short line" {
		t.Errorf("short line should be trimmed of whitespace only, got %q", got)
	}
}

// TestResetAgentLoopDetector_UnknownSession drives the agent==nil guard
// (loop_detector.go lines ~157-159).
func TestResetAgentLoopDetector_UnknownSession(t *testing.T) {
	m := newTestManager()
	// No agent registered under this session; must be a harmless no-op.
	m.ResetAgentLoopDetector("nonexistent")
}

// TestResetAgentToolLoopDetector_UnknownSession drives the agent==nil
// guard in the tool-loop variant (tool_loop_detector.go lines ~158-160).
func TestResetAgentToolLoopDetector_UnknownSession(t *testing.T) {
	m := newTestManager()
	m.ResetAgentToolLoopDetector("nonexistent")
}
