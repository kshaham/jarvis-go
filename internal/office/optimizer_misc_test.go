package office

import (
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// optimizer.go: AnalyzeAndOptimize / InjectOptimizationSuggestions /
// buildOptimizationSuggestion
// ---------------------------------------------------------------------------

// registerScoredAgent registers an agent and gives it tasks with the given
// completed/total split so CalculateAgentScore produces a deterministic
// completion-rate-driven score.
func registerScoredAgent(m *Manager, sid, cfgID, name string, completed, failed, pending int) {
	m.RegisterAgent(sid, cfgID, &TeamMemberConfig{ID: cfgID, Name: name, Tier: "worker"})
	a := m.GetAgent(sid)
	for i := 0; i < completed; i++ {
		a.AddTask(&Task{ID: cfgID + "-c" + string(rune('0'+i)), Status: TaskCompleted})
	}
	for i := 0; i < failed; i++ {
		a.AddTask(&Task{ID: cfgID + "-f" + string(rune('0'+i)), Status: TaskFailed})
	}
	for i := 0; i < pending; i++ {
		a.AddTask(&Task{ID: cfgID + "-p" + string(rune('0'+i)), Status: TaskPending})
	}
}

// TestAnalyzeAndOptimize_TooSmallTeam verifies fewer than 3 agents yields
// no suggestions.
func TestAnalyzeAndOptimize_TooSmallTeam(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m, _, _ := newCtrlManager(t, DefaultConfig())
	registerScoredAgent(m, "sess-1", "a", "Alpha", 3, 0, 0)
	registerScoredAgent(m, "sess-2", "b", "Beta", 1, 2, 0)

	if got := m.AnalyzeAndOptimize(); got != nil {
		t.Errorf("expected nil for a 2-agent team, got %+v", got)
	}
}

// TestAnalyzeAndOptimize_ProducesSuggestionForBottom verifies a clear
// top/bottom performer split yields a PERFORMANCE INSIGHT suggestion that
// names the top performer.
func TestAnalyzeAndOptimize_ProducesSuggestionForBottom(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m, _, _ := newCtrlManager(t, DefaultConfig())

	// Top: all complete. Middle: mixed. Bottom: all failed.
	registerScoredAgent(m, "sess-top", "top", "TopDog", 4, 0, 0)
	registerScoredAgent(m, "sess-mid", "mid", "Middle", 2, 1, 1)
	registerScoredAgent(m, "sess-bot", "bot", "Loser", 0, 4, 0)

	opts := m.AnalyzeAndOptimize()
	if len(opts) == 0 {
		t.Fatal("expected at least one optimization suggestion")
	}
	// The bottom performer must be targeted, and the suggestion must
	// reference the top performer's name.
	var botOpt *PromptOptimization
	for i := range opts {
		if opts[i].TargetConfigID == "bot" {
			botOpt = &opts[i]
		}
	}
	if botOpt == nil {
		t.Fatalf("expected a suggestion targeting the bottom performer, got %+v", opts)
	}
	if !strings.Contains(botOpt.Suggestion, "PERFORMANCE INSIGHT") {
		t.Errorf("expected PERFORMANCE INSIGHT header, got %q", botOpt.Suggestion)
	}
	if !strings.Contains(botOpt.BasedOn, "TopDog") {
		t.Errorf("expected BasedOn to reference top performer, got %q", botOpt.BasedOn)
	}
}

// TestInjectOptimizationSuggestions_WritesToTarget verifies suggestions are
// delivered to the targeted agent's session, and that an empty slice is a
// no-op.
func TestInjectOptimizationSuggestions_WritesToTarget(t *testing.T) {
	m, store, _ := newCtrlManager(t, DefaultConfig())
	m.RegisterAgent("sess-target", "tgt", &TeamMemberConfig{ID: "tgt", Name: "Target", Tier: "worker"})

	// Empty slice: no-op.
	m.InjectOptimizationSuggestions(nil)
	if len(store.writesFor("sess-target")) != 0 {
		t.Error("expected no writes for empty suggestions")
	}

	m.InjectOptimizationSuggestions([]PromptOptimization{
		{TargetConfigID: "tgt", Suggestion: "[PERFORMANCE INSIGHT] do better", BasedOn: "Alpha"},
		{TargetConfigID: "ghost", Suggestion: "ignored", BasedOn: "x"}, // unknown -> skipped
	})

	found := false
	for _, w := range store.writesFor("sess-target") {
		if strings.Contains(w.Input, "[PERFORMANCE INSIGHT] do better") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected suggestion delivered to target, got %v", store.writesFor("sess-target"))
	}
}

// TestBuildOptimizationSuggestion_IncludesGapHints verifies the suggestion
// text reflects the completion-rate and collaboration gaps between top and
// bottom performers.
func TestBuildOptimizationSuggestion_IncludesGapHints(t *testing.T) {
	top := AgentScore{Name: "TopDog", OverallScore: 0.95, CompletionRate: 1.0, CollaborationScore: 0.9, Grade: "A"}
	bottom := AgentScore{Name: "Loser", OverallScore: 0.2, CompletionRate: 0.1, CollaborationScore: 0.1, Grade: "F"}
	topL := &AgentLearningData{}
	bottomL := &AgentLearningData{}

	got := buildOptimizationSuggestion(top, bottom, topL, bottomL)
	if !strings.Contains(got, "Focus on completing tasks") {
		t.Errorf("expected completion-gap hint, got %q", got)
	}
	if !strings.Contains(got, "Reflect more often") {
		t.Errorf("expected collaboration-gap hint, got %q", got)
	}
	if !strings.Contains(got, "TopDog") {
		t.Errorf("expected top performer name in suggestion, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// scoring: GetTopPerformers / GetBottomPerformers
// ---------------------------------------------------------------------------

func TestGetTopAndBottomPerformers(t *testing.T) {
	scores := []AgentScore{
		{ConfigID: "a", OverallScore: 0.9},
		{ConfigID: "b", OverallScore: 0.3},
		{ConfigID: "c", OverallScore: 0.6},
	}
	top := GetTopPerformers(scores, 2)
	if len(top) != 2 || top[0].ConfigID != "a" || top[1].ConfigID != "c" {
		t.Errorf("unexpected top performers: %+v", top)
	}
	bottom := GetBottomPerformers(scores, 2)
	if len(bottom) != 2 || bottom[0].ConfigID != "b" || bottom[1].ConfigID != "c" {
		t.Errorf("unexpected bottom performers: %+v", bottom)
	}
	// n larger than slice clamps.
	if got := GetTopPerformers(scores, 10); len(got) != 3 {
		t.Errorf("expected clamp to 3, got %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// small string helpers
// ---------------------------------------------------------------------------

func TestTruncRunes(t *testing.T) {
	if got := truncRunes("hello", 100); got != "hello" {
		t.Errorf("expected no truncation, got %q", got)
	}
	// Truncate inside a multibyte rune boundary: "héllo" — é is 2 bytes.
	s := "héllo"
	got := truncRunes(s, 2) // byte index 2 is mid-é; should back off to 1
	if got != "h" {
		t.Errorf("expected rune-safe truncation to 'h', got %q", got)
	}
}

func TestTruncateProbeOutput(t *testing.T) {
	short := "small output"
	if got := truncateProbeOutput(short); got != short {
		t.Errorf("expected short output unchanged, got %q", got)
	}
	long := strings.Repeat("x", 600)
	got := truncateProbeOutput(long)
	if !strings.HasSuffix(got, "…(truncated)") {
		t.Errorf("expected truncation suffix, got tail %q", got[len(got)-20:])
	}
}

func TestFormatTokenCount(t *testing.T) {
	cases := map[int64]string{
		0:         "0",
		999:       "999",
		1500:      "1.5k",
		2_500_000: "2.5M",
	}
	for in, want := range cases {
		if got := formatTokenCount(in); got != want {
			t.Errorf("formatTokenCount(%d)=%q want %q", in, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// IsInActiveMeeting
// ---------------------------------------------------------------------------

func TestIsInActiveMeeting_PublicAPI(t *testing.T) {
	m, _, _ := newCtrlManager(t, DefaultConfig())
	if m.IsInActiveMeeting("sess-x") {
		t.Error("expected no active meeting initially")
	}
	mtg := m.StartMeeting("brainstorm", []string{"sess-x", "sess-y"})
	if !m.IsInActiveMeeting("sess-x") {
		t.Error("expected sess-x in active meeting")
	}
	if m.IsInActiveMeeting("sess-other") {
		t.Error("expected sess-other not in meeting")
	}
	m.EndMeeting(mtg.ID)
	if m.IsInActiveMeeting("sess-x") {
		t.Error("expected no active meeting after EndMeeting")
	}
}

// ---------------------------------------------------------------------------
// SweepTriageMaps / SweepBugTasks
// ---------------------------------------------------------------------------

// TestSweepTriageMaps_DropsDeadEntries verifies entries for task IDs not in
// the live set are reclaimed while live ones are kept.
func TestSweepTriageMaps_DropsDeadEntries(t *testing.T) {
	m, _, _ := newCtrlManager(t, DefaultConfig())
	m.mu.Lock()
	m.triageAttempts["live"] = 1
	m.lastTriageAt["live"] = time.Now()
	m.triageAttempts["dead"] = 2
	m.lastTriageAt["dead"] = time.Now()
	m.mu.Unlock()

	m.SweepTriageMaps(map[string]struct{}{"live": {}})

	m.mu.RLock()
	_, liveOK := m.triageAttempts["live"]
	_, deadOK := m.triageAttempts["dead"]
	m.mu.RUnlock()
	if !liveOK {
		t.Error("expected live triage entry retained")
	}
	if deadOK {
		t.Error("expected dead triage entry swept")
	}
}

// TestSweepBugTasks_RemovesOldResolved verifies a resolved bug older than
// 24h is swept, while a recent resolved bug and open bugs are retained.
func TestSweepBugTasks_RemovesOldResolved(t *testing.T) {
	m, _, _ := newCtrlManager(t, DefaultConfig())
	m.AddBugTask(&BugTask{ID: "old", Status: "resolved", CreatedAt: time.Now().Add(-48 * time.Hour)})
	m.AddBugTask(&BugTask{ID: "recent", Status: "resolved", CreatedAt: time.Now()})
	m.AddBugTask(&BugTask{ID: "open", Status: "open", CreatedAt: time.Now().Add(-72 * time.Hour)})

	swept := m.SweepBugTasks()
	if swept != 1 {
		t.Errorf("expected 1 swept bug, got %d", swept)
	}
	ids := map[string]bool{}
	for _, bt := range m.GetBugTasks() {
		ids[bt.ID] = true
	}
	if ids["old"] {
		t.Error("expected old resolved bug removed")
	}
	if !ids["recent"] || !ids["open"] {
		t.Error("expected recent-resolved and open bugs retained")
	}
}

// ---------------------------------------------------------------------------
// project_docs: extractDocExcerpt
// ---------------------------------------------------------------------------

// TestExtractDocExcerpt verifies the H1 title and code fences are stripped
// and the first prose paragraph survives.
func TestExtractDocExcerpt(t *testing.T) {
	md := "# Project Title\n\nThis is the intro paragraph.\n\n```go\ncode here\n```\n\nMore prose."
	got := extractDocExcerpt(md)
	if strings.Contains(got, "Project Title") {
		t.Errorf("expected H1 title stripped, got %q", got)
	}
	if strings.Contains(got, "code here") {
		t.Errorf("expected code fence stripped, got %q", got)
	}
	if !strings.Contains(got, "This is the intro paragraph.") {
		t.Errorf("expected intro prose retained, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// claudefiles_defaults logging helpers
// ---------------------------------------------------------------------------

func TestCommandAndAgentNamesForLogging(t *testing.T) {
	cmds := []ClaudeCommand{{Name: "deploy"}, {Name: "review"}}
	got := CommandNamesForLogging(cmds)
	if len(got) != 2 || got[0] != "/deploy" || got[1] != "/review" {
		t.Errorf("unexpected command names: %v", got)
	}

	agents := []ClaudeAgent{{Name: "planner"}, {Name: "reviewer"}}
	gotA := AgentNamesForLogging(agents)
	if len(gotA) != 2 || gotA[0] != "planner" || gotA[1] != "reviewer" {
		t.Errorf("unexpected agent names: %v", gotA)
	}
}

// ---------------------------------------------------------------------------
// claudehooks_adapter: daemon URL getter/setter
// ---------------------------------------------------------------------------

func TestDaemonURL_RoundTrip(t *testing.T) {
	m, _, _ := newCtrlManager(t, DefaultConfig())
	if m.GetDaemonURL() != "" {
		t.Error("expected empty daemon URL by default")
	}
	m.SetDaemonURL("http://127.0.0.1:8800")
	if got := m.GetDaemonURL(); got != "http://127.0.0.1:8800" {
		t.Errorf("expected daemon URL set, got %q", got)
	}
}
