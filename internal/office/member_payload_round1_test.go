package office

import (
	"testing"
)

// TestBuildMemberPayload_StatusBranches exercises every status-resolution
// branch in BuildMemberPayload: nil agent, idle, SDK-working, in-progress task.
func TestBuildMemberPayload_StatusBranches(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()

	// nil / no-config -> nil payload.
	if got := m.BuildMemberPayload(nil); got != nil {
		t.Error("expected nil payload for nil agent")
	}
	bare := &AgentState{ConfigID: "x"} // Config nil
	if got := m.BuildMemberPayload(bare); got != nil {
		t.Error("expected nil payload for agent with nil Config")
	}

	// Idle agent (no tasks, not SDK-working).
	m.RegisterAgent("sess-idle", "idle", &TeamMemberConfig{ID: "idle", Name: "Idle", Role: "FE", Color: "#fff"})
	idle := m.GetAgent("sess-idle")
	p := m.BuildMemberPayload(idle)
	if p["status"] != "idle" {
		t.Errorf("expected idle status, got %v", p["status"])
	}
	if p["avatarInitial"] != "I" {
		t.Errorf("expected avatar initial 'I', got %v", p["avatarInitial"])
	}

	// SDK-working agent -> working.
	m.RegisterAgent("sess-sdk", "sdk", &TeamMemberConfig{ID: "sdk", Name: "Sdk", Role: "BE"})
	sdk := m.GetAgent("sess-sdk")
	sdk.SetSDKWorking(true)
	if got := m.BuildMemberPayload(sdk)["status"]; got != "working" {
		t.Errorf("expected working status for SDK-working agent, got %v", got)
	}

	// Agent with an active (in-progress) task -> working.
	m.RegisterAgent("sess-task", "tsk", &TeamMemberConfig{ID: "tsk", Name: "Tsk", Role: "QA"})
	tsk := m.GetAgent("sess-task")
	tsk.AddTask(&Task{ID: "t1", Title: "Work", Status: TaskInProgress})
	if got := m.BuildMemberPayload(tsk)["status"]; got != "working" {
		t.Errorf("expected working status for an active-task agent, got %v", got)
	}
}

// TestBuildMemberPayload_PayloadShape asserts the payload carries the expected
// identity + scoring fields so downstream SSE consumers stay stable.
func TestBuildMemberPayload_PayloadShape(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.RegisterAgent("sess-a", "alpha", &TeamMemberConfig{
		ID: "alpha", Name: "Alpha", Role: "Frontend", Tier: "worker", Color: "#abc",
	})
	a := m.GetAgent("sess-a")

	p := m.BuildMemberPayload(a)
	for _, key := range []string{
		"sessionId", "configId", "name", "role", "tier", "status",
		"activeTask", "learningCount", "skillCount", "score", "usage",
		"lastActivityAt", "voice",
	} {
		if _, ok := p[key]; !ok {
			t.Errorf("payload missing key %q", key)
		}
	}
	if p["configId"] != "alpha" || p["name"] != "Alpha" || p["role"] != "Frontend" {
		t.Errorf("identity fields wrong: %+v", p)
	}
}
