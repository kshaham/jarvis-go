package office

import (
	"strings"
	"testing"
	"time"
)

// TestProcessLeaderTaskResponse_TimeoutBroadcastsSelfDirect drives the
// <-timeout.C fallback arm (standup.go:595-604): when the leader never
// responds, the self-direct fallback prompt is broadcast to every
// non-leader agent. We shrink leaderTaskResponseTimeout via its seam so
// the test doesn't wait the production 5 minutes.
func TestProcessLeaderTaskResponse_TimeoutBroadcastsSelfDirect(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m, store, _ := newCtrlManager(t, DefaultConfig())
	m.RegisterAgent("sess-lead", "lead", &TeamMemberConfig{ID: "lead", Name: "Lead", Tier: "strategic"})
	m.RegisterAgent("sess-w", "worker", &TeamMemberConfig{ID: "worker", Name: "Worker", Tier: "worker"})
	leader := m.GetAgent("sess-lead")
	worker := m.GetAgent("sess-w")

	orig := leaderTaskResponseTimeout
	leaderTaskResponseTimeout = 20 * time.Millisecond
	t.Cleanup(func() { leaderTaskResponseTimeout = orig })

	slot := newWaitSlot()
	// Never deliver to the slot — force the timeout.
	m.processLeaderTaskResponse(slot, leader, []*AgentState{leader, worker}, store, "ship the theme")

	// The worker (non-leader) should have received the self-direct prompt.
	var sawSelfDirect bool
	for _, in := range store.screenInputs("sess-w") {
		if strings.Contains(in, "[SELF-DIRECT FALLBACK]") {
			sawSelfDirect = true
		}
	}
	if !sawSelfDirect {
		t.Errorf("expected self-direct fallback prompt to the worker, got %v",
			store.screenInputs("sess-w"))
	}
	// The leader must NOT receive the self-direct prompt (it's excluded).
	for _, in := range store.screenInputs("sess-lead") {
		if strings.Contains(in, "[SELF-DIRECT FALLBACK]") {
			t.Error("leader should be excluded from the self-direct fallback")
		}
	}
}

// TestTriggerLeaderTaskAssignment_NilSessionsSkipsPrompt drives the
// sessions==nil warn+skip in triggerLeaderTaskAssignment (standup.go:565-567):
// with no session store, the strategic lead still gets a wait slot armed,
// but no prompt is written. We use a manager whose sessions is nil.
func TestTriggerLeaderTaskAssignment_NilSessionsSkipsPrompt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// nil SessionStore so m.sessions is nil inside the trigger.
	m := NewManager(nil, func(string, interface{}) {}, DefaultConfig())
	m.RegisterAgent("sess-lead", "lead", &TeamMemberConfig{ID: "lead", Name: "Lead", Tier: "strategic", Role: "Lead"})
	lead := m.GetAgent("sess-lead")

	transcript := []StandupMessage{
		{Type: "status_report", SpeakerName: "Lead", Content: "reviewed"},
	}

	// Shrink the downstream goroutine's timeout so it doesn't linger.
	orig := leaderTaskResponseTimeout
	leaderTaskResponseTimeout = 10 * time.Millisecond
	t.Cleanup(func() { leaderTaskResponseTimeout = orig })

	// Must not panic despite nil sessions.
	m.triggerLeaderTaskAssignment("standup-nilsess", []string{"sess-lead"}, transcript)

	// A wait slot should still have been armed on the lead (slot install
	// precedes the sessions==nil check).
	lead.mu.Lock()
	slot := lead.LeaderTaskSlot
	lead.mu.Unlock()
	if slot == nil {
		t.Error("expected a LeaderTaskSlot armed on the lead even with nil sessions")
	}
	// Settle the background goroutine.
	time.Sleep(40 * time.Millisecond)
}
