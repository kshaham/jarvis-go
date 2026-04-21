package telegram

// R35 · Lead-agent two-way chat state.
//
// When the operator runs `/lead <text>` or `/ask <text>` in Telegram,
// the Handler injects the prompt into the lead session and registers
// an expectation that the lead's NEXT assistant text will be
// forwarded back to the originating chat. Entries expire after
// LeadReplyTTL to prevent stale mappings from capturing unrelated
// turns.

import (
	"fmt"
	"html"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// LeadReplyTTL bounds how long a pending-reply expectation stays
// alive. 10 minutes matches the pending-leader-query TTL so the
// reverse flow has symmetric semantics.
const LeadReplyTTL = 10 * time.Minute

// LeadReplyMaxChars caps the forwarded message body. Telegram
// rejects messages over 4096 chars; we leave headroom for the
// "💬 <name>: " prefix + HTML escaping overhead.
const LeadReplyMaxChars = 3900

// leadReplyEntry records the target chat for an outgoing
// lead-reply forward.
type leadReplyEntry struct {
	ChatID    string
	LeadName  string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// leadReplyState is the handler's in-memory pending-reply map. One
// entry per lead session ID. A new /ask to the same lead overwrites
// the previous entry (the most recent requester wins).
type leadReplyState struct {
	mu      sync.Mutex
	entries map[string]*leadReplyEntry
}

func newLeadReplyState() *leadReplyState {
	return &leadReplyState{entries: make(map[string]*leadReplyEntry)}
}

// expect registers or refreshes an expectation for the given lead
// session. chatID is where the forwarded reply goes. leadName is
// the display name rendered in the forwarded message.
func (s *leadReplyState) expect(sessionID, chatID, leadName string) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[sessionID] = &leadReplyEntry{
		ChatID:    chatID,
		LeadName:  leadName,
		CreatedAt: now,
		ExpiresAt: now.Add(LeadReplyTTL),
	}
}

// takeLive returns the entry for sessionID if it exists and is not
// expired, removing it from the map. Returns nil otherwise. Single-
// shot semantics: one reply per /ask.
func (s *leadReplyState) takeLive(sessionID string) *leadReplyEntry {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[sessionID]
	if !ok {
		return nil
	}
	delete(s.entries, sessionID)
	if now.After(e.ExpiresAt) {
		return nil
	}
	return e
}

// sweepExpired drops entries past their TTL. Called from the
// interaction cleanup loop.
func (s *leadReplyState) sweepExpired() int {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	dropped := 0
	for sid, e := range s.entries {
		if now.After(e.ExpiresAt) {
			delete(s.entries, sid)
			dropped++
		}
	}
	return dropped
}

// size returns the current entry count. Diagnostic helper.
func (s *leadReplyState) size() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

// renderLeadReply builds the HTML-safe Telegram message body for a
// forwarded lead reply. Truncates at LeadReplyMaxChars with a
// "[truncated]" suffix so operators know to check the dashboard.
func renderLeadReply(leadName, text string) string {
	body := strings.TrimSpace(text)
	if body == "" {
		return ""
	}
	truncated := false
	if len(body) > LeadReplyMaxChars {
		body = body[:LeadReplyMaxChars]
		truncated = true
	}
	out := fmt.Sprintf("💬 <b>%s</b>:\n%s",
		html.EscapeString(leadName),
		html.EscapeString(body))
	if truncated {
		out += "\n\n<i>…[truncated; full reply in dashboard]</i>"
	}
	return out
}

// tryForwardLeadReply is the function wired into
// Manager.SetLeadReplyCallback. Pure orchestration: pull the entry,
// render, enqueue. Returns true if a message was enqueued.
func (h *Handler) tryForwardLeadReply(sessionID, text string) bool {
	if h == nil || h.leadReply == nil {
		return false
	}
	if strings.TrimSpace(text) == "" {
		return false
	}
	entry := h.leadReply.takeLive(sessionID)
	if entry == nil {
		return false
	}
	body := renderLeadReply(entry.LeadName, text)
	if body == "" {
		return false
	}
	if h.queue == nil {
		slog.Warn("[telegram/handler] no queue for lead reply", "sessionID", sessionID)
		return false
	}
	h.queue.Enqueue(entry.ChatID, body, nil)
	slog.Info("[telegram/handler] forwarded lead reply", "sessionID", sessionID, "chatID", entry.ChatID, "bytes", len(body))
	return true
}
