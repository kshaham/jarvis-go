package pty

import (
	"encoding/json"
	"strings"
)

// ParsedClaudeMessage represents a parsed JSON message from Claude CLI output.
type ParsedClaudeMessage struct {
	Type         string  `json:"type"`
	SessionID    string  `json:"session_id,omitempty"`
	Content      string  `json:"content,omitempty"`
	TotalCostUSD float64 `json:"total_cost_usd,omitempty"`
	InputTokens  int64   `json:"input_tokens,omitempty"`
	OutputTokens int64   `json:"output_tokens,omitempty"`
}

// ParseClaudeMessage detects and parses a JSON message block from terminal output.
// Claude CLI emits JSON messages on single lines starting with {"type":".
// Returns nil if no valid message is found.
func ParseClaudeMessage(line string) *ParsedClaudeMessage {
	line = strings.TrimSpace(line)

	// Quick check: must start with JSON object containing "type"
	if !strings.HasPrefix(line, `{"type":"`) {
		return nil
	}

	// Try to find the end of the JSON object on this line
	// Handle case where there may be trailing text after the JSON
	jsonStr := extractJSONObject(line)
	if jsonStr == "" {
		return nil
	}

	var msg ParsedClaudeMessage
	if err := json.Unmarshal([]byte(jsonStr), &msg); err != nil {
		return nil
	}

	// Only accept known message types
	switch msg.Type {
	case "system", "assistant", "result", "error", "tool_use", "tool_result":
		return &msg
	default:
		return nil
	}
}

// ParseClaudeMessages scans multi-line text for JSON message blocks.
// Returns all successfully parsed messages.
func ParseClaudeMessages(text string) []ParsedClaudeMessage {
	if !strings.Contains(text, `{"type":"`) {
		return nil
	}

	var messages []ParsedClaudeMessage
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		if msg := ParseClaudeMessage(line); msg != nil {
			messages = append(messages, *msg)
		}
	}
	return messages
}

// extractJSONObject extracts a complete JSON object from the beginning of a string.
// Uses brace counting to handle nested objects.
func extractJSONObject(s string) string {
	if len(s) == 0 || s[0] != '{' {
		return ""
	}

	depth := 0
	inString := false
	escaped := false

	for i := 0; i < len(s); i++ {
		c := s[i]

		if escaped {
			escaped = false
			continue
		}

		if c == '\\' && inString {
			escaped = true
			continue
		}

		if c == '"' {
			inString = !inString
			continue
		}

		if inString {
			continue
		}

		if c == '{' {
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 {
				return s[:i+1]
			}
		}
	}

	return "" // Incomplete JSON
}
