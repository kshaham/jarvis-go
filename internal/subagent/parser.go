package subagent

import (
	"encoding/json"
	"fmt"
	"strings"
)

// EntryType classifies a JSONL transcript line.
type EntryType string

const (
	EntryAssistant  EntryType = "assistant"
	EntryUser       EntryType = "user"
	EntryToolUse    EntryType = "tool_use"
	EntryToolResult EntryType = "tool_result"
	EntryProgress   EntryType = "progress"
)

// Entry is the structured representation of a parsed JSONL transcript line.
type Entry struct {
	Type      EntryType `json:"type"`
	Content   string    `json:"content"`
	ToolName  string    `json:"toolName,omitempty"`
	ToolInput string    `json:"toolInput,omitempty"`
	ToolUseID string    `json:"toolUseId,omitempty"`
	SessionID string    `json:"sessionId,omitempty"`
	IsError   bool      `json:"isError,omitempty"`

	// Token usage from assistant messages
	InputTokens  int64 `json:"inputTokens,omitempty"`
	OutputTokens int64 `json:"outputTokens,omitempty"`

	// Model name from assistant messages
	Model string `json:"model,omitempty"`
}

// inputTruncateLength is the max length for tool input summaries.
const inputTruncateLength = 100

// textPreviewLength is the max length for text previews in tool results.
const textPreviewLength = 500

// ParseJSONLLine parses a single JSONL transcript line into a structured Entry.
// Returns nil, nil for empty or whitespace-only lines.
func ParseJSONLLine(line string) (*Entry, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil, nil
	}

	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	entryType, _ := raw["type"].(string)

	switch entryType {
	case "assistant", "user":
		return parseMessageEntry(raw, entryType)
	case "progress":
		return parseProgressEntry(raw)
	default:
		// Try to parse as a message with role field (alternative format)
		if msg, ok := raw["message"].(map[string]interface{}); ok {
			if role, ok := msg["role"].(string); ok {
				return parseMessageFromNested(raw, msg, role)
			}
		}
		return &Entry{
			Type:    EntryType(entryType),
			Content: truncate(line, textPreviewLength),
		}, nil
	}
}

// parseMessageEntry handles top-level message entries.
func parseMessageEntry(raw map[string]interface{}, entryType string) (*Entry, error) {
	entry := &Entry{
		Type: EntryType(entryType),
	}

	msg, _ := raw["message"].(map[string]interface{})
	if msg == nil {
		// Content might be at top level
		entry.Content = extractTextContent(raw["content"])
		return entry, nil
	}

	return parseMessageFromNested(raw, msg, entryType)
}

// parseMessageFromNested extracts data from a nested message object.
func parseMessageFromNested(raw map[string]interface{}, msg map[string]interface{}, role string) (*Entry, error) {
	entry := &Entry{
		Type: EntryType(role),
	}

	// Extract model
	if model, ok := msg["model"].(string); ok {
		entry.Model = model
	}

	// Extract token usage
	if usage, ok := msg["usage"].(map[string]interface{}); ok {
		if inputTokens, ok := usage["input_tokens"].(float64); ok {
			entry.InputTokens = int64(inputTokens)
		}
		if outputTokens, ok := usage["output_tokens"].(float64); ok {
			entry.OutputTokens = int64(outputTokens)
		}
	}

	// Parse content - can be string or array of content blocks
	content := msg["content"]
	if content == nil {
		content = raw["content"]
	}

	entries := extractContentBlocks(content)
	if len(entries) > 0 {
		// If we found tool_use or tool_result blocks, return those individually
		// But also merge the text content into a single entry
		var textParts []string
		for _, e := range entries {
			switch e.Type {
			case EntryToolUse:
				// Return tool_use as separate entry
				e.InputTokens = entry.InputTokens
				e.OutputTokens = entry.OutputTokens
				e.Model = entry.Model
				return e, nil
			case EntryToolResult:
				e.InputTokens = entry.InputTokens
				e.OutputTokens = entry.OutputTokens
				e.Model = entry.Model
				return e, nil
			default:
				if e.Content != "" {
					textParts = append(textParts, e.Content)
				}
			}
		}
		entry.Content = truncate(strings.Join(textParts, "\n"), textPreviewLength)
	} else {
		entry.Content = extractTextContent(content)
	}

	return entry, nil
}

// extractContentBlocks processes content which may be a string or an array
// of content blocks (text, tool_use, tool_result).
func extractContentBlocks(content interface{}) []*Entry {
	if content == nil {
		return nil
	}

	// String content
	if s, ok := content.(string); ok {
		return []*Entry{{Type: EntryAssistant, Content: s}}
	}

	// Array of content blocks
	arr, ok := content.([]interface{})
	if !ok {
		return nil
	}

	var entries []*Entry
	for _, item := range arr {
		block, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		blockType, _ := block["type"].(string)
		switch blockType {
		case "text":
			text, _ := block["text"].(string)
			if text != "" {
				entries = append(entries, &Entry{
					Type:    EntryAssistant,
					Content: text,
				})
			}

		case "tool_use":
			name, _ := block["name"].(string)
			id, _ := block["id"].(string)
			inputSummary := summarizeInput(block["input"])
			entries = append(entries, &Entry{
				Type:      EntryToolUse,
				ToolName:  name,
				ToolUseID: id,
				ToolInput: inputSummary,
			})

		case "tool_result":
			toolUseID, _ := block["tool_use_id"].(string)
			isError, _ := block["is_error"].(bool)
			resultContent := extractToolResultContent(block["content"])
			entries = append(entries, &Entry{
				Type:      EntryToolResult,
				ToolUseID: toolUseID,
				Content:   truncate(resultContent, textPreviewLength),
				IsError:   isError,
			})
		}
	}

	return entries
}

// parseProgressEntry handles progress-type entries.
func parseProgressEntry(raw map[string]interface{}) (*Entry, error) {
	entry := &Entry{
		Type: EntryProgress,
	}

	if data, ok := raw["data"].(map[string]interface{}); ok {
		if query, ok := data["query"].(string); ok {
			entry.Content = "Query: " + query
		}
		if hookEvent, ok := data["hookEvent"].(string); ok {
			entry.Content = "Hook: " + hookEvent
		}
	}

	return entry, nil
}

// summarizeInput creates a short summary of a tool's input.
func summarizeInput(input interface{}) string {
	if input == nil {
		return ""
	}

	inputMap, ok := input.(map[string]interface{})
	if !ok {
		return truncate(fmt.Sprintf("%v", input), inputTruncateLength)
	}

	var parts []string
	for key, val := range inputMap {
		valStr := fmt.Sprintf("%v", val)
		if len(valStr) > inputTruncateLength {
			valStr = valStr[:inputTruncateLength] + "..."
		}
		parts = append(parts, key+"="+valStr)
		if len(parts) >= 3 {
			parts = append(parts, "...")
			break
		}
	}
	return strings.Join(parts, ", ")
}

// extractTextContent extracts a text string from a content field.
func extractTextContent(content interface{}) string {
	if content == nil {
		return ""
	}
	if s, ok := content.(string); ok {
		return truncate(s, textPreviewLength)
	}
	if arr, ok := content.([]interface{}); ok {
		var parts []string
		for _, item := range arr {
			if block, ok := item.(map[string]interface{}); ok {
				if text, ok := block["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}
		return truncate(strings.Join(parts, "\n"), textPreviewLength)
	}
	return ""
}

// extractToolResultContent extracts text from a tool_result content field.
func extractToolResultContent(content interface{}) string {
	if content == nil {
		return ""
	}
	if s, ok := content.(string); ok {
		return s
	}
	if arr, ok := content.([]interface{}); ok {
		var parts []string
		for _, item := range arr {
			if block, ok := item.(map[string]interface{}); ok {
				if text, ok := block["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// truncate shortens a string to maxLen, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
