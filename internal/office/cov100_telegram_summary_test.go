package office

import (
	"strings"
	"testing"
	"time"
)

// TestBuildSprintSummary_RichState drives the uptime-hours, in-progress,
// bug-task, active-meeting, and nil-config branches of buildSprintSummary.
func TestBuildSprintSummary_RichState(t *testing.T) {
	m := newTestManager()
	// Uptime > 1h to hit the hours>0 branch.
	m.mu.Lock()
	m.teamSpawnedAt = time.Now().Add(-90 * time.Minute)
	m.mu.Unlock()

	m.RegisterAgent("sess-1", "vex", &TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend"})
	a := m.GetAgent("sess-1")
	a.AddTask(&Task{ID: "t-done", Title: "Done task", Status: TaskCompleted})
	a.AddTask(&Task{ID: "t-wip", Title: "WIP task", Status: TaskInProgress})

	// Inject a nil-config agent so the nil-config skip runs.
	m.mu.Lock()
	m.agents["sess-nil"] = NewAgentState("sess-nil", "nilcfg", nil)
	m.mu.Unlock()

	// Bug tasks: one open, one resolved.
	m.AddBugTask(&BugTask{ID: "b1", Status: "open"})
	m.AddBugTask(&BugTask{ID: "b2", Status: "resolved"})

	// Active meeting.
	m.StartMeeting("brainstorm", []string{"sess-1"})

	out := m.buildSprintSummary()
	for _, want := range []string{"Uptime: 1h 30m", "In Progress", "WIP task", "Bug Tasks", "1 open, 1 resolved", "Active Meetings"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected summary to contain %q, got:\n%s", want, out)
		}
	}
}

// TestBuildSprintSummary_UptimeMinutesOnly hits the hours==0 (minutes-only)
// uptime branch.
func TestBuildSprintSummary_UptimeMinutesOnly(t *testing.T) {
	m := newTestManager()
	m.mu.Lock()
	m.teamSpawnedAt = time.Now().Add(-5 * time.Minute)
	m.mu.Unlock()
	m.RegisterAgent("s", "c", &TeamMemberConfig{ID: "c", Name: "C", Role: "R"})
	out := m.buildSprintSummary()
	if !strings.Contains(out, "Uptime: 5m") {
		t.Errorf("expected minutes-only uptime, got:\n%s", out)
	}
}

// TestBuildSprintSummary_TruncatesLongOutput drives the 3900-byte rune-safe
// truncation.
func TestBuildSprintSummary_TruncatesLongOutput(t *testing.T) {
	m := newTestManager()
	// Register many agents with long in-progress task titles so the summary
	// blows past 3900 bytes.
	for i := 0; i < 60; i++ {
		sid := "sess-" + string(rune('A'+i%26)) + string(rune('0'+i/26))
		cfg := &TeamMemberConfig{ID: sid, Name: strings.Repeat("名", 30), Role: "Engineer"}
		m.RegisterAgent(sid, sid, cfg)
		m.GetAgent(sid).AddTask(&Task{ID: sid + "-t", Title: strings.Repeat("работа", 20), Status: TaskInProgress})
	}
	out := m.buildSprintSummary()
	if len(out) > 3900 {
		t.Errorf("expected output truncated to <=3900 bytes, got %d", len(out))
	}
	// Must remain valid UTF-8 (no split multibyte rune).
	if !utf8ValidString(out) {
		t.Error("truncated summary is not valid UTF-8")
	}
}

// TestGetLeadStatusText_PinnedGhostFallback covers the pinned-missing +
// no-strategic-fallback false return.
func TestGetLeadStatusText_PinnedGhostFallback(t *testing.T) {
	m := newTestManager()
	if _, ok := m.GetLeadStatusText("ghost"); ok {
		t.Error("expected ok=false for missing pinned + no strategic")
	}
}

// TestGetLeadStatusText_ActiveTaskTrimsProgress drives the active-task
// branch including the >3 progress-log trim, the StartedAt "running"
// duration, and the tier line — branches the existing HasLead test misses.
func TestGetLeadStatusText_ActiveTaskTrimsProgress(t *testing.T) {
	m := newTestManager()
	m.RegisterAgent("sess-lead", "lead", &TeamMemberConfig{ID: "lead", Name: "Lead", Role: "Manager", Tier: "strategic"})
	a := m.GetAgent("sess-lead")
	task := &Task{ID: "lt", Title: "Ship release", Status: TaskInProgress, StartedAt: time.Now().Add(-time.Hour)}
	task.ProgressLog = []TaskProgressEntry{
		{Timestamp: time.Now(), Note: "step 1"},
		{Timestamp: time.Now(), Note: "step 2"},
		{Timestamp: time.Now(), Note: "step 3"},
		{Timestamp: time.Now(), Note: "step 4"},
	}
	a.AddTask(task)

	text, ok := m.GetLeadStatusText("")
	if !ok {
		t.Fatal("expected ok=true for strategic lead with active task")
	}
	for _, want := range []string{"Lead", "strategic tier", "Ship release", "Recent progress", "step 4"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected status text to contain %q, got:\n%s", want, text)
		}
	}
}

// TestGetLeadStatusText_NoActiveTask drives the "No active task" else
// branch via a strategic lead with only pending work.
func TestGetLeadStatusText_NoActiveTask(t *testing.T) {
	m := newTestManager()
	m.RegisterAgent("sess-lead", "lead", &TeamMemberConfig{ID: "lead", Name: "Lead", Role: "Manager", Tier: "strategic"})
	a := m.GetAgent("sess-lead")
	a.AddTask(&Task{ID: "p1", Title: "Pending", Status: TaskPending})
	text, ok := m.GetLeadStatusText("")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if !strings.Contains(text, "No active task") {
		t.Errorf("expected 'No active task', got:\n%s", text)
	}
}
