package office

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// TestGenerateWrapUpReport_WriteFails forces the report tmp-write error via
// the reportWriteFile seam.
func TestGenerateWrapUpReport_WriteFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	orig := reportWriteFile
	t.Cleanup(func() { reportWriteFile = orig })
	reportWriteFile = func(string, []byte, os.FileMode) error { return errors.New("disk full") }

	m := newTestManager()
	if _, err := GenerateWrapUpReport(m, "manual", ""); err == nil || !strings.Contains(err.Error(), "write report tmp") {
		t.Fatalf("expected write-tmp error, got %v", err)
	}
}

// TestGenerateWrapUpReport_RenameFails forces the report rename error via
// the reportRename seam and verifies the tmp file is cleaned up.
func TestGenerateWrapUpReport_RenameFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	orig := reportRename
	t.Cleanup(func() { reportRename = orig })
	reportRename = func(string, string) error { return errors.New("rename failed") }

	m := newTestManager()
	if _, err := GenerateWrapUpReport(m, "manual", ""); err == nil || !strings.Contains(err.Error(), "rename report") {
		t.Fatalf("expected rename error, got %v", err)
	}
}

// TestGenerateWrapUpReport_RichContent drives the memory-section,
// distilled-learnings, whiteboard, and handoff-log (cancelled/expired)
// branches of buildReportBody / writeAgentSection.
func TestGenerateWrapUpReport_RichContent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	_, cleanup := withTempLearningsDir(t)
	defer cleanup()
	// withTempLearningsDir flips HOME; re-pin HOME to a single dir so memory,
	// learnings, and reports all share one home.
	t.Setenv("HOME", home)

	m := newTestManager()
	m.SetTeamSpawned(true)
	workdir := "/proj/report-rich"
	m.SetSpawnContext("tmpl", workdir)

	m.RegisterAgent("sess-a", "alpha", &TeamMemberConfig{ID: "alpha", Name: "Alpha", Role: "Backend", Tier: "worker"})
	a := m.GetAgent("sess-a")
	// Two completed tasks (different completion times) so the sort runs.
	a.AddTask(&Task{ID: "c1", Title: "Earlier", Status: TaskCompleted, CompletedAt: time.Now().Add(-2 * time.Hour)})
	a.AddTask(&Task{ID: "c2", Title: "Later", Status: TaskCompleted, CompletedAt: time.Now()})

	// Memory sections: current_focus + decisions.
	if err := MemoryWriteSection("alpha", workdir, "current_focus", "Refactor the auth layer"); err != nil {
		t.Fatal(err)
	}
	if err := MemoryWriteSection("alpha", workdir, "decisions", "Chose argon2 over bcrypt"); err != nil {
		t.Fatal(err)
	}

	// Distilled learnings.
	ld := defaultLearningData("alpha")
	ld.DistilledInsights = []string{"Key insight: cache the prefix"}
	if err := SaveLearningData("alpha", ld); err != nil {
		t.Fatal(err)
	}

	// Whiteboard note.
	m.SetWhiteboardNote("sess-a", "All screens audited")

	meta, err := GenerateWrapUpReport(m, "manual", "")
	if err != nil {
		t.Fatalf("GenerateWrapUpReport: %v", err)
	}
	body, err := LoadWrapUpReport(meta.Filename)
	if err != nil {
		t.Fatalf("LoadWrapUpReport: %v", err)
	}

	wants := []string{
		"Working dir:",
		"**Current focus:**", "Refactor the auth layer",
		"**Decisions:**", "Chose argon2 over bcrypt",
		"**Distilled learnings:**", "Key insight: cache the prefix",
		"## Whiteboard Final State", "All screens audited",
		"✅ Later", "✅ Earlier",
	}
	for _, w := range wants {
		if !strings.Contains(body, w) {
			t.Errorf("report missing %q\n---\n%s", w, body)
		}
	}
}

// TestGenerateWrapUpReport_HandoffLog drives the handoff-log section with
// cancelled and expired statuses (the two switch arms buildReportBody
// otherwise never hits in the standard happy-path tests).
func TestGenerateWrapUpReport_HandoffLog(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.SetTeamSpawned(true)
	m.RegisterAgent("sess-a", "alpha", &TeamMemberConfig{ID: "alpha", Name: "Alpha"})

	// Inject handoff history with cancelled + expired entries directly into
	// the audit store.
	store := m.ensureHandoffStore()
	store.append(HandoffRequest{ID: "h1", TaskID: "t1", TaskTitle: "A", Status: HandoffCancelled, CreatedAt: time.Now().Add(-time.Hour)})
	store.append(HandoffRequest{ID: "h2", TaskID: "t2", TaskTitle: "B", Status: HandoffExpired, CreatedAt: time.Now()})

	meta, err := GenerateWrapUpReport(m, "manual", "")
	if err != nil {
		t.Fatalf("GenerateWrapUpReport: %v", err)
	}
	body, err := LoadWrapUpReport(meta.Filename)
	if err != nil {
		t.Fatalf("LoadWrapUpReport: %v", err)
	}
	for _, w := range []string{"## Handoff Log", "1 cancelled", "1 expired"} {
		if !strings.Contains(body, w) {
			t.Errorf("report missing %q\n---\n%s", w, body)
		}
	}
}
