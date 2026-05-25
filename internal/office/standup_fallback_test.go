package office

import (
	"sync"
	"testing"
)

// recordingPromptSessions captures every WriteInkPrompt call so the
// self-direct fallback test can assert exactly which agents got
// nudged and with what payload.
type recordingPromptSessions struct {
	mockSessionStore
	mu      sync.Mutex
	prompts map[string]string // sessionID -> last prompt
}

func newRecordingPromptSessions() *recordingPromptSessions {
	return &recordingPromptSessions{prompts: map[string]string{}}
}

func (r *recordingPromptSessions) WriteInkPrompt(sessionID, prompt string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prompts[sessionID] = prompt
	return true
}

func TestBroadcastSelfDirectFallback_NudgesNonLeaders(t *testing.T) {
	m := newTestManager()
	leader := &TeamMemberConfig{ID: "lead-1", Name: "Lead", Tier: "strategic"}
	worker := &TeamMemberConfig{ID: "work-1", Name: "Worker"}
	worker2 := &TeamMemberConfig{ID: "work-2", Name: "Worker2"}

	m.RegisterAgent("sess-lead-001", leader.ID, leader)
	m.RegisterAgent("sess-work-001", worker.ID, worker)
	m.RegisterAgent("sess-work-002", worker2.ID, worker2)

	leaderAgent := m.GetAgent("sess-lead-001")
	roster := m.GetRoster()
	if len(roster) != 3 {
		t.Fatalf("roster size=%d want 3", len(roster))
	}

	rec := newRecordingPromptSessions()
	m.broadcastSelfDirectFallback(roster, leaderAgent, rec, "build the thing")

	rec.mu.Lock()
	defer rec.mu.Unlock()
	// Leader must NOT be prompted — the fallback is for the agents
	// the leader failed to dispatch tasks to.
	if _, gotLeader := rec.prompts["sess-lead-001"]; gotLeader {
		t.Error("leader should not receive the self-direct prompt")
	}
	// Both workers must be prompted.
	if _, ok := rec.prompts["sess-work-001"]; !ok {
		t.Error("worker 1 missing the self-direct prompt")
	}
	if _, ok := rec.prompts["sess-work-002"]; !ok {
		t.Error("worker 2 missing the self-direct prompt")
	}
	// Prompt body must reference the mission so agents have context.
	for sid, p := range rec.prompts {
		if !contains(p, "build the thing") {
			t.Errorf("agent %s prompt missing mission line: %q", sid, p)
		}
		if !contains(p, "SELF-DIRECT FALLBACK") {
			t.Errorf("agent %s prompt missing tag: %q", sid, p)
		}
	}
}

func TestBroadcastSelfDirectFallback_NilSessionsNoOp(t *testing.T) {
	// Defensive: nil SessionStore must not panic.
	m := newTestManager()
	m.RegisterAgent("sess-a", "cfg-a", &TeamMemberConfig{ID: "cfg-a"})
	m.broadcastSelfDirectFallback(m.GetRoster(), nil, nil, "")
}

func TestBroadcastSelfDirectFallback_NoLeaderStillNudgesEveryone(t *testing.T) {
	// When the leader pointer is nil (e.g., leader has already been
	// removed from the roster by the time the fallback fires), every
	// agent should still get the self-direct prompt — none of them
	// is the leader to skip.
	m := newTestManager()
	m.RegisterAgent("sess-a", "cfg-a", &TeamMemberConfig{ID: "cfg-a"})
	m.RegisterAgent("sess-b", "cfg-b", &TeamMemberConfig{ID: "cfg-b"})

	rec := newRecordingPromptSessions()
	m.broadcastSelfDirectFallback(m.GetRoster(), nil, rec, "")

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.prompts) != 2 {
		t.Errorf("prompts=%d want 2 (both agents notified when leader=nil)", len(rec.prompts))
	}
}

// contains is a tiny test helper to avoid importing strings in this
// already-strings-heavy package.
func contains(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
