package pty

import "testing"

// Codex and SDK sessions accumulate token usage in totalInputTokens /
// totalOutputTokens just like a PTY Session, but lacked the Tokens()
// accessor the token-stats handler uses — so their usage was invisible
// in /api/token-stats. These guard the accessors that close that gap.

func TestCodexSessionTokens(t *testing.T) {
	s := &CodexSession{totalInputTokens: 100, totalOutputTokens: 40}
	in, out, total := s.Tokens()
	if in != 100 || out != 40 || total != 140 {
		t.Fatalf("CodexSession.Tokens() = (%d,%d,%d), want (100,40,140)", in, out, total)
	}
}

func TestSDKSessionTokens(t *testing.T) {
	s := &SDKSession{totalInputTokens: 7, totalOutputTokens: 3}
	in, out, total := s.Tokens()
	if in != 7 || out != 3 || total != 10 {
		t.Fatalf("SDKSession.Tokens() = (%d,%d,%d), want (7,3,10)", in, out, total)
	}
}
