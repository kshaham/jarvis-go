package handler

// Round-4 unit coverage for buildLibraryExperience's two residual arms:
//   - the duration calc when both StartedAt and CompletedAt are set (2388-2390)
//   - the approach fallback to the last ProgressLog note when no completion
//     summary was supplied (2394-2398)
//
// The function is pure (state -> ExperienceRecord) so it's exercised directly
// rather than through the async library-write goroutine the handler fires.

import (
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/office"
)

func TestBuildLibraryExperience_DurationAndProgressFallback(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	mgr := office.NewManager(stubSessions{}, func(string, interface{}) {}, nil)
	mgr.RegisterAgent("s-lib", "cfg-lib", &office.TeamMemberConfig{
		ID: "cfg-lib", Name: "Libby", Role: "scout",
	})
	agent := mgr.GetAgent("s-lib")
	wd := t.TempDir()
	agent.SetWorktreePath(wd)

	started := time.Now().Add(-90 * time.Second)
	completed := started.Add(60 * time.Second)
	task := &office.Task{
		ID:           "task-dur",
		Title:        "wire the thing",
		Labels:       []string{"infra"},
		StartedAt:    started,
		CompletedAt:  completed,
		FinalCostUSD: 0.42,
		ProgressLog: []office.TaskProgressEntry{
			{Note: "first step"},
			{Note: "last step note"},
		},
	}

	// completionNote == "" => approach falls back to the last progress note.
	rec := buildLibraryExperience(agent, task, "")

	if rec.DurationMS != 60_000 {
		t.Errorf("DurationMS = %d, want 60000 (60s between StartedAt/CompletedAt)", rec.DurationMS)
	}
	if rec.ApproachSummary != "last step note" {
		t.Errorf("ApproachSummary = %q, want fallback to last progress note 'last step note'", rec.ApproachSummary)
	}
	if rec.AgentName != "Libby" || rec.AgentRole != "scout" {
		t.Errorf("agent identity not carried: name=%q role=%q", rec.AgentName, rec.AgentRole)
	}
	if rec.TaskID != "task-dur" || rec.WorkingDir != wd {
		t.Errorf("task/workdir not carried: id=%q wd=%q (want wd=%q)", rec.TaskID, rec.WorkingDir, wd)
	}
	if rec.CompletionStatus != "completed" {
		t.Errorf("CompletionStatus = %q, want completed", rec.CompletionStatus)
	}
	if rec.CostUSD != 0.42 {
		t.Errorf("CostUSD = %v, want 0.42", rec.CostUSD)
	}
}

// TestBuildLibraryExperience_ExplicitNoteWinsZeroDuration covers the inverse:
// a supplied completionNote is preserved verbatim and an unstarted task yields
// a zero duration (the IsZero guard skips the subtraction).
func TestBuildLibraryExperience_ExplicitNoteWinsZeroDuration(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	mgr := office.NewManager(stubSessions{}, func(string, interface{}) {}, nil)
	mgr.RegisterAgent("s-lib2", "cfg-lib2", &office.TeamMemberConfig{ID: "cfg-lib2", Name: "Lib2"})
	agent := mgr.GetAgent("s-lib2")

	task := &office.Task{ID: "task-zero", Title: "no timestamps"}
	rec := buildLibraryExperience(agent, task, "operator summary")

	if rec.DurationMS != 0 {
		t.Errorf("DurationMS = %d, want 0 for a task with zero StartedAt/CompletedAt", rec.DurationMS)
	}
	if rec.ApproachSummary != "operator summary" {
		t.Errorf("ApproachSummary = %q, want the explicit note", rec.ApproachSummary)
	}
}
