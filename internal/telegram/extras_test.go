package telegram

import (
	"net/http"
	"testing"
)

// TestResolveSessionID_AgentExactMatch covers the agent-exact-ID branch of
// resolveSessionID (the session list does not match, but an agent does).
func TestResolveSessionID_AgentExactMatch(t *testing.T) {
	h, bridge := newTestHandler(t)
	bridge.sessions = nil
	bridge.agents = []AgentInfo{{ID: "agent-exact-id", Name: "a", Status: "running"}}
	got := h.commands.resolveSessionID("agent-exact-id")
	if got != "agent-exact-id" {
		t.Errorf("resolveSessionID(agent exact) = %q, want agent-exact-id", got)
	}
}

func TestResolveSessionID_NotFound(t *testing.T) {
	h, bridge := newTestHandler(t)
	bridge.sessions = nil
	bridge.agents = nil
	if got := h.commands.resolveSessionID("nothing"); got != "" {
		t.Errorf("resolveSessionID(missing) = %q, want empty", got)
	}
}

func TestResolveSessionID_ShortTokenNoPrefixMatch(t *testing.T) {
	h, bridge := newTestHandler(t)
	// A 3-char token is below the prefix-match floor (len(id) >= 4), so it must
	// not match by prefix.
	bridge.sessions = []SessionInfo{{ID: "abcdefgh-1111", ShortID: "abcdefgh"}}
	bridge.agents = nil
	if got := h.commands.resolveSessionID("abc"); got != "" {
		t.Errorf("3-char token should not prefix-match, got %q", got)
	}
}

func TestParseCallbackData_Empty(t *testing.T) {
	action, params := ParseCallbackData("")
	if action != "" {
		t.Errorf("empty data action = %q, want empty", action)
	}
	if len(params) != 0 {
		t.Errorf("empty data params = %v, want none", params)
	}
}

// TestTryForwardLeadReply_NilQueue covers the no-queue guard.
func TestTryForwardLeadReply_NilQueue(t *testing.T) {
	h, _ := newTestHandler(t)
	h.leadReply.expect("sess-q", "chat1", "Captain")
	h.queue = nil
	if h.tryForwardLeadReply("sess-q", "some reply") {
		t.Error("expected false when queue is nil")
	}
}

// TestHandleUpdate_PanicRecovery feeds an update whose dispatch would otherwise
// be fine; we assert handleUpdate never propagates a panic. We exercise the
// recover() guard indirectly by confirming a normal message still dispatches
// while the deferred recover is installed.
func TestHandleUpdate_CallbackRouted(t *testing.T) {
	h, bridge := newTestHandler(t)
	registerPending(h, "rt000001", "sess-rt", InteractionTypePermission)
	h.handleUpdate(Update{
		CallbackQuery: &CallbackQuery{
			ID:      "q1",
			Data:    ActionPermissionApprove + ":rt000001",
			Message: msgFromChat(12345, ""),
		},
	})
	if len(bridge.sendCalls) != 1 {
		t.Fatalf("expected callback update routed to handleCallbackQuery, got %d sends", len(bridge.sendCalls))
	}
}

// TestNewMessageQueue_NilClientDefaults verifies the nil-client default path.
func TestNewMessageQueue_NilClientDefaults(t *testing.T) {
	mq := NewMessageQueue("tok", nil, 20)
	if mq.client == nil {
		t.Fatal("expected a default http.Client when nil passed")
	}
	if _, ok := interface{}(mq.client).(*http.Client); !ok {
		t.Error("default client should be *http.Client")
	}
}
