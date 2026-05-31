package telegram

import (
	"strconv"
	"strings"
	"testing"
)

// msgFromChat builds a Message originating from the given numeric chat ID.
func msgFromChat(chatID int64, text string) *Message {
	return &Message{
		MessageID: 1,
		Chat:      Chat{ID: chatID, Type: "private"},
		Text:      text,
	}
}

// ---------------------------------------------------------------------------
// handleMessage -- chat-ID auth gate
// ---------------------------------------------------------------------------

func TestHandleMessage_UnauthorizedChat(t *testing.T) {
	h, _ := newTestHandler(t) // ChatID configured as "12345"
	h.handleMessage(msgFromChat(99999, "/status"))
	got := lastEnqueued(h)
	if !strings.Contains(got, "Not authorized") {
		t.Errorf("expected unauthorized rejection, got %q", got)
	}
}

func TestHandleMessage_ChatIDNotConfigured(t *testing.T) {
	h, _ := newTestHandler(t)
	_ = h.settings.Update(func(s *Settings) { s.ChatID = "" }) // unconfigured
	h.handleMessage(msgFromChat(12345, "/status"))
	got := lastEnqueued(h)
	if !strings.Contains(got, "Not authorized") {
		t.Errorf("expected rejection when chat ID unconfigured, got %q", got)
	}
	if !strings.Contains(got, "configure a chat ID") {
		t.Errorf("expected specific unconfigured message, got %q", got)
	}
}

func TestHandleMessage_VerifyDisabledAllowsAnyChat(t *testing.T) {
	h, _ := newTestHandler(t)
	_ = h.settings.Update(func(s *Settings) { s.RequireChatIDVerify = false })
	h.handleMessage(msgFromChat(77777, "/status"))
	got := lastEnqueued(h)
	if !strings.Contains(got, "2h30m") {
		t.Errorf("expected status to dispatch when verification disabled, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// handleMessage -- routing
// ---------------------------------------------------------------------------

func TestHandleMessage_EmptyTextNoop(t *testing.T) {
	h, _ := newTestHandler(t)
	h.handleMessage(msgFromChat(12345, "   "))
	if queueLen(h) != 0 {
		t.Errorf("blank message should produce no output, got %d enqueued", queueLen(h))
	}
}

func TestHandleMessage_CommandRouted(t *testing.T) {
	h, _ := newTestHandler(t)
	h.handleMessage(msgFromChat(12345, "/sessions"))
	got := lastEnqueued(h)
	if !strings.Contains(got, "test-session") {
		t.Errorf("expected /sessions output, got %q", got)
	}
}

func TestHandleMessage_LinkedSessionForward(t *testing.T) {
	h, bridge := newTestHandler(t)
	// Link the configured chat directly.
	h.linkSession("12345", "sess-linked")
	h.handleMessage(msgFromChat(12345, "plain text input"))

	if len(bridge.sendCalls) != 1 {
		t.Fatalf("expected text forwarded to linked session, got %d sends", len(bridge.sendCalls))
	}
	if bridge.sendCalls[0].Message != "plain text input" {
		t.Errorf("forwarded message = %q", bridge.sendCalls[0].Message)
	}
	if bridge.sendCalls[0].SessionID != "sess-linked" {
		t.Errorf("forwarded to wrong session: %q", bridge.sendCalls[0].SessionID)
	}
}

func TestHandleMessage_LinkedSessionForwardFails(t *testing.T) {
	h, bridge := newTestHandler(t)
	bridge.sendResult = false
	h.linkSession("12345", "sess-linked")
	h.handleMessage(msgFromChat(12345, "input"))
	got := lastEnqueued(h)
	if !strings.Contains(got, "Failed to send message to linked session") {
		t.Errorf("expected delivery-failure message, got %q", got)
	}
}

func TestHandleMessage_LeaderQueryReply(t *testing.T) {
	h, bridge := newTestHandler(t)
	ob := newMockOfficeBridge()
	ob.hasPendingQuery = true
	ob.pendingSessionID = "leader-sess"
	h.SetOfficeBridge(ob)

	h.handleMessage(msgFromChat(12345, "here is my answer"))

	if len(bridge.sendCalls) != 1 {
		t.Fatalf("expected reply delivered to leader, got %d sends", len(bridge.sendCalls))
	}
	if bridge.sendCalls[0].SessionID != "leader-sess" {
		t.Errorf("reply routed to %q, want leader-sess", bridge.sendCalls[0].SessionID)
	}
	ob.mu.Lock()
	cleared := ob.clearPendingCalls
	ob.mu.Unlock()
	if cleared != 1 {
		t.Errorf("expected ClearPendingLeaderQuery called once, got %d", cleared)
	}
	got := lastEnqueued(h)
	if !strings.Contains(got, "Reply sent to team leader") {
		t.Errorf("expected leader-reply confirmation, got %q", got)
	}
}

func TestHandleMessage_LeaderQueryReplyFails(t *testing.T) {
	h, bridge := newTestHandler(t)
	bridge.sendResult = false
	ob := newMockOfficeBridge()
	ob.hasPendingQuery = true
	ob.pendingSessionID = "leader-sess"
	h.SetOfficeBridge(ob)

	h.handleMessage(msgFromChat(12345, "answer"))
	got := lastEnqueued(h)
	if !strings.Contains(got, "Failed to deliver reply to team leader") {
		t.Errorf("expected leader delivery-failure message, got %q", got)
	}
}

func TestHandleMessage_NoRouteHint(t *testing.T) {
	h, _ := newTestHandler(t)
	h.handleMessage(msgFromChat(12345, "just some text"))
	got := lastEnqueued(h)
	if !strings.Contains(got, "No active session linked") {
		t.Errorf("expected no-route hint, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// handleUpdate -- routing + panic recovery
// ---------------------------------------------------------------------------

func TestHandleUpdate_Message(t *testing.T) {
	h, _ := newTestHandler(t)
	h.handleUpdate(Update{UpdateID: 1, Message: msgFromChat(12345, "/status")})
	if queueLen(h) == 0 {
		t.Error("expected message update to produce output")
	}
}

func TestHandleUpdate_EmptyUpdateNoop(t *testing.T) {
	h, _ := newTestHandler(t)
	// Neither Message nor CallbackQuery set -- should be a clean no-op.
	h.handleUpdate(Update{UpdateID: 5})
	if queueLen(h) != 0 {
		t.Errorf("empty update should produce no output, got %d", queueLen(h))
	}
}

// ---------------------------------------------------------------------------
// handleCallbackQuery -- auth + all actions + missing/expired
// ---------------------------------------------------------------------------

// registerPending seeds a pending interaction for the given shortID.
func registerPending(h *Handler, shortID, sessionID, typ string) {
	h.pendingMu.Lock()
	h.pendingInteractions[shortID] = &PendingInteraction{
		SessionID: sessionID,
		ShortID:   shortID,
		Type:      typ,
	}
	h.pendingMu.Unlock()
}

func callbackFromChat(chatID int64, data string) *CallbackQuery {
	return &CallbackQuery{
		ID:      "cbq-1",
		Data:    data,
		Message: msgFromChat(chatID, ""),
	}
}

func TestHandleCallback_Unauthorized(t *testing.T) {
	h, bridge := newTestHandler(t)
	registerPending(h, "abc12345", "sess-1", InteractionTypePermission)
	h.handleCallbackQuery(callbackFromChat(99999, ActionPermissionApprove+":abc12345"))
	if len(bridge.sendCalls) != 0 {
		t.Errorf("unauthorized callback should not act on session, got %d sends", len(bridge.sendCalls))
	}
	// Interaction must remain (not consumed) since auth failed first.
	h.pendingMu.Lock()
	_, exists := h.pendingInteractions["abc12345"]
	h.pendingMu.Unlock()
	if !exists {
		t.Error("interaction should survive an unauthorized callback")
	}
}

func TestHandleCallback_PermissionApprove(t *testing.T) {
	h, bridge := newTestHandler(t)
	registerPending(h, "abc12345", "sess-app", InteractionTypePermission)
	h.handleCallbackQuery(callbackFromChat(12345, ActionPermissionApprove+":abc12345"))
	if len(bridge.sendCalls) != 1 || bridge.sendCalls[0].Message != "yes" {
		t.Fatalf("expected 'yes' sent to session, got %+v", bridge.sendCalls)
	}
	if bridge.sendCalls[0].SessionID != "sess-app" {
		t.Errorf("approve routed to %q", bridge.sendCalls[0].SessionID)
	}
	// Interaction consumed.
	h.pendingMu.Lock()
	_, exists := h.pendingInteractions["abc12345"]
	h.pendingMu.Unlock()
	if exists {
		t.Error("interaction should be consumed after handling")
	}
}

func TestHandleCallback_PermissionDeny(t *testing.T) {
	h, bridge := newTestHandler(t)
	registerPending(h, "deny0001", "sess-d", InteractionTypePermission)
	h.handleCallbackQuery(callbackFromChat(12345, ActionPermissionDeny+":deny0001"))
	if len(bridge.sendCalls) != 1 || bridge.sendCalls[0].Message != "no" {
		t.Fatalf("expected 'no' sent, got %+v", bridge.sendCalls)
	}
}

func TestHandleCallback_PermissionTrust(t *testing.T) {
	h, bridge := newTestHandler(t)
	registerPending(h, "trust001", "sess-t", InteractionTypePermission)
	h.handleCallbackQuery(callbackFromChat(12345, ActionPermissionTrust+":trust001"))
	if len(bridge.sendCalls) != 1 || bridge.sendCalls[0].Message != "trust" {
		t.Fatalf("expected 'trust' sent, got %+v", bridge.sendCalls)
	}
}

func TestHandleCallback_PlanApprove(t *testing.T) {
	h, bridge := newTestHandler(t)
	registerPending(h, "plan0001", "orch-1", InteractionTypePlan)
	h.handleCallbackQuery(callbackFromChat(12345, ActionPlanApprove+":plan0001"))
	if len(bridge.sendCalls) != 1 || bridge.sendCalls[0].Message != "yes" {
		t.Fatalf("expected 'yes' for plan approve, got %+v", bridge.sendCalls)
	}
}

func TestHandleCallback_PlanDecline(t *testing.T) {
	h, bridge := newTestHandler(t)
	registerPending(h, "plan0002", "orch-2", InteractionTypePlan)
	h.handleCallbackQuery(callbackFromChat(12345, ActionPlanDecline+":plan0002"))
	if len(bridge.sendCalls) != 1 || bridge.sendCalls[0].Message != "no" {
		t.Fatalf("expected 'no' for plan decline, got %+v", bridge.sendCalls)
	}
}

func TestHandleCallback_ExpiredInteraction(t *testing.T) {
	h, bridge := newTestHandler(t)
	// No pending interaction registered.
	h.handleCallbackQuery(callbackFromChat(12345, ActionPermissionApprove+":missing0"))
	if len(bridge.sendCalls) != 0 {
		t.Errorf("expired/missing interaction should not act, got %d sends", len(bridge.sendCalls))
	}
}

func TestHandleCallback_InvalidData(t *testing.T) {
	h, bridge := newTestHandler(t)
	// Empty data -> action == "" path.
	h.handleCallbackQuery(callbackFromChat(12345, ""))
	if len(bridge.sendCalls) != 0 {
		t.Errorf("invalid callback data should not act, got %d sends", len(bridge.sendCalls))
	}
}

func TestHandleCallback_MissingSessionParam(t *testing.T) {
	h, bridge := newTestHandler(t)
	// Action with no params.
	h.handleCallbackQuery(callbackFromChat(12345, ActionPermissionApprove))
	if len(bridge.sendCalls) != 0 {
		t.Errorf("callback missing session id should not act, got %d sends", len(bridge.sendCalls))
	}
}

func TestHandleCallback_VerifyDisabled(t *testing.T) {
	h, bridge := newTestHandler(t)
	_ = h.settings.Update(func(s *Settings) { s.RequireChatIDVerify = false })
	registerPending(h, "anychat0", "sess-x", InteractionTypePermission)
	// Callback from an arbitrary chat should be honored when verify is off.
	h.handleCallbackQuery(callbackFromChat(55555, ActionPermissionApprove+":anychat0"))
	if len(bridge.sendCalls) != 1 {
		t.Fatalf("expected callback honored with verify disabled, got %d sends", len(bridge.sendCalls))
	}
}

// ---------------------------------------------------------------------------
// getOfficeBridge / StandupCompleteEnabled / Settings accessors
// ---------------------------------------------------------------------------

func TestGetOfficeBridge_NilByDefault(t *testing.T) {
	h, _ := newTestHandler(t)
	if h.getOfficeBridge() != nil {
		t.Error("expected nil office bridge before SetOfficeBridge")
	}
}

func TestStandupCompleteEnabled(t *testing.T) {
	h, _ := newTestHandler(t)
	if !h.StandupCompleteEnabled() {
		t.Error("StandupComplete defaults to true")
	}
	_ = h.settings.Update(func(s *Settings) { s.Notifications.StandupComplete = false })
	if h.StandupCompleteEnabled() {
		t.Error("expected StandupComplete disabled after update")
	}
}

func TestHandler_SettingsAccessor(t *testing.T) {
	h, _ := newTestHandler(t)
	if h.Settings() == nil {
		t.Error("Settings() returned nil")
	}
	if h.Settings().Get().ChatID != "12345" {
		t.Errorf("unexpected chat ID via accessor: %q", h.Settings().Get().ChatID)
	}
}

// ---------------------------------------------------------------------------
// linkSession / unlinkSession lifecycle
// ---------------------------------------------------------------------------

func TestLinkSession_ReplacesExisting(t *testing.T) {
	h, bridge := newTestHandler(t)
	h.linkSession("chat-a", "sess-1")
	h.linkSession("chat-a", "sess-2") // relink should unsubscribe old, subscribe new
	h.mu.RLock()
	linked := h.linkedSessions["chat-a"]
	h.mu.RUnlock()
	if linked != "sess-2" {
		t.Errorf("expected relink to sess-2, got %q", linked)
	}
	// Two subscribe calls total (one per link).
	if len(bridge.subscribeCalls) != 2 {
		t.Errorf("expected 2 subscribe calls across relink, got %d", len(bridge.subscribeCalls))
	}
}

func TestUnlinkSession_Idempotent(t *testing.T) {
	h, _ := newTestHandler(t)
	// Unlinking a chat that was never linked must not panic.
	h.unlinkSession("never-linked")
	h.mu.RLock()
	_, exists := h.linkedSessions["never-linked"]
	h.mu.RUnlock()
	if exists {
		t.Error("unlink of non-linked chat should leave no entry")
	}
}

// ---------------------------------------------------------------------------
// Concurrency: handleMessage + drain under -race
// ---------------------------------------------------------------------------

func TestHandleMessage_ConcurrentDispatch(t *testing.T) {
	h, _ := newTestHandler(t)
	const n = 50
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < n; i++ {
			h.handleMessage(msgFromChat(12345, "/status"))
		}
	}()
	for i := 0; i < n; i++ {
		h.handleMessage(msgFromChat(12345, "/sessions"))
		_ = strconv.Itoa(i)
	}
	<-done
	// We dispatched 2n commands; the queue should hold messages without races.
	if queueLen(h) == 0 {
		t.Error("expected enqueued messages after concurrent dispatch")
	}
}
