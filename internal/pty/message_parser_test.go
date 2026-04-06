package pty

import (
	"testing"
)

func TestParseClaudeMessage_ValidSystem(t *testing.T) {
	line := `{"type":"system","content":"Hello, Claude here"}`
	msg := ParseClaudeMessage(line)
	if msg == nil {
		t.Fatal("expected non-nil message")
	}
	if msg.Type != "system" {
		t.Errorf("expected type system, got %s", msg.Type)
	}
	if msg.Content != "Hello, Claude here" {
		t.Errorf("unexpected content: %s", msg.Content)
	}
}

func TestParseClaudeMessage_ValidResult(t *testing.T) {
	line := `{"type":"result","total_cost_usd":0.05,"input_tokens":1000,"output_tokens":500}`
	msg := ParseClaudeMessage(line)
	if msg == nil {
		t.Fatal("expected non-nil message")
	}
	if msg.Type != "result" {
		t.Errorf("expected type result, got %s", msg.Type)
	}
	if msg.TotalCostUSD != 0.05 {
		t.Errorf("expected cost 0.05, got %f", msg.TotalCostUSD)
	}
	if msg.InputTokens != 1000 {
		t.Errorf("expected 1000 input tokens, got %d", msg.InputTokens)
	}
	if msg.OutputTokens != 500 {
		t.Errorf("expected 500 output tokens, got %d", msg.OutputTokens)
	}
}

func TestParseClaudeMessage_UnknownType(t *testing.T) {
	line := `{"type":"banana","content":"nope"}`
	msg := ParseClaudeMessage(line)
	if msg != nil {
		t.Error("expected nil for unknown type")
	}
}

func TestParseClaudeMessage_NotJSON(t *testing.T) {
	tests := []string{
		"Hello world",
		"",
		"   ",
		`not {"type":"system"}`,
		`[{"type":"system"}]`,
	}
	for _, input := range tests {
		msg := ParseClaudeMessage(input)
		if msg != nil {
			t.Errorf("expected nil for input %q, got %+v", input, msg)
		}
	}
}

func TestParseClaudeMessage_WithTrailingText(t *testing.T) {
	line := `  {"type":"assistant","content":"test"}  some trailing text`
	msg := ParseClaudeMessage(line)
	if msg == nil {
		t.Fatal("expected non-nil message")
	}
	if msg.Type != "assistant" {
		t.Errorf("expected type assistant, got %s", msg.Type)
	}
}

func TestParseClaudeMessages_MultiLine(t *testing.T) {
	text := `some random output
{"type":"system","content":"init"}
more random output
{"type":"assistant","content":"hello"}
trailing text`

	messages := ParseClaudeMessages(text)
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	if messages[0].Type != "system" {
		t.Errorf("first message: expected system, got %s", messages[0].Type)
	}
	if messages[1].Type != "assistant" {
		t.Errorf("second message: expected assistant, got %s", messages[1].Type)
	}
}

func TestParseClaudeMessages_NoMessages(t *testing.T) {
	text := "just some plain terminal output\nno JSON here"
	messages := ParseClaudeMessages(text)
	if messages != nil {
		t.Errorf("expected nil, got %d messages", len(messages))
	}
}

func TestExtractJSONObject_NestedObjects(t *testing.T) {
	input := `{"type":"result","data":{"nested":true}} trailing`
	result := extractJSONObject(input)
	if result != `{"type":"result","data":{"nested":true}}` {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestExtractJSONObject_Incomplete(t *testing.T) {
	input := `{"type":"result","data":`
	result := extractJSONObject(input)
	if result != "" {
		t.Errorf("expected empty string for incomplete JSON, got: %s", result)
	}
}

func TestExtractJSONObject_StringsWithBraces(t *testing.T) {
	input := `{"type":"result","content":"hello {world}"}trailing`
	result := extractJSONObject(input)
	if result != `{"type":"result","content":"hello {world}"}` {
		t.Errorf("unexpected result: %s", result)
	}
}
