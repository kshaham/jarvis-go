package office

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDistillLearningsInPlace_CapsDistilledInsights drives the
// maxDistilledInsights cap (learning.go:367-369): a batch that produces
// several category insights on top of an already-full DistilledInsights
// slice must be trimmed to the most-recent maxDistilledInsights.
func TestDistillLearningsInPlace_CapsDistilledInsights(t *testing.T) {
	ld := &AgentLearningData{ConfigID: "cap", Name: "Cap"}
	// Pre-seed exactly maxDistilledInsights existing insights.
	for i := 0; i < maxDistilledInsights; i++ {
		ld.DistilledInsights = append(ld.DistilledInsights, "old insight")
	}
	// A full batch spanning several categories so distillation appends
	// MORE insights, pushing the total over the cap.
	cats := []string{"a", "b", "c", "d", "e"}
	for i := 0; i < distillBatchSize; i++ {
		ld.Learnings = append(ld.Learnings, AgentLearning{
			Category: cats[i%len(cats)],
			Content:  "learning content",
		})
	}

	distillLearningsInPlace(ld)

	if len(ld.DistilledInsights) != maxDistilledInsights {
		t.Errorf("expected DistilledInsights capped to %d, got %d",
			maxDistilledInsights, len(ld.DistilledInsights))
	}
	// The newest insights (the category summaries) must be retained — i.e.
	// at least one "Key <cat> learnings:" line survives at the tail.
	tail := strings.Join(ld.DistilledInsights, "\n")
	if !strings.Contains(tail, "Key ") {
		t.Errorf("expected newest category insights retained after cap, got %q", tail)
	}
}

// TestBuildCrossAgentContextCompact_SkipsNonJsonAndEmptyPeer drives two
// skip branches: the non-.json directory-entry skip (learning.go:522-523)
// and the empty-learnings peer skip (learning.go:530-531).
func TestBuildCrossAgentContextCompact_SkipsNonJsonAndEmptyPeer(t *testing.T) {
	learnDir, cleanup := withTempLearningsDir(t)
	defer cleanup()

	// A peer with a real learning (must appear).
	if err := AddLearningEntry("vex", "technique", "vex insight", "t1", "T1"); err != nil {
		t.Fatalf("seed vex: %v", err)
	}
	// A peer json file with ZERO learnings (must be skipped at line 530).
	emptyPeer := `{"configId":"empty","name":"Empty","learnings":[],"skills":[],"distilledInsights":[]}`
	if err := os.WriteFile(filepath.Join(learnDir, "empty.json"), []byte(emptyPeer), 0o644); err != nil {
		t.Fatal(err)
	}
	// A non-.json file (must be skipped at line 522).
	if err := os.WriteFile(filepath.Join(learnDir, "notes.txt"), []byte("ignore me"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A subdirectory ending in .json (IsDir skip at line 522).
	if err := os.MkdirAll(filepath.Join(learnDir, "weird.json"), 0o755); err != nil {
		t.Fatal(err)
	}

	out := BuildCrossAgentContextCompact("self")
	if !strings.Contains(out, "vex insight") {
		t.Errorf("expected vex's learning in output, got %q", out)
	}
	if strings.Contains(out, "Empty") {
		t.Errorf("empty-learnings peer should be skipped, got %q", out)
	}
	if strings.Contains(out, "ignore me") {
		t.Errorf("non-json file should be skipped, got %q", out)
	}
}

// TestBuildCrossAgentContext_SkipsNonJsonAndEmptyPeer drives the same two
// skip branches in the verbose BuildCrossAgentContext (learning.go:462-463
// non-json skip; the empty-peer body lower in the same loop).
func TestBuildCrossAgentContext_SkipsNonJsonAndEmptyPeer(t *testing.T) {
	learnDir, cleanup := withTempLearningsDir(t)
	defer cleanup()

	if err := AddLearningEntry("vex", "technique", "vex detailed insight", "t1", "T1"); err != nil {
		t.Fatalf("seed vex: %v", err)
	}
	emptyPeer := `{"configId":"empty","name":"Empty","learnings":[],"skills":[],"distilledInsights":[]}`
	if err := os.WriteFile(filepath.Join(learnDir, "empty.json"), []byte(emptyPeer), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(learnDir, "readme.md"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := BuildCrossAgentContext("self")
	if !strings.Contains(out, "vex detailed insight") {
		t.Errorf("expected vex's learning in verbose output, got %q", out)
	}
	if strings.Contains(out, "not json") {
		t.Errorf("non-json file should be skipped, got %q", out)
	}
}
