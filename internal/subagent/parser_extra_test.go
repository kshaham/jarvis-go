package subagent

import (
	"encoding/json"
	"strings"
	"testing"
)

// --- extractTextContent (was 0% covered) ---

func TestExtractTextContent_Nil(t *testing.T) {
	if got := extractTextContent(nil); got != "" {
		t.Errorf("extractTextContent(nil) = %q, want empty", got)
	}
}

func TestExtractTextContent_String(t *testing.T) {
	if got := extractTextContent("hello world"); got != "hello world" {
		t.Errorf("extractTextContent(string) = %q, want hello world", got)
	}
}

func TestExtractTextContent_StringTruncated(t *testing.T) {
	long := strings.Repeat("x", textPreviewLength+100)
	got := extractTextContent(long)
	if len(got) != textPreviewLength {
		t.Errorf("expected truncation to %d, got len %d", textPreviewLength, len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("expected truncated string to end with ..., got %q", got[len(got)-5:])
	}
}

func TestExtractTextContent_Array(t *testing.T) {
	arr := []interface{}{
		map[string]interface{}{"type": "text", "text": "first"},
		map[string]interface{}{"type": "text", "text": "second"},
		map[string]interface{}{"type": "image"}, // no text field, ignored
		"plain string item",                     // not a map, ignored
	}
	got := extractTextContent(arr)
	if got != "first\nsecond" {
		t.Errorf("extractTextContent(array) = %q, want %q", got, "first\nsecond")
	}
}

func TestExtractTextContent_UnsupportedType(t *testing.T) {
	// A number is neither string nor []interface{} → empty.
	if got := extractTextContent(42.0); got != "" {
		t.Errorf("extractTextContent(number) = %q, want empty", got)
	}
}

// --- extractToolResultContent (was 25% covered) ---

func TestExtractToolResultContent_Nil(t *testing.T) {
	if got := extractToolResultContent(nil); got != "" {
		t.Errorf("extractToolResultContent(nil) = %q, want empty", got)
	}
}

func TestExtractToolResultContent_String(t *testing.T) {
	// Unlike extractTextContent, the string path here does NOT truncate.
	long := strings.Repeat("y", textPreviewLength+200)
	got := extractToolResultContent(long)
	if got != long {
		t.Errorf("extractToolResultContent(string) should be returned verbatim")
	}
}

func TestExtractToolResultContent_Array(t *testing.T) {
	arr := []interface{}{
		map[string]interface{}{"type": "text", "text": "line1"},
		map[string]interface{}{"type": "text", "text": "line2"},
		map[string]interface{}{"foo": "bar"}, // no text → skipped
	}
	got := extractToolResultContent(arr)
	if got != "line1\nline2" {
		t.Errorf("extractToolResultContent(array) = %q, want %q", got, "line1\nline2")
	}
}

func TestExtractToolResultContent_UnsupportedType(t *testing.T) {
	if got := extractToolResultContent(true); got != "" {
		t.Errorf("extractToolResultContent(bool) = %q, want empty", got)
	}
}

// --- summarizeInput (was 66.7% covered) ---

func TestSummarizeInput_Nil(t *testing.T) {
	if got := summarizeInput(nil); got != "" {
		t.Errorf("summarizeInput(nil) = %q, want empty", got)
	}
}

func TestSummarizeInput_NonMap(t *testing.T) {
	// A plain string is not a map → falls through to fmt.Sprintf + truncate.
	got := summarizeInput("just a string value")
	if got != "just a string value" {
		t.Errorf("summarizeInput(string) = %q, want %q", got, "just a string value")
	}
}

func TestSummarizeInput_NonMapTruncated(t *testing.T) {
	long := strings.Repeat("z", inputTruncateLength+50)
	got := summarizeInput(long)
	if len(got) != inputTruncateLength {
		t.Errorf("expected truncation to %d, got len %d", inputTruncateLength, len(got))
	}
}

func TestSummarizeInput_SingleKey(t *testing.T) {
	got := summarizeInput(map[string]interface{}{"file_path": "/src/main.go"})
	if got != "file_path=/src/main.go" {
		t.Errorf("summarizeInput single = %q, want %q", got, "file_path=/src/main.go")
	}
}

func TestSummarizeInput_LongValueTruncated(t *testing.T) {
	longVal := strings.Repeat("a", inputTruncateLength+20)
	got := summarizeInput(map[string]interface{}{"command": longVal})
	if !strings.HasSuffix(got, "...") {
		t.Errorf("expected long value to be truncated with ..., got %q", got)
	}
	if !strings.HasPrefix(got, "command=") {
		t.Errorf("expected prefix command=, got %q", got)
	}
}

func TestSummarizeInput_CapsAtThreeKeys(t *testing.T) {
	// More than 3 keys → should append "..." and stop at 3 entries + "...".
	got := summarizeInput(map[string]interface{}{
		"a": "1", "b": "2", "c": "3", "d": "4", "e": "5",
	})
	parts := strings.Split(got, ", ")
	// 3 real keys + the trailing "..." marker = 4 parts.
	if len(parts) != 4 {
		t.Errorf("expected 4 parts (3 keys + ...), got %d: %q", len(parts), got)
	}
	if parts[len(parts)-1] != "..." {
		t.Errorf("expected last part to be ..., got %q", parts[len(parts)-1])
	}
}

// --- parseMessageEntry: top-level content (no nested message) ---

func TestParseJSONLLine_TopLevelContentNoMessage(t *testing.T) {
	// "assistant" type but no "message" key; content is at top level.
	line := `{"type":"assistant","content":"top level text"}`
	entry, err := ParseJSONLLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Type != EntryAssistant {
		t.Errorf("expected assistant type, got %s", entry.Type)
	}
	if entry.Content != "top level text" {
		t.Errorf("expected top-level content, got %q", entry.Content)
	}
}

// --- parseMessageFromNested: content falls back to raw["content"] ---

func TestParseJSONLLine_NestedMessageContentFromRaw(t *testing.T) {
	// message has role but no content; content is on raw at top level.
	line := `{"type":"user","content":"raw fallback content","message":{"role":"user"}}`
	entry, err := ParseJSONLLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Content != "raw fallback content" {
		t.Errorf("expected raw fallback content, got %q", entry.Content)
	}
}

// --- Unknown type with role in nested message (alternative format) ---

func TestParseJSONLLine_UnknownTypeWithNestedRole(t *testing.T) {
	// type is not assistant/user/progress, but message.role is present.
	line := `{"type":"weird","message":{"role":"assistant","content":"via nested role"}}`
	entry, err := ParseJSONLLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Type != EntryAssistant {
		t.Errorf("expected assistant type from nested role, got %s", entry.Type)
	}
	if entry.Content != "via nested role" {
		t.Errorf("expected content via nested role, got %q", entry.Content)
	}
}

// --- Unknown type with no message → default fallthrough ---

func TestParseJSONLLine_UnknownTypeNoMessage(t *testing.T) {
	line := `{"type":"summary","other":"data"}`
	entry, err := ParseJSONLLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Type != EntryType("summary") {
		t.Errorf("expected type summary, got %s", entry.Type)
	}
	// Content should be the truncated raw line.
	if !strings.Contains(entry.Content, "summary") {
		t.Errorf("expected raw line in content, got %q", entry.Content)
	}
}

// --- Multiple text blocks merged into a single content entry ---

func TestParseJSONLLine_MultipleTextBlocksMerged(t *testing.T) {
	msg := map[string]interface{}{
		"type": "assistant",
		"message": map[string]interface{}{
			"role": "assistant",
			"content": []interface{}{
				map[string]interface{}{"type": "text", "text": "part one"},
				map[string]interface{}{"type": "text", "text": "part two"},
			},
		},
	}
	b, _ := json.Marshal(msg)
	entry, err := ParseJSONLLine(string(b))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Content != "part one\npart two" {
		t.Errorf("expected merged text, got %q", entry.Content)
	}
}

// --- tool_use entry carries token/model from outer message ---

func TestParseJSONLLine_ToolUseCarriesTokensAndModel(t *testing.T) {
	msg := map[string]interface{}{
		"type": "assistant",
		"message": map[string]interface{}{
			"role":  "assistant",
			"model": "claude-opus-4-20250514",
			"usage": map[string]interface{}{"input_tokens": 10.0, "output_tokens": 20.0},
			"content": []interface{}{
				map[string]interface{}{
					"type": "tool_use", "name": "Grep", "id": "tu_g",
					"input": map[string]interface{}{"pattern": "foo"},
				},
			},
		},
	}
	b, _ := json.Marshal(msg)
	entry, err := ParseJSONLLine(string(b))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Type != EntryToolUse {
		t.Fatalf("expected tool_use, got %s", entry.Type)
	}
	if entry.Model != "claude-opus-4-20250514" {
		t.Errorf("expected model propagated, got %q", entry.Model)
	}
	if entry.InputTokens != 10 || entry.OutputTokens != 20 {
		t.Errorf("expected tokens propagated, got in=%d out=%d", entry.InputTokens, entry.OutputTokens)
	}
	if !strings.Contains(entry.ToolInput, "pattern=foo") {
		t.Errorf("expected tool input summary, got %q", entry.ToolInput)
	}
}

// --- tool_result with array content ---

func TestParseJSONLLine_ToolResultArrayContent(t *testing.T) {
	msg := map[string]interface{}{
		"type": "user",
		"message": map[string]interface{}{
			"role": "user",
			"content": []interface{}{
				map[string]interface{}{
					"type":        "tool_result",
					"tool_use_id": "tu_arr",
					"content": []interface{}{
						map[string]interface{}{"type": "text", "text": "result text"},
					},
				},
			},
		},
	}
	b, _ := json.Marshal(msg)
	entry, err := ParseJSONLLine(string(b))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Type != EntryToolResult {
		t.Fatalf("expected tool_result, got %s", entry.Type)
	}
	if entry.Content != "result text" {
		t.Errorf("expected result text, got %q", entry.Content)
	}
}

// --- progress entry with query and hookEvent ---

func TestParseJSONLLine_ProgressQuery(t *testing.T) {
	line := `{"type":"progress","data":{"query":"find me"}}`
	entry, err := ParseJSONLLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Content != "Query: find me" {
		t.Errorf("expected query content, got %q", entry.Content)
	}
}

func TestParseJSONLLine_ProgressHookEvent(t *testing.T) {
	line := `{"type":"progress","data":{"hookEvent":"PreToolUse"}}`
	entry, err := ParseJSONLLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Content != "Hook: PreToolUse" {
		t.Errorf("expected hook content, got %q", entry.Content)
	}
}

func TestParseJSONLLine_ProgressNoData(t *testing.T) {
	line := `{"type":"progress"}`
	entry, err := ParseJSONLLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Type != EntryProgress {
		t.Errorf("expected progress type, got %s", entry.Type)
	}
	if entry.Content != "" {
		t.Errorf("expected empty content with no data, got %q", entry.Content)
	}
}

// --- empty content blocks array → falls to extractTextContent ---

func TestParseJSONLLine_EmptyContentArray(t *testing.T) {
	// Array with only non-text/non-tool blocks yields no entries,
	// so parseMessageFromNested falls back to extractTextContent.
	msg := map[string]interface{}{
		"type": "assistant",
		"message": map[string]interface{}{
			"role": "assistant",
			"content": []interface{}{
				map[string]interface{}{"type": "thinking"},
			},
		},
	}
	b, _ := json.Marshal(msg)
	entry, err := ParseJSONLLine(string(b))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// extractTextContent over that array finds no "text" key → empty.
	if entry.Content != "" {
		t.Errorf("expected empty content, got %q", entry.Content)
	}
}

// --- truncate edge: maxLen <= 3 ---

func TestTruncate_MaxLenLessThanThree(t *testing.T) {
	if got := truncate("hello", 2); got != "he" {
		t.Errorf("truncate(hello,2) = %q, want he", got)
	}
	if got := truncate("hello", 3); got != "hel" {
		t.Errorf("truncate(hello,3) = %q, want hel", got)
	}
}

// --- event type marker methods (types.go, was 0%) ---

func TestSubagentEventTypeMarkers(t *testing.T) {
	cases := []struct {
		ev   SubagentEvent
		want string
	}{
		{SubagentDiscovered{}, "discovered"},
		{SubagentUpdated{}, "updated"},
		{SubagentCompleted{}, "completed"},
		{SubagentTool{}, "tool"},
		{SubagentProgress{}, "progress"},
		{SubagentMessage{}, "message"},
	}
	for _, tc := range cases {
		if got := tc.ev.subagentEventType(); got != tc.want {
			t.Errorf("%T.subagentEventType() = %q, want %q", tc.ev, got, tc.want)
		}
	}
}
