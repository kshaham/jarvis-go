package telegram

import (
	"strings"
	"testing"
	"time"
)

func TestLeadReplyState_ExpectAndTakeLive(t *testing.T) {
	s := newLeadReplyState()
	s.expect("sess-1", "chat-42", "Captain")
	e := s.takeLive("sess-1")
	if e == nil {
		t.Fatal("expected entry, got nil")
	}
	if e.ChatID != "chat-42" || e.LeadName != "Captain" {
		t.Errorf("entry mismatch: %+v", e)
	}
	// Single-shot semantics: second take returns nil.
	if s.takeLive("sess-1") != nil {
		t.Error("takeLive should consume the entry")
	}
}

func TestLeadReplyState_TakeLiveExpired(t *testing.T) {
	s := newLeadReplyState()
	s.expect("sess-1", "chat-42", "Captain")
	// Force-age the entry by mutating ExpiresAt.
	s.mu.Lock()
	s.entries["sess-1"].ExpiresAt = time.Now().Add(-time.Minute)
	s.mu.Unlock()
	if e := s.takeLive("sess-1"); e != nil {
		t.Error("expired entry should return nil")
	}
}

func TestLeadReplyState_SweepExpired(t *testing.T) {
	s := newLeadReplyState()
	s.expect("sess-1", "chat-1", "A")
	s.expect("sess-2", "chat-2", "B")
	s.mu.Lock()
	s.entries["sess-1"].ExpiresAt = time.Now().Add(-time.Minute)
	s.mu.Unlock()
	dropped := s.sweepExpired()
	if dropped != 1 {
		t.Errorf("expected 1 dropped, got %d", dropped)
	}
	if s.size() != 1 {
		t.Errorf("expected 1 surviving entry, got %d", s.size())
	}
	if e := s.takeLive("sess-2"); e == nil {
		t.Error("sess-2 should still be live after sweep")
	}
}

func TestLeadReplyState_OverwriteOnReExpect(t *testing.T) {
	s := newLeadReplyState()
	s.expect("sess-1", "chat-original", "A")
	s.expect("sess-1", "chat-new", "B")
	e := s.takeLive("sess-1")
	if e == nil {
		t.Fatal("expected entry")
	}
	if e.ChatID != "chat-new" || e.LeadName != "B" {
		t.Errorf("expected overwrite with chat-new/B, got %+v", e)
	}
}

func TestRenderLeadReply_HappyPath(t *testing.T) {
	got := renderLeadReply("Captain", "I shipped the auth middleware.")
	if !strings.Contains(got, "Captain") || !strings.Contains(got, "shipped the auth") {
		t.Errorf("rendered body missing expected parts: %s", got)
	}
	if !strings.HasPrefix(got, "💬") {
		t.Errorf("rendered body should start with 💬 emoji; got: %s", got)
	}
}

func TestRenderLeadReply_EscapesHTML(t *testing.T) {
	got := renderLeadReply("Captain", "run <script>alert(1)</script>")
	if strings.Contains(got, "<script>") {
		t.Errorf("HTML not escaped: %s", got)
	}
}

func TestRenderLeadReply_TruncatesOversize(t *testing.T) {
	big := strings.Repeat("x", LeadReplyMaxChars+500)
	got := renderLeadReply("Captain", big)
	if !strings.Contains(got, "truncated") {
		t.Errorf("oversize body should get truncated marker; got len=%d", len(got))
	}
	if len(got) > LeadReplyMaxChars+200 { // +200 for the prefix + truncation suffix
		t.Errorf("rendered body still too long after truncation: %d", len(got))
	}
}

func TestRenderLeadReply_EmptyText(t *testing.T) {
	if got := renderLeadReply("Captain", "   "); got != "" {
		t.Errorf("whitespace-only text should render empty, got: %q", got)
	}
}
