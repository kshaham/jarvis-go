package office

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeLearningsDirAFile points HOME at a temp dir and makes
// ~/.jarvis/learnings a regular FILE so ReadDir/MkdirAll on it fail.
func makeLearningsDirAFile(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".jarvis"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".jarvis", "learnings"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestListLearningFiles_ReadDirError covers the ReadDir error branch.
func TestListLearningFiles_ReadDirError(t *testing.T) {
	makeLearningsDirAFile(t)
	if got := ListLearningFiles(); len(got) != 0 {
		t.Errorf("expected empty when learnings dir unreadable, got %v", got)
	}
}

// TestLoadLearningsForUI_ReadError covers the non-NotExist read error
// branch: the path is a directory.
func TestLoadLearningsForUI_ReadError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".jarvis", "learnings")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Make the config's learning path a directory → ReadFile errors (EISDIR).
	if err := os.MkdirAll(learningPath("dirconfig"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := LoadLearningsForUI("dirconfig")
	if err == nil || err == ErrLearningsNotFound {
		t.Fatalf("expected a read error (not NotFound), got %v", err)
	}
}

// TestWriteLearningData_MkdirFails makes the learnings parent a file so
// MkdirAll fails.
func TestWriteLearningData_MkdirFails(t *testing.T) {
	makeLearningsDirAFile(t)
	err := writeLearningData(defaultLearningData("x"))
	if err == nil {
		t.Fatal("expected MkdirAll error")
	}
}

// TestWriteLearningData_WriteFails makes the tmp path a directory.
func TestWriteLearningData_WriteFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".jarvis", "learnings")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(learningPath("wf")+".tmp", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeLearningData(defaultLearningData("wf")); err == nil {
		t.Fatal("expected WriteFile error when tmp is a directory")
	}
}

// TestWriteLearningData_RenameFails makes the target path a non-empty dir.
func TestWriteLearningData_RenameFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".jarvis", "learnings")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(learningPath("rf"), "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeLearningData(defaultLearningData("rf")); err == nil {
		t.Fatal("expected Rename error when target is a non-empty directory")
	}
	if _, err := os.Stat(learningPath("rf") + ".tmp"); !os.IsNotExist(err) {
		t.Error("expected orphan tmp removed after rename failure")
	}
}

// TestLoadLearningDataUnsafe_QuarantineRenameFails forces the
// quarantine-rename error branch: the learning file is corrupt AND its
// path is a non-empty directory so os.Rename of it fails. Because a
// non-empty directory can't be JSON-parsed, we instead make the corrupt
// TARGET (path.corrupt-*) impossible to create by making the rename
// source unrenamable — simplest reliable way: corrupt file in a dir we
// then make read-only so the rename within it fails.
func TestLoadLearningDataUnsafe_QuarantineRenameFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: read-only dir rename would still succeed")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".jarvis", "learnings")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Write a corrupt learning file.
	path := learningPath("badq")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Make the parent dir read-only so the in-dir rename fails (EACCES).
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	ld := loadLearningDataUnsafe("badq")
	// Even when quarantine fails, a fresh default is returned.
	if ld == nil || ld.ConfigID != "badq" || len(ld.Learnings) != 0 {
		t.Errorf("expected fresh default learning data, got %+v", ld)
	}
}

// TestDistillLearningsInPlace_BelowBatch covers the early return when
// there are fewer than distillBatchSize learnings.
func TestDistillLearningsInPlace_BelowBatch(t *testing.T) {
	ld := &AgentLearningData{Learnings: []AgentLearning{{Content: "one"}}}
	before := len(ld.DistilledInsights)
	distillLearningsInPlace(ld)
	if len(ld.DistilledInsights) != before {
		t.Error("expected no distillation below batch size")
	}
}

// TestDistillLearningsInPlace_EmptyCategoryAndTruncation covers the
// cat=="" → "general" default and the >80-char truncation inside the
// insight builder.
func TestDistillLearningsInPlace_EmptyCategoryAndTruncation(t *testing.T) {
	longContent := strings.Repeat("z", 120)
	var learnings []AgentLearning
	for i := 0; i < distillBatchSize; i++ {
		learnings = append(learnings, AgentLearning{
			Category: "", // forces the "general" default
			Content:  longContent,
		})
	}
	ld := &AgentLearningData{Learnings: learnings}
	distillLearningsInPlace(ld)
	if len(ld.DistilledInsights) == 0 {
		t.Fatal("expected a distilled insight")
	}
	insight := ld.DistilledInsights[0]
	if !strings.Contains(insight, "general") {
		t.Errorf("expected 'general' category in insight, got %q", insight)
	}
	if !strings.Contains(insight, "...") {
		t.Errorf("expected truncated content marker in insight, got %q", insight)
	}
}

// TestTopSkills_FewerThanN covers the n>len(sorted) clamp.
func TestTopSkills_FewerThanN(t *testing.T) {
	skills := []AgentSkillEntry{
		{Name: "a", UsageCount: 1},
		{Name: "b", UsageCount: 5},
	}
	got := topSkills(skills, 10) // n > len
	if len(got) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(got))
	}
	if got[0].Name != "b" {
		t.Errorf("expected highest-usage first, got %q", got[0].Name)
	}
}

// TestBuildCrossAgentContext_ReadDirError covers the ReadDir error branch.
func TestBuildCrossAgentContext_ReadDirError(t *testing.T) {
	makeLearningsDirAFile(t)
	if got := BuildCrossAgentContext("anyone"); got != "" {
		t.Errorf("expected empty cross-agent context on ReadDir error, got %q", got)
	}
	if got := BuildCrossAgentContextCompact("anyone"); got != "" {
		t.Errorf("expected empty compact context on ReadDir error, got %q", got)
	}
}

// TestBuildCrossAgentContext_EmptyCategoryDefault covers the cat==""
// default inside the recent-learnings loop.
func TestBuildCrossAgentContext_EmptyCategoryDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".jarvis", "learnings")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ld := AgentLearningData{
		ConfigID:  "peer",
		Name:      "Peer",
		Learnings: []AgentLearning{{Category: "", Content: "uncategorized insight"}},
	}
	data, _ := json.Marshal(ld)
	if err := os.WriteFile(filepath.Join(dir, "peer.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	out := BuildCrossAgentContext("self")
	if !strings.Contains(out, "[general]") {
		t.Errorf("expected 'general' default category, got %q", out)
	}
	if !strings.Contains(out, "uncategorized insight") {
		t.Errorf("expected the learning content, got %q", out)
	}
}

// TestBuildLearningsPromptSection_SkillsAndInsights exercises the skills
// + distilled-insights branches and the cat=="" default in the recent
// learnings loop.
func TestBuildLearningsPromptSection_SkillsAndInsights(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".jarvis", "learnings")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ld := AgentLearningData{
		ConfigID:          "promo",
		Learnings:         []AgentLearning{{Category: "", Content: "do the thing"}},
		Skills:            []AgentSkillEntry{{Name: "refactor", UsageCount: 3}},
		DistilledInsights: []string{"distilled wisdom"},
	}
	data, _ := json.Marshal(ld)
	if err := os.WriteFile(filepath.Join(dir, "promo.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	out := BuildLearningsPromptSection("promo")
	for _, want := range []string{"Recent learnings:", "[general]", "Top skills:", "refactor", "Distilled insights:", "distilled wisdom"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected section to contain %q, got:\n%s", want, out)
		}
	}
}
