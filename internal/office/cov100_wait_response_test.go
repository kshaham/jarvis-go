package office

import (
	"strings"
	"testing"
	"time"
)

// fastMeetingConfig clones DefaultConfig but shrinks the standup +
// brainstorm timeouts/intervals so the wait loops resolve in tens of
// milliseconds instead of minutes. Nudge interval is set ABOVE the
// response timeout so the timeout fires first in the no-response tests.
func fastMeetingConfig() *Config {
	c := DefaultConfig()
	c.StandupResponseTimeout = 40 * time.Millisecond
	c.StandupNudgeInterval = 10 * time.Second // never reached before timeout
	c.StandupMaxNudges = 3
	c.BrainstormTimeout = 40 * time.Millisecond
	return c
}

// TestWaitForStandupResponse_AgentUnreachable drives the agent==nil
// branch: an unregistered sessionID yields the "<name> was unreachable"
// fallback with timedOut=true.
func TestWaitForStandupResponse_AgentUnreachable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := NewManager(&mockSessionStore{}, func(string, interface{}) {}, fastMeetingConfig())
	msg, timedOut := m.waitForStandupResponse("ghost", "Ghost", &mockSessionStore{}, m.config)
	if !timedOut {
		t.Error("expected timedOut=true for unreachable agent")
	}
	if !strings.Contains(msg, "unreachable") {
		t.Errorf("expected 'unreachable' message, got %q", msg)
	}
}

// TestWaitForStandupResponse_Timeout drives the <-timeout.C arm: the
// agent never submits, so the loop returns the "did not respond in time"
// fallback once the (tiny) response timeout elapses.
func TestWaitForStandupResponse_Timeout(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := NewManager(&mockSessionStore{}, func(string, interface{}) {}, fastMeetingConfig())
	m.RegisterAgent("sess-q", "q", &TeamMemberConfig{ID: "q", Name: "Quill", Tier: "worker"})

	start := time.Now()
	msg, timedOut := m.waitForStandupResponse("sess-q", "Quill", &mockSessionStore{}, m.config)
	if !timedOut {
		t.Error("expected timedOut=true on response timeout")
	}
	if !strings.Contains(msg, "did not respond in time") {
		t.Errorf("expected timeout message, got %q", msg)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("timeout took too long (%v); config not honoured?", elapsed)
	}
}

// TestWaitForStandupResponse_AbortSentinel drives the
// meetingAbortedSentinel branch: a sentinel delivered into the standup
// slot returns the "[standup aborted...]" fallback with timedOut=true.
func TestWaitForStandupResponse_AbortSentinel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := NewManager(&mockSessionStore{}, func(string, interface{}) {}, fastMeetingConfig())
	m.RegisterAgent("sess-a", "a", &TeamMemberConfig{ID: "a", Name: "Ada", Tier: "worker"})

	type result struct {
		msg      string
		timedOut bool
	}
	done := make(chan result, 1)
	go func() {
		msg, timedOut := m.waitForStandupResponse("sess-a", "Ada", &mockSessionStore{}, m.config)
		done <- result{msg, timedOut}
	}()

	// Poll until the wait goroutine installs the StandupSlot, then deliver
	// the abort sentinel into it (mirrors abortMeetingsFacilitatedBy).
	agent := m.GetAgent("sess-a")
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		agent.mu.Lock()
		slot := agent.StandupSlot
		agent.mu.Unlock()
		if slot != nil && slot.deliver(meetingAbortedSentinel) {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}

	select {
	case r := <-done:
		if !r.timedOut {
			t.Error("abort sentinel should report timedOut=true")
		}
		if !strings.Contains(r.msg, "aborted") {
			t.Errorf("expected 'aborted' message, got %q", r.msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waitForStandupResponse did not return after sentinel delivery")
	}
}

// TestWaitForBrainstormResponse_AgentUnreachable drives the agent==nil
// branch of the brainstorm waiter.
func TestWaitForBrainstormResponse_AgentUnreachable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := NewManager(&mockSessionStore{}, func(string, interface{}) {}, fastMeetingConfig())
	got := m.waitForBrainstormResponse("ghost", "Ghost")
	if !strings.Contains(got, "unreachable") {
		t.Errorf("expected 'unreachable' message, got %q", got)
	}
}

// TestWaitForBrainstormResponse_Timeout drives the <-timer.C arm.
func TestWaitForBrainstormResponse_Timeout(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := NewManager(&mockSessionStore{}, func(string, interface{}) {}, fastMeetingConfig())
	m.RegisterAgent("sess-b", "b", &TeamMemberConfig{ID: "b", Name: "Bex", Tier: "worker"})

	start := time.Now()
	got := m.waitForBrainstormResponse("sess-b", "Bex")
	if !strings.Contains(got, "did not respond in time") {
		t.Errorf("expected timeout message, got %q", got)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("brainstorm timeout took too long: %v", elapsed)
	}
}

// TestWaitForBrainstormResponse_AbortSentinel drives the
// meetingAbortedSentinel branch of the brainstorm waiter.
func TestWaitForBrainstormResponse_AbortSentinel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := NewManager(&mockSessionStore{}, func(string, interface{}) {}, fastMeetingConfig())
	m.RegisterAgent("sess-c", "c", &TeamMemberConfig{ID: "c", Name: "Cyd", Tier: "worker"})

	done := make(chan string, 1)
	go func() { done <- m.waitForBrainstormResponse("sess-c", "Cyd") }()

	agent := m.GetAgent("sess-c")
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		agent.mu.Lock()
		slot := agent.BrainstormSlot
		agent.mu.Unlock()
		if slot != nil && slot.deliver(meetingAbortedSentinel) {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}

	select {
	case got := <-done:
		if !strings.Contains(got, "aborted") {
			t.Errorf("expected 'aborted' message, got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waitForBrainstormResponse did not return after sentinel delivery")
	}
}

// TestWaitForBrainstormResponse_RealResponse drives the normal
// (non-sentinel) response delivery so the proposal is returned verbatim.
func TestWaitForBrainstormResponse_RealResponse(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := NewManager(&mockSessionStore{}, func(string, interface{}) {}, DefaultConfig())
	m.config.BrainstormTimeout = 2 * time.Second
	m.RegisterAgent("sess-d", "d", &TeamMemberConfig{ID: "d", Name: "Dex", Tier: "worker"})

	done := make(chan string, 1)
	go func() { done <- m.waitForBrainstormResponse("sess-d", "Dex") }()

	agent := m.GetAgent("sess-d")
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		agent.mu.Lock()
		slot := agent.BrainstormSlot
		agent.mu.Unlock()
		if slot != nil && slot.deliver("here is my real proposal") {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}

	select {
	case got := <-done:
		if got != "here is my real proposal" {
			t.Errorf("expected verbatim proposal, got %q", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("waitForBrainstormResponse did not return the real response")
	}
}
