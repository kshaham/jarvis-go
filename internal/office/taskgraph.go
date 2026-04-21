package office

// R30 · Task dependency graph.
//
// BuildTaskGraph aggregates every task across every agent's queue,
// deduplicates by task ID, topologically sorts via Kahn's algorithm,
// computes the longest path through the DAG (the critical path), and
// detects cycles. The result feeds a single read-only endpoint that
// the Office UI renders as an SVG DAG.
//
// Edge direction is `prereq → dependent`. A task's BlockedBy slice
// names its prerequisites, so an edge exists from each prereq to the
// blocked task.
//
// Phantom prereqs: if a task's BlockedBy names an ID that doesn't
// exist in the current aggregate, it's rendered as an "unknown" node
// with its title set to "[unknown: <id>]" and status "unknown". This
// is better than silently dropping the edge — stale references often
// indicate tasks that were completed and swept, or bad client data.

import (
	"sort"
)

// TaskGraphNode is one node in the dependency DAG.
type TaskGraphNode struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Status        string   `json:"status"`
	Priority      string   `json:"priority"`
	SessionID     string   `json:"sessionId,omitempty"`
	AgentName     string   `json:"agentName,omitempty"`
	ConfigID      string   `json:"configId,omitempty"`
	BlockedBy     []string `json:"blockedBy,omitempty"`
	BlockerDesc   string   `json:"blockerDescription,omitempty"`
	Depth         int      `json:"depth"`
	OnCriticalPath bool    `json:"onCriticalPath"`
	// Unknown is true when the task was referenced via another task's
	// blockedBy but doesn't exist in the current roster — a phantom
	// prereq. The UI renders these as placeholder nodes.
	Unknown bool `json:"unknown,omitempty"`
}

// TaskGraphResponse is the full endpoint payload.
type TaskGraphResponse struct {
	Nodes         []TaskGraphNode `json:"nodes"`
	CriticalPath  []string        `json:"criticalPath"`
	CycleDetected bool            `json:"cycleDetected"`
	CycleNodeIDs  []string        `json:"cycleNodeIds,omitempty"`
}

// BuildTaskGraph returns the full dependency DAG for the current
// roster. Safe to call at any frequency — complexity is O(tasks +
// edges). Returns a pointer so the caller can pass the value
// unchanged into WriteJSON without copying.
func BuildTaskGraph(m *Manager) *TaskGraphResponse {
	// 1. Aggregate all tasks across agents. Dedup by ID — if two
	// agents somehow reference the same task ID (from a handoff mid-
	// transition, for example), the first wins.
	roster := m.GetRoster()
	nodes := make(map[string]*TaskGraphNode)
	for _, a := range roster {
		agentName := ""
		if a.Config != nil {
			agentName = a.Config.Name
		}
		for _, t := range a.GetTasks() {
			if _, seen := nodes[t.ID]; seen {
				continue
			}
			// Copy the BlockedBy slice so later mutation of the task
			// doesn't aliasthe graph.
			var blockedCopy []string
			if len(t.BlockedBy) > 0 {
				blockedCopy = make([]string, len(t.BlockedBy))
				copy(blockedCopy, t.BlockedBy)
			}
			nodes[t.ID] = &TaskGraphNode{
				ID:          t.ID,
				Title:       t.Title,
				Status:      string(t.Status),
				Priority:    t.Priority,
				SessionID:   a.SessionID,
				AgentName:   agentName,
				ConfigID:    a.ConfigID,
				BlockedBy:   blockedCopy,
				BlockerDesc: t.BlockerDescription,
			}
		}
	}

	// 2. Create phantom nodes for any blockedBy ID that isn't in the
	// aggregate. A missing prereq is a real signal (stale ref) — we
	// surface it rather than drop the edge.
	for _, n := range nodes {
		for _, b := range n.BlockedBy {
			if _, ok := nodes[b]; !ok {
				nodes[b] = &TaskGraphNode{
					ID:      b,
					Title:   "[unknown: " + b + "]",
					Status:  "unknown",
					Unknown: true,
				}
			}
		}
	}

	// 3. Build adjacency: forward edges prereq → dependent, plus
	// in-degree counts for Kahn's.
	forward := make(map[string][]string, len(nodes))
	indegree := make(map[string]int, len(nodes))
	for id := range nodes {
		indegree[id] = 0
	}
	for id, n := range nodes {
		for _, prereq := range n.BlockedBy {
			forward[prereq] = append(forward[prereq], id)
			indegree[id]++
		}
	}

	// 4. Topological sort. Seed the queue with nodes that have no
	// prereqs. Process in sorted ID order for determinism so tests
	// can assert exact outputs.
	var queue []string
	for id, d := range indegree {
		if d == 0 {
			queue = append(queue, id)
		}
	}
	sort.Strings(queue)

	topo := make([]string, 0, len(nodes))
	depth := make(map[string]int, len(nodes))
	for len(queue) > 0 {
		// Pop front.
		cur := queue[0]
		queue = queue[1:]
		topo = append(topo, cur)

		// Push successors whose in-degree drops to 0.
		succ := append([]string(nil), forward[cur]...)
		sort.Strings(succ)
		for _, s := range succ {
			// A successor's depth is max(deps' depths) + 1.
			if depth[s] < depth[cur]+1 {
				depth[s] = depth[cur] + 1
			}
			indegree[s]--
			if indegree[s] == 0 {
				queue = append(queue, s)
			}
		}
		// Keep queue sorted after each append so ties resolve by ID.
		sort.Strings(queue)
	}

	// 5. Cycle detection: any node with remaining in-degree > 0 is
	// part of (or downstream of) a cycle.
	cycleNodes := make([]string, 0)
	if len(topo) != len(nodes) {
		for id, d := range indegree {
			if d > 0 {
				cycleNodes = append(cycleNodes, id)
			}
		}
		sort.Strings(cycleNodes)
	}

	// 6. Critical path: longest path in DAG. Traverse topo order; for
	// each node, track the best predecessor. Skip if in a cycle (no
	// well-defined longest path).
	prev := make(map[string]string, len(nodes))
	longest := make(map[string]int, len(nodes))
	var bestEnd string
	for _, id := range topo {
		// longest[id] defaults to 0 for sources.
		for _, b := range nodes[id].BlockedBy {
			if longest[b]+1 > longest[id] {
				longest[id] = longest[b] + 1
				prev[id] = b
			}
		}
		if longest[id] > longest[bestEnd] || (longest[id] == longest[bestEnd] && id < bestEnd) {
			bestEnd = id
		}
	}
	var criticalPath []string
	if len(topo) > 0 && bestEnd != "" {
		// Walk backward from bestEnd.
		var rev []string
		for cur := bestEnd; cur != ""; cur = prev[cur] {
			rev = append(rev, cur)
			if p, ok := prev[cur]; !ok || p == "" {
				break
			}
		}
		// Reverse.
		criticalPath = make([]string, len(rev))
		for i, v := range rev {
			criticalPath[len(rev)-1-i] = v
		}
		// A critical path of length 1 (single node) isn't interesting
		// to highlight — blank it out so the UI can skip the decoration.
		if len(criticalPath) < 2 {
			criticalPath = nil
		}
	}

	// 7. Stamp depth + critical-path onto each node.
	onCrit := make(map[string]bool, len(criticalPath))
	for _, id := range criticalPath {
		onCrit[id] = true
	}
	for id, n := range nodes {
		n.Depth = depth[id]
		n.OnCriticalPath = onCrit[id]
	}

	// 8. Return nodes in a stable order (by depth, then agent name,
	// then priority, then ID). The frontend assumes nothing about
	// order, but stable output makes tests easy.
	out := make([]TaskGraphNode, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, *n)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Depth != out[j].Depth {
			return out[i].Depth < out[j].Depth
		}
		if out[i].AgentName != out[j].AgentName {
			return out[i].AgentName < out[j].AgentName
		}
		if out[i].Priority != out[j].Priority {
			return priorityRank(out[i].Priority) < priorityRank(out[j].Priority)
		}
		return out[i].ID < out[j].ID
	})

	return &TaskGraphResponse{
		Nodes:         out,
		CriticalPath:  criticalPath,
		CycleDetected: len(cycleNodes) > 0,
		CycleNodeIDs:  cycleNodes,
	}
}

// priorityRank returns a sort ordinal (lower = more important).
func priorityRank(p string) int {
	switch p {
	case "high":
		return 0
	case "medium":
		return 1
	case "low":
		return 2
	}
	return 3
}
