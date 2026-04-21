package office

import (
	"reflect"
	"testing"
)

// helper: make a Task with just ID / Title / BlockedBy set.
func gtask(id, title string, blockedBy ...string) *Task {
	return &Task{
		ID:        id,
		Title:     title,
		Status:    TaskInProgress,
		Priority:  "medium",
		BlockedBy: blockedBy,
	}
}

func findNode(resp *TaskGraphResponse, id string) *TaskGraphNode {
	for i := range resp.Nodes {
		if resp.Nodes[i].ID == id {
			return &resp.Nodes[i]
		}
	}
	return nil
}

func TestTaskGraph_Empty(t *testing.T) {
	m := newTestManager()
	g := BuildTaskGraph(m)
	if len(g.Nodes) != 0 {
		t.Errorf("empty manager should have 0 nodes, got %d", len(g.Nodes))
	}
	if len(g.CriticalPath) != 0 {
		t.Errorf("empty manager should have no critical path, got %v", g.CriticalPath)
	}
	if g.CycleDetected {
		t.Error("empty manager should not report a cycle")
	}
}

func TestTaskGraph_LinearChain(t *testing.T) {
	m := newTestManager()
	vex := &TeamMemberConfig{ID: "vex", Name: "Vex"}
	m.RegisterAgent("s-vex", "vex", vex)
	agent := m.GetAgent("s-vex")
	agent.AddTask(gtask("t-a", "build auth"))
	agent.AddTask(gtask("t-b", "write tests", "t-a"))
	agent.AddTask(gtask("t-c", "ship it", "t-b"))

	g := BuildTaskGraph(m)
	if len(g.Nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(g.Nodes))
	}
	if d := findNode(g, "t-a").Depth; d != 0 {
		t.Errorf("t-a depth: want 0, got %d", d)
	}
	if d := findNode(g, "t-b").Depth; d != 1 {
		t.Errorf("t-b depth: want 1, got %d", d)
	}
	if d := findNode(g, "t-c").Depth; d != 2 {
		t.Errorf("t-c depth: want 2, got %d", d)
	}
	want := []string{"t-a", "t-b", "t-c"}
	if !reflect.DeepEqual(g.CriticalPath, want) {
		t.Errorf("critical path: want %v, got %v", want, g.CriticalPath)
	}
	if g.CycleDetected {
		t.Error("linear chain should not report a cycle")
	}
	for _, id := range want {
		if !findNode(g, id).OnCriticalPath {
			t.Errorf("%s should be on critical path", id)
		}
	}
}

func TestTaskGraph_Diamond(t *testing.T) {
	m := newTestManager()
	vex := &TeamMemberConfig{ID: "vex", Name: "Vex"}
	m.RegisterAgent("s-vex", "vex", vex)
	agent := m.GetAgent("s-vex")
	// A → B, A → C, B+C → D
	agent.AddTask(gtask("t-a", "root"))
	agent.AddTask(gtask("t-b", "b", "t-a"))
	agent.AddTask(gtask("t-c", "c", "t-a"))
	agent.AddTask(gtask("t-d", "d", "t-b", "t-c"))

	g := BuildTaskGraph(m)
	if d := findNode(g, "t-d").Depth; d != 2 {
		t.Errorf("t-d depth: want 2, got %d", d)
	}
	// Both A→B→D and A→C→D have length 3. Deterministic tiebreak
	// picks the path whose end-node has the lexicographically earlier
	// preceding chain. Either is correct; we just assert the length
	// and that both endpoints sit on the path.
	if len(g.CriticalPath) != 3 {
		t.Errorf("diamond critical path: want length 3, got %d (%v)", len(g.CriticalPath), g.CriticalPath)
	}
	if g.CriticalPath[0] != "t-a" || g.CriticalPath[2] != "t-d" {
		t.Errorf("diamond critical path should start t-a and end t-d, got %v", g.CriticalPath)
	}
}

func TestTaskGraph_DisconnectedComponents(t *testing.T) {
	m := newTestManager()
	vex := &TeamMemberConfig{ID: "vex", Name: "Vex"}
	m.RegisterAgent("s-vex", "vex", vex)
	agent := m.GetAgent("s-vex")
	// Component 1: A → B
	agent.AddTask(gtask("t-a", "one"))
	agent.AddTask(gtask("t-b", "two", "t-a"))
	// Component 2: X → Y → Z
	agent.AddTask(gtask("t-x", "x"))
	agent.AddTask(gtask("t-y", "y", "t-x"))
	agent.AddTask(gtask("t-z", "z", "t-y"))

	g := BuildTaskGraph(m)
	if len(g.Nodes) != 5 {
		t.Fatalf("expected 5 nodes, got %d", len(g.Nodes))
	}
	// Critical path sits in the longer component (X→Y→Z).
	if len(g.CriticalPath) != 3 {
		t.Errorf("critical path: want length 3, got %d (%v)", len(g.CriticalPath), g.CriticalPath)
	}
	if g.CriticalPath[0] != "t-x" || g.CriticalPath[2] != "t-z" {
		t.Errorf("critical path should be t-x → t-y → t-z, got %v", g.CriticalPath)
	}
}

func TestTaskGraph_MultipleAgents(t *testing.T) {
	m := newTestManager()
	m.RegisterAgent("s-vex", "vex", &TeamMemberConfig{ID: "vex", Name: "Vex"})
	m.RegisterAgent("s-bolt", "bolt", &TeamMemberConfig{ID: "bolt", Name: "Bolt"})
	m.GetAgent("s-vex").AddTask(gtask("t-front", "frontend"))
	m.GetAgent("s-bolt").AddTask(gtask("t-api", "API"))
	m.GetAgent("s-bolt").AddTask(gtask("t-qa", "QA", "t-front", "t-api"))

	g := BuildTaskGraph(m)
	if len(g.Nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(g.Nodes))
	}
	front := findNode(g, "t-front")
	api := findNode(g, "t-api")
	qa := findNode(g, "t-qa")
	if front.AgentName != "Vex" {
		t.Errorf("t-front agent: want Vex, got %s", front.AgentName)
	}
	if api.AgentName != "Bolt" {
		t.Errorf("t-api agent: want Bolt, got %s", api.AgentName)
	}
	if qa.AgentName != "Bolt" {
		t.Errorf("t-qa agent: want Bolt, got %s", qa.AgentName)
	}
	if qa.Depth != 1 {
		t.Errorf("t-qa depth: want 1, got %d", qa.Depth)
	}
}

func TestTaskGraph_PhantomPrereq(t *testing.T) {
	m := newTestManager()
	m.RegisterAgent("s-vex", "vex", &TeamMemberConfig{ID: "vex", Name: "Vex"})
	// t-a references a prereq that doesn't exist.
	m.GetAgent("s-vex").AddTask(gtask("t-a", "needs missing", "t-missing"))

	g := BuildTaskGraph(m)
	if len(g.Nodes) != 2 {
		t.Fatalf("expected 2 nodes (real + phantom), got %d", len(g.Nodes))
	}
	phantom := findNode(g, "t-missing")
	if phantom == nil || !phantom.Unknown {
		t.Errorf("t-missing should be a phantom Unknown node, got %+v", phantom)
	}
	if phantom.Status != "unknown" {
		t.Errorf("phantom status should be 'unknown', got %s", phantom.Status)
	}
}

func TestTaskGraph_Cycle(t *testing.T) {
	m := newTestManager()
	m.RegisterAgent("s-vex", "vex", &TeamMemberConfig{ID: "vex", Name: "Vex"})
	agent := m.GetAgent("s-vex")
	// A ← B ← A (cycle)
	agent.AddTask(gtask("t-a", "a", "t-b"))
	agent.AddTask(gtask("t-b", "b", "t-a"))

	g := BuildTaskGraph(m)
	if !g.CycleDetected {
		t.Fatal("expected cycle detected")
	}
	if len(g.CycleNodeIDs) != 2 {
		t.Fatalf("expected 2 cycle nodes, got %d (%v)", len(g.CycleNodeIDs), g.CycleNodeIDs)
	}
}
