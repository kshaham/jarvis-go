package office

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestScanLearningHits_EmptyNameFallsBackToConfigID drives the
// agentName=="" fallback (knowledge.go:274-275): a learning file with no
// Name field surfaces hits attributed to the configID (filename stem).
// It also exercises the distilled-insight branch with a non-empty needle.
func TestScanLearningHits_EmptyNameFallsBackToConfigID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := learningsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Name intentionally empty so the configID fallback fires.
	ld := AgentLearningData{
		ConfigID: "vexconfig",
		Name:     "",
		Learnings: []AgentLearning{
			{Category: "tech", Content: "use widget batching for performance", CreatedAt: time.Now()},
		},
		DistilledInsights: []string{"key insight: batch widget updates"},
	}
	data, _ := json.Marshal(ld)
	if err := os.WriteFile(filepath.Join(dir, "vexconfig.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	hits := scanLearningHits("widget", KnowledgeSearchOptions{})
	if len(hits) == 0 {
		t.Fatal("expected hits for the 'widget' needle")
	}
	for _, h := range hits {
		if h.AgentName != "vexconfig" {
			t.Errorf("expected agentName fallback to configID 'vexconfig', got %q", h.AgentName)
		}
	}
	// Both a learning hit and a distilled-insight hit should be present.
	var sawDistilled, sawLearning bool
	for _, h := range hits {
		switch h.Section {
		case "distilled":
			sawDistilled = true
		case "tech":
			sawLearning = true
		}
	}
	if !sawLearning {
		t.Error("expected a learning-category hit")
	}
	if !sawDistilled {
		t.Error("expected a distilled-insight hit")
	}
}

// TestSearchKnowledge_TruncatesToLimit drives the len(hits)>limit
// truncation (knowledge.go:114-116): more matching hits than the
// requested limit are trimmed to exactly the limit.
func TestSearchKnowledge_TruncatesToLimit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := learningsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// One agent with several matching learnings — more than our limit.
	learnings := make([]AgentLearning, 0, 6)
	for i := 0; i < 6; i++ {
		learnings = append(learnings, AgentLearning{
			Category:  "tech",
			Content:   "matching keyword finding number " + string(rune('a'+i)),
			CreatedAt: time.Now().Add(time.Duration(i) * time.Minute),
		})
	}
	ld := AgentLearningData{ConfigID: "agent", Name: "Agent", Learnings: learnings}
	data, _ := json.Marshal(ld)
	if err := os.WriteFile(filepath.Join(dir, "agent.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	hits, err := SearchKnowledge(KnowledgeSearchOptions{Query: "keyword", Limit: 3})
	if err != nil {
		t.Fatalf("SearchKnowledge: %v", err)
	}
	if len(hits) != 3 {
		t.Errorf("expected exactly 3 hits after truncation to limit, got %d", len(hits))
	}
}
