package pty

// R21 · Tool-event parser tests. Validates that:
//   - tool_use content blocks in assistant messages are captured
//   - tool_result content blocks in user messages are matched by ID
//   - canonical input hashing is stable under key reordering
//   - string / array tool_result content both hash correctly
//   - unmatched tool_result blocks are silently skipped

import (
	"testing"
)

const assistantWithToolUse = `{
  "type": "assistant",
  "message": {
    "role": "assistant",
    "content": [
      {"type": "text", "text": "Let me check the file."},
      {"type": "tool_use", "id": "tu-1", "name": "Read", "input": {"path": "/etc/hosts"}}
    ]
  }
}`

const userWithToolResult = `{
  "type": "user",
  "message": {
    "role": "user",
    "content": [
      {"type": "tool_result", "tool_use_id": "tu-1", "content": "127.0.0.1 localhost\n"}
    ]
  }
}`

const userWithArrayToolResult = `{
  "type": "user",
  "message": {
    "role": "user",
    "content": [
      {"type": "tool_result", "tool_use_id": "tu-2", "content": [
        {"type": "text", "text": "hello "},
        {"type": "text", "text": "world"}
      ]}
    ]
  }
}`

func TestToolEventTracker_PairsToolUseAndResult(t *testing.T) {
	tr := newToolEventTracker()
	tr.observeAssistant([]byte(assistantWithToolUse))

	emits := tr.observeUser([]byte(userWithToolResult))
	if len(emits) != 1 {
		t.Fatalf("expected 1 emit, got %d", len(emits))
	}
	e := emits[0]
	if e.Name != "Read" {
		t.Fatalf("expected Name=Read, got %q", e.Name)
	}
	if e.ToolUseID != "tu-1" {
		t.Fatalf("expected ToolUseID=tu-1, got %q", e.ToolUseID)
	}
	if e.InputHash == "" {
		t.Fatal("expected non-empty InputHash")
	}
	if e.ResultHash == "" {
		t.Fatal("expected non-empty ResultHash")
	}
}

func TestToolEventTracker_UnmatchedResultSkipped(t *testing.T) {
	tr := newToolEventTracker()
	// No assistant observed — the user message has nothing to pair.
	emits := tr.observeUser([]byte(userWithToolResult))
	if len(emits) != 0 {
		t.Fatalf("expected 0 emits for unmatched result, got %d", len(emits))
	}
}

func TestToolEventTracker_ArrayContentHashesSameAsString(t *testing.T) {
	tr1 := newToolEventTracker()
	tr1.observeAssistant([]byte(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"tu-2","name":"Bash","input":{"cmd":"echo hi"}}]}}`))
	arrayEmits := tr1.observeUser([]byte(userWithArrayToolResult))
	if len(arrayEmits) != 1 {
		t.Fatalf("expected 1 emit, got %d", len(arrayEmits))
	}

	tr2 := newToolEventTracker()
	tr2.observeAssistant([]byte(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"tu-3","name":"Bash","input":{"cmd":"echo hi"}}]}}`))
	stringEmits := tr2.observeUser([]byte(`{
  "type": "user",
  "message": {"role": "user", "content": [
    {"type": "tool_result", "tool_use_id": "tu-3", "content": "hello world"}
  ]}
}`))
	if len(stringEmits) != 1 {
		t.Fatalf("expected 1 emit, got %d", len(stringEmits))
	}

	if arrayEmits[0].ResultHash != stringEmits[0].ResultHash {
		t.Fatalf("expected array-flatten and direct string to hash identically; got %q vs %q",
			arrayEmits[0].ResultHash, stringEmits[0].ResultHash)
	}
}

func TestHashCanonicalJSON_StableUnderKeyReordering(t *testing.T) {
	a := hashCanonicalJSON([]byte(`{"a":1,"b":2,"c":3}`))
	b := hashCanonicalJSON([]byte(`{"c":3,"a":1,"b":2}`))
	if a == "" || b == "" {
		t.Fatal("expected non-empty hashes")
	}
	if a != b {
		t.Fatalf("expected stable hash across key order; got %q vs %q", a, b)
	}
}

func TestLooksLikeUserMessage_ChecksTopLevelType(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"user envelope", `{"type":"user","message":{"role":"user"}}`, true},
		{"assistant envelope", `{"type":"assistant","message":{"role":"assistant"}}`, false},
		{"system envelope", `{"type":"system"}`, false},
		// R21 code-review regression guard: an assistant message whose
		// text content contains the literal substring `"role":"user"`
		// (agent discussing a JSON schema, say) must NOT be treated
		// as user-shaped. The previous string-contains check falsely
		// matched here.
		{"assistant text with role user substring",
			`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"the payload {\"role\":\"user\"} was sent"}]}}`,
			false},
		{"malformed json", `{{not json}}`, false},
	}
	for _, tc := range cases {
		got := looksLikeUserMessage([]byte(tc.in))
		if got != tc.want {
			t.Errorf("%s: looksLikeUserMessage = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestToolEventTracker_ResetClearsPending(t *testing.T) {
	tr := newToolEventTracker()
	tr.observeAssistant([]byte(assistantWithToolUse))
	tr.reset()
	// After reset, the tool_use is gone so no pairing happens.
	emits := tr.observeUser([]byte(userWithToolResult))
	if len(emits) != 0 {
		t.Fatalf("expected 0 emits after reset, got %d", len(emits))
	}
}
