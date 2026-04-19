package office

// R20 · Output-repeat loop advisory.
//
// True tool-level loop detection needs structured tool_use/tool_result
// events which the Go SDK doesn't currently surface — that's its own
// round. But *text emission* repetition is already hookable via
// pty.EventOutput.Data which flows per-chunk through the web
// forwarder. Hashing those chunks with a small rolling window catches
// the coarse failure mode: an agent emitting the exact same text
// block three or more times in a short span.
//
// Conservative choices driven by the R18/R19 "fail loud, don't
// auto-act" principle:
//   - Ring of 5 hashes per agent
//   - Fires at >= 3 matches out of the last 5 distinct pushes
//   - Only fires ONCE per continuous streak (resets when the streak
//     breaks) — avoids notification spam on long loops
//   - ADVISORY ONLY: broadcasts office:agentPossibleLoop and flips a
//     flag on the member payload. No auto-stop, no auto-nudge. The
//     operator decides with the R18 kill/inject buttons.
//
// Chunks shorter than loopMinHashBytes are ignored — single-character
// or whitespace-only emissions are noisy and not diagnostic of loops.

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"strings"
	"sync"
)

const (
	loopRingSize       = 5
	loopMatchThreshold = 3
	loopMinHashBytes   = 32
)

type loopDetector struct {
	mu        sync.Mutex
	ring      [loopRingSize]string // zero-value "" means "empty slot"
	idx       int
	advisoryFired bool
}

func newLoopDetector() *loopDetector { return &loopDetector{} }

// observe ingests a chunk. Returns (triggered, hash) where triggered
// is true exactly once per loop streak. The caller broadcasts an SSE
// event on triggered; subsequent chunks in the same streak are
// silent. The streak breaks (and advisoryFired resets) the next time
// a new, non-matching hash lands in the ring.
func (d *loopDetector) observe(chunk string) (triggered bool, hashHex string) {
	trimmed := strings.TrimSpace(chunk)
	if len(trimmed) < loopMinHashBytes {
		return false, ""
	}
	sum := sha256.Sum256([]byte(trimmed))
	h := hex.EncodeToString(sum[:])

	d.mu.Lock()
	defer d.mu.Unlock()

	d.ring[d.idx] = h
	d.idx = (d.idx + 1) % loopRingSize

	// Count occurrences of the just-pushed hash inside the ring.
	matches := 0
	for _, v := range d.ring {
		if v == h {
			matches++
		}
	}

	if matches >= loopMatchThreshold {
		if d.advisoryFired {
			return false, h
		}
		d.advisoryFired = true
		return true, h
	}
	// Streak broken — new non-matching hash ending a prior loop.
	if d.advisoryFired && matches == 1 {
		d.advisoryFired = false
	}
	return false, h
}

// reset clears the ring + fired flag. Called at turn-exit boundaries
// so loops are scoped to a single turn. Cross-turn repetition doesn't
// count: a healthy agent legitimately rebuilds / retests / re-reads
// files across turns.
func (d *loopDetector) reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i := range d.ring {
		d.ring[i] = ""
	}
	d.idx = 0
	d.advisoryFired = false
}

// ObserveAgentOutput is the public entry point called from the web
// forwarder on EventOutput. Lazily creates a detector on the agent if
// one isn't attached yet and broadcasts office:agentPossibleLoop on
// the first trigger of a streak. Returns whether the agent is
// currently in a detected loop so the member payload can expose it.
//
// Manager broadcast is used rather than returning the event up, so
// this is a self-contained side effect from the forwarder's POV.
func (m *Manager) ObserveAgentOutput(sessionID, chunk string) bool {
	agent := m.GetAgent(sessionID)
	if agent == nil {
		return false
	}
	agent.mu.Lock()
	if agent.loopDetect == nil {
		agent.loopDetect = newLoopDetector()
	}
	det := agent.loopDetect
	name := sessionID
	if agent.Config != nil {
		name = agent.Config.Name
	}
	agent.mu.Unlock()

	triggered, h := det.observe(chunk)
	inLoop := det.isFired()
	agent.mu.Lock()
	agent.InLoop = inLoop
	agent.mu.Unlock()

	if triggered {
		slog.Warn("agent output loop advisory",
			"session", sessionID, "name", name, "hash", h[:16])
		m.broadcast("office:agentPossibleLoop", map[string]interface{}{
			"sessionId": sessionID,
			"agentName": name,
			"hashPrefix": h[:16],
		})
	}
	return inLoop
}

// isFired exposes the fired flag for payload snapshotting.
func (d *loopDetector) isFired() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.advisoryFired
}

// ResetAgentLoopDetector clears an agent's loop ring at turn
// boundaries. Safe to call when no detector has been attached.
func (m *Manager) ResetAgentLoopDetector(sessionID string) {
	agent := m.GetAgent(sessionID)
	if agent == nil {
		return
	}
	agent.mu.Lock()
	det := agent.loopDetect
	agent.InLoop = false
	agent.mu.Unlock()
	if det != nil {
		det.reset()
	}
}
