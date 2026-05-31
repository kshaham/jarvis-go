package telegram

import (
	"strings"
	"testing"
	"time"
)

// startedHandler returns a handler whose started flag is forced true WITHOUT
// launching the polling/queue goroutines, so Notify* methods (gated on
// started) run and enqueue into the inspectable queue slice.
func startedHandler(t *testing.T) (*Handler, *mockServerBridge) {
	t.Helper()
	h, bridge := newTestHandler(t)
	h.started.Store(true)
	return h, bridge
}

func TestNotify_GatedOnStarted(t *testing.T) {
	h, _ := newTestHandler(t) // not started
	h.NotifySessionStart("s", "name", "/dir")
	if queueLen(h) != 0 {
		t.Errorf("Notify before Start should be a no-op, got %d enqueued", queueLen(h))
	}
}

func TestNotifySessionStart(t *testing.T) {
	h, _ := startedHandler(t)
	h.NotifySessionStart("s", "my-sess", "/work/dir")
	got := lastEnqueued(h)
	if !strings.Contains(got, "my-sess") || !strings.Contains(got, "/work/dir") {
		t.Errorf("session start notification missing fields, got %q", got)
	}
}

func TestNotifySessionStart_DisabledPref(t *testing.T) {
	h, _ := startedHandler(t)
	_ = h.settings.Update(func(s *Settings) { s.Notifications.SessionStart = false })
	h.NotifySessionStart("s", "name", "/dir")
	if queueLen(h) != 0 {
		t.Errorf("disabled SessionStart should not enqueue, got %d", queueLen(h))
	}
}

func TestNotifySessionComplete(t *testing.T) {
	h, _ := startedHandler(t)
	h.NotifySessionComplete("s", "done-sess", 90*time.Second)
	got := lastEnqueued(h)
	if !strings.Contains(got, "done-sess") || !strings.Contains(got, "1m30s") {
		t.Errorf("session complete notification missing fields, got %q", got)
	}
}

func TestNotifySessionComplete_Disabled(t *testing.T) {
	h, _ := startedHandler(t)
	_ = h.settings.Update(func(s *Settings) { s.Notifications.SessionComplete = false })
	h.NotifySessionComplete("s", "x", time.Second)
	if queueLen(h) != 0 {
		t.Error("disabled SessionComplete should not enqueue")
	}
}

func TestNotifySessionError(t *testing.T) {
	h, _ := startedHandler(t)
	h.NotifySessionError("s", "err-sess", "boom")
	got := lastEnqueued(h)
	if !strings.Contains(got, "err-sess") || !strings.Contains(got, "boom") {
		t.Errorf("session error notification missing fields, got %q", got)
	}
}

func TestNotifyPermissionPrompt_RegistersInteraction(t *testing.T) {
	h, _ := startedHandler(t)
	sessionID := "abcdef12-3456-7890-abcd-ef1234567890"
	h.NotifyPermissionPrompt(sessionID, map[string]interface{}{"tool": "Bash"})

	// A pending interaction keyed by the short ID should exist.
	short := ShortID(sessionID)
	h.pendingMu.Lock()
	pi, exists := h.pendingInteractions[short]
	h.pendingMu.Unlock()
	if !exists {
		t.Fatal("expected pending interaction registered for permission prompt")
	}
	if pi.Type != InteractionTypePermission {
		t.Errorf("interaction type = %q, want permission", pi.Type)
	}
	if pi.ToolName != "Bash" {
		t.Errorf("tool name = %q, want Bash", pi.ToolName)
	}
	// The enqueued message should carry the inline keyboard.
	h.queue.mu.Lock()
	var kb *InlineKeyboardMarkup
	if len(h.queue.queue) > 0 {
		kb = h.queue.queue[len(h.queue.queue)-1].Keyboard
	}
	h.queue.mu.Unlock()
	if kb == nil {
		t.Error("permission prompt should enqueue a keyboard")
	}
}

func TestNotifyPermissionPrompt_Disabled(t *testing.T) {
	h, _ := startedHandler(t)
	_ = h.settings.Update(func(s *Settings) { s.Notifications.PermissionPrompt = false })
	h.NotifyPermissionPrompt("sess", map[string]interface{}{"tool": "Bash"})
	if queueLen(h) != 0 {
		t.Error("disabled PermissionPrompt should not enqueue")
	}
}

func TestNotifyElicitation(t *testing.T) {
	h, _ := startedHandler(t)
	h.NotifyElicitation("abcdef12-x", map[string]interface{}{"question": "Pick a name?"})
	got := lastEnqueued(h)
	if !strings.Contains(got, "Pick a name?") {
		t.Errorf("elicitation notification missing question, got %q", got)
	}
}

func TestNotifyStuckState(t *testing.T) {
	h, _ := startedHandler(t)
	h.NotifyStuckState("abcdef12-x", "thinking", 120000)
	got := lastEnqueued(h)
	if !strings.Contains(got, "thinking") {
		t.Errorf("stuck-state notification missing state, got %q", got)
	}
}

func TestNotifyRespawnBlocked(t *testing.T) {
	h, _ := startedHandler(t)
	h.NotifyRespawnBlocked("abcdef12-x", "rate limit", "wait 5m")
	got := lastEnqueued(h)
	if !strings.Contains(got, "rate limit") {
		t.Errorf("respawn-blocked notification missing reason, got %q", got)
	}
}

func TestNotifyPlanGenerated_RegistersInteraction(t *testing.T) {
	h, _ := startedHandler(t)
	orchID := "abcdef12-3456-7890-abcd-ef1234567890"
	h.NotifyPlanGenerated(orchID, "ship feature", 5)

	short := ShortID(orchID)
	h.pendingMu.Lock()
	pi, exists := h.pendingInteractions[short]
	h.pendingMu.Unlock()
	if !exists {
		t.Fatal("expected pending interaction for plan")
	}
	if pi.Type != InteractionTypePlan {
		t.Errorf("interaction type = %q, want plan", pi.Type)
	}
	got := lastEnqueued(h)
	if !strings.Contains(got, "ship feature") {
		t.Errorf("plan notification missing task, got %q", got)
	}
}

func TestNotifyPlanGenerated_Disabled(t *testing.T) {
	h, _ := startedHandler(t)
	_ = h.settings.Update(func(s *Settings) { s.Notifications.PlanApproval = false })
	h.NotifyPlanGenerated("orch", "task", 3)
	if queueLen(h) != 0 {
		t.Error("disabled PlanApproval should not enqueue")
	}
}

func TestSendFormattedMessage(t *testing.T) {
	h, _ := startedHandler(t)
	h.SendFormattedMessage("chat-xyz", "hello office", nil)
	h.queue.mu.Lock()
	defer h.queue.mu.Unlock()
	if len(h.queue.queue) != 1 {
		t.Fatalf("expected 1 enqueued message, got %d", len(h.queue.queue))
	}
	if h.queue.queue[0].ChatID != "chat-xyz" || h.queue.queue[0].Text != "hello office" {
		t.Errorf("unexpected enqueued message: %+v", h.queue.queue[0])
	}
}

func TestSendFormattedMessage_GatedOnStarted(t *testing.T) {
	h, _ := newTestHandler(t) // not started
	h.SendFormattedMessage("c", "text", nil)
	if queueLen(h) != 0 {
		t.Error("SendFormattedMessage before Start should be a no-op")
	}
}
