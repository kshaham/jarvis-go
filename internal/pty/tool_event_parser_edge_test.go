package pty

import (
	"encoding/json"
	"testing"
)

func TestParseContentBlocks_InvalidJSONReturnsNil(t *testing.T) {
	if got := parseContentBlocks([]byte("{not valid json")); got != nil {
		t.Errorf("parseContentBlocks on invalid JSON = %v, want nil", got)
	}
}

func TestParseContentBlocks_ValidEnvelope(t *testing.T) {
	raw := []byte(`{"type":"assistant","message":{"role":"assistant","content":[` +
		`{"type":"text","text":"hi"},` +
		`{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}}]}}`)
	blocks := parseContentBlocks(raw)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(blocks))
	}
	if blocks[1].Type != "tool_use" || blocks[1].Name != "Bash" {
		t.Errorf("second block = %+v, want a tool_use named Bash", blocks[1])
	}
}

func TestHashCanonicalJSON_EmptyReturnsEmpty(t *testing.T) {
	if got := hashCanonicalJSON(nil); got != "" {
		t.Errorf("hashCanonicalJSON(nil) = %q, want empty", got)
	}
	if got := hashCanonicalJSON(json.RawMessage("")); got != "" {
		t.Errorf("hashCanonicalJSON(empty) = %q, want empty", got)
	}
}

func TestHashToolResultContent_StringVsArraySameHash(t *testing.T) {
	// A plain string and an array of one text block carrying the same text
	// must hash identically (the CLI emits both shapes).
	strHash := hashToolResultContent(json.RawMessage(`"hello world"`))
	arrHash := hashToolResultContent(json.RawMessage(`[{"type":"text","text":"hello world"}]`))
	if strHash == "" || arrHash == "" {
		t.Fatal("hashes must be non-empty")
	}
	if strHash != arrHash {
		t.Errorf("string hash %q != array hash %q; should match for equal text", strHash, arrHash)
	}
}

func TestHashToolResultContent_EmptyReturnsEmpty(t *testing.T) {
	if got := hashToolResultContent(nil); got != "" {
		t.Errorf("hashToolResultContent(nil) = %q, want empty", got)
	}
}

func TestHashToolResultContent_FallbackForUnknownShape(t *testing.T) {
	// A JSON number is neither a string nor a text-block array, so the
	// function falls back to hashing the raw bytes. Must be non-empty.
	if got := hashToolResultContent(json.RawMessage(`12345`)); got == "" {
		t.Error("fallback hash of a numeric value should be non-empty")
	}
}

func TestCanonicalize_ScalarAndArrayPassThrough(t *testing.T) {
	// Non-object top levels are returned trimmed as-is.
	if got := canonicalize(json.RawMessage(`  "str"  `)); got != `"str"` {
		t.Errorf("canonicalize(scalar) = %q, want quoted string", got)
	}
	if got := canonicalize(json.RawMessage(`[1,2,3]`)); got != `[1,2,3]` {
		t.Errorf("canonicalize(array) = %q, want unchanged array", got)
	}
	if got := canonicalize(json.RawMessage("   ")); got != "" {
		t.Errorf("canonicalize(whitespace) = %q, want empty", got)
	}
}

func TestCanonicalize_ObjectKeysSorted(t *testing.T) {
	a := canonicalize(json.RawMessage(`{"b":2,"a":1}`))
	b := canonicalize(json.RawMessage(`{"a":1,"b":2}`))
	if a != b {
		t.Errorf("canonicalize should sort keys: %q != %q", a, b)
	}
	if a != `{"a":1,"b":2}` {
		t.Errorf("canonicalized form = %q, want sorted keys", a)
	}
}

func TestToolEventTracker_ObserveAssistant_IgnoresNonToolUse(t *testing.T) {
	tr := newToolEventTracker()
	// Only text blocks -> nothing pending.
	tr.observeAssistant([]byte(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hi"}]}}`))
	tr.mu.Lock()
	n := len(tr.pending)
	tr.mu.Unlock()
	if n != 0 {
		t.Errorf("text-only assistant should leave 0 pending, got %d", n)
	}
}

func TestToolEventTracker_ObserveUser_EmptyOnNoBlocks(t *testing.T) {
	tr := newToolEventTracker()
	if got := tr.observeUser([]byte(`{"type":"user","message":{"role":"user","content":[]}}`)); got != nil {
		t.Errorf("observeUser with no tool_result blocks = %v, want nil", got)
	}
}
