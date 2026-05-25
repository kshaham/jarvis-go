package claudehooks

import (
	"net/http"
	"testing"
)

// R3.1 · WorktreeCreate / WorktreeRemove behaviour tests. Asserts
// that WorktreeCreate updates AgentState.WorktreePath via the new
// SetWorktreePath method, and that WorktreeRemove broadcasts without
// clearing the path (operator-owned worktrees outlive any inner
// Task-tool worktree).

func TestWorktreeCreate_UpdatesAgentState(t *testing.T) {
	reg := newFakeRegistry("sess-001A")
	d := NewDispatcher(reg, nil)

	rec := fireEvent(t, d, "sess-001A", "WorktreeCreate", WorktreeCreateEvent{
		BaseEvent:      BaseEvent{SessionID: "sess-001A", HookEventName: "WorktreeCreate"},
		WorktreePath:   "/tmp/myrepo/.claude/worktrees/task-123",
		WorktreeBranch: "feature/task-123",
		BaseBranch:     "main",
	})
	if rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
		t.Fatalf("expected 2xx, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := reg.state["sess-001A"].worktreePath; got != "/tmp/myrepo/.claude/worktrees/task-123" {
		t.Errorf("worktreePath=%q, want the path from the event", got)
	}

	// Broadcast assertion.
	if len(reg.broadcasts) == 0 {
		t.Fatal("expected an SSE broadcast")
	}
	found := false
	for _, b := range reg.broadcasts {
		if b.event == "office:agentWorktreeCreated" {
			found = true
			break
		}
	}
	if !found {
		t.Error("missing office:agentWorktreeCreated broadcast")
	}
}

func TestWorktreeRemove_DoesNotClearPath(t *testing.T) {
	reg := newFakeRegistry("sess-002A")
	reg.state["sess-002A"].worktreePath = "/tmp/myrepo/.claude/worktrees/jarvis-owned"
	d := NewDispatcher(reg, nil)

	rec := fireEvent(t, d, "sess-002A", "WorktreeRemove", WorktreeRemoveEvent{
		BaseEvent:    BaseEvent{SessionID: "sess-002A", HookEventName: "WorktreeRemove"},
		WorktreePath: "/tmp/myrepo/.claude/worktrees/inner-task",
		RemoveReason: "subagent_finish",
	})
	if rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
		t.Fatalf("expected 2xx, got %d", rec.Code)
	}
	if got := reg.state["sess-002A"].worktreePath; got != "/tmp/myrepo/.claude/worktrees/jarvis-owned" {
		t.Errorf("WorktreeRemove must not bash the outer worktree path; got %q", got)
	}
}

func TestWorktreeCreate_UnregisteredAgentIsNoOp(t *testing.T) {
	reg := newFakeRegistry() // no sessions registered
	d := NewDispatcher(reg, nil)

	rec := fireEvent(t, d, "s-unknown", "WorktreeCreate", WorktreeCreateEvent{
		BaseEvent:    BaseEvent{SessionID: "s-unknown", HookEventName: "WorktreeCreate"},
		WorktreePath: "/tmp/anywhere",
	})
	// fail-open: 204 is acceptable for unregistered agents.
	if rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
		t.Fatalf("expected fail-open 2xx, got %d", rec.Code)
	}
}
