package office

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedLearningFile writes a learning JSON file for configID under the
// test HOME's ~/.jarvis/learnings/ directory.
func seedLearningFile(t *testing.T, configID string, ld AgentLearningData) {
	t.Helper()
	home := os.Getenv("HOME")
	dir := filepath.Join(home, ".jarvis", "learnings")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ld.ConfigID = configID
	data, _ := json.Marshal(ld)
	if err := os.WriteFile(filepath.Join(dir, configID+".json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestAddLearningEntry_CapsAtMax drives the maxLearnings cap branch
// (learning.go ~265). We seed a file already at the cap, then add one
// more so the slice is trimmed back down to maxLearnings.
func TestAddLearningEntry_CapsAtMax(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	full := make([]AgentLearning, maxLearnings)
	for i := range full {
		full[i] = AgentLearning{Category: "x", Content: "old"}
	}
	seedLearningFile(t, "capper", AgentLearningData{Learnings: full})

	if err := AddLearningEntry("capper", "new-cat", "newest", "t1", "Task"); err != nil {
		t.Fatal(err)
	}
	ld := LoadLearningData("capper")
	// Adding one past the cap trims to maxLearnings before auto-distill
	// may consume a batch, so the count must never exceed the cap.
	if len(ld.Learnings) > maxLearnings {
		t.Errorf("expected learnings capped at <= %d, got %d", maxLearnings, len(ld.Learnings))
	}
	// The newest entry must be present (oldest got dropped, never the new one).
	last := ld.Learnings[len(ld.Learnings)-1]
	if last.Content != "newest" {
		t.Errorf("expected newest entry retained, got %q", last.Content)
	}
}

// TestAddSkillEntry_CapsAtMax drives the maxSkills cap branch (~311).
func TestAddSkillEntry_CapsAtMax(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	full := make([]AgentSkillEntry, maxSkills)
	for i := range full {
		full[i] = AgentSkillEntry{Name: "old", Description: "d"}
	}
	seedLearningFile(t, "skiller", AgentLearningData{Skills: full})

	if err := AddSkillEntry("skiller", "brand-new-skill", "desc"); err != nil {
		t.Fatal(err)
	}
	ld := LoadLearningData("skiller")
	if len(ld.Skills) != maxSkills {
		t.Errorf("expected skills capped at %d, got %d", maxSkills, len(ld.Skills))
	}
}

// TestBuildLearningsPromptSection_TruncatesToTen drives the >10
// truncation branch in BuildLearningsPromptSection (~391). We seed 12
// learnings; only the 10 most recent should be rendered.
func TestBuildLearningsPromptSection_TruncatesToTen(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var ls []AgentLearning
	for i := 0; i < 12; i++ {
		ls = append(ls, AgentLearning{Category: "cat", Content: "learning-" + itoa(i)})
	}
	seedLearningFile(t, "many", AgentLearningData{Learnings: ls})

	out := BuildLearningsPromptSection("many")
	// learning-0 and learning-1 are the two oldest and should be dropped.
	if strings.Contains(out, "learning-0\n") || strings.Contains(out, "learning-1\n") {
		t.Errorf("expected the two oldest learnings dropped, got:\n%s", out)
	}
	// learning-11 (newest) must be present.
	if !strings.Contains(out, "learning-11") {
		t.Errorf("expected newest learning present, got:\n%s", out)
	}
}

// TestBuildCrossAgentContext_ExcludeEmptyAndTruncate drives several
// branches of BuildCrossAgentContext: the excludeConfigID skip (~462),
// the empty-learnings skip (~471), the >3-recent truncation (~477), and
// the name=="" fallback to configID (~483).
func TestBuildCrossAgentContext_ExcludeEmptyAndTruncate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// "self" is excluded.
	seedLearningFile(t, "self", AgentLearningData{
		Name:      "Self",
		Learnings: []AgentLearning{{Category: "c", Content: "should-not-appear"}},
	})
	// "empty" has no learnings => skipped.
	seedLearningFile(t, "empty", AgentLearningData{Name: "Empty"})
	// "peer" has >3 learnings and an EMPTY name => uses configID as label,
	// and only the last 3 learnings are shown.
	var many []AgentLearning
	for i := 0; i < 5; i++ {
		many = append(many, AgentLearning{Category: "c", Content: "peer-learning-" + itoa(i)})
	}
	seedLearningFile(t, "peer", AgentLearningData{Name: "", Learnings: many})

	out := BuildCrossAgentContext("self")

	if strings.Contains(out, "should-not-appear") {
		t.Error("excluded agent's learnings should not appear")
	}
	// Empty peer name falls back to its configID "peer".
	if !strings.Contains(out, "peer:") {
		t.Errorf("expected configID fallback label 'peer:', got:\n%s", out)
	}
	// Only the 3 most recent (peer-learning-2/3/4) should be shown.
	if strings.Contains(out, "peer-learning-0") || strings.Contains(out, "peer-learning-1") {
		t.Errorf("expected only the 3 most recent peer learnings, got:\n%s", out)
	}
	if !strings.Contains(out, "peer-learning-4") {
		t.Errorf("expected the newest peer learning, got:\n%s", out)
	}
}
