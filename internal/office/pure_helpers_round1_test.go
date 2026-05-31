package office

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSanitizeInvariants_TruncatesAndCaps covers the per-item length
// truncation (rune-safe) and the maxInvariants cap branches.
func TestSanitizeInvariants_TruncatesAndCaps(t *testing.T) {
	// A single oversized item must be truncated to maxInvariantLen bytes
	// (backed off to a rune boundary).
	long := strings.Repeat("a", maxInvariantLen+50)
	got := sanitizeInvariants([]string{long})
	if len(got) != 1 {
		t.Fatalf("expected 1 sanitized item, got %d", len(got))
	}
	if len(got[0]) > maxInvariantLen {
		t.Errorf("expected truncation to <= %d bytes, got %d", maxInvariantLen, len(got[0]))
	}

	// More than maxInvariants items must be capped.
	items := make([]string, maxInvariants+10)
	for i := range items {
		items[i] = "rule"
	}
	capped := sanitizeInvariants(items)
	if len(capped) != maxInvariants {
		t.Errorf("expected cap at %d, got %d", maxInvariants, len(capped))
	}

	// Blank items are dropped.
	mixed := sanitizeInvariants([]string{"  ", "keep", "\t\n", "also"})
	if len(mixed) != 2 || mixed[0] != "keep" || mixed[1] != "also" {
		t.Errorf("expected blanks dropped, got %#v", mixed)
	}

	// Rune-safe truncation: a multibyte rune at the boundary must not be split.
	multibyte := strings.Repeat("é", maxInvariantLen) // each é is 2 bytes
	r := sanitizeInvariants([]string{multibyte})
	if len(r) != 1 {
		t.Fatal("expected 1 item")
	}
	if !utf8ValidString(r[0]) {
		t.Errorf("expected valid UTF-8 after truncation")
	}
}

func utf8ValidString(s string) bool {
	for _, r := range s {
		if r == '�' {
			return false
		}
	}
	return true
}

// TestMinFloat covers both ordering branches.
func TestMinFloat(t *testing.T) {
	if got := minFloat(1.0, 2.0); got != 1.0 {
		t.Errorf("expected 1.0, got %v", got)
	}
	if got := minFloat(5.0, 3.0); got != 3.0 {
		t.Errorf("expected 3.0, got %v", got)
	}
	if got := minFloat(4.0, 4.0); got != 4.0 {
		t.Errorf("expected 4.0 for equal inputs, got %v", got)
	}
}

// TestUsableVerdict covers the empty-results error branch and the pass-through.
func TestUsableVerdict(t *testing.T) {
	// Not all-pass + no results -> evaluator error stamped.
	got := usableVerdict(CriteriaVerdict{AllPass: false, Results: nil})
	if got.EvaluatorError == "" {
		t.Error("expected an EvaluatorError when results are empty and not all-pass")
	}

	// Has results -> returned unchanged.
	v := CriteriaVerdict{AllPass: false, Results: []CriterionResult{{Criterion: "x", Pass: false}}}
	out := usableVerdict(v)
	if out.EvaluatorError != "" || len(out.Results) != 1 {
		t.Errorf("expected pass-through, got %+v", out)
	}

	// AllPass true with empty results is still a usable verdict (not an error).
	allPass := usableVerdict(CriteriaVerdict{AllPass: true})
	if allPass.EvaluatorError != "" {
		t.Errorf("expected no error for all-pass verdict, got %q", allPass.EvaluatorError)
	}
}

// TestStripCriteriaFence removes a leading ```json fence and trailing ```.
func TestStripCriteriaFence(t *testing.T) {
	fenced := "```json\n{\"allPass\":true}\n```"
	if got := stripCriteriaFence(fenced); got != `{"allPass":true}` {
		t.Errorf("expected fence stripped, got %q", got)
	}
	// No fence: returned trimmed.
	plain := "  {\"allPass\":false}  "
	if got := stripCriteriaFence(plain); got != `{"allPass":false}` {
		t.Errorf("expected trimmed plain JSON, got %q", got)
	}
	// Bare ``` fence (no language tag).
	bare := "```\nvalue\n```"
	if got := stripCriteriaFence(bare); got != "value" {
		t.Errorf("expected bare fence stripped, got %q", got)
	}
}

// TestDashIfEmpty returns the placeholder for empty input.
func TestDashIfEmpty(t *testing.T) {
	if got := dashIfEmpty(""); got != "(untitled task)" {
		t.Errorf("expected placeholder, got %q", got)
	}
	if got := dashIfEmpty("Real Title"); got != "Real Title" {
		t.Errorf("expected pass-through, got %q", got)
	}
}

// TestBuildStableSystemPromptCodex renders the codex-flavoured stable prompt
// (no Anthropic prompt-cache note) including mission + invariants.
func TestBuildStableSystemPromptCodex(t *testing.T) {
	got := BuildStableSystemPromptCodex("strategic", "Ship it", []string{"never break the build"})
	if !strings.Contains(got, "Jarvis Team Operating Context") {
		t.Errorf("expected header, got %q", got[:min(80, len(got))])
	}
	if !strings.Contains(got, "Ship it") {
		t.Error("expected mission embedded")
	}
	if !strings.Contains(got, "never break the build") {
		t.Error("expected invariant embedded")
	}
	// Codex variant must NOT mention Anthropic prompt caching.
	if strings.Contains(got, "prompt cache discounts") {
		t.Error("codex prompt should not include the Anthropic cache note")
	}
}

// TestLoadSnapshot_ErrorPaths covers the invalid-name and missing-file errors.
func TestLoadSnapshot_ErrorPaths(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := LoadSnapshot("../escape"); err == nil {
		t.Error("expected error for invalid snapshot name")
	}
	if _, err := LoadSnapshot("does-not-exist"); err == nil {
		t.Error("expected error for missing snapshot file")
	}
}

// TestDeleteSnapshot_InvalidAndIdempotent covers the validation error and the
// idempotent missing-file success.
func TestDeleteSnapshot_InvalidAndIdempotent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := DeleteSnapshot("bad/name"); err == nil {
		t.Error("expected error for invalid snapshot name")
	}
	// Deleting a nonexistent (but validly-named) snapshot is a silent success.
	if err := DeleteSnapshot("never-created"); err != nil {
		t.Errorf("expected idempotent delete to succeed, got %v", err)
	}
}

// TestListSnapshots_MissingDirAndJunk returns nil for a missing dir and skips
// non-snapshot files.
func TestListSnapshots_MissingDirAndJunk(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// Missing dir -> (nil, nil).
	snaps, err := ListSnapshots()
	if err != nil {
		t.Errorf("expected no error for missing snapshot dir, got %v", err)
	}
	if len(snaps) != 0 {
		t.Errorf("expected empty list, got %d", len(snaps))
	}

	// Create the dir with a junk file + an invalid-named json.
	dir := snapshotDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad name.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	snaps2, err := ListSnapshots()
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(snaps2) != 0 {
		t.Errorf("expected junk + bad-name files skipped, got %d", len(snaps2))
	}
}

// TestRecordExperiment_AppendsJSONL appends a record and verifies the JSONL
// file grows by one line per call.
func TestRecordExperiment_AppendsJSONL(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	rec := ExperimentRecord{
		TeamTemplate:   "alpha",
		Mission:        "ship",
		TaskCount:      5,
		CompletedCount: 4,
		AvgScore:       0.8,
		Duration:       "1h",
		TopPerformer:   "Vex",
	}
	if err := RecordExperiment(rec); err != nil {
		t.Fatalf("RecordExperiment: %v", err)
	}
	if err := RecordExperiment(rec); err != nil {
		t.Fatalf("second RecordExperiment: %v", err)
	}

	data, err := os.ReadFile(experimentsPath())
	if err != nil {
		t.Fatalf("read experiments log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 JSONL lines, got %d", len(lines))
	}
	if !strings.Contains(lines[0], "alpha") || !strings.Contains(lines[0], "Vex") {
		t.Errorf("unexpected record content: %q", lines[0])
	}
}
