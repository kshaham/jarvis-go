package office

import (
	"testing"
)

// TestLatestCompletedStandupID_NoneReturnsNotExist covers the empty-dir branch
// (no standups yet -> os.ErrNotExist).
func TestLatestCompletedStandupID_NoneReturnsNotExist(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := LatestCompletedStandupID(); err == nil {
		t.Error("expected an error when no standups exist")
	}
}

// TestGetBottomPerformers_ClampsN covers the n>len clamp branch.
func TestGetBottomPerformers_ClampsN(t *testing.T) {
	scores := []AgentScore{
		{ConfigID: "a", OverallScore: 0.2},
		{ConfigID: "b", OverallScore: 0.9},
	}
	got := GetBottomPerformers(scores, 10) // n > len -> clamp to 2
	if len(got) != 2 {
		t.Fatalf("expected clamp to 2, got %d", len(got))
	}
	if got[0].ConfigID != "a" {
		t.Errorf("expected lowest score first, got %q", got[0].ConfigID)
	}
	// Empty input -> empty result, no panic.
	if got := GetBottomPerformers(nil, 3); len(got) != 0 {
		t.Errorf("expected empty result for nil scores, got %d", len(got))
	}
}

// TestClearDevInstance_DefaultsEmptyName verifies an empty slot name defaults
// to the "app" slot and the slot is removed + broadcast emitted.
func TestClearDevInstance_DefaultsEmptyName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m, _, rec := newCtrlManager(t, DefaultConfig())
	if err := m.SetDevInstance("app", "http://jarvis:3000", 0, true); err != nil {
		t.Fatal(err)
	}

	m.ClearDevInstance("", false) // empty name -> defaults to "app"

	if _, ok := m.GetDevInstance("app"); ok {
		t.Error("expected the default app slot cleared")
	}
	if !rec.has("office:devInstanceCleared") {
		t.Error("expected office:devInstanceCleared broadcast")
	}

	// Clearing a nonexistent slot is a safe no-op (still broadcasts).
	m.ClearDevInstance("ghost", false)
}

// TestSaveTaskScreenshot_ValidationErrors covers the invalid-taskID and
// empty-data branches not exercised by the happy-path test.
func TestSaveTaskScreenshot_ValidationErrors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if _, err := SaveTaskScreenshot("../bad", "image/png", []byte{1}, 1); err == nil {
		t.Error("expected error for invalid taskID")
	}
	if _, err := SaveTaskScreenshot("task-1", "image/png", nil, 1); err == nil {
		t.Error("expected error for empty image data")
	}
}

// TestHasTaskScreenshot covers the false (none) and true (present) branches.
func TestHasTaskScreenshot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if HasTaskScreenshot("task-none") {
		t.Error("expected no screenshot for a task with none")
	}

	// A 1x1 PNG header is enough for the size/mime checks.
	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 1, 2, 3, 4}
	if _, err := SaveTaskScreenshot("task-shot", "image/png", png, 1700000000); err != nil {
		t.Fatalf("SaveTaskScreenshot: %v", err)
	}
	if !HasTaskScreenshot("task-shot") {
		t.Error("expected HasTaskScreenshot true after saving one")
	}
}
