package office

// R21 · Tool-level loop advisory.
//
// Sister to loop_detector.go but operates on structured tool
// invocations (pty.EventToolInvocation) rather than raw text output.
// Same invariants — 5-slot ring, 3-match threshold, fires once per
// streak — but the signal is stronger because (toolName, resultHash)
// duplication is almost always a loop (agent re-running the same
// tool with the same args producing the same output).
//
// Orange "loop?" badge in the text-level advisory becomes red
// "⟲ <tool>" for tool-level: the higher-fidelity signal warrants a
// louder UX cue.
//
// Advisory-only like R20 — no auto-stop, no auto-nudge. Operator uses
// R18 kill/inject if the agent doesn't break out on its own.

import (
	"log/slog"
	"sync"
)

const (
	toolLoopRingSize       = 5
	toolLoopMatchThreshold = 3
)

// toolKey is the composite signal: tool name concatenated with the
// result hash. Same tool + different result = different key (agent
// is making progress). Same tool + same result = possible loop.
type toolKey string

func makeToolKey(name, resultHash string) toolKey {
	return toolKey(name + "|" + resultHash)
}

type toolLoopDetector struct {
	mu            sync.Mutex
	ring          [toolLoopRingSize]toolKey
	idx           int
	advisoryFired bool
	lastToolName  string
}

func newToolLoopDetector() *toolLoopDetector { return &toolLoopDetector{} }

// observe ingests a completed tool invocation. Returns (triggered,
// toolName) — triggered fires exactly once per loop streak, and the
// returned name is the tool being looped on.
func (d *toolLoopDetector) observe(name, resultHash string) (triggered bool, toolName string) {
	if name == "" || resultHash == "" {
		return false, ""
	}
	k := makeToolKey(name, resultHash)

	d.mu.Lock()
	defer d.mu.Unlock()

	d.ring[d.idx] = k
	d.idx = (d.idx + 1) % toolLoopRingSize

	matches := 0
	for _, v := range d.ring {
		if v == k {
			matches++
		}
	}

	if matches >= toolLoopMatchThreshold {
		if d.advisoryFired {
			return false, name
		}
		d.advisoryFired = true
		d.lastToolName = name
		return true, name
	}
	if d.advisoryFired && matches == 1 {
		d.advisoryFired = false
	}
	return false, ""
}

// reset clears ring + fired flag. Called at turn boundaries.
func (d *toolLoopDetector) reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i := range d.ring {
		d.ring[i] = ""
	}
	d.idx = 0
	d.advisoryFired = false
	d.lastToolName = ""
}

// isFired exposes fired state for payload snapshotting.
func (d *toolLoopDetector) isFired() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.advisoryFired
}

// loopingTool returns the currently-looping tool name (if any). Used
// by BuildMemberPayload to show `⟲ Bash` instead of a generic badge.
func (d *toolLoopDetector) loopingTool() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.lastToolName
}

// ObserveAgentToolInvocation feeds the detector and broadcasts
// office:agentToolLoop on the first trigger of a streak. Safe to
// call with partial data — short-circuits if the invocation doesn't
// have both name and resultHash.
func (m *Manager) ObserveAgentToolInvocation(sessionID, name, resultHash string) bool {
	agent := m.GetAgent(sessionID)
	if agent == nil {
		return false
	}
	agent.mu.Lock()
	if agent.toolLoopDetect == nil {
		agent.toolLoopDetect = newToolLoopDetector()
	}
	det := agent.toolLoopDetect
	agName := sessionID
	if agent.Config != nil {
		agName = agent.Config.Name
	}
	agent.mu.Unlock()

	triggered, toolName := det.observe(name, resultHash)
	fired := det.isFired()
	loopedTool := det.loopingTool()

	agent.mu.Lock()
	agent.ToolInLoop = fired
	agent.LoopingTool = loopedTool
	agent.mu.Unlock()

	if triggered {
		slog.Warn("agent tool-loop advisory",
			"session", sessionID, "name", agName, "tool", toolName)
		m.broadcast("office:agentToolLoop", map[string]interface{}{
			"sessionId":  sessionID,
			"agentName":  agName,
			"tool":       toolName,
			"resultHash": resultHash[:min(16, len(resultHash))],
		})
	}
	return fired
}

// ResetAgentToolLoopDetector clears the tool-loop ring on a turn
// boundary. Called from the SSE forwarder on EventIdle alongside the
// text-level detector reset.
func (m *Manager) ResetAgentToolLoopDetector(sessionID string) {
	agent := m.GetAgent(sessionID)
	if agent == nil {
		return
	}
	agent.mu.Lock()
	det := agent.toolLoopDetect
	agent.ToolInLoop = false
	agent.LoopingTool = ""
	agent.mu.Unlock()
	if det != nil {
		det.reset()
	}
}
