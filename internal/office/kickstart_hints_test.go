package office

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/claudeman/jarvis-go/internal/library"
)

// withTempLibraryForOffice points library.Dir() at a temp dir for
// the duration of a single test. Avoids polluting the real
// ~/.jarvis/office-memory/library/ during the office test suite.
func withTempLibraryForOffice(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, sub := range []string{"experiences", "workflows", "standups", "experiments"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	restore := library.SetDirForTest(root)
	t.Cleanup(restore)
	return root
}

func TestBuildLibraryHintsSection_EmptyWhenLibraryEmpty(t *testing.T) {
	withTempLibraryForOffice(t)
	cfg := &TeamMemberConfig{ID: "vex", Name: "Vex", Role: "backend-engineer"}
	got := BuildLibraryHintsSection(cfg, "/repo/internal/auth")
	if got != "" {
		t.Errorf("expected empty on empty library, got:\n%s", got)
	}
}

func TestBuildLibraryHintsSection_RendersWhenHitsExist(t *testing.T) {
	withTempLibraryForOffice(t)
	// Seed a strong, recent, on-role, on-dir hit.
	rec := library.ExperienceRecord{
		AgentConfigID:    "vex",
		AgentName:        "Vex",
		AgentRole:        "backend-engineer",
		TaskID:           "task-1",
		TaskTitle:        "Add JWT refresh",
		TaskLabels:       []string{"auth", "jwt"},
		WorkingDir:       "/repo/internal/auth",
		CompletionStatus: "completed",
		OutcomeScore:     0.90,
		StartedAt:        time.Now().Add(-time.Hour),
		CompletedAt:      time.Now().Add(-30 * time.Minute),
		ApproachSummary:  "Built middleware first, then wired the handler.",
	}
	if err := library.WriteExperience(rec); err != nil {
		t.Fatal(err)
	}
	cfg := &TeamMemberConfig{ID: "iris", Name: "Iris", Role: "backend-engineer"}
	got := BuildLibraryHintsSection(cfg, "/repo/internal/auth")
	if got == "" {
		t.Fatal("expected non-empty section, got empty")
	}
	for _, want := range []string{
		"[outcome 0.90]",
		"Add JWT refresh",
		"Built middleware first",
		"Labels: auth, jwt",
		"experiences/vex/task-1.md",
		"mcp__pageindex__get_page_content",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("section missing %q\n--- section ---\n%s", want, got)
		}
	}
}

func TestBuildLibraryHintsSection_NilConfigEmpty(t *testing.T) {
	withTempLibraryForOffice(t)
	if got := BuildLibraryHintsSection(nil, "/anywhere"); got != "" {
		t.Errorf("expected empty for nil config, got:\n%s", got)
	}
}

func TestBuildKickstartPrompt_IncludesLibrarySectionWhenAvailable(t *testing.T) {
	withTempLibraryForOffice(t)
	rec := library.ExperienceRecord{
		AgentConfigID:    "lead",
		AgentRole:        "strategic-lead",
		TaskID:           "t-kick",
		TaskTitle:        "Plan the sprint",
		TaskLabels:       []string{"planning"},
		WorkingDir:       "/repo",
		CompletionStatus: "completed",
		OutcomeScore:     0.85,
		StartedAt:        time.Now().Add(-2 * time.Hour),
		CompletedAt:      time.Now().Add(-time.Hour),
		ApproachSummary:  "Scoped tightly, broke into 3 milestones.",
	}
	if err := library.WriteExperience(rec); err != nil {
		t.Fatal(err)
	}
	cfg := &TeamMemberConfig{ID: "new-lead", Name: "NewLead", Role: "strategic-lead", Tier: "strategic"}
	prompt := BuildKickstartPrompt(cfg, "/repo", nil, "ship the thing", "", true)
	if !strings.Contains(prompt, "## What Worked Before") {
		t.Errorf("kickstart missing 'What Worked Before' section\n--- prompt ---\n%s", prompt)
	}
	if !strings.Contains(prompt, "Plan the sprint") {
		t.Errorf("kickstart missing seeded hit title\n--- prompt ---\n%s", prompt)
	}
}

func TestBuildKickstartPrompt_SectionAbsentWhenLibraryEmpty(t *testing.T) {
	// Pointing library at an empty dir means the section header
	// must NOT appear (we skip it entirely rather than rendering an
	// empty placeholder that wastes prompt budget).
	withTempLibraryForOffice(t)
	cfg := &TeamMemberConfig{ID: "vex", Name: "Vex", Role: "backend-engineer"}
	prompt := BuildKickstartPrompt(cfg, "/repo", nil, "", "", false)
	if strings.Contains(prompt, "## What Worked Before") {
		t.Errorf("section should be absent on empty library; prompt:\n%s", prompt)
	}
}

// nextTaskHintRecordingStore captures every WriteInkPrompt call so
// we can assert exactly what PromptNextTask wrote.
type nextTaskHintRecordingStore struct {
	mockSessionStore
	lastPrompt string
}

func (r *nextTaskHintRecordingStore) WriteInkPrompt(sessionID, prompt string) bool {
	r.lastPrompt = prompt
	return true
}

func TestPromptNextTask_AppendsLibraryHintWhenRelevant(t *testing.T) {
	withTempLibraryForOffice(t)
	// Seed a strong, on-role, on-labels hit.
	rec := library.ExperienceRecord{
		AgentConfigID:    "vex",
		AgentRole:        "backend-engineer",
		TaskID:           "past-1",
		TaskTitle:        "Add JWT refresh",
		TaskLabels:       []string{"auth", "jwt"},
		WorkingDir:       "/repo/internal/auth",
		CompletionStatus: "completed",
		OutcomeScore:     0.92,
		StartedAt:        time.Now().Add(-time.Hour),
		CompletedAt:      time.Now().Add(-30 * time.Minute),
		ApproachSummary:  "Middleware then handler. Tested with curl.",
	}
	if err := library.WriteExperience(rec); err != nil {
		t.Fatal(err)
	}

	rec2 := &nextTaskHintRecordingStore{}
	m := NewManager(rec2, func(string, interface{}) {}, nil)
	m.RegisterAgent("sess-iris-001", "iris",
		&TeamMemberConfig{ID: "iris", Name: "Iris", Role: "backend-engineer"})
	agent := m.GetAgent("sess-iris-001")
	if agent == nil {
		t.Fatal("agent not registered")
	}
	// Give the agent a queued task whose ID matches what we'll
	// pass to PromptNextTask. The hint lookup uses the task's
	// labels + the agent's worktree path.
	agent.SetWorktreePath("/repo/internal/auth")
	agent.AddTask(&Task{
		ID:     "task-new",
		Title:  "Add JWT refresh v2",
		Status: TaskPending,
		Labels: []string{"auth", "jwt"},
	})

	m.PromptNextTask("sess-iris-001", "Add JWT refresh v2", "task-new")

	if !strings.Contains(rec2.lastPrompt, "[NEXT TASK]") {
		t.Errorf("missing standard next-task header:\n%s", rec2.lastPrompt)
	}
	if !strings.Contains(rec2.lastPrompt, "[SIMILAR PAST TASK]") {
		t.Errorf("missing hint header:\n%s", rec2.lastPrompt)
	}
	if !strings.Contains(rec2.lastPrompt, "Add JWT refresh") {
		t.Errorf("hint missing seeded title:\n%s", rec2.lastPrompt)
	}
}

func TestPromptNextTask_NoHintWhenLibraryEmpty(t *testing.T) {
	withTempLibraryForOffice(t)
	rec := &nextTaskHintRecordingStore{}
	m := NewManager(rec, func(string, interface{}) {}, nil)
	m.RegisterAgent("sess-001", "alpha",
		&TeamMemberConfig{ID: "alpha", Name: "Alpha", Role: "backend"})
	m.PromptNextTask("sess-001", "Some task", "t-1")
	if strings.Contains(rec.lastPrompt, "[SIMILAR PAST TASK]") {
		t.Errorf("unexpected hint with empty library:\n%s", rec.lastPrompt)
	}
}
