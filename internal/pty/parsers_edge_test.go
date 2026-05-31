package pty

import (
	"testing"
	"time"
)

// TestExtractJSONObject_NonBraceStart covers the early-return when the input
// does not start with '{'.
func TestExtractJSONObject_NonBraceStart(t *testing.T) {
	if got := extractJSONObject("not an object"); got != "" {
		t.Errorf("extractJSONObject(non-brace) = %q, want empty", got)
	}
	if got := extractJSONObject(""); got != "" {
		t.Errorf("extractJSONObject(empty) = %q, want empty", got)
	}
}

// TestExtractJSONObject_EscapedQuoteInString covers the escape-handling branch
// (a backslash-escaped quote inside a string must not close the string).
func TestExtractJSONObject_EscapedQuoteInString(t *testing.T) {
	in := `{"type":"result","content":"he said \"hi\" and {left}"}trailing`
	want := `{"type":"result","content":"he said \"hi\" and {left}"}`
	if got := extractJSONObject(in); got != want {
		t.Errorf("extractJSONObject with escaped quotes = %q, want %q", got, want)
	}
}

// TestExtractJSONObject_EscapedBackslashThenBrace ensures a literal escaped
// backslash inside a string is handled (escaped flag reset correctly).
func TestExtractJSONObject_EscapedBackslash(t *testing.T) {
	in := `{"path":"C:\\dir"}rest`
	want := `{"path":"C:\\dir"}`
	if got := extractJSONObject(in); got != want {
		t.Errorf("extractJSONObject with escaped backslash = %q, want %q", got, want)
	}
}

// TestParseClaudeMessage_MalformedJSONReturnsNil covers the json.Unmarshal
// error branch: a line that starts with the magic prefix but is not valid JSON.
func TestParseClaudeMessage_MalformedAfterPrefix(t *testing.T) {
	// Starts with {"type":" but the object never closes properly after
	// extractJSONObject — actually extractJSONObject returns "" for an
	// incomplete object, hitting the jsonStr=="" branch.
	if msg := ParseClaudeMessage(`{"type":"system"`); msg != nil {
		t.Errorf("incomplete JSON after prefix should yield nil, got %+v", msg)
	}
	// A well-formed object whose unmarshal fails because of a type mismatch
	// (input_tokens as a string) exercises the Unmarshal-error branch.
	if msg := ParseClaudeMessage(`{"type":"result","input_tokens":"not-a-number"}`); msg != nil {
		t.Errorf("type-mismatched JSON should yield nil, got %+v", msg)
	}
}

// TestToolEventTracker_ObserveAssistant_EmptyIDSkipped covers the branch where
// a tool_use block has an empty id and is therefore skipped.
func TestToolEventTracker_ObserveAssistant_EmptyIDSkipped(t *testing.T) {
	tr := newToolEventTracker()
	tr.observeAssistant([]byte(`{"type":"assistant","message":{"role":"assistant","content":[` +
		`{"type":"tool_use","id":"","name":"Bash","input":{}}]}}`))
	tr.mu.Lock()
	n := len(tr.pending)
	tr.mu.Unlock()
	if n != 0 {
		t.Errorf("a tool_use with an empty id must be skipped; pending=%d, want 0", n)
	}
}

// TestCodexSession_SendPrompt_QueueFullDrops fills the prompt queue to capacity
// and asserts the next SendPrompt is dropped (the non-blocking default branch).
func TestCodexSession_SendPrompt_QueueFullDrops(t *testing.T) {
	s := NewCodexSession(SessionConfig{})
	// Fill the bounded prompts channel (codexPromptQueue slots).
	for i := 0; i < codexPromptQueue; i++ {
		s.SendPrompt(nil, "fill")
	}
	// Queue is full; this send must be dropped without blocking.
	done := make(chan struct{})
	go func() {
		s.SendPrompt(nil, "overflow")
		close(done)
	}()
	select {
	case <-done:
		// good: non-blocking drop returned promptly
	case <-time.After(2 * time.Second):
		t.Fatal("SendPrompt blocked on a full queue; should drop non-blockingly")
	}
	// Drain one and confirm the queue still only held the original fills (the
	// overflow was dropped, not enqueued at the back).
	got := 0
	for {
		select {
		case <-s.prompts:
			got++
		default:
			if got != codexPromptQueue {
				t.Errorf("queue held %d prompts, want %d (overflow dropped)", got, codexPromptQueue)
			}
			return
		}
	}
}
