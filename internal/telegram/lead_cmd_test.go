package telegram

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// /lead_status and /lead (no args) -> cmdLeadStatus
// ---------------------------------------------------------------------------

func TestCmd_LeadStatus_NoBridge(t *testing.T) {
	h, _ := newTestHandler(t)
	// No office bridge installed.
	h.commands.HandleCommand("chat1", "/lead_status")
	got := lastEnqueued(h)
	if !strings.Contains(got, "No team is currently running") {
		t.Errorf("expected no-team message, got %q", got)
	}
}

func TestCmd_LeadStatus_TeamInactive(t *testing.T) {
	h, _ := newTestHandler(t)
	ob := newMockOfficeBridge()
	ob.teamActive = false
	h.SetOfficeBridge(ob)
	h.commands.HandleCommand("chat1", "/lead")
	got := lastEnqueued(h)
	if !strings.Contains(got, "No team is currently running") {
		t.Errorf("expected no-team message for inactive team, got %q", got)
	}
}

func TestCmd_LeadStatus_NoEligibleLead(t *testing.T) {
	h, _ := newTestHandler(t)
	ob := newMockOfficeBridge()
	ob.leadStatusOK = false
	h.SetOfficeBridge(ob)
	h.commands.HandleCommand("chat1", "/lead_status")
	got := lastEnqueued(h)
	if !strings.Contains(got, "No strategic-tier agent") {
		t.Errorf("expected no-lead message, got %q", got)
	}
}

func TestCmd_LeadStatus_Success(t *testing.T) {
	h, _ := newTestHandler(t)
	ob := newMockOfficeBridge()
	ob.leadStatus = "Captain is building the deploy pipeline."
	h.SetOfficeBridge(ob)
	h.commands.HandleCommand("chat1", "/lead")
	got := lastEnqueued(h)
	if !strings.Contains(got, "deploy pipeline") {
		t.Errorf("expected rendered lead status, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// /ask, /prompt, /lead <text> -> cmdAskLead
// ---------------------------------------------------------------------------

func TestCmd_AskLead_EmptyArgs(t *testing.T) {
	h, _ := newTestHandler(t)
	ob := newMockOfficeBridge()
	h.SetOfficeBridge(ob)
	h.commands.HandleCommand("chat1", "/ask")
	got := lastEnqueued(h)
	if !strings.Contains(got, "Usage:") {
		t.Errorf("expected usage hint for empty /ask, got %q", got)
	}
}

func TestCmd_AskLead_NoBridge(t *testing.T) {
	h, _ := newTestHandler(t)
	h.commands.HandleCommand("chat1", "/ask do the thing")
	got := lastEnqueued(h)
	if !strings.Contains(got, "No team is currently running") {
		t.Errorf("expected no-team message, got %q", got)
	}
}

func TestCmd_AskLead_NoLeadAgent(t *testing.T) {
	h, _ := newTestHandler(t)
	ob := newMockOfficeBridge()
	ob.leadSessionID = "" // no resolvable lead
	h.SetOfficeBridge(ob)
	h.commands.HandleCommand("chat1", "/ask hi")
	got := lastEnqueued(h)
	if !strings.Contains(got, "No strategic-tier agent") {
		t.Errorf("expected no-lead message, got %q", got)
	}
}

func TestCmd_AskLead_Success(t *testing.T) {
	h, bridge := newTestHandler(t)
	ob := newMockOfficeBridge()
	h.SetOfficeBridge(ob)
	h.commands.HandleCommand("chat1", "/ask ship the release")

	// The prompt should have been injected into the lead session.
	if len(bridge.sendCalls) != 1 {
		t.Fatalf("expected 1 SendToSession call, got %d", len(bridge.sendCalls))
	}
	call := bridge.sendCalls[0]
	if call.SessionID != "lead-sess-1" {
		t.Errorf("prompt routed to wrong session: %q", call.SessionID)
	}
	if !strings.Contains(call.Message, "ship the release") {
		t.Errorf("injected prompt missing operator text: %q", call.Message)
	}
	if !strings.Contains(call.Message, "Operator via Telegram") {
		t.Errorf("injected prompt missing operator prefix: %q", call.Message)
	}

	// A lead-reply expectation should now be registered.
	if h.leadReply.size() != 1 {
		t.Errorf("expected 1 lead-reply expectation, got %d", h.leadReply.size())
	}

	got := lastEnqueued(h)
	if !strings.Contains(got, "Sent to") || !strings.Contains(got, "Captain") {
		t.Errorf("expected confirmation referencing lead name, got %q", got)
	}
}

func TestCmd_AskLead_InjectionFails(t *testing.T) {
	h, bridge := newTestHandler(t)
	bridge.sendResult = false
	ob := newMockOfficeBridge()
	h.SetOfficeBridge(ob)
	h.commands.HandleCommand("chat1", "/ask do it")
	got := lastEnqueued(h)
	if !strings.Contains(got, "Failed to inject prompt") {
		t.Errorf("expected injection-failure message, got %q", got)
	}
	// No expectation should be registered when injection fails.
	if h.leadReply.size() != 0 {
		t.Errorf("expected no lead-reply expectation on inject failure, got %d", h.leadReply.size())
	}
}

func TestCmd_PromptAlias(t *testing.T) {
	h, bridge := newTestHandler(t)
	ob := newMockOfficeBridge()
	h.SetOfficeBridge(ob)
	h.commands.HandleCommand("chat1", "/prompt restart the build")
	if len(bridge.sendCalls) != 1 {
		t.Fatalf("/prompt should inject like /ask, got %d sends", len(bridge.sendCalls))
	}
}

func TestCmd_LeadWithText_RoutesToAsk(t *testing.T) {
	h, bridge := newTestHandler(t)
	ob := newMockOfficeBridge()
	h.SetOfficeBridge(ob)
	h.commands.HandleCommand("chat1", "/lead what is the status")
	if len(bridge.sendCalls) != 1 {
		t.Fatalf("/lead <text> should route to cmdAskLead, got %d sends", len(bridge.sendCalls))
	}
}

// ---------------------------------------------------------------------------
// Lead reply forwarding via the office bridge callback
// ---------------------------------------------------------------------------

func TestSetOfficeBridge_InstallsCallback(t *testing.T) {
	h, _ := newTestHandler(t)
	ob := newMockOfficeBridge()
	h.SetOfficeBridge(ob)
	if ob.getCallback() == nil {
		t.Fatal("expected SetOfficeBridge to install a lead-reply callback")
	}
}

func TestSetOfficeBridge_NilDoesNotPanic(t *testing.T) {
	h, _ := newTestHandler(t)
	ob := newMockOfficeBridge()
	h.SetOfficeBridge(ob)
	// Clearing with nil should be a no-op (no callback install attempt).
	h.SetOfficeBridge(nil)
	if h.getOfficeBridge() != nil {
		t.Error("expected office bridge to be cleared to nil")
	}
}

func TestLeadReplyForward_EndToEnd(t *testing.T) {
	h, bridge := newTestHandler(t)
	ob := newMockOfficeBridge()
	h.SetOfficeBridge(ob)

	// Operator asks the lead.
	h.commands.HandleCommand("chat1", "/ask whats up")
	_ = drainQueue(h)

	// Lead finishes a turn -> office fires the callback.
	cb := ob.getCallback()
	if cb == nil {
		t.Fatal("no callback installed")
	}
	cb("lead-sess-1", "All green, deploying now.")

	got := lastEnqueued(h)
	if !strings.Contains(got, "All green, deploying now.") {
		t.Errorf("expected forwarded lead reply, got %q", got)
	}
	if !strings.Contains(got, "Captain") {
		t.Errorf("expected lead name in forwarded reply, got %q", got)
	}
	// The forwarded message should go back to the originating chat.
	h.queue.mu.Lock()
	var routedChat string
	if len(h.queue.queue) > 0 {
		routedChat = h.queue.queue[len(h.queue.queue)-1].ChatID
	}
	h.queue.mu.Unlock()
	if routedChat != "chat1" {
		t.Errorf("forwarded reply routed to %q, want chat1", routedChat)
	}

	// Single-shot: a second callback for the same session forwards nothing.
	_ = drainQueue(h)
	cb("lead-sess-1", "second turn")
	if queueLen(h) != 0 {
		t.Error("second lead turn should not be forwarded (single-shot)")
	}

	_ = bridge
}

func TestTryForwardLeadReply_NoExpectation(t *testing.T) {
	h, _ := newTestHandler(t)
	if h.tryForwardLeadReply("unknown-sess", "hello") {
		t.Error("expected no forward when no expectation registered")
	}
}

func TestTryForwardLeadReply_EmptyText(t *testing.T) {
	h, _ := newTestHandler(t)
	h.leadReply.expect("sess-x", "chat1", "Captain")
	if h.tryForwardLeadReply("sess-x", "   ") {
		t.Error("expected no forward for whitespace-only reply")
	}
}

func TestTryForwardLeadReply_NilHandler(t *testing.T) {
	var h *Handler
	if h.tryForwardLeadReply("s", "t") {
		t.Error("nil handler should return false, not panic")
	}
}
