package office

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildReportBody_FullMissionHandoffsAndWhiteboard exercises the report
// sections that the existing report tests leave uncovered: the full-mission
// block (mission longer than the snippet), the handoff log with multiple
// resolution statuses, and the whiteboard final-state section.
func TestBuildReportBody_FullMissionHandoffsAndWhiteboard(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()

	// A mission longer than 200 chars triggers the separate "## Mission"
	// full-text section in addition to the header snippet.
	longMission := strings.Repeat("Deliver an exceptional product. ", 12)
	m.SetTeamMission(longMission)
	m.SetTeamSpawned(true)
	m.SetSpawnContext("alpha-team", "/work/proj")

	// Two agents; one hands a task to the other (accepted), and a second
	// handoff is rejected, to populate the handoff log + executive summary
	// counters.
	m.RegisterAgent("sess-a", "alpha", &TeamMemberConfig{ID: "alpha", Name: "Alpha", Role: "FE"})
	m.RegisterAgent("sess-b", "beta", &TeamMemberConfig{ID: "beta", Name: "Beta", Role: "BE"})
	alpha := m.GetAgent("sess-a")
	alpha.AddTask(&Task{ID: "t-acc", Title: "Accepted task", Status: TaskInProgress})
	alpha.AddTask(&Task{ID: "t-rej", Title: "Rejected task", Status: TaskInProgress})

	if _, err := m.InitiateHandoff("sess-a", "t-acc", "beta", "please take"); err != nil {
		t.Fatalf("initiate accepted handoff: %v", err)
	}
	if _, err := m.AcceptHandoff("sess-b", "t-acc"); err != nil {
		t.Fatalf("accept handoff: %v", err)
	}
	if _, err := m.InitiateHandoff("sess-a", "t-rej", "beta", "this one too"); err != nil {
		t.Fatalf("initiate rejected handoff: %v", err)
	}
	if _, err := m.RejectHandoff("sess-b", "t-rej", "too busy"); err != nil {
		t.Fatalf("reject handoff: %v", err)
	}

	// Whiteboard notes for the final-state section.
	m.SetWhiteboardNote("sess-a", "Alpha's final notes here")
	m.SetWhiteboardNote("sess-b", "")              // blank -> skipped
	m.SetWhiteboardNote("sess-ghost", "no agent")  // unknown session -> name falls back to sid

	meta, err := GenerateWrapUpReport(m, "manual", "end of sprint")
	if err != nil {
		t.Fatalf("GenerateWrapUpReport: %v", err)
	}

	body, err := LoadWrapUpReport(meta.Filename)
	if err != nil {
		t.Fatalf("LoadWrapUpReport: %v", err)
	}

	wantSubstrings := []string{
		"## Mission",                        // full mission section
		longMission[:50],                    // full mission text present
		"## Handoff Log",                    // handoff section
		"## Whiteboard Final State",         // whiteboard section
		"Alpha's final notes here",          // whiteboard content
		"**Working dir:** `/work/proj`",     // spawn context
		"**Template:** `alpha-team`",        // spawn context
		"1 accepted",                        // exec summary handoff counter
		"1 rejected",                        // exec summary handoff counter
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(body, want) {
			t.Errorf("report body missing %q", want)
		}
	}
	// The blank whiteboard note for beta must be skipped: the whiteboard
	// section should not render an indented "> " block for Beta's content.
	wbIdx := strings.Index(body, "## Whiteboard Final State")
	if wbIdx >= 0 {
		wbSection := body[wbIdx:]
		// Only Alpha's note should appear as a quoted block in this section.
		if strings.Contains(wbSection, "Beta") && strings.Contains(wbSection, "> ") {
			// Beta's note was blank; ensure it isn't quoted in the whiteboard.
			if strings.Contains(wbSection, "> \n") || strings.Contains(wbSection, "**Beta:**\n\n> ") {
				t.Error("blank whiteboard note for Beta should be skipped")
			}
		}
	}
}

// TestLoadWrapUpReport_InvalidFilename rejects names that don't match the
// canonical wrap-up-*.md pattern (path traversal guard).
func TestLoadWrapUpReport_InvalidFilename(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cases := []string{
		"../etc/passwd",
		"random.txt",
		"wrap-up-bad.md/../../escape",
		"",
	}
	for _, name := range cases {
		if _, err := LoadWrapUpReport(name); err == nil {
			t.Errorf("expected error for invalid filename %q", name)
		}
	}
}

// TestLoadWrapUpReport_MissingFile returns an error for a validly-named but
// nonexistent report.
func TestLoadWrapUpReport_MissingFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := LoadWrapUpReport("wrap-up-20200101-120000.md"); err == nil {
		t.Error("expected error for nonexistent report")
	}
}

// TestListWrapUpReports_SkipsNonReportFiles verifies the lister ignores files
// that aren't valid report names and returns the rest newest-first.
func TestListWrapUpReports_SkipsNonReportFiles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := WrapUpReportsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Two valid reports + one junk file + a subdirectory.
	for _, name := range []string{"wrap-up-20240101-090000.md", "wrap-up-20240102-090000.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("# Team Wrap-Up Report\n**Trigger:** manual\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}

	reports, err := ListWrapUpReports()
	if err != nil {
		t.Fatalf("ListWrapUpReports: %v", err)
	}
	if len(reports) != 2 {
		t.Fatalf("expected 2 valid reports, got %d (%+v)", len(reports), reports)
	}
	// Newest-first ordering.
	if reports[0].Filename != "wrap-up-20240102-090000.md" {
		t.Errorf("expected newest report first, got %q", reports[0].Filename)
	}
	if reports[0].Trigger != "manual" {
		t.Errorf("expected parsed trigger 'manual', got %q", reports[0].Trigger)
	}
}

// TestListWrapUpReports_MissingDir returns an empty list (not an error) when
// the reports directory doesn't exist.
func TestListWrapUpReports_MissingDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	reports, err := ListWrapUpReports()
	if err != nil {
		t.Errorf("expected no error for missing dir, got %v", err)
	}
	if len(reports) != 0 {
		t.Errorf("expected empty list, got %d", len(reports))
	}
}
