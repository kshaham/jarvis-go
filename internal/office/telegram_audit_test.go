package office

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Pending leader query lifecycle
// ---------------------------------------------------------------------------

// TestPendingLeaderQuery_Lifecycle verifies set/has/get/clear and that an
// expired query is reported as not-pending.
func TestPendingLeaderQuery_Lifecycle(t *testing.T) {
	m, _, _ := newCtrlManager(t, DefaultConfig())

	if m.HasPendingLeaderQuery() {
		t.Error("expected no pending query initially")
	}
	if m.GetPendingLeaderSessionID() != "" {
		t.Error("expected empty session ID initially")
	}

	m.SetPendingLeaderQuery("sess-lead", "Should I deploy?")
	if !m.HasPendingLeaderQuery() {
		t.Error("expected pending query after set")
	}
	if got := m.GetPendingLeaderSessionID(); got != "sess-lead" {
		t.Errorf("expected sess-lead, got %q", got)
	}

	m.ClearPendingLeaderQuery()
	if m.HasPendingLeaderQuery() {
		t.Error("expected no pending query after clear")
	}
	if m.GetPendingLeaderSessionID() != "" {
		t.Error("expected empty session ID after clear")
	}
}

// TestPendingLeaderQuery_Expired verifies a query past its ExpiresAt is
// treated as not-pending.
func TestPendingLeaderQuery_Expired(t *testing.T) {
	m, _, _ := newCtrlManager(t, DefaultConfig())

	m.mu.Lock()
	m.pendingLeaderQuery = &PendingLeaderQuery{
		SessionID: "sess-lead",
		Question:  "stale?",
		CreatedAt: time.Now().Add(-20 * time.Minute),
		ExpiresAt: time.Now().Add(-10 * time.Minute),
	}
	m.mu.Unlock()

	if m.HasPendingLeaderQuery() {
		t.Error("expected expired query to be reported not-pending")
	}
}

// ---------------------------------------------------------------------------
// IsTeamActive / SetTelegramNotifier
// ---------------------------------------------------------------------------

// TestIsTeamActive verifies it tracks the spawned flag.
func TestIsTeamActive(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m, _, _ := newCtrlManager(t, DefaultConfig())
	defer m.StopIdleMonitoring()

	if m.IsTeamActive() {
		t.Error("expected team inactive before spawn")
	}
	m.SetTeamSpawned(true)
	if !m.IsTeamActive() {
		t.Error("expected team active after spawn")
	}
	m.SetTeamSpawned(false)
	if m.IsTeamActive() {
		t.Error("expected team inactive after teardown")
	}
}

// recordingNotifier captures SendOfficeUpdate calls.
type recordingNotifier struct {
	updates []string
}

func (r *recordingNotifier) SendOfficeUpdate(text string)     { r.updates = append(r.updates, text) }
func (r *recordingNotifier) IsConnected() bool                { return true }
func (r *recordingNotifier) NotifyStandupComplete(s string)   { r.updates = append(r.updates, s) }

// TestSetTelegramNotifier verifies the notifier is stored (observable via a
// buildSprintSummary delivery is out of scope; here we just confirm it
// doesn't panic and the getter path through GetTelegramConfig stays sane).
func TestSetTelegramNotifier(t *testing.T) {
	m, _, _ := newCtrlManager(t, DefaultConfig())
	n := &recordingNotifier{}
	m.SetTelegramNotifier(n)
	m.mu.RLock()
	stored := m.telegramNotifier
	m.mu.RUnlock()
	if stored == nil {
		t.Error("expected telegram notifier to be stored")
	}
}

// ---------------------------------------------------------------------------
// buildSprintSummary
// ---------------------------------------------------------------------------

// TestBuildSprintSummary_RostersAndTasks verifies the summary lists working
// vs idle agents and the in-progress task title appears.
func TestBuildSprintSummary_RostersAndTasks(t *testing.T) {
	m, _, _ := newCtrlManager(t, DefaultConfig())

	m.RegisterAgent("sess-busy", "busy", &TeamMemberConfig{ID: "busy", Name: "Busy", Role: "Backend", Tier: "worker"})
	m.RegisterAgent("sess-idle", "idle", &TeamMemberConfig{ID: "idle", Name: "Idle", Role: "Frontend", Tier: "worker"})
	busy := m.GetAgent("sess-busy")
	busy.AddTask(&Task{ID: "wip", Title: "Refactor router", Status: TaskInProgress})
	busy.AddTask(&Task{ID: "done", Title: "Old task", Status: TaskCompleted})

	out := m.buildSprintSummary()
	if !strings.Contains(out, "Office Sprint Update") {
		t.Errorf("expected header, got %q", out)
	}
	if !strings.Contains(out, "Busy") || !strings.Contains(out, "Refactor router") {
		t.Errorf("expected busy agent + task in summary, got %q", out)
	}
	if !strings.Contains(out, "Idle") || !strings.Contains(out, "idle") {
		t.Errorf("expected idle agent in summary, got %q", out)
	}
}

// TestBuildSprintSummary_NoAgents verifies the no-agents branch.
func TestBuildSprintSummary_NoAgents(t *testing.T) {
	m, _, _ := newCtrlManager(t, DefaultConfig())
	out := m.buildSprintSummary()
	if !strings.Contains(out, "No agents registered") {
		t.Errorf("expected 'No agents registered', got %q", out)
	}
}

// ---------------------------------------------------------------------------
// buildTelegramStandupSummary
// ---------------------------------------------------------------------------

// TestBuildTelegramStandupSummary verifies one line per reporting agent,
// truncation of the response to its first line, and the "(no response)"
// fallback path is not triggered when a report exists.
func TestBuildTelegramStandupSummary(t *testing.T) {
	transcript := []StandupMessage{
		{SpeakerName: "Vex", Type: "status_report", Content: "Shipped auth\nmore detail"},
		{SpeakerName: "Bolt", Type: "status_report", Content: "Working on API"},
		{SpeakerName: "Lead", Type: "opening", Content: "Let's go"}, // non-report ignored
	}
	out := buildTelegramStandupSummary("su-1", []string{"sess-a", "sess-b"}, transcript)

	if !strings.Contains(out, "Standup complete") {
		t.Errorf("expected header, got %q", out)
	}
	if !strings.Contains(out, "Vex") || !strings.Contains(out, "Shipped auth") {
		t.Errorf("expected Vex's first-line report, got %q", out)
	}
	// Only the first line should appear (no "more detail").
	if strings.Contains(out, "more detail") {
		t.Errorf("expected only first line of Vex report, got %q", out)
	}
	if !strings.Contains(out, "Bolt") || !strings.Contains(out, "Working on API") {
		t.Errorf("expected Bolt's report, got %q", out)
	}
	// The non-report ("opening") message must not appear.
	if strings.Contains(out, "Let's go") {
		t.Errorf("expected non-status_report messages excluded, got %q", out)
	}
	// Vex should appear exactly once.
	if c := strings.Count(out, "Vex"); c != 1 {
		t.Errorf("expected Vex once, got %d in %q", c, out)
	}
}

// TestBuildTelegramStandupSummary_LastReportWinsPerSpeaker documents that
// when a speaker has multiple status_report messages, the LAST one is the
// one rendered (the reported map overwrites on each pass), and the speaker
// still appears only once.
func TestBuildTelegramStandupSummary_LastReportWinsPerSpeaker(t *testing.T) {
	transcript := []StandupMessage{
		{SpeakerName: "Vex", Type: "status_report", Content: "first update"},
		{SpeakerName: "Vex", Type: "status_report", Content: "later update"},
	}
	out := buildTelegramStandupSummary("su-1", []string{"sess-a"}, transcript)
	if !strings.Contains(out, "later update") {
		t.Errorf("expected last report to win, got %q", out)
	}
	if strings.Contains(out, "first update") {
		t.Errorf("expected earlier report overwritten, got %q", out)
	}
	if c := strings.Count(out, "Vex"); c != 1 {
		t.Errorf("expected Vex once, got %d", c)
	}
}

// TestBuildTelegramStandupSummary_NoResponseFallback verifies the
// "(no response)" fallback when a status_report has empty content.
func TestBuildTelegramStandupSummary_NoResponseFallback(t *testing.T) {
	transcript := []StandupMessage{
		{SpeakerName: "Ghost", Type: "status_report", Content: ""},
	}
	out := buildTelegramStandupSummary("su-1", []string{"sess-a"}, transcript)
	if !strings.Contains(out, "(no response)") {
		t.Errorf("expected (no response) fallback, got %q", out)
	}
}

// ---------------------------------------------------------------------------
// AuditWriter Count / Log / Entries / MarshalJSON
// ---------------------------------------------------------------------------

// TestAuditWriter_LogCountAndRing verifies Log appends, Count reports size,
// the ring caps at maxSize keeping the newest, and MarshalJSON round-trips.
func TestAuditWriter_LogCountAndRing(t *testing.T) {
	aw := NewAuditWriter(3)
	if aw.Count() != 0 {
		t.Fatalf("expected empty audit writer, got %d", aw.Count())
	}

	for i := 0; i < 5; i++ {
		aw.Log(AuditEntry{Event: "evt", AgentName: string(rune('A' + i))})
	}
	// Capped at 3, keeping the newest (C, D, E).
	if aw.Count() != 3 {
		t.Errorf("expected ring capped at 3, got %d", aw.Count())
	}
	entries := aw.Entries()
	if entries[0].AgentName != "C" || entries[2].AgentName != "E" {
		t.Errorf("expected newest 3 retained (C..E), got %+v", entries)
	}

	// MarshalJSON yields a JSON array of the 3 entries.
	data, err := json.Marshal(aw)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded []AuditEntry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded) != 3 {
		t.Errorf("expected 3 entries in JSON, got %d", len(decoded))
	}
}
