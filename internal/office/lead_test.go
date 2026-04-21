package office

import (
	"strings"
	"testing"
)

// R35 · GetLeadAgentInfo resolver. Priority: pinned alive > first
// strategic > none.
func TestGetLeadAgentInfo_NoAgents(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	sid, name := m.GetLeadAgentInfo("")
	if sid != "" || name != "" {
		t.Errorf("expected empties, got sid=%q name=%q", sid, name)
	}
}

func TestGetLeadAgentInfo_NoStrategicAgent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.RegisterAgent("s-vex", "vex", &TeamMemberConfig{ID: "vex", Name: "Vex", Tier: "worker"})
	sid, _ := m.GetLeadAgentInfo("")
	if sid != "" {
		t.Errorf("worker-only roster should return empty lead; got %s", sid)
	}
}

func TestGetLeadAgentInfo_AutoPicksStrategic(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.RegisterAgent("s-worker", "worker1", &TeamMemberConfig{ID: "worker1", Name: "Worker", Tier: "worker"})
	m.RegisterAgent("s-cap", "captain", &TeamMemberConfig{ID: "captain", Name: "Captain", Tier: "strategic"})
	sid, name := m.GetLeadAgentInfo("")
	if sid != "s-cap" {
		t.Errorf("expected s-cap as auto-lead, got %s", sid)
	}
	if name != "Captain" {
		t.Errorf("expected display name Captain, got %s", name)
	}
}

func TestGetLeadAgentInfo_DeterministicOrderOnTie(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.RegisterAgent("s-xavier", "xavier", &TeamMemberConfig{ID: "xavier", Name: "Xavier", Tier: "strategic"})
	m.RegisterAgent("s-alpha", "alpha", &TeamMemberConfig{ID: "alpha", Name: "Alpha", Tier: "strategic"})
	sid, _ := m.GetLeadAgentInfo("")
	// Sorted by ConfigID -> alpha wins.
	if sid != "s-alpha" {
		t.Errorf("expected s-alpha (ConfigID sort winner), got %s", sid)
	}
}

func TestGetLeadAgentInfo_PinnedAliveTakesPriority(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.RegisterAgent("s-alpha", "alpha", &TeamMemberConfig{ID: "alpha", Name: "Alpha", Tier: "strategic"})
	m.RegisterAgent("s-bravo", "bravo", &TeamMemberConfig{ID: "bravo", Name: "Bravo", Tier: "worker"})
	// Pin the worker -- still wins because the pin resolves to an
	// alive session regardless of tier.
	sid, name := m.GetLeadAgentInfo("s-bravo")
	if sid != "s-bravo" || name != "Bravo" {
		t.Errorf("expected pinned s-bravo Bravo, got sid=%s name=%s", sid, name)
	}
}

func TestGetLeadAgentInfo_PinnedDeadFallsBack(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.RegisterAgent("s-alpha", "alpha", &TeamMemberConfig{ID: "alpha", Name: "Alpha", Tier: "strategic"})
	sid, _ := m.GetLeadAgentInfo("s-nonexistent")
	if sid != "s-alpha" {
		t.Errorf("dead pin should fall back to auto-pick; got %s", sid)
	}
}

func TestGetLeadStatusText_NoLead(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	_, ok := m.GetLeadStatusText("")
	if ok {
		t.Error("no agents -> ok=false expected")
	}
}

func TestGetLeadStatusText_HasLead(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.RegisterAgent("s-cap", "captain", &TeamMemberConfig{ID: "captain", Name: "Captain", Role: "Tech Lead", Tier: "strategic"})
	agent := m.GetAgent("s-cap")
	agent.AddTask(&Task{ID: "t1", Title: "ship auth", Status: TaskInProgress, ProgressLog: []TaskProgressEntry{
		{Note: "investigated failing tests"},
		{Note: "drafted middleware"},
	}})
	text, ok := m.GetLeadStatusText("")
	if !ok {
		t.Fatal("expected ok=true with strategic agent + task")
	}
	for _, want := range []string{"Captain", "Tech Lead", "ship auth", "investigated failing tests", "drafted middleware"} {
		if !strings.Contains(text, want) {
			t.Errorf("status text missing %q; got: %s", want, text)
		}
	}
}

func TestSetLeadReplyCallback_InvokesOnDispatch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	var gotSID, gotText string
	m.SetLeadReplyCallback(func(sid, text string) {
		gotSID = sid
		gotText = text
	})
	m.DispatchLeadReply("s-cap", "hello")
	if gotSID != "s-cap" || gotText != "hello" {
		t.Errorf("callback got sid=%q text=%q", gotSID, gotText)
	}
}

func TestSetLeadReplyCallback_NilIsNoOp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	// Should not panic even with no callback installed.
	m.DispatchLeadReply("s-cap", "hello")
	m.SetLeadReplyCallback(nil)
	m.DispatchLeadReply("s-cap", "hello")
}
