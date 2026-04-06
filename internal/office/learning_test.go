package office

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeLearningFile writes ld as JSON to the temp learnings directory.
func writeLearningFile(t *testing.T, dir string, ld *AgentLearningData) {
	t.Helper()
	data, err := json.MarshalIndent(ld, "", "  ")
	if err != nil {
		t.Fatalf("marshal learning data: %v", err)
	}
	path := filepath.Join(dir, ld.ConfigID+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write learning file: %v", err)
	}
}

// withTempLearningsDir overrides HOME so that learningsDir() and learningPath()
// resolve into a test-controlled temp directory. Returns the learnings sub-path
// and a cleanup function that restores the original HOME.
func withTempLearningsDir(t *testing.T) (string, func()) {
	t.Helper()
	tmp := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmp)
	learningsPath := filepath.Join(tmp, ".jarvis", "learnings")
	if err := os.MkdirAll(learningsPath, 0o755); err != nil {
		t.Fatalf("create learnings dir: %v", err)
	}
	cleanup := func() {
		os.Setenv("HOME", origHome)
	}
	return learningsPath, cleanup
}

// TestLoadLearningData_Empty verifies that a missing file returns a zero-value
// AgentLearningData with initialised (non-nil) slices.
func TestLoadLearningData_Empty(t *testing.T) {
	_, cleanup := withTempLearningsDir(t)
	defer cleanup()

	ld := LoadLearningData("nonexistent-agent")

	if ld == nil {
		t.Fatal("expected non-nil AgentLearningData for missing file")
	}
	if ld.ConfigID != "nonexistent-agent" {
		t.Errorf("expected configID 'nonexistent-agent', got %q", ld.ConfigID)
	}
	if ld.Learnings == nil {
		t.Error("expected non-nil Learnings slice")
	}
	if ld.Skills == nil {
		t.Error("expected non-nil Skills slice")
	}
	if ld.DistilledInsights == nil {
		t.Error("expected non-nil DistilledInsights slice")
	}
	if len(ld.Learnings) != 0 {
		t.Errorf("expected 0 learnings, got %d", len(ld.Learnings))
	}
}

// TestLoadLearningData_Existing writes a file to disk and verifies that
// LoadLearningData reads it back correctly.
func TestLoadLearningData_Existing(t *testing.T) {
	learningsPath, cleanup := withTempLearningsDir(t)
	defer cleanup()

	expected := &AgentLearningData{
		ConfigID: "agent-x",
		Name:     "Agent X",
		Learnings: []AgentLearning{
			{Category: "technique", Content: "Use Go channels for concurrency", CreatedAt: time.Now()},
		},
		Skills: []AgentSkillEntry{
			{Name: "test-skill", Description: "A test skill", UsageCount: 2, DiscoveredAt: time.Now(), LastUsedAt: time.Now()},
		},
		DistilledInsights: []string{"summary one"},
	}

	writeLearningFile(t, learningsPath, expected)

	got := LoadLearningData("agent-x")

	if got.ConfigID != "agent-x" {
		t.Errorf("expected configID 'agent-x', got %q", got.ConfigID)
	}
	if len(got.Learnings) != 1 {
		t.Fatalf("expected 1 learning, got %d", len(got.Learnings))
	}
	if got.Learnings[0].Content != "Use Go channels for concurrency" {
		t.Errorf("unexpected learning content: %q", got.Learnings[0].Content)
	}
	if len(got.Skills) != 1 || got.Skills[0].Name != "test-skill" {
		t.Errorf("unexpected skills: %v", got.Skills)
	}
	if len(got.DistilledInsights) != 1 || got.DistilledInsights[0] != "summary one" {
		t.Errorf("unexpected distilled insights: %v", got.DistilledInsights)
	}
}

// TestAddLearningEntry verifies that a learning entry is appended and persisted.
func TestAddLearningEntry(t *testing.T) {
	_, cleanup := withTempLearningsDir(t)
	defer cleanup()

	const configID = "add-entry-agent"

	if err := AddLearningEntry(configID, "technique", "always test your code", "task-1", "Test Coverage"); err != nil {
		t.Fatalf("AddLearningEntry failed: %v", err)
	}

	ld := LoadLearningData(configID)
	if len(ld.Learnings) != 1 {
		t.Fatalf("expected 1 learning, got %d", len(ld.Learnings))
	}
	entry := ld.Learnings[0]
	if entry.Category != "technique" {
		t.Errorf("expected category 'technique', got %q", entry.Category)
	}
	if entry.Content != "always test your code" {
		t.Errorf("unexpected content: %q", entry.Content)
	}
	if entry.TaskID != "task-1" {
		t.Errorf("expected taskID 'task-1', got %q", entry.TaskID)
	}
	if entry.TaskTitle != "Test Coverage" {
		t.Errorf("expected taskTitle 'Test Coverage', got %q", entry.TaskTitle)
	}
	if ld.Metrics.TotalLearnings != 1 {
		t.Errorf("expected TotalLearnings=1, got %d", ld.Metrics.TotalLearnings)
	}
}

// TestAddLearningEntry_MaxCap verifies that the learning list is capped at
// maxLearnings (50) and that the oldest entries are discarded.
func TestAddLearningEntry_MaxCap(t *testing.T) {
	_, cleanup := withTempLearningsDir(t)
	defer cleanup()

	const configID = "cap-agent"

	// Add maxLearnings + 5 entries.
	for i := 0; i < maxLearnings+5; i++ {
		content := fmt.Sprintf("learning number %d", i)
		if err := AddLearningEntry(configID, "general", content, "", ""); err != nil {
			t.Fatalf("AddLearningEntry %d failed: %v", i, err)
		}
	}

	ld := LoadLearningData(configID)
	if len(ld.Learnings) > maxLearnings {
		t.Errorf("expected at most %d learnings, got %d", maxLearnings, len(ld.Learnings))
	}

	// The last entry should be the most recently added one.
	last := ld.Learnings[len(ld.Learnings)-1]
	if !strings.Contains(last.Content, fmt.Sprintf("%d", maxLearnings+4)) {
		t.Errorf("expected last entry to be learning %d, got %q", maxLearnings+4, last.Content)
	}
}

// TestAddLearningEntry_AutoDistill verifies that distillation is triggered
// automatically when the learning count reaches distillTriggerCount.
func TestAddLearningEntry_AutoDistill(t *testing.T) {
	_, cleanup := withTempLearningsDir(t)
	defer cleanup()

	const configID = "distill-trigger-agent"

	// Add exactly distillTriggerCount entries.
	for i := 0; i < distillTriggerCount; i++ {
		if err := AddLearningEntry(configID, "pitfall", fmt.Sprintf("pitfall %d", i), "", ""); err != nil {
			t.Fatalf("AddLearningEntry %d failed: %v", i, err)
		}
	}

	ld := LoadLearningData(configID)

	// After auto-distill, the oldest distillBatchSize entries are consumed and
	// at least one distilled insight is created.
	if len(ld.DistilledInsights) == 0 {
		t.Error("expected at least one distilled insight after auto-distill trigger")
	}
	// The Learnings slice should have been reduced by distillBatchSize.
	expectedRemaining := distillTriggerCount - distillBatchSize
	if len(ld.Learnings) != expectedRemaining {
		t.Errorf("expected %d learnings after distillation, got %d", expectedRemaining, len(ld.Learnings))
	}
}

// TestDistillLearnings verifies that DistillLearnings groups entries by
// category and produces correctly formatted insight strings.
func TestDistillLearnings(t *testing.T) {
	_, cleanup := withTempLearningsDir(t)
	defer cleanup()

	ld := &AgentLearningData{
		ConfigID:          "distill-agent",
		Learnings:         make([]AgentLearning, 0),
		Skills:            make([]AgentSkillEntry, 0),
		DistilledInsights: make([]string, 0),
	}

	// Add a full batch of learnings across two categories.
	for i := 0; i < distillBatchSize; i++ {
		cat := "technique"
		if i%2 == 0 {
			cat = "pitfall"
		}
		ld.Learnings = append(ld.Learnings, AgentLearning{
			Category:  cat,
			Content:   fmt.Sprintf("entry %d", i),
			CreatedAt: time.Now(),
		})
	}

	DistillLearnings(ld)

	if len(ld.DistilledInsights) == 0 {
		t.Fatal("expected at least one distilled insight")
	}
	// Each insight should start with "Key <category> learnings:"
	for _, insight := range ld.DistilledInsights {
		if !strings.HasPrefix(insight, "Key ") {
			t.Errorf("unexpected insight format: %q", insight)
		}
	}
	// The batch should have been consumed from Learnings.
	if len(ld.Learnings) != 0 {
		t.Errorf("expected 0 learnings remaining after full distillation, got %d", len(ld.Learnings))
	}
}

// TestAddSkillEntry verifies that a new skill is appended.
func TestAddSkillEntry(t *testing.T) {
	_, cleanup := withTempLearningsDir(t)
	defer cleanup()

	const configID = "skill-agent"

	if err := AddSkillEntry(configID, "GoRoutinePattern", "Use goroutines for concurrent tasks"); err != nil {
		t.Fatalf("AddSkillEntry failed: %v", err)
	}

	ld := LoadLearningData(configID)
	if len(ld.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(ld.Skills))
	}
	skill := ld.Skills[0]
	if skill.Name != "GoRoutinePattern" {
		t.Errorf("expected skill name 'GoRoutinePattern', got %q", skill.Name)
	}
	if skill.Description != "Use goroutines for concurrent tasks" {
		t.Errorf("unexpected description: %q", skill.Description)
	}
	if skill.UsageCount != 1 {
		t.Errorf("expected UsageCount=1, got %d", skill.UsageCount)
	}
}

// TestAddSkillEntry_Dedup verifies that adding a skill with the same name
// (case-insensitive) increments its UsageCount instead of creating a duplicate.
func TestAddSkillEntry_Dedup(t *testing.T) {
	_, cleanup := withTempLearningsDir(t)
	defer cleanup()

	const configID = "dedup-skill-agent"

	if err := AddSkillEntry(configID, "MySkill", "first description"); err != nil {
		t.Fatalf("first AddSkillEntry failed: %v", err)
	}
	if err := AddSkillEntry(configID, "myskill", "updated description"); err != nil {
		t.Fatalf("second AddSkillEntry failed: %v", err)
	}

	ld := LoadLearningData(configID)
	if len(ld.Skills) != 1 {
		t.Fatalf("expected 1 skill after dedup, got %d", len(ld.Skills))
	}
	skill := ld.Skills[0]
	if skill.UsageCount != 2 {
		t.Errorf("expected UsageCount=2 after dedup, got %d", skill.UsageCount)
	}
	if skill.Description != "updated description" {
		t.Errorf("expected updated description, got %q", skill.Description)
	}
}

// TestBuildLearningsPromptSection verifies that the prompt section includes
// recent learnings, top skills, and distilled insights.
func TestBuildLearningsPromptSection(t *testing.T) {
	_, cleanup := withTempLearningsDir(t)
	defer cleanup()

	const configID = "prompt-section-agent"

	// Add a learning.
	if err := AddLearningEntry(configID, "technique", "use table-driven tests", "t1", "Testing"); err != nil {
		t.Fatalf("AddLearningEntry failed: %v", err)
	}

	// Add a skill.
	if err := AddSkillEntry(configID, "TableTest", "write table-driven tests"); err != nil {
		t.Fatalf("AddSkillEntry failed: %v", err)
	}

	// Add a distilled insight manually by loading and saving.
	ld := LoadLearningData(configID)
	ld.DistilledInsights = append(ld.DistilledInsights, "Key technique learnings: use mocks")
	if err := SaveLearningData(configID, ld); err != nil {
		t.Fatalf("SaveLearningData failed: %v", err)
	}

	section := BuildLearningsPromptSection(configID)

	if section == "" {
		t.Fatal("expected non-empty prompt section")
	}
	if !strings.Contains(section, "Recent learnings:") {
		t.Error("expected 'Recent learnings:' in section")
	}
	if !strings.Contains(section, "use table-driven tests") {
		t.Error("expected learning content in section")
	}
	if !strings.Contains(section, "Top skills:") {
		t.Error("expected 'Top skills:' in section")
	}
	if !strings.Contains(section, "TableTest") {
		t.Error("expected skill name 'TableTest' in section")
	}
	if !strings.Contains(section, "Distilled insights:") {
		t.Error("expected 'Distilled insights:' in section")
	}
	if !strings.Contains(section, "Key technique learnings") {
		t.Error("expected distilled insight content in section")
	}
}

// TestBuildCrossAgentContext verifies that the cross-agent context excludes
// the caller but includes learnings from other agents.
func TestBuildCrossAgentContext(t *testing.T) {
	learningsPath, cleanup := withTempLearningsDir(t)
	defer cleanup()

	// Write learning data for two agents.
	selfAgent := &AgentLearningData{
		ConfigID: "self-agent",
		Name:     "Self Agent",
		Learnings: []AgentLearning{
			{Category: "general", Content: "self learning", CreatedAt: time.Now()},
		},
		Skills:            make([]AgentSkillEntry, 0),
		DistilledInsights: make([]string, 0),
	}
	otherAgent := &AgentLearningData{
		ConfigID: "other-agent",
		Name:     "Other Agent",
		Learnings: []AgentLearning{
			{Category: "technique", Content: "shared learning from other", CreatedAt: time.Now()},
		},
		Skills:            make([]AgentSkillEntry, 0),
		DistilledInsights: make([]string, 0),
	}

	writeLearningFile(t, learningsPath, selfAgent)
	writeLearningFile(t, learningsPath, otherAgent)

	context := BuildCrossAgentContext("self-agent")

	if context == "" {
		t.Fatal("expected non-empty cross-agent context")
	}
	if !strings.Contains(context, "Other Agent") {
		t.Error("expected 'Other Agent' in cross-agent context")
	}
	if !strings.Contains(context, "shared learning from other") {
		t.Error("expected other agent's learning content in cross-agent context")
	}
	if !strings.Contains(context, "What your teammates learned recently:") {
		t.Error("expected section header in cross-agent context")
	}
	// The self agent's own learning should not appear as another agent's entry.
	if strings.Contains(context, "self learning") {
		t.Error("cross-agent context should not include the excluded agent's learnings")
	}
}
