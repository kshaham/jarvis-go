package pty

import (
	"encoding/json"
	"testing"
)

// TestToolEventTracker_ObserveAssistant_EmptyBlocksEarlyReturn covers the
// len(blocks)==0 early return in observeAssistant (tool_event_parser.go:74-76):
// an envelope whose content array is empty yields zero blocks.
func TestToolEventTracker_ObserveAssistant_EmptyBlocksEarlyReturn(t *testing.T) {
	tr := newToolEventTracker()
	tr.observeAssistant([]byte(`{"type":"assistant","message":{"role":"assistant","content":[]}}`))
	tr.mu.Lock()
	n := len(tr.pending)
	tr.mu.Unlock()
	if n != 0 {
		t.Errorf("empty-content assistant should leave 0 pending, got %d", n)
	}

	// An unparseable envelope also yields zero blocks via the early return.
	tr.observeAssistant([]byte(`{not valid json`))
	tr.mu.Lock()
	n = len(tr.pending)
	tr.mu.Unlock()
	if n != 0 {
		t.Errorf("invalid assistant envelope should leave 0 pending, got %d", n)
	}
}

// TestToolEventTracker_ObserveUser_SkipsNonToolResultBlock covers the loop
// `continue` for a content block that is present but not a tool_result
// (tool_event_parser.go:103-104). A user message carrying only a text block
// runs the loop body and skips it, returning nil.
func TestToolEventTracker_ObserveUser_SkipsNonToolResultBlock(t *testing.T) {
	tr := newToolEventTracker()
	// One text block (not tool_result) -> the loop body runs and `continue`s.
	got := tr.observeUser([]byte(`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"just text"}]}}`))
	if got != nil {
		t.Errorf("observeUser with only a non-tool_result block = %v, want nil", got)
	}
}

// TestToolEventTracker_ObserveUser_ToolResultNoPendingMatch covers the
// `!ok` continue: a tool_result whose tool_use_id has no pending entry is
// skipped (tool_event_parser.go:106-109 region).
func TestToolEventTracker_ObserveUser_ToolResultNoPendingMatch(t *testing.T) {
	tr := newToolEventTracker()
	got := tr.observeUser([]byte(`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"never-seen","content":"ok"}]}}`))
	if got != nil {
		t.Errorf("a tool_result with no matching pending tool_use = %v, want nil", got)
	}
}

// TestCanonicalize_MalformedObjectFallsBackToTrimmed covers the
// json.Unmarshal-error branch in canonicalize (tool_event_parser.go:223-225):
// a value that starts with '{' but is not valid JSON returns the trimmed
// string verbatim instead of panicking.
func TestCanonicalize_MalformedObjectFallsBackToTrimmed(t *testing.T) {
	in := json.RawMessage(`  {"a": }  `) // starts with { but invalid JSON
	got := canonicalize(in)
	if got != `{"a": }` {
		t.Errorf("canonicalize(malformed object) = %q, want the trimmed raw string", got)
	}
}
