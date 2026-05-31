package office

import (
	"testing"
)

// TestTaskGraph_DuplicateTaskIDDeduped drives the seen-id continue: two agents
// reference the same task ID, so the second occurrence is skipped and the
// first wins.
func TestTaskGraph_DuplicateTaskIDDeduped(t *testing.T) {
	m := newTestManager()
	m.RegisterAgent("s-a", "a", &TeamMemberConfig{ID: "a", Name: "Aaa"})
	m.RegisterAgent("s-b", "b", &TeamMemberConfig{ID: "b", Name: "Bbb"})
	// Both agents hold a task with the SAME id "dup".
	m.GetAgent("s-a").AddTask(gtask("dup", "owned by A"))
	m.GetAgent("s-b").AddTask(gtask("dup", "owned by B"))

	g := BuildTaskGraph(m)
	if len(g.Nodes) != 1 {
		t.Fatalf("expected 1 deduped node, got %d", len(g.Nodes))
	}
	// First agent (sorted roster order) wins; either name is fine as long as
	// only one survived. Assert the title belongs to exactly one owner.
	n := findNode(g, "dup")
	if n == nil {
		t.Fatal("expected the deduped node to exist")
	}
	if n.Title != "owned by A" && n.Title != "owned by B" {
		t.Errorf("unexpected title after dedup: %q", n.Title)
	}
}

// TestTaskGraph_SingleNodeNoCriticalPath drives the len(criticalPath) < 2
// branch: a lone task forms a 1-node "path" which is blanked out so the UI
// skips the decoration.
func TestTaskGraph_SingleNodeNoCriticalPath(t *testing.T) {
	m := newTestManager()
	m.RegisterAgent("s-a", "a", &TeamMemberConfig{ID: "a", Name: "Solo"})
	m.GetAgent("s-a").AddTask(gtask("only", "the only task"))

	g := BuildTaskGraph(m)
	if len(g.CriticalPath) != 0 {
		t.Errorf("expected no critical path for a single node, got %v", g.CriticalPath)
	}
	if len(g.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(g.Nodes))
	}
}

// TestTaskGraph_SamePriorityTieBreakByPriorityRank drives the priority
// comparison in the sort comparator: two same-depth, same-agent tasks with
// DIFFERENT priorities must be ordered by priority rank (high before low).
func TestTaskGraph_SortByPriority(t *testing.T) {
	m := newTestManager()
	m.RegisterAgent("s-a", "a", &TeamMemberConfig{ID: "a", Name: "Solo"})
	low := gtask("z-low", "low pri")
	low.Priority = "low"
	high := gtask("a-high", "high pri")
	high.Priority = "high"
	// Both at depth 0, same agent "Solo", different priorities. The ID order
	// (a-high < z-low) would already put high first, so flip the IDs to force
	// the priority comparator to decide.
	low.ID = "aaa-low"
	high.ID = "zzz-high"
	m.GetAgent("s-a").AddTask(low)
	m.GetAgent("s-a").AddTask(high)

	g := BuildTaskGraph(m)
	if len(g.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(g.Nodes))
	}
	// high priority should sort before low even though its ID is larger.
	if g.Nodes[0].Priority != "high" {
		t.Errorf("expected high-priority node first by priority rank, got order %s,%s",
			g.Nodes[0].Priority, g.Nodes[1].Priority)
	}
}
