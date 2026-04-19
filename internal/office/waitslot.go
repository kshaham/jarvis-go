package office

// R19 · Close-guard around the per-agent response channels used by
// standup / brainstorm / leader-task flows.
//
// Before this helper the channels were raw `chan string, 1` values
// stored directly on AgentState. When the waiter returned (timeout or
// reply received), the channel reference was eventually replaced on
// the next round, but a late reply arriving during the gap could land
// in the *new* round's channel and pollute it. Submit paths used the
// non-blocking select/default pattern so they couldn't panic, but
// cross-round pollution was possible.
//
// waitSlot fixes that with a tiny atomic closed flag:
//   - newWaitSlot() returns a fresh slot
//   - reader selects on slot.ch()
//   - Submit paths call slot.deliver(v); returns false if the slot
//     has been finished (late reply) OR the buffer is full (double
//     reply). Both are accounted in lateResponses for observability.
//   - waitFor* functions defer slot.finish() so any reply arriving
//     after the reader returns is dropped at the sender.
//
// This makes late replies safe (logged + counted, never delivered)
// without needing round IDs or agent cooperation.

import (
	"log/slog"
	"sync/atomic"
)

// waitSlotBuf is the per-slot channel capacity. 1 is enough: only a
// single reply per round is expected; the close-guard covers late
// duplicates.
const waitSlotBuf = 1

type waitSlot struct {
	c      chan string
	closed atomic.Bool
}

func newWaitSlot() *waitSlot {
	return &waitSlot{c: make(chan string, waitSlotBuf)}
}

// ch exposes the channel for the reader side to select on.
func (s *waitSlot) ch() <-chan string { return s.c }

// deliver attempts a non-blocking send and returns whether the send
// succeeded. Returns false (and logs a WARN) if the slot has been
// finished — a "late reply" condition that the caller can count.
func (s *waitSlot) deliver(v string) bool {
	if s == nil || s.closed.Load() {
		return false
	}
	select {
	case s.c <- v:
		return true
	default:
		return false
	}
}

// finish marks the slot as done. Subsequent deliver() calls return
// false. Safe to call more than once; the atomic store is idempotent.
func (s *waitSlot) finish() {
	if s != nil {
		s.closed.Store(true)
	}
}

// lateResponsesDropped counts reply submissions that arrived after
// their slot was finished (cross-round leak or post-timeout reply).
// Read from BuildMemberPayload and broadcast so operators can see if
// agents are consistently replying too late.
var lateResponsesDropped atomic.Uint64

// recordLateResponse bumps the global counter. Called from each
// Submit* handler when deliver() returns false while the slot was
// already closed. The WARN fires at the first drop and every 50th
// after that, matching the R19 drop-counter pattern in sdk_session.go.
func recordLateResponse(kind, sessionID string) {
	total := lateResponsesDropped.Add(1)
	if total == 1 || total%50 == 0 {
		slog.Warn("late response submission dropped",
			"kind", kind, "session", sessionID, "totalDropped", total)
	}
}

// LateResponsesDropped returns the global count of late-response
// drops since process start. Exposed for the member payload.
func LateResponsesDropped() uint64 {
	return lateResponsesDropped.Load()
}
