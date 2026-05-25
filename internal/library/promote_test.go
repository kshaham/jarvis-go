package library

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// stubSkillWriter records every call so tests can assert content.
type stubSkillWriter struct {
	mu      sync.Mutex
	calls   map[string]string // slug -> body
	failOn  string
	failErr error
}

func newStubSkillWriter() *stubSkillWriter {
	return &stubSkillWriter{calls: map[string]string{}}
}

func (s *stubSkillWriter) write(name, body string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if name == s.failOn {
		return s.failErr
	}
	s.calls[name] = body
	return nil
}

// seedWorkflow places a workflow.md on disk so promote.go can read
// it. Returns the relative path the ledger entry should use.
func seedWorkflow(t *testing.T, slug, body string) string {
	t.Helper()
	w := Workflow{
		Slug:                 slug,
		Title:                "Test " + slug,
		DerivedFromTaskIDs:   []string{"t1", "t2", "t3"},
		AvgOutcome:           0.85,
		ReuseCountAtCreation: 3,
		AgentRole:            "backend",
		TaskLabels:           []string{slug},
		Steps:                []string{body},
		CreatedAt:            time.Now(),
	}
	if err := WriteWorkflow(w); err != nil {
		t.Fatal(err)
	}
	return "workflows/" + slug + ".md"
}

func seedLedgerForPromotion(t *testing.T, path string, reuse int, avg float64) *Ledger {
	t.Helper()
	l, _ := OpenLedger()
	for i := 0; i < reuse; i++ {
		if err := l.Increment(path, avg); err != nil {
			t.Fatal(err)
		}
	}
	return l
}

func TestRunPromotion_HappyPath(t *testing.T) {
	withTempLibrary(t)
	path := seedWorkflow(t, "auth", "Build middleware first.")
	ledger := seedLedgerForPromotion(t, path, 5, 0.85)

	writer := newStubSkillWriter()
	res, err := RunPromotion(PromoteOptions{}, ledger, writer.write)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Promoted) != 1 || res.Promoted[0] != path {
		t.Errorf("Promoted=%v want [%s]", res.Promoted, path)
	}
	if _, ok := writer.calls["auth"]; !ok {
		t.Error("writeSkill was not called for 'auth'")
	}
	// Ledger should now show PromotedAt set.
	entry, _ := ledger.Get(path)
	if entry == nil || entry.PromotedAt.IsZero() {
		t.Errorf("ledger PromotedAt not stamped: %+v", entry)
	}
}

func TestRunPromotion_BelowReuseSkipped(t *testing.T) {
	withTempLibrary(t)
	path := seedWorkflow(t, "rare", "x")
	ledger := seedLedgerForPromotion(t, path, 2, 0.95) // reuse < 5

	writer := newStubSkillWriter()
	res, _ := RunPromotion(PromoteOptions{}, ledger, writer.write)

	if len(res.Promoted) != 0 {
		t.Errorf("Promoted=%v want []", res.Promoted)
	}
	if len(res.Skipped) != 1 {
		t.Errorf("Skipped=%v want 1 entry", res.Skipped)
	}
	if _, called := writer.calls["rare"]; called {
		t.Error("writer called despite below-threshold reuse")
	}
}

func TestRunPromotion_BelowAvgOutcomeSkipped(t *testing.T) {
	withTempLibrary(t)
	path := seedWorkflow(t, "lossy", "x")
	ledger := seedLedgerForPromotion(t, path, 5, 0.60) // avg < 0.75

	writer := newStubSkillWriter()
	res, _ := RunPromotion(PromoteOptions{}, ledger, writer.write)

	if len(res.Promoted) != 0 {
		t.Errorf("Promoted=%v want []", res.Promoted)
	}
	if len(res.Skipped) != 1 {
		t.Errorf("Skipped=%v want 1 entry", res.Skipped)
	}
}

func TestRunPromotion_AlreadyPromotedNotRedone(t *testing.T) {
	withTempLibrary(t)
	path := seedWorkflow(t, "auth", "x")
	ledger := seedLedgerForPromotion(t, path, 5, 0.85)
	if err := ledger.MarkPromoted(path); err != nil {
		t.Fatal(err)
	}

	writer := newStubSkillWriter()
	res, _ := RunPromotion(PromoteOptions{}, ledger, writer.write)

	if len(res.Promoted) != 0 {
		t.Errorf("Promoted=%v want []", res.Promoted)
	}
	if len(writer.calls) != 0 {
		t.Errorf("writer called %d times; want 0", len(writer.calls))
	}
}

func TestRunPromotion_RevokedWithinCoolDownSkipped(t *testing.T) {
	withTempLibrary(t)
	path := seedWorkflow(t, "auth", "x")
	ledger := seedLedgerForPromotion(t, path, 5, 0.85)
	if err := ledger.MarkRevoked(path, "operator concern"); err != nil {
		t.Fatal(err)
	}
	// Now() returns 1 hour after revocation; cool-down is 24h.
	now := time.Now().Add(1 * time.Hour)

	writer := newStubSkillWriter()
	res, _ := RunPromotion(PromoteOptions{
		NowFunc: func() time.Time { return now },
	}, ledger, writer.write)

	if len(res.Promoted) != 0 {
		t.Errorf("Promoted=%v want []", res.Promoted)
	}
	if len(res.Skipped) != 1 {
		t.Errorf("Skipped=%v want 1 entry", res.Skipped)
	}
}

func TestRunPromotion_RevokedAfterCoolDownReadmitted(t *testing.T) {
	withTempLibrary(t)
	path := seedWorkflow(t, "auth", "x")
	ledger := seedLedgerForPromotion(t, path, 5, 0.85)
	if err := ledger.MarkRevoked(path, "operator concern"); err != nil {
		t.Fatal(err)
	}
	// Now() returns 25 hours after revocation — past the default
	// 24h cool-down. Promotion should proceed.
	now := time.Now().Add(25 * time.Hour)

	writer := newStubSkillWriter()
	res, _ := RunPromotion(PromoteOptions{
		NowFunc: func() time.Time { return now },
	}, ledger, writer.write)

	if len(res.Promoted) != 1 {
		t.Errorf("Promoted=%v want 1 entry after cool-down", res.Promoted)
	}
}

func TestRunPromotion_ExperiencePathsFilteredOut(t *testing.T) {
	// Bare experience paths must not graduate — only workflows do.
	withTempLibrary(t)
	rec := ExperienceRecord{
		AgentConfigID:    "vex",
		TaskID:           "task-1",
		TaskTitle:        "Test",
		CompletionStatus: "completed",
		OutcomeScore:     0.9,
	}
	if err := WriteExperience(rec); err != nil {
		t.Fatal(err)
	}
	expPath := "experiences/vex/task-1.md"
	ledger := seedLedgerForPromotion(t, expPath, 10, 0.95)

	writer := newStubSkillWriter()
	res, _ := RunPromotion(PromoteOptions{}, ledger, writer.write)

	if len(res.Promoted) != 0 {
		t.Errorf("Promoted=%v want [] (experience paths shouldn't graduate)", res.Promoted)
	}
	if len(res.Skipped) != 1 {
		t.Errorf("Skipped=%v want 1 (experience path filtered)", res.Skipped)
	}
}

func TestRunPromotion_WriterFailureReportedAndLedgerNotMarked(t *testing.T) {
	// If writeSkill returns an error, the ledger should NOT be
	// marked promoted (so a retry next tick can succeed).
	withTempLibrary(t)
	path := seedWorkflow(t, "auth", "x")
	ledger := seedLedgerForPromotion(t, path, 5, 0.85)

	writer := newStubSkillWriter()
	writer.failOn = "auth"
	writer.failErr = errors.New("disk full")

	res, _ := RunPromotion(PromoteOptions{}, ledger, writer.write)

	if len(res.Failed) != 1 {
		t.Errorf("Failed=%v want 1", res.Failed)
	}
	entry, _ := ledger.Get(path)
	if entry != nil && !entry.PromotedAt.IsZero() {
		t.Error("PromotedAt should NOT be set when writer failed")
	}
}

func TestRunPromotion_DryRunReportsButDoesntCall(t *testing.T) {
	// writeSkill nil = dry-run; returns Promoted list without
	// side effects.
	withTempLibrary(t)
	path := seedWorkflow(t, "auth", "x")
	ledger := seedLedgerForPromotion(t, path, 5, 0.85)

	res, err := RunPromotion(PromoteOptions{}, ledger, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Promoted) != 1 {
		t.Errorf("Promoted=%v want 1 entry", res.Promoted)
	}
	entry, _ := ledger.Get(path)
	if entry != nil && !entry.PromotedAt.IsZero() {
		t.Error("dry-run should not mark PromotedAt")
	}
}

func TestRunPromotion_NilLedgerIsNoOp(t *testing.T) {
	withTempLibrary(t)
	res, err := RunPromotion(PromoteOptions{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Promoted) != 0 || len(res.Skipped) != 0 {
		t.Errorf("expected empty result for nil ledger; got %+v", res)
	}
}

func TestRunPromotion_SummaryFormats(t *testing.T) {
	res := PromoteResult{
		Promoted: []string{"a", "b"},
		Skipped:  []string{"c"},
		Failed:   []string{},
	}
	if got := res.Summary(); !strings.Contains(got, "promoted=2") ||
		!strings.Contains(got, "skipped=1") ||
		!strings.Contains(got, "failed=0") {
		t.Errorf("summary=%q missing expected counts", got)
	}
}
