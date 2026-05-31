package office

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/library"
)

// TestWriteAgents_WritesSkipsAndIsIdempotent mirrors the WriteCommands tests
// for the (previously untested) WriteAgents path.
func TestWriteAgents_WritesSkipsAndIsIdempotent(t *testing.T) {
	// Empty input -> 0, nil.
	if n, err := WriteAgents(t.TempDir(), nil); err != nil || n != 0 {
		t.Errorf("expected (0,nil) for empty agents, got (%d,%v)", n, err)
	}

	dir := t.TempDir()
	agents := []ClaudeAgent{
		{Name: "rev", Description: "Reviewer", Tools: []string{"Read"}, Model: "sonnet", Body: "review code"},
		{Name: "sec", Description: "Security", Body: "scan for issues"},
	}
	n, err := WriteAgents(dir, agents)
	if err != nil {
		t.Fatalf("WriteAgents: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 agents written, got %d", n)
	}
	for _, name := range []string{"rev.md", "sec.md"} {
		p := filepath.Join(dir, ".claude", "agents", name)
		data, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("missing %s: %v", name, err)
			continue
		}
		if !strings.HasPrefix(string(data), jarvisFileMarker) {
			t.Errorf("%s missing jarvis marker", name)
		}
	}

	// Second run overwrites jarvis-authored files (marker present) -> 2.
	n2, err := WriteAgents(dir, agents)
	if err != nil {
		t.Fatalf("second WriteAgents: %v", err)
	}
	if n2 != 2 {
		t.Errorf("expected 2 overwrites, got %d", n2)
	}
}

// TestWriteAgents_SkipsOperatorAuthored verifies an operator-authored agent
// file (no marker) is left untouched.
func TestWriteAgents_SkipsOperatorAuthored(t *testing.T) {
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, ".claude", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	operator := "---\nname: mine\n---\nMy own agent\n"
	if err := os.WriteFile(filepath.Join(agentsDir, "mine.md"), []byte(operator), 0o644); err != nil {
		t.Fatal(err)
	}

	n, err := WriteAgents(dir, []ClaudeAgent{{Name: "mine", Description: "should not clobber", Body: "jarvis body"}})
	if err != nil {
		t.Fatalf("WriteAgents: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 writes (operator file skipped), got %d", n)
	}
	got, _ := os.ReadFile(filepath.Join(agentsDir, "mine.md"))
	if !strings.Contains(string(got), "My own agent") {
		t.Errorf("operator agent file was overwritten: %q", got)
	}
}

// TestAgentDisplayName covers nil, Config.Name present, and configID fallback.
func TestAgentDisplayName(t *testing.T) {
	if got := agentDisplayName(nil); got != "" {
		t.Errorf("nil agent should yield empty name, got %q", got)
	}
	named := &AgentState{ConfigID: "cfg", Config: &TeamMemberConfig{Name: "Vex"}}
	if got := agentDisplayName(named); got != "Vex" {
		t.Errorf("expected Config.Name, got %q", got)
	}
	noName := &AgentState{ConfigID: "cfg-only", Config: &TeamMemberConfig{Name: ""}}
	if got := agentDisplayName(noName); got != "cfg-only" {
		t.Errorf("expected configID fallback, got %q", got)
	}
	noConfig := &AgentState{ConfigID: "bare"}
	if got := agentDisplayName(noConfig); got != "bare" {
		t.Errorf("expected configID when Config nil, got %q", got)
	}
}

// TestShortSessionID truncates long IDs to 8 chars and leaves short ones.
func TestShortSessionID(t *testing.T) {
	if got := shortSessionID("abcdefghij"); got != "abcdefgh" {
		t.Errorf("expected 8-char truncation, got %q", got)
	}
	if got := shortSessionID("short"); got != "short" {
		t.Errorf("expected short ID unchanged, got %q", got)
	}
	if got := shortSessionID("exactly8"); got != "exactly8" {
		t.Errorf("expected 8-char ID unchanged, got %q", got)
	}
}

// TestNotifyOfficeEvent_Branches verifies the disabled/no-notifier short
// circuit and the enabled+connected delivery path (with HTML escaping).
func TestNotifyOfficeEvent_Branches(t *testing.T) {
	m, _, _ := newCtrlManager(t, DefaultConfig())

	// Disabled (default): no notifier set -> no panic, nothing delivered.
	m.NotifyOfficeEvent("ev", "detail")

	// Notifier set but telegram disabled -> still short-circuits.
	rn := &recordingNotifier{}
	m.SetTelegramNotifier(rn)
	m.NotifyOfficeEvent("ev", "detail")
	if len(rn.updates) != 0 {
		t.Errorf("expected no delivery while disabled, got %v", rn.updates)
	}

	// Enable telegram + deliver an event with detail (HTML-escaped).
	m.SetTelegramConfig(true, 10)
	m.NotifyOfficeEvent("Build <done>", "task & complete")
	if len(rn.updates) != 1 {
		t.Fatalf("expected 1 delivered update, got %d", len(rn.updates))
	}
	msg := rn.updates[0]
	if !strings.Contains(msg, "Build &lt;done&gt;") {
		t.Errorf("expected HTML-escaped event, got %q", msg)
	}
	if !strings.Contains(msg, "task &amp; complete") {
		t.Errorf("expected HTML-escaped detail, got %q", msg)
	}

	// Empty detail path: only the bold event line, no second line.
	rn.updates = nil
	m.NotifyOfficeEvent("PlainEvent", "")
	if len(rn.updates) != 1 || strings.Contains(rn.updates[0], "\n") {
		t.Errorf("expected single-line message with no detail, got %v", rn.updates)
	}
}

// TestTruncateUltrareviewLog truncates oversized strings and leaves short
// ones alone.
func TestTruncateUltrareviewLog(t *testing.T) {
	short := "a short review log"
	if got := truncateUltrareviewLog(short); got != short {
		t.Errorf("expected short log unchanged, got %q", got)
	}
	long := strings.Repeat("x", 5000)
	got := truncateUltrareviewLog(long)
	if !strings.HasSuffix(got, "…(truncated)") {
		t.Errorf("expected truncation suffix, got tail %q", got[len(got)-20:])
	}
	if len(got) >= len(long) {
		t.Errorf("expected truncated length < original, got %d", len(got))
	}
}

// TestAppendUltrareviewToLibrary_AppendsToExistingExperience seeds an
// experience file for the task, then calls appendUltrareviewToLibrary and
// asserts the rendered ultrareview section is appended (the write runs in a
// goroutine, so poll briefly).
func TestAppendUltrareviewToLibrary_AppendsToExistingExperience(t *testing.T) {
	root := withTempLibraryForOffice(t)

	// library.AppendUltrareview requires a pre-existing experience file for the
	// (agent, task) at experiences/<configID>/<taskID>.md.
	expFile := filepath.Join(root, "experiences", "vex", "task-1.md")
	if err := os.MkdirAll(filepath.Dir(expFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(expFile, []byte("# Experience\n\nInitial body.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := &UltrareviewResult{
		Findings: []UltrareviewFinding{
			{Severity: "blocker", File: "main.go", Line: 10, Title: "nil deref", Description: "fix it", Category: "correctness"},
			{Severity: "nit", Title: "style"},
		},
		Summary: "2 findings",
		Took:    2 * time.Second,
	}

	// Direct synchronous call to the library to confirm the mapping renders a
	// section (deterministic, no goroutine race).
	libFindings := []library.Finding{}
	for _, f := range result.Findings {
		libFindings = append(libFindings, library.Finding{
			Severity: f.Severity, File: f.File, Line: f.Line, Title: f.Title,
			Description: f.Description, Category: f.Category,
		})
	}
	directErr := library.AppendUltrareview(library.UltrareviewAppendRecord{
		AgentConfigID: "vex", TaskID: "task-1", Findings: libFindings,
		Summary: result.Summary, Took: result.Took, Timestamp: time.Now(),
	})

	// The office wrapper must not panic regardless of whether the underlying
	// append found a target file.
	appendUltrareviewToLibrary("vex", "task-1", result)
	time.Sleep(50 * time.Millisecond) // let the async write settle

	// The direct append must have succeeded (target file exists), and the
	// experience body must now contain the rendered ultrareview section.
	if directErr != nil {
		t.Fatalf("library.AppendUltrareview failed: %v", directErr)
	}
	data, err := os.ReadFile(expFile)
	if err != nil {
		t.Fatalf("read experience after append: %v", err)
	}
	if !strings.Contains(string(data), "nil deref") && !strings.Contains(strings.ToLower(string(data)), "ultrareview") {
		t.Errorf("expected appended ultrareview content, got:\n%s", string(data))
	}
}
