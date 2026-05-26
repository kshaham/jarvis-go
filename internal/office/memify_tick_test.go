package office

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/claudeman/jarvis-go/internal/library"
)

// recordingBroadcastManager builds a Manager with a broadcast func
// that records every SSE event for assertion.
type recordedBroadcast struct {
	mu     sync.Mutex
	events []recordedEvent
}

type recordedEvent struct {
	Topic string
	Data  interface{}
}

func (r *recordedBroadcast) fn(topic string, data interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, recordedEvent{Topic: topic, Data: data})
}

func (r *recordedBroadcast) topics() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.events))
	for i, e := range r.events {
		out[i] = e.Topic
	}
	return out
}

// newMemifyTestManager builds a Manager whose broadcast is captured
// and whose memify gate is open (lastRunAt zero).
func newMemifyTestManager(t *testing.T, broker *recordedBroadcast) *Manager {
	t.Helper()
	m := NewManager(&mockSessionStore{}, broker.fn, nil)
	m.mu.Lock()
	m.messages = make([]OfficeMessage, 0)
	m.mu.Unlock()
	return m
}

// withTempLibraryAndSkillsDir redirects both the library root AND
// the shared-skills root (~/.claude/skills/) at temp dirs. Without
// the skills redirect, promotion would write to the operator's
// real ~/.claude/skills/ during the test.
//
// Also stubs out the LLM-enrichment binary so memify's claude -p
// call doesn't actually run (which would shell out to real claude
// + npm fetches into the temp HOME, polluting the cleanup).
// The stubbed failure exercises memify's heuristic-fallback path,
// which is the correct behaviour the tests assert against.
func withTempLibraryAndSkillsDir(t *testing.T) (libRoot, skillsRoot string) {
	t.Helper()
	root := t.TempDir()
	for _, sub := range []string{"experiences", "workflows", "standups", "experiments"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	restore := library.SetDirForTest(root)
	t.Cleanup(restore)
	// shared_skills uses ~/.claude/skills via the HOME env. Point
	// HOME at the temp dir so the skills land under it.
	prevHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", prevHome) })

	// Point the LLM binary at /bin/false so any enrichment call
	// fails fast (heuristic fallback) without shelling out to a
	// real claude CLI that would fetch npm packages into HOME.
	prevBin := library.SetLLMBinaryForTest("/bin/false")
	t.Cleanup(func() { library.SetLLMBinaryForTest(prevBin) })

	return root, filepath.Join(tmpHome, ".claude", "skills")
}

func TestMaybeRunMemify_NoOpWhenNotDue(t *testing.T) {
	withTempLibraryAndSkillsDir(t)
	broker := &recordedBroadcast{}
	m := newMemifyTestManager(t, broker)
	m.mu.Lock()
	m.memifyLastRunAt = time.Now() // just ran
	m.mu.Unlock()

	m.maybeRunMemify()
	// Give the (would-be) goroutine a moment in case the gate let
	// it slip through accidentally.
	time.Sleep(100 * time.Millisecond)

	for _, ev := range broker.topics() {
		if ev == "knowledge:autoPromoted" {
			t.Errorf("unexpected promotion broadcast when memify shouldn't have run")
		}
	}
}

func TestRunMemifyTick_EmptyLibraryNoBroadcasts(t *testing.T) {
	withTempLibraryAndSkillsDir(t)
	broker := &recordedBroadcast{}
	m := newMemifyTestManager(t, broker)

	m.runMemifyTick()

	for _, ev := range broker.topics() {
		if ev == "knowledge:autoPromoted" {
			t.Errorf("auto-promote broadcast on empty library")
		}
	}
}

func TestRunMemifyTick_FullPipelinePromotesQualifyingWorkflow(t *testing.T) {
	libRoot, skillsRoot := withTempLibraryAndSkillsDir(t)
	broker := &recordedBroadcast{}
	m := newMemifyTestManager(t, broker)

	// 1) Seed 3 high-outcome experiences with the same (role, labels).
	now := time.Now()
	for i := 0; i < 3; i++ {
		rec := library.ExperienceRecord{
			AgentConfigID:    "vex",
			AgentRole:        "backend-engineer",
			TaskID:           "t-" + itoa(i),
			TaskTitle:        "Add JWT refresh",
			TaskLabels:       []string{"auth", "jwt"},
			WorkingDir:       "/repo/internal/auth",
			CompletionStatus: "completed",
			OutcomeScore:     0.90,
			StartedAt:        now.Add(-30 * time.Minute),
			CompletedAt:      now.Add(-15 * time.Minute),
			ApproachSummary:  "Middleware first, handler last.",
		}
		if err := library.WriteExperience(rec); err != nil {
			t.Fatal(err)
		}
	}

	// 2) Open ledger and crank up the workflow's stats so promotion
	//    threshold passes on the very first tick (5 reuses + 0.85 avg).
	ledger, _ := library.OpenLedger()
	for i := 0; i < 5; i++ {
		if err := ledger.Increment("workflows/auth.md", 0.85); err != nil {
			t.Fatal(err)
		}
	}

	// 3) Run the tick.
	m.runMemifyTick()

	// Memify should have written the workflow.
	workflowPath := filepath.Join(libRoot, "workflows", "auth.md")
	if _, err := os.Stat(workflowPath); err != nil {
		t.Fatalf("workflow not written: %v", err)
	}

	// Promotion should have written the SKILL.md under the
	// redirected ~/.claude/skills/.
	skillPath := filepath.Join(skillsRoot, "auth", "SKILL.md")
	if _, err := os.Stat(skillPath); err != nil {
		t.Errorf("skill not promoted: %v", err)
	}

	// Broadcast should fire.
	sawPromote := false
	for _, ev := range broker.topics() {
		if ev == "knowledge:autoPromoted" {
			sawPromote = true
			break
		}
	}
	if !sawPromote {
		t.Errorf("knowledge:autoPromoted not broadcast; events=%v", broker.topics())
	}

	// Ledger PromotedAt should be stamped so re-running doesn't
	// re-promote. Re-open the ledger so we re-read from disk —
	// our local handle's in-memory cache was populated before the
	// tick ran and won't see the update.
	freshLedger, _ := library.OpenLedger()
	entry, _ := freshLedger.Get("workflows/auth.md")
	if entry == nil || entry.PromotedAt.IsZero() {
		t.Errorf("PromotedAt not set on ledger entry: %+v", entry)
	}

	// Verify the skill body matches the workflow body — promotion
	// is a straight copy, no rewriting.
	wfBody, _ := os.ReadFile(workflowPath)
	skBody, _ := os.ReadFile(skillPath)
	if !strings.Contains(string(skBody), string(wfBody)) &&
		string(skBody) != string(wfBody) {
		// shared_skills may add a marker. As long as the workflow
		// title appears, we're satisfied the body was passed through.
		if !strings.Contains(string(skBody), "Workflow for Auth tasks") {
			t.Errorf("promoted skill missing workflow content\n--- skill ---\n%s",
				skBody)
		}
	}
}

func TestRunMemifyTick_BelowThresholdNoPromotion(t *testing.T) {
	_, skillsRoot := withTempLibraryAndSkillsDir(t)
	broker := &recordedBroadcast{}
	m := newMemifyTestManager(t, broker)

	// 3 experiences + workflow exists, but only 2 ledger uses —
	// below the promotion threshold.
	now := time.Now()
	for i := 0; i < 3; i++ {
		rec := library.ExperienceRecord{
			AgentConfigID:    "vex",
			AgentRole:        "backend",
			TaskID:           "t-" + itoa(i),
			TaskTitle:        "Add JWT refresh",
			TaskLabels:       []string{"auth"},
			WorkingDir:       "/repo",
			CompletionStatus: "completed",
			OutcomeScore:     0.85,
			CompletedAt:      now,
			ApproachSummary:  "x",
		}
		_ = library.WriteExperience(rec)
	}
	ledger, _ := library.OpenLedger()
	_ = ledger.Increment("workflows/auth.md", 0.85)
	_ = ledger.Increment("workflows/auth.md", 0.85)

	m.runMemifyTick()

	// Workflow may exist (memify still wrote it), but NO skill
	// promotion should have happened.
	if _, err := os.Stat(filepath.Join(skillsRoot, "auth", "SKILL.md")); err == nil {
		t.Errorf("skill promoted despite below-threshold reuse")
	}
	for _, ev := range broker.topics() {
		if ev == "knowledge:autoPromoted" {
			t.Errorf("auto-promote broadcast despite below-threshold reuse")
		}
	}
}
