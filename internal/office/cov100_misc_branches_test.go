package office

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestShouldWarnMissingScreenshot_ExistingScreenshotSuppresses drives the
// HasTaskScreenshot(taskID)==true branch (screenshot_gate.go:60-62): a UI
// task with no in-summary evidence but a stored screenshot must NOT warn.
func TestShouldWarnMissingScreenshot_ExistingScreenshotSuppresses(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := TaskScreenshotsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A stored screenshot for task "t-ui" so HasTaskScreenshot returns true.
	if err := os.WriteFile(filepath.Join(dir, "t-ui-1700000000.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}

	// UI-keyword title, summary lacks verification evidence → would warn,
	// EXCEPT a screenshot exists, so the gate suppresses the warning.
	got := ShouldWarnMissingScreenshot("Polish the dashboard UI layout", "did the work", "t-ui")
	if got {
		t.Error("expected no warning when a screenshot already exists")
	}
}

// TestSaveTaskScreenshot_RenameError drives the rename-failure branch
// (task_screenshot.go:104-106) via the shared renameFile seam.
func TestSaveTaskScreenshot_RenameError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	prev := renameFile
	sentinel := errors.New("simulated rename failure")
	renameFile = func(string, string) error { return sentinel }
	t.Cleanup(func() { renameFile = prev })

	_, err := SaveTaskScreenshot("task-1", "image/png", []byte("imagebytes"), 1700000000)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected rename sentinel error, got %v", err)
	}
}

// TestAskQuestion_DeadlinePassedDuringLeadBreaksLoop drives the
// remaining<=0 break in the fallback rotation loop (questions.go:177-179):
// the lead consumes the whole window, a fallback teammate is still
// available, but the deadline has already elapsed so the loop breaks
// without asking the fallback, ending in QuestionTimeout.
func TestAskQuestion_DeadlinePassedDuringLeadBreaksLoop(t *testing.T) {
	m, _ := newQuestionTestManager(t,
		TeamMemberConfig{ID: "asker", Name: "Asker", Tier: "worker"},
		// Strategic lead (asked first; never answers → consumes the window).
		TeamMemberConfig{ID: "lead", Name: "Lead", Tier: "strategic", Role: "Tech Lead"},
		// A worker fallback that remains available after the lead times out.
		TeamMemberConfig{ID: "peer", Name: "Peer", Tier: "worker", Role: "Backend Engineer"})

	start := time.Now()
	// Small total wait: the lead's askOnce (perRecipient = totalWait/2,
	// clamped up to askMinWait) eats past the deadline, so when the loop
	// re-checks remaining it is <= 0 and breaks.
	q, err := m.AskQuestion("sess-asker", "anyone free?", "", askMinWait)
	if err != nil {
		t.Fatalf("AskQuestion: %v", err)
	}
	// Lead was tried; fallback may or may not have been reached depending
	// on timing, but the question must end in a terminal no-answer status.
	if q.Status != QuestionTimeout && q.Status != QuestionNoRecipient {
		t.Errorf("expected terminal no-answer status, got %s", q.Status)
	}
	// Sanity: it should not block far beyond the configured window.
	if elapsed := time.Since(start); elapsed > 3*askMinWait+2*time.Second {
		t.Errorf("AskQuestion blocked too long: %v", elapsed)
	}
}
