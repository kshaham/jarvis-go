package subagent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExtractAgentID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"agent-abc123.jsonl", "abc123"},
		{"agent-def-456.jsonl", "def-456"},
		{"notanagent.jsonl", ""},
		{"agent-.jsonl", ""},
	}
	for _, tc := range tests {
		if got := extractAgentID(tc.input); got != tc.expected {
			t.Errorf("extractAgentID(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestWatcher_NewAndStop(t *testing.T) {
	w := NewWatcher(context.Background(), "/tmp/nonexistent")
	if w.AgentCount() != 0 {
		t.Errorf("expected 0 agents, got %d", w.AgentCount())
	}
	w.Stop()
}

func TestWatcher_ScanForAgents(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "proj", "sess", "subagents")
	os.MkdirAll(subDir, 0755)
	os.WriteFile(filepath.Join(subDir, "agent-test1.jsonl"), []byte(`{"type":"test"}`+"\n"), 0644)

	w := NewWatcher(context.Background(), dir)
	w.scanForAgents()

	if w.AgentCount() != 1 {
		t.Errorf("expected 1 agent, got %d", w.AgentCount())
	}
}

func TestWatcher_RemoveAgent(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "proj", "sess", "subagents")
	os.MkdirAll(subDir, 0755)
	os.WriteFile(filepath.Join(subDir, "agent-rm1.jsonl"), []byte(`{"type":"test"}`+"\n"), 0644)

	w := NewWatcher(context.Background(), dir)
	w.scanForAgents()

	if w.AgentCount() != 1 {
		t.Fatalf("expected 1 agent before removal, got %d", w.AgentCount())
	}

	ok := w.RemoveAgent("rm1")
	if !ok {
		t.Error("expected RemoveAgent to return true")
	}
	if w.AgentCount() != 0 {
		t.Errorf("expected 0 agents after removal, got %d", w.AgentCount())
	}

	// Remove non-existent should return false
	if w.RemoveAgent("nonexistent") {
		t.Error("expected RemoveAgent to return false for non-existent agent")
	}
}

func TestWatcher_GetAgent(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "proj", "sess", "subagents")
	os.MkdirAll(subDir, 0755)
	os.WriteFile(filepath.Join(subDir, "agent-get1.jsonl"), []byte(`{"type":"test"}`+"\n"), 0644)

	w := NewWatcher(context.Background(), dir)
	w.scanForAgents()

	agent := w.GetAgent("get1")
	if agent == nil {
		t.Fatal("expected to find agent get1")
	}
	if agent.AgentID != "get1" {
		t.Errorf("expected agentID get1, got %s", agent.AgentID)
	}

	// Non-existent
	if w.GetAgent("nonexistent") != nil {
		t.Error("expected nil for non-existent agent")
	}
}

// --- JSONL Parser tests ---

func TestParseJSONLLine_Empty(t *testing.T) {
	entry, err := ParseJSONLLine("")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if entry != nil {
		t.Error("expected nil entry for empty line")
	}

	entry, err = ParseJSONLLine("   ")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if entry != nil {
		t.Error("expected nil entry for whitespace line")
	}
}

func TestParseJSONLLine_InvalidJSON(t *testing.T) {
	_, err := ParseJSONLLine("not json at all")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseJSONLLine_UserMessage(t *testing.T) {
	line := `{"type":"user","message":{"role":"user","content":"Implement the feature"}}`
	entry, err := ParseJSONLLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Type != EntryUser {
		t.Errorf("expected user type, got %s", entry.Type)
	}
	if entry.Content != "Implement the feature" {
		t.Errorf("unexpected content: %s", entry.Content)
	}
}

func TestParseJSONLLine_AssistantMessageWithTokens(t *testing.T) {
	line := `{"type":"assistant","message":{"role":"assistant","model":"claude-sonnet-4-20250514","usage":{"input_tokens":1500,"output_tokens":800},"content":"I will implement that for you."}}`
	entry, err := ParseJSONLLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Type != EntryAssistant {
		t.Errorf("expected assistant type, got %s", entry.Type)
	}
	if entry.InputTokens != 1500 {
		t.Errorf("expected 1500 input tokens, got %d", entry.InputTokens)
	}
	if entry.OutputTokens != 800 {
		t.Errorf("expected 800 output tokens, got %d", entry.OutputTokens)
	}
	if entry.Model != "claude-sonnet-4-20250514" {
		t.Errorf("expected model claude-sonnet-4-20250514, got %s", entry.Model)
	}
}

func TestParseJSONLLine_ToolUse(t *testing.T) {
	msg := map[string]interface{}{
		"type": "assistant",
		"message": map[string]interface{}{
			"role": "assistant",
			"content": []interface{}{
				map[string]interface{}{
					"type":  "tool_use",
					"name":  "Read",
					"id":    "tool_abc123",
					"input": map[string]interface{}{"file_path": "/src/main.go"},
				},
			},
		},
	}
	lineBytes, _ := json.Marshal(msg)
	entry, err := ParseJSONLLine(string(lineBytes))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Type != EntryToolUse {
		t.Errorf("expected tool_use type, got %s", entry.Type)
	}
	if entry.ToolName != "Read" {
		t.Errorf("expected tool name Read, got %s", entry.ToolName)
	}
	if entry.ToolUseID != "tool_abc123" {
		t.Errorf("expected tool use ID tool_abc123, got %s", entry.ToolUseID)
	}
}

func TestParseJSONLLine_ToolResult(t *testing.T) {
	msg := map[string]interface{}{
		"type": "assistant",
		"message": map[string]interface{}{
			"role": "assistant",
			"content": []interface{}{
				map[string]interface{}{
					"type":        "tool_result",
					"tool_use_id": "tool_abc123",
					"content":     "File contents here...",
					"is_error":    false,
				},
			},
		},
	}
	lineBytes, _ := json.Marshal(msg)
	entry, err := ParseJSONLLine(string(lineBytes))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Type != EntryToolResult {
		t.Errorf("expected tool_result type, got %s", entry.Type)
	}
	if entry.ToolUseID != "tool_abc123" {
		t.Errorf("expected tool use ID tool_abc123, got %s", entry.ToolUseID)
	}
	if entry.IsError {
		t.Error("expected IsError to be false")
	}
}

func TestParseJSONLLine_ToolResultError(t *testing.T) {
	msg := map[string]interface{}{
		"type": "assistant",
		"message": map[string]interface{}{
			"role": "assistant",
			"content": []interface{}{
				map[string]interface{}{
					"type":        "tool_result",
					"tool_use_id": "tool_err123",
					"content":     "File not found",
					"is_error":    true,
				},
			},
		},
	}
	lineBytes, _ := json.Marshal(msg)
	entry, err := ParseJSONLLine(string(lineBytes))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !entry.IsError {
		t.Error("expected IsError to be true")
	}
}

func TestParseJSONLLine_ProgressEntry(t *testing.T) {
	line := `{"type":"progress","data":{"query":"search for files","resultCount":5}}`
	entry, err := ParseJSONLLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Type != EntryProgress {
		t.Errorf("expected progress type, got %s", entry.Type)
	}
}

// --- Truncate tests ---

func TestTruncate(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is a longer string", 10, "this is..."},
		{"ab", 5, "ab"},
		{"", 5, ""},
	}
	for _, tc := range tests {
		if got := truncate(tc.input, tc.maxLen); got != tc.expected {
			t.Errorf("truncate(%q, %d) = %q, want %q", tc.input, tc.maxLen, got, tc.expected)
		}
	}
}

// --- Model extraction tests ---

func TestExtractModelShort(t *testing.T) {
	tests := []struct {
		model    string
		expected string
	}{
		{"claude-sonnet-4-20250514", "sonnet"},
		{"claude-haiku-3.5-20241022", "haiku"},
		{"claude-opus-4-20250514", "opus"},
		{"gpt-4", ""},
		{"", ""},
	}
	for _, tc := range tests {
		if got := extractModelShort(tc.model); got != tc.expected {
			t.Errorf("extractModelShort(%q) = %q, want %q", tc.model, got, tc.expected)
		}
	}
}

// --- Watcher integration tests ---

func TestWatcher_TailFileWithParsing(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "proj", "sess", "subagents")
	os.MkdirAll(subDir, 0755)

	// Write a JSONL file with mixed entries
	filePath := filepath.Join(subDir, "agent-parse1.jsonl")
	lines := []string{
		`{"type":"user","message":{"role":"user","content":"Implement authentication"}}`,
		`{"type":"assistant","message":{"role":"assistant","model":"claude-sonnet-4-20250514","usage":{"input_tokens":500,"output_tokens":200},"content":"I will implement authentication."}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Read","id":"tu1","input":{"file_path":"/src/auth.go"}}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_result","tool_use_id":"tu1","content":"file contents"}]}}`,
	}
	content := ""
	for _, line := range lines {
		content += line + "\n"
	}
	os.WriteFile(filePath, []byte(content), 0644)

	w := NewWatcher(context.Background(), dir)
	w.scanForAgents()

	// Check that agent was discovered and parsed
	agent := w.GetAgent("parse1")
	if agent == nil {
		t.Fatal("expected to find agent parse1")
	}
	if agent.Description == "" {
		t.Error("expected description to be extracted from first user message")
	}
	if agent.Model != "claude-sonnet-4-20250514" {
		t.Errorf("expected model claude-sonnet-4-20250514, got %s", agent.Model)
	}
	if agent.ModelShort != "sonnet" {
		t.Errorf("expected modelShort sonnet, got %s", agent.ModelShort)
	}
	if agent.InputTokens != 500 {
		t.Errorf("expected 500 input tokens, got %d", agent.InputTokens)
	}
	if agent.OutputTokens != 200 {
		t.Errorf("expected 200 output tokens, got %d", agent.OutputTokens)
	}
	if agent.ToolCallCount != 1 {
		t.Errorf("expected 1 tool call, got %d", agent.ToolCallCount)
	}
}

func TestWatcher_ToolCallLinking(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "proj", "sess", "subagents")
	os.MkdirAll(subDir, 0755)

	filePath := filepath.Join(subDir, "agent-link1.jsonl")
	lines := []string{
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","id":"tu_bash","input":{"command":"ls -la"}}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_result","tool_use_id":"tu_bash","content":"total 42"}]}}`,
	}
	content := ""
	for _, line := range lines {
		content += line + "\n"
	}
	os.WriteFile(filePath, []byte(content), 0644)

	w := NewWatcher(context.Background(), dir)
	w.scanForAgents()

	// Drain events and check for tool linking
	var toolEvents []SubagentTool
	timeout := time.After(500 * time.Millisecond)
	for done := false; !done; {
		select {
		case ev := <-w.Events():
			if te, ok := ev.(SubagentTool); ok {
				toolEvents = append(toolEvents, te)
			}
		case <-timeout:
			done = true
		}
	}

	if len(toolEvents) < 2 {
		t.Fatalf("expected at least 2 tool events, got %d", len(toolEvents))
	}

	// First should be started
	if toolEvents[0].Status != "started" || toolEvents[0].ToolName != "Bash" {
		t.Errorf("first event: expected started/Bash, got %s/%s", toolEvents[0].Status, toolEvents[0].ToolName)
	}
	// Second should be completed and linked back to Bash
	if toolEvents[1].Status != "completed" || toolEvents[1].ToolName != "Bash" {
		t.Errorf("second event: expected completed/Bash, got %s/%s", toolEvents[1].Status, toolEvents[1].ToolName)
	}
}

func TestWatcher_LRUEviction(t *testing.T) {
	dir := t.TempDir()

	// Create maxTrackedAgents + 5 agent files
	for i := 0; i < maxTrackedAgents+5; i++ {
		subDir := filepath.Join(dir, fmt.Sprintf("proj%d", i), "sess", "subagents")
		os.MkdirAll(subDir, 0755)
		os.WriteFile(
			filepath.Join(subDir, fmt.Sprintf("agent-lru%d.jsonl", i)),
			[]byte(`{"type":"test"}`+"\n"),
			0644,
		)
	}

	w := NewWatcher(context.Background(), dir)
	w.scanForAgents()

	count := w.AgentCount()
	if count > maxTrackedAgents {
		t.Errorf("expected at most %d agents (LRU eviction), got %d", maxTrackedAgents, count)
	}
}

func TestWatcher_IdleTransition(t *testing.T) {
	w := NewWatcher(context.Background(), "/tmp/nonexistent")

	// Manually add an agent with old activity time
	w.mu.Lock()
	w.agents["idle-test"] = &SubagentInfo{
		AgentID:        "idle-test",
		Status:         StatusActive,
		LastActivityAt: time.Now().Add(-2 * time.Minute), // well past idle timeout
		StartedAt:      time.Now().Add(-5 * time.Minute),
	}
	w.accessOrder["idle-test"] = time.Now()
	w.mu.Unlock()

	w.checkIdleAgents()

	agent := w.GetAgent("idle-test")
	if agent == nil {
		t.Fatal("expected to find agent idle-test")
	}
	if agent.Status != StatusIdle {
		t.Errorf("expected status idle, got %s", agent.Status)
	}
}

func TestWatcher_StaleCleanup(t *testing.T) {
	w := NewWatcher(context.Background(), "/tmp/nonexistent")

	// Add stale idle agent (5 hours old)
	w.mu.Lock()
	w.agents["stale-idle"] = &SubagentInfo{
		AgentID:        "stale-idle",
		Status:         StatusIdle,
		FilePath:       "/tmp/fake-path.jsonl",
		LastActivityAt: time.Now().Add(-5 * time.Hour),
		StartedAt:      time.Now().Add(-6 * time.Hour),
	}
	w.accessOrder["stale-idle"] = time.Now().Add(-5 * time.Hour)

	// Add recent idle agent (1 hour old)
	w.agents["recent-idle"] = &SubagentInfo{
		AgentID:        "recent-idle",
		Status:         StatusIdle,
		FilePath:       "/tmp/fake-path2.jsonl",
		LastActivityAt: time.Now().Add(-1 * time.Hour),
		StartedAt:      time.Now().Add(-2 * time.Hour),
	}
	w.accessOrder["recent-idle"] = time.Now().Add(-1 * time.Hour)
	w.mu.Unlock()

	w.cleanupStaleAgents()

	if w.GetAgent("stale-idle") != nil {
		t.Error("expected stale idle agent to be cleaned up")
	}
	if w.GetAgent("recent-idle") == nil {
		t.Error("expected recent idle agent to still exist")
	}
}

func TestWatcher_MarkCompleted(t *testing.T) {
	w := NewWatcher(context.Background(), "/tmp/nonexistent")

	w.mu.Lock()
	w.agents["complete-test"] = &SubagentInfo{
		AgentID:        "complete-test",
		Status:         StatusActive,
		LastActivityAt: time.Now(),
		StartedAt:      time.Now().Add(-10 * time.Minute),
	}
	w.mu.Unlock()

	w.MarkCompleted("complete-test")

	agent := w.GetAgent("complete-test")
	if agent == nil {
		t.Fatal("expected to find agent")
	}
	if agent.Status != StatusCompleted {
		t.Errorf("expected completed status, got %s", agent.Status)
	}

	// Should emit SubagentCompleted event
	select {
	case ev := <-w.Events():
		if _, ok := ev.(SubagentCompleted); !ok {
			t.Errorf("expected SubagentCompleted event, got %T", ev)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("timeout waiting for completion event")
	}
}

func TestWatcher_AgentsByLastActivity(t *testing.T) {
	w := NewWatcher(context.Background(), "/tmp/nonexistent")

	now := time.Now()
	w.mu.Lock()
	w.agents["old"] = &SubagentInfo{
		AgentID:        "old",
		LastActivityAt: now.Add(-10 * time.Minute),
	}
	w.agents["new"] = &SubagentInfo{
		AgentID:        "new",
		LastActivityAt: now.Add(-1 * time.Minute),
	}
	w.agents["mid"] = &SubagentInfo{
		AgentID:        "mid",
		LastActivityAt: now.Add(-5 * time.Minute),
	}
	w.mu.Unlock()

	sorted := w.AgentsByLastActivity()
	if len(sorted) != 3 {
		t.Fatalf("expected 3 agents, got %d", len(sorted))
	}
	if sorted[0].AgentID != "new" {
		t.Errorf("expected most recent first, got %s", sorted[0].AgentID)
	}
	if sorted[2].AgentID != "old" {
		t.Errorf("expected oldest last, got %s", sorted[2].AgentID)
	}
}
