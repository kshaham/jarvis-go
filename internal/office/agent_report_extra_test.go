package office

import (
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// AgentState accessors
// ---------------------------------------------------------------------------

// TestNextPendingTask_ReturnsFirstPendingOrAcknowledged verifies the first
// pending/acknowledged task is returned (skipping in-progress/completed),
// as a deep copy, and nil when none remain.
func TestNextPendingTask_ReturnsFirstPendingOrAcknowledged(t *testing.T) {
	a := NewAgentState("sess-1", "cfg-1", &TeamMemberConfig{ID: "cfg-1", Name: "A"})

	// No tasks -> nil.
	if a.NextPendingTask() != nil {
		t.Fatal("expected nil with empty queue")
	}

	a.AddTask(&Task{ID: "t-done", Status: TaskCompleted})
	a.AddTask(&Task{ID: "t-active", Status: TaskInProgress})
	a.AddTask(&Task{ID: "t-pending", Title: "real next", Status: TaskPending})

	next := a.NextPendingTask()
	if next == nil || next.ID != "t-pending" {
		t.Fatalf("expected t-pending, got %+v", next)
	}
	// Deep copy: mutating the returned task must not affect the queue.
	next.Title = "MUTATED"
	again := a.NextPendingTask()
	if again.Title == "MUTATED" {
		t.Error("NextPendingTask must return a deep copy")
	}

	// Acknowledged also counts.
	a2 := NewAgentState("sess-2", "cfg-2", &TeamMemberConfig{ID: "cfg-2"})
	a2.AddTask(&Task{ID: "t-ack", Status: TaskAcknowledged})
	if got := a2.NextPendingTask(); got == nil || got.ID != "t-ack" {
		t.Errorf("expected acknowledged task returned, got %+v", got)
	}
}

// TestSetTaskBlockerDescription verifies the blocker description is set on
// the matching task and false is returned for an unknown ID.
func TestSetTaskBlockerDescription(t *testing.T) {
	a := NewAgentState("sess-1", "cfg-1", &TeamMemberConfig{ID: "cfg-1"})
	a.AddTask(&Task{ID: "t-1", Status: TaskBlocked})

	if !a.SetTaskBlockerDescription("t-1", "waiting on API key") {
		t.Fatal("expected true for existing task")
	}
	got := a.GetTasks()[0]
	if got.BlockerDescription != "waiting on API key" {
		t.Errorf("expected blocker description set, got %q", got.BlockerDescription)
	}
	if a.SetTaskBlockerDescription("ghost", "x") {
		t.Error("expected false for unknown task ID")
	}
}

// TestAppendAndFlushTurnText verifies buffering and flushing of turn text,
// the empty-string no-op, and the 128 KB cap.
func TestAppendAndFlushTurnText(t *testing.T) {
	a := NewAgentState("sess-1", "cfg-1", &TeamMemberConfig{ID: "cfg-1"})

	a.AppendTurnText("") // no-op
	a.AppendTurnText("hello ")
	a.AppendTurnText("world")

	if got := a.FlushTurnText(); got != "hello world" {
		t.Errorf("expected accumulated text, got %q", got)
	}
	// After flush, the buffer is empty.
	if got := a.FlushTurnText(); got != "" {
		t.Errorf("expected empty buffer after flush, got %q", got)
	}

	// Overflow cap: write more than turnTextBufCap; result must be capped.
	big := strings.Repeat("a", turnTextBufCap+5000)
	a.AppendTurnText(big)
	got := a.FlushTurnText()
	if len(got) != turnTextBufCap {
		t.Errorf("expected turn text capped at %d, got %d", turnTextBufCap, len(got))
	}
}

// TestSetEventsDropped verifies the drop counter is stored.
func TestSetEventsDropped(t *testing.T) {
	a := NewAgentState("sess-1", "cfg-1", &TeamMemberConfig{ID: "cfg-1"})
	a.SetEventsDropped(42)
	a.mu.Lock()
	got := a.EventsDropped
	a.mu.Unlock()
	if got != 42 {
		t.Errorf("expected EventsDropped=42, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// report.go pure formatters
// ---------------------------------------------------------------------------

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{2 * time.Hour, "2h 0m"},
		{90 * time.Minute, "1h 30m"},
		{5 * time.Minute, "5m 0s"},
		{90 * time.Second, "1m 30s"},
		{45 * time.Second, "45s"},
		{0, "0s"},
	}
	for _, c := range cases {
		if got := formatDuration(c.in); got != c.want {
			t.Errorf("formatDuration(%s)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestHandoffName(t *testing.T) {
	if got := handoffName("Vex", "vex-cfg"); got != "Vex" {
		t.Errorf("expected name preferred, got %q", got)
	}
	if got := handoffName("", "vex-cfg"); got != "vex-cfg" {
		t.Errorf("expected fallback to configID, got %q", got)
	}
}

func TestIndentBlock(t *testing.T) {
	got := indentBlock("line1\nline2\n", "> ")
	want := "> line1\n> line2"
	if got != want {
		t.Errorf("indentBlock=%q want %q", got, want)
	}
	// Single line.
	if got := indentBlock("solo", "  "); got != "  solo" {
		t.Errorf("indentBlock single=%q", got)
	}
}

// TestWriteHandoffLine verifies the rendered handoff line includes both
// names, the title, status, and the reject reason when applicable.
func TestWriteHandoffLine(t *testing.T) {
	var b strings.Builder
	writeHandoffLine(&b, &HandoffRequest{
		FromName:     "Vex",
		ToName:       "Bolt",
		TaskTitle:    "Refactor auth",
		Status:       HandoffRejected,
		RejectReason: "too busy",
		CreatedAt:    time.Date(2026, 5, 30, 9, 15, 0, 0, time.UTC),
	})
	out := b.String()
	for _, want := range []string{"Vex", "Bolt", "Refactor auth", "rejected", "too busy", "09:15"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected handoff line to contain %q, got %q", want, out)
		}
	}

	// Accepted status: no reject reason appended even if present.
	var b2 strings.Builder
	writeHandoffLine(&b2, &HandoffRequest{
		FromName: "Vex", ToName: "Bolt", TaskTitle: "Task", Status: HandoffAccepted,
		RejectReason: "should not appear", CreatedAt: time.Now(),
	})
	if strings.Contains(b2.String(), "should not appear") {
		t.Error("reject reason must not appear for accepted handoffs")
	}
}

// ---------------------------------------------------------------------------
// taskgraph priorityRank
// ---------------------------------------------------------------------------

func TestPriorityRank(t *testing.T) {
	cases := map[string]int{"high": 0, "medium": 1, "low": 2, "": 3, "weird": 3}
	for in, want := range cases {
		if got := priorityRank(in); got != want {
			t.Errorf("priorityRank(%q)=%d want %d", in, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// brainstorm helpers
// ---------------------------------------------------------------------------

// TestGetLastBrainstormAt verifies the getter reflects a StartBrainstorm
// (which records lastBrainstormAt under the lock).
func TestGetLastBrainstormAt(t *testing.T) {
	m, _, _ := newCtrlManager(t, idleTestConfig())
	if !m.GetLastBrainstormAt().IsZero() {
		t.Error("expected zero lastBrainstormAt before any brainstorm")
	}
	m.RegisterAgent("sess-a", "a", &TeamMemberConfig{ID: "a", Name: "A", Tier: "worker"})
	m.RegisterAgent("sess-b", "b", &TeamMemberConfig{ID: "b", Name: "B", Tier: "worker"})
	if mtg := m.StartBrainstorm([]string{"sess-a", "sess-b"}); mtg == nil {
		t.Fatal("expected brainstorm to start")
	}
	if m.GetLastBrainstormAt().IsZero() {
		t.Error("expected lastBrainstormAt set after StartBrainstorm")
	}
}

// TestStartMeetingByID verifies the deterministic-ID meeting helper stores
// a meeting under the requested ID and marks it active.
func TestStartMeetingByID(t *testing.T) {
	m, _, _ := newCtrlManager(t, DefaultConfig())
	mtg := m.startMeetingByID("fixed-id", "brainstorm", []string{"x", "y"})
	if mtg.ID != "fixed-id" || !mtg.Active {
		t.Fatalf("unexpected meeting: %+v", mtg)
	}
	if m.ActiveMeetingCount() != 1 {
		t.Errorf("expected 1 active meeting, got %d", m.ActiveMeetingCount())
	}
}

// ---------------------------------------------------------------------------
// AppendMissionInsight
// ---------------------------------------------------------------------------

// TestAppendMissionInsight verifies a learned insight is appended to the
// mission, and that empty mission/insight are no-ops.
func TestAppendMissionInsight(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m, _, _ := newCtrlManager(t, DefaultConfig())

	// No mission set -> no-op.
	m.AppendMissionInsight("first insight")
	if m.GetTeamMission() != "" {
		t.Errorf("expected no mission when none set, got %q", m.GetTeamMission())
	}

	m.SetTeamMission("Build the dashboard")
	// Empty insight -> no-op.
	m.AppendMissionInsight("")
	if m.GetTeamMission() != "Build the dashboard" {
		t.Errorf("empty insight must not change mission, got %q", m.GetTeamMission())
	}

	m.AppendMissionInsight("prefer server components")
	got := m.GetTeamMission()
	if !strings.Contains(got, "Build the dashboard") || !strings.Contains(got, "[Learned] prefer server components") {
		t.Errorf("expected appended insight, got %q", got)
	}
}
