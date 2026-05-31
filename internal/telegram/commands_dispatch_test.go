package telegram

import (
	"errors"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// /list_agents
// ---------------------------------------------------------------------------

func TestCmd_ListAgents_Empty(t *testing.T) {
	h, _ := newTestHandler(t)
	h.commands.HandleCommand("chat1", "/list_agents")
	got := lastEnqueued(h)
	if !strings.Contains(got, "No active agents") {
		t.Errorf("expected 'No active agents', got %q", got)
	}
}

func TestCmd_ListAgents_WithAgents(t *testing.T) {
	h, bridge := newTestHandler(t)
	bridge.agents = []AgentInfo{{ID: "agent-xyz", Name: "worker-7", Status: "running"}}
	h.commands.HandleCommand("chat1", "/list_agents")
	got := lastEnqueued(h)
	if !strings.Contains(got, "worker-7") {
		t.Errorf("expected agent name in output, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// /send -- happy path, bad target, send failure
// ---------------------------------------------------------------------------

func TestCmd_Send_Success(t *testing.T) {
	h, bridge := newTestHandler(t)
	// The mock has one session with ShortID "abcdef12".
	h.commands.HandleCommand("chat1", "/send abcdef12 hello world")

	if len(bridge.sendCalls) != 1 {
		t.Fatalf("expected 1 SendToSession call, got %d", len(bridge.sendCalls))
	}
	call := bridge.sendCalls[0]
	if call.SessionID != "abcdef12-3456-7890-abcd-ef1234567890" {
		t.Errorf("send routed to wrong session: %q", call.SessionID)
	}
	if call.Message != "hello world" {
		t.Errorf("message body = %q, want 'hello world'", call.Message)
	}
	got := lastEnqueued(h)
	if !strings.Contains(got, "Message sent") {
		t.Errorf("expected success confirmation, got %q", got)
	}
}

func TestCmd_Send_UnknownSession(t *testing.T) {
	h, bridge := newTestHandler(t)
	h.commands.HandleCommand("chat1", "/send zzzzzzzz hello")
	if len(bridge.sendCalls) != 0 {
		t.Errorf("should not call SendToSession for unknown session, got %d calls", len(bridge.sendCalls))
	}
	got := lastEnqueued(h)
	if !strings.Contains(got, "not found") {
		t.Errorf("expected 'not found', got %q", got)
	}
}

func TestCmd_Send_DeliveryFails(t *testing.T) {
	h, bridge := newTestHandler(t)
	bridge.sendResult = false
	h.commands.HandleCommand("chat1", "/send abcdef12 hi")
	got := lastEnqueued(h)
	if !strings.Contains(got, "Failed to send") {
		t.Errorf("expected failure message, got %q", got)
	}
}

func TestCmd_Send_MissingMessageBody(t *testing.T) {
	h, _ := newTestHandler(t)
	// Only a target ID, no message.
	h.commands.HandleCommand("chat1", "/send abcdef12")
	got := lastEnqueued(h)
	if !strings.Contains(got, "Usage") {
		t.Errorf("expected usage hint for missing body, got %q", got)
	}
}

func TestCmd_Send_ResolveByPrefix(t *testing.T) {
	h, bridge := newTestHandler(t)
	// "abcdef12" is the short id but a 4+ char prefix of the full ID should
	// also resolve.
	h.commands.HandleCommand("chat1", "/send abcd hello")
	if len(bridge.sendCalls) != 1 {
		t.Fatalf("expected prefix resolution to send, got %d calls", len(bridge.sendCalls))
	}
}

// ---------------------------------------------------------------------------
// /new -- default name, custom name, create error
// ---------------------------------------------------------------------------

func TestCmd_New_DefaultName(t *testing.T) {
	h, _ := newTestHandler(t)
	h.commands.HandleCommand("chat1", "/new")
	got := lastEnqueued(h)
	if !strings.Contains(got, "Session created") {
		t.Errorf("expected creation confirmation, got %q", got)
	}
	// ShortID of the created session should appear.
	if !strings.Contains(got, "newid123") {
		t.Errorf("expected created session short id in output, got %q", got)
	}
}

func TestCmd_New_CreateError(t *testing.T) {
	h, bridge := newTestHandler(t)
	bridge.createSessionErr = errors.New("disk full")
	h.commands.HandleCommand("chat1", "/new foo")
	got := lastEnqueued(h)
	if !strings.Contains(got, "Failed to create session") {
		t.Errorf("expected creation failure message, got %q", got)
	}
	if !strings.Contains(got, "disk full") {
		t.Errorf("expected underlying error in message, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// /spawn_agent -- usage, success, error
// ---------------------------------------------------------------------------

func TestCmd_SpawnAgent_Usage(t *testing.T) {
	h, _ := newTestHandler(t)
	h.commands.HandleCommand("chat1", "/spawn_agent")
	got := lastEnqueued(h)
	if !strings.Contains(got, "Usage") {
		t.Errorf("expected usage for empty spawn_agent, got %q", got)
	}
}

func TestCmd_SpawnAgent_Success(t *testing.T) {
	h, _ := newTestHandler(t)
	h.commands.HandleCommand("chat1", "/spawn_agent refactor the parser")
	got := lastEnqueued(h)
	if !strings.Contains(got, "Agent spawned") {
		t.Errorf("expected spawn confirmation, got %q", got)
	}
	if !strings.Contains(got, "refactor the parser") {
		t.Errorf("expected task echoed in output, got %q", got)
	}
}

func TestCmd_SpawnAgent_Error(t *testing.T) {
	h, bridge := newTestHandler(t)
	bridge.createSessionErr = errors.New("no capacity")
	h.commands.HandleCommand("chat1", "/spawn_agent do thing")
	got := lastEnqueued(h)
	if !strings.Contains(got, "Failed to spawn agent") {
		t.Errorf("expected spawn failure message, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// /stop_agent -- usage, not found, success, stop error
// ---------------------------------------------------------------------------

func TestCmd_StopAgent_Usage(t *testing.T) {
	h, _ := newTestHandler(t)
	h.commands.HandleCommand("chat1", "/stop_agent")
	got := lastEnqueued(h)
	if !strings.Contains(got, "Usage") {
		t.Errorf("expected usage, got %q", got)
	}
}

func TestCmd_StopAgent_NotFound(t *testing.T) {
	h, _ := newTestHandler(t)
	h.commands.HandleCommand("chat1", "/stop_agent nope1234")
	got := lastEnqueued(h)
	if !strings.Contains(got, "not found") {
		t.Errorf("expected not-found, got %q", got)
	}
}

func TestCmd_StopAgent_Success(t *testing.T) {
	h, bridge := newTestHandler(t)
	bridge.agents = []AgentInfo{{ID: "agent-1234", Name: "a", Status: "running"}}
	h.commands.HandleCommand("chat1", "/stop_agent agent-1234")
	got := lastEnqueued(h)
	if !strings.Contains(got, "stopped") {
		t.Errorf("expected stop confirmation, got %q", got)
	}
}

func TestCmd_StopAgent_StopError(t *testing.T) {
	h, bridge := newTestHandler(t)
	bridge.agents = []AgentInfo{{ID: "agent-1234", Name: "a", Status: "running"}}
	bridge.stopSessionErr = errors.New("already dead")
	h.commands.HandleCommand("chat1", "/stop_agent agent-1234")
	got := lastEnqueued(h)
	if !strings.Contains(got, "Failed to stop agent") {
		t.Errorf("expected failure message, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// /link -- explicit ID, default to most recent, none available, unknown ID
// ---------------------------------------------------------------------------

func TestCmd_Link_ExplicitID(t *testing.T) {
	h, bridge := newTestHandler(t)
	h.commands.HandleCommand("chat1", "/link abcdef12")

	// linkSession subscribes to output.
	if len(bridge.subscribeCalls) != 1 {
		t.Fatalf("expected 1 subscribe call, got %d", len(bridge.subscribeCalls))
	}
	h.mu.RLock()
	linked := h.linkedSessions["chat1"]
	h.mu.RUnlock()
	if linked != "abcdef12-3456-7890-abcd-ef1234567890" {
		t.Errorf("chat not linked to expected session: %q", linked)
	}
	got := lastEnqueued(h)
	if !strings.Contains(got, "Linked to session") {
		t.Errorf("expected link confirmation, got %q", got)
	}
}

func TestCmd_Link_DefaultMostRecent(t *testing.T) {
	h, _ := newTestHandler(t)
	h.commands.HandleCommand("chat1", "/link")
	h.mu.RLock()
	linked := h.linkedSessions["chat1"]
	h.mu.RUnlock()
	if linked == "" {
		t.Error("expected default link to most recent session")
	}
}

func TestCmd_Link_NoSessions(t *testing.T) {
	h, bridge := newTestHandler(t)
	bridge.sessions = nil
	h.commands.HandleCommand("chat1", "/link")
	got := lastEnqueued(h)
	if !strings.Contains(got, "No active sessions") {
		t.Errorf("expected no-sessions message, got %q", got)
	}
}

func TestCmd_Link_UnknownID(t *testing.T) {
	h, _ := newTestHandler(t)
	h.commands.HandleCommand("chat1", "/link zzzzzzzz")
	got := lastEnqueued(h)
	if !strings.Contains(got, "not found") {
		t.Errorf("expected not-found, got %q", got)
	}
}

func TestCmd_Link_WithOutputForwardingNote(t *testing.T) {
	h, _ := newTestHandler(t)
	_ = h.settings.Update(func(s *Settings) { s.ForwardOutputToChat = true })
	h.commands.HandleCommand("chat1", "/link abcdef12")
	got := lastEnqueued(h)
	if !strings.Contains(got, "Output forwarding is enabled") {
		t.Errorf("expected forwarding note, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// /unlink -- linked and not-linked
// ---------------------------------------------------------------------------

func TestCmd_Unlink_NotLinked(t *testing.T) {
	h, _ := newTestHandler(t)
	h.commands.HandleCommand("chat1", "/unlink")
	got := lastEnqueued(h)
	if !strings.Contains(got, "No session linked") {
		t.Errorf("expected not-linked message, got %q", got)
	}
}

func TestCmd_Unlink_Linked(t *testing.T) {
	h, _ := newTestHandler(t)
	h.commands.HandleCommand("chat1", "/link abcdef12")
	_ = drainQueue(h)
	h.commands.HandleCommand("chat1", "/unlink")
	got := lastEnqueued(h)
	if !strings.Contains(got, "Session unlinked") {
		t.Errorf("expected unlink confirmation, got %q", got)
	}
	h.mu.RLock()
	_, stillLinked := h.linkedSessions["chat1"]
	h.mu.RUnlock()
	if stillLinked {
		t.Error("chat should no longer be linked after /unlink")
	}
}

// ---------------------------------------------------------------------------
// /linked -- shows status, with and without detail
// ---------------------------------------------------------------------------

func TestCmd_Linked_NotLinked(t *testing.T) {
	h, _ := newTestHandler(t)
	h.commands.HandleCommand("chat1", "/linked")
	got := lastEnqueued(h)
	if !strings.Contains(got, "No session linked") {
		t.Errorf("expected not-linked message, got %q", got)
	}
}

func TestCmd_Linked_WithDetail(t *testing.T) {
	h, bridge := newTestHandler(t)
	bridge.detailedStatus = "Working: editing main.go"
	h.commands.HandleCommand("chat1", "/link abcdef12")
	_ = drainQueue(h)
	h.commands.HandleCommand("chat1", "/linked")
	got := lastEnqueued(h)
	if !strings.Contains(got, "Linked to session") {
		t.Errorf("expected linked header, got %q", got)
	}
	if !strings.Contains(got, "Working: editing main.go") {
		t.Errorf("expected detailed status appended, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// /output -- not linked vs linked
// ---------------------------------------------------------------------------

func TestCmd_Output_NotLinked(t *testing.T) {
	h, _ := newTestHandler(t)
	h.commands.HandleCommand("chat1", "/output")
	got := lastEnqueued(h)
	if !strings.Contains(got, "No session linked") {
		t.Errorf("expected not-linked message, got %q", got)
	}
}

func TestCmd_Output_Linked(t *testing.T) {
	h, _ := newTestHandler(t)
	h.commands.HandleCommand("chat1", "/link abcdef12")
	_ = drainQueue(h)
	h.commands.HandleCommand("chat1", "/output")
	got := lastEnqueued(h)
	if !strings.Contains(got, "Output buffer flushed") {
		t.Errorf("expected flush confirmation, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// /stop -- not linked, success, stop error
// ---------------------------------------------------------------------------

func TestCmd_Stop_NotLinked(t *testing.T) {
	h, _ := newTestHandler(t)
	h.commands.HandleCommand("chat1", "/stop")
	got := lastEnqueued(h)
	if !strings.Contains(got, "No session linked") {
		t.Errorf("expected not-linked message, got %q", got)
	}
}

func TestCmd_Stop_Success(t *testing.T) {
	h, _ := newTestHandler(t)
	h.commands.HandleCommand("chat1", "/link abcdef12")
	_ = drainQueue(h)
	h.commands.HandleCommand("chat1", "/stop")
	got := lastEnqueued(h)
	if !strings.Contains(got, "stopped and unlinked") {
		t.Errorf("expected stop+unlink confirmation, got %q", got)
	}
	h.mu.RLock()
	_, stillLinked := h.linkedSessions["chat1"]
	h.mu.RUnlock()
	if stillLinked {
		t.Error("chat should be unlinked after /stop")
	}
}

func TestCmd_Stop_Error(t *testing.T) {
	h, bridge := newTestHandler(t)
	h.commands.HandleCommand("chat1", "/link abcdef12")
	_ = drainQueue(h)
	bridge.stopSessionErr = errors.New("kill -9 failed")
	h.commands.HandleCommand("chat1", "/stop")
	got := lastEnqueued(h)
	if !strings.Contains(got, "Failed to stop session") {
		t.Errorf("expected stop failure message, got %q", got)
	}
	// On stop error the chat stays linked.
	h.mu.RLock()
	_, stillLinked := h.linkedSessions["chat1"]
	h.mu.RUnlock()
	if !stillLinked {
		t.Error("chat should remain linked when stop fails")
	}
}

// ---------------------------------------------------------------------------
// HandleCommand parsing edge cases
// ---------------------------------------------------------------------------

func TestHandleCommand_StartAlias(t *testing.T) {
	h, _ := newTestHandler(t)
	h.commands.HandleCommand("chat1", "/start")
	got := lastEnqueued(h)
	if !strings.Contains(got, "/help") {
		t.Errorf("/start should behave like /help, got %q", got)
	}
}

func TestHandleCommand_CaseInsensitive(t *testing.T) {
	h, _ := newTestHandler(t)
	h.commands.HandleCommand("chat1", "/STATUS")
	got := lastEnqueued(h)
	if !strings.Contains(got, "2h30m") {
		t.Errorf("uppercase command should dispatch to /status, got %q", got)
	}
}

func TestHandleCommand_UnknownEscapesHTML(t *testing.T) {
	h, _ := newTestHandler(t)
	h.commands.HandleCommand("chat1", "/<script>")
	got := lastEnqueued(h)
	if strings.Contains(got, "<script>") {
		t.Errorf("unknown command should HTML-escape the echoed command, got %q", got)
	}
	if !strings.Contains(got, "Unknown command") {
		t.Errorf("expected unknown-command prefix, got %q", got)
	}
}
