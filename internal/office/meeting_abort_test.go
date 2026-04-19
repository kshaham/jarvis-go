package office

// R19 · When a facilitator dies mid-meeting, HandleAgentDeath should
// mark the meeting inactive, deliver the sentinel to participant
// waitSlots so their readers unblock immediately, and broadcast
// office:meetingAborted.

import (
	"sync"
	"testing"
	"time"
)

func TestAbortMeeting_DeliversSentinelAndBroadcasts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Cleanup(func() { time.Sleep(80 * time.Millisecond) })

	store := &recordingSessionStore{}
	var mu sync.Mutex
	events := []string{}
	m := NewManager(store, func(event string, _ interface{}) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, event)
	}, nil)

	// Two participants; facilitator is the one who dies.
	m.RegisterAgent("sess-fac", "cfg-fac", &TeamMemberConfig{ID: "cfg-fac", Name: "Facilitator", Tier: "strategic"})
	m.RegisterAgent("sess-p2", "cfg-p2", &TeamMemberConfig{ID: "cfg-p2", Name: "Other", Tier: "worker"})

	// Fabricate a live brainstorm meeting with the facilitator mapped.
	meeting := &Meeting{
		ID:                   "mtg-1",
		Type:                 "brainstorm",
		Participants:         []string{"sess-fac", "sess-p2"},
		StartedAt:            time.Now(),
		Active:               true,
		FacilitatorSessionID: "sess-fac",
	}
	m.mu.Lock()
	m.meetings[meeting.ID] = meeting
	m.mu.Unlock()

	// Arm the OTHER participant's BrainstormSlot — they're the one
	// we expect to receive the sentinel.
	other := m.GetAgent("sess-p2")
	slot := newWaitSlot()
	other.mu.Lock()
	other.BrainstormSlot = slot
	other.mu.Unlock()

	received := make(chan string, 1)
	go func() { received <- <-slot.ch() }()

	// Kill the facilitator.
	m.HandleAgentDeath("sess-fac")

	select {
	case got := <-received:
		if got != meetingAbortedSentinel {
			t.Fatalf("expected sentinel, got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("participant did not receive sentinel")
	}

	// Meeting should be marked inactive.
	m.mu.RLock()
	live := m.meetings["mtg-1"]
	m.mu.RUnlock()
	if live == nil || live.Active {
		t.Fatal("expected meeting marked inactive")
	}

	mu.Lock()
	defer mu.Unlock()
	var sawAbort, sawDied bool
	for _, ev := range events {
		if ev == "office:meetingAborted" {
			sawAbort = true
		}
		if ev == "office:agentDied" {
			sawDied = true
		}
	}
	if !sawAbort {
		t.Fatalf("expected office:meetingAborted, got %v", events)
	}
	if !sawDied {
		t.Fatalf("expected office:agentDied still broadcast after abort, got %v", events)
	}
}

// R21 code-review regression guard: when a brainstorm facilitator
// dies, the sentinel must only hit BrainstormSlot. A participant
// with a concurrent unrelated StandupSlot (e.g., from a different
// flow) must NOT receive the sentinel on that slot.
func TestAbortMeeting_BrainstormTypeDoesNotTouchStandupSlot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Cleanup(func() { time.Sleep(80 * time.Millisecond) })

	store := &recordingSessionStore{}
	m := NewManager(store, func(event string, _ interface{}) {}, nil)

	m.RegisterAgent("sess-fac", "cfg-fac", &TeamMemberConfig{ID: "cfg-fac", Name: "Facilitator", Tier: "strategic"})
	m.RegisterAgent("sess-p", "cfg-p", &TeamMemberConfig{ID: "cfg-p", Name: "Part", Tier: "worker"})

	meeting := &Meeting{
		ID: "mtg-b", Type: "brainstorm",
		Participants:         []string{"sess-fac", "sess-p"},
		StartedAt:            time.Now(),
		Active:               true,
		FacilitatorSessionID: "sess-fac",
	}
	m.mu.Lock()
	m.meetings[meeting.ID] = meeting
	m.mu.Unlock()

	// Arm BOTH slots on the participant — the brainstorm abort must
	// only poke the brainstorm slot.
	other := m.GetAgent("sess-p")
	bslot := newWaitSlot()
	sslot := newWaitSlot()
	other.mu.Lock()
	other.BrainstormSlot = bslot
	other.StandupSlot = sslot
	other.mu.Unlock()

	brainstormReceived := make(chan string, 1)
	standupReceived := make(chan string, 1)
	go func() { brainstormReceived <- <-bslot.ch() }()
	go func() { standupReceived <- <-sslot.ch() }()

	m.HandleAgentDeath("sess-fac")

	select {
	case got := <-brainstormReceived:
		if got != meetingAbortedSentinel {
			t.Fatalf("brainstorm slot: expected sentinel, got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("brainstorm slot did not receive sentinel")
	}

	// Standup slot should NOT have received anything.
	select {
	case got := <-standupReceived:
		t.Fatalf("standup slot incorrectly received %q — brainstorm abort must not leak into standup slot", got)
	case <-time.After(200 * time.Millisecond):
		// expected: no delivery
	}
}

func TestAbortMeeting_NoFacilitatorMatch_NoOp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Cleanup(func() { time.Sleep(80 * time.Millisecond) })

	store := &recordingSessionStore{}
	var mu sync.Mutex
	events := []string{}
	m := NewManager(store, func(event string, _ interface{}) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, event)
	}, nil)

	m.RegisterAgent("sess-x", "cfg-x", &TeamMemberConfig{ID: "cfg-x", Name: "Solo", Tier: "worker"})
	// Meeting facilitated by a DIFFERENT session — death of sess-x
	// should not abort it.
	meeting := &Meeting{
		ID: "mtg-2", Type: "brainstorm", Participants: []string{"sess-y"},
		StartedAt: time.Now(), Active: true, FacilitatorSessionID: "sess-y",
	}
	m.mu.Lock()
	m.meetings[meeting.ID] = meeting
	m.mu.Unlock()

	m.HandleAgentDeath("sess-x")

	m.mu.RLock()
	live := m.meetings["mtg-2"]
	m.mu.RUnlock()
	if live == nil || !live.Active {
		t.Fatal("expected unrelated meeting to remain active")
	}
}
