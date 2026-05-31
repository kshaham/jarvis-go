package subagent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// extractContentBlocks: content that is neither string nor []interface{}
// (e.g. a number) returns nil.
func TestExtractContentBlocks_NonArrayNonString(t *testing.T) {
	if got := extractContentBlocks(42.0); got != nil {
		t.Errorf("expected nil for non-array/non-string content, got %v", got)
	}
}

// extractContentBlocks: nil content returns nil.
func TestExtractContentBlocks_Nil(t *testing.T) {
	if got := extractContentBlocks(nil); got != nil {
		t.Errorf("expected nil for nil content, got %v", got)
	}
}

// extractContentBlocks: array containing a non-map item is skipped.
func TestExtractContentBlocks_SkipsNonMapItems(t *testing.T) {
	arr := []interface{}{
		"a bare string", // not a map → skipped
		map[string]interface{}{"type": "text", "text": "kept"},
	}
	got := extractContentBlocks(arr)
	if len(got) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(got))
	}
	if got[0].Content != "kept" {
		t.Errorf("expected kept text, got %q", got[0].Content)
	}
}

// extractContentBlocks: a tool_use block with empty input string still yields
// an entry, and an empty text block is dropped.
func TestExtractContentBlocks_EmptyTextDropped(t *testing.T) {
	arr := []interface{}{
		map[string]interface{}{"type": "text", "text": ""}, // empty → dropped
		map[string]interface{}{"type": "unknown_block"},    // unknown → ignored
	}
	got := extractContentBlocks(arr)
	if len(got) != 0 {
		t.Errorf("expected no entries, got %d", len(got))
	}
}

// checkIdleAgents: a non-active agent (already idle/completed) is skipped by
// the early continue, leaving its status untouched.
func TestWatcher_CheckIdleSkipsNonActive(t *testing.T) {
	w := NewWatcher(context.Background(), t.TempDir())
	w.mu.Lock()
	w.agents["already-idle"] = &SubagentInfo{
		AgentID: "already-idle", Status: StatusIdle,
		LastActivityAt: time.Now().Add(-1 * time.Hour),
	}
	w.agents["done"] = &SubagentInfo{
		AgentID: "done", Status: StatusCompleted,
		LastActivityAt: time.Now().Add(-1 * time.Hour),
	}
	// Active but NOT yet past idle timeout → stays active.
	w.agents["fresh-active"] = &SubagentInfo{
		AgentID: "fresh-active", Status: StatusActive,
		LastActivityAt: time.Now(),
	}
	w.mu.Unlock()

	w.checkIdleAgents()

	if w.GetAgent("already-idle").Status != StatusIdle {
		t.Error("already-idle agent status should be unchanged")
	}
	if w.GetAgent("done").Status != StatusCompleted {
		t.Error("completed agent status should be unchanged")
	}
	if w.GetAgent("fresh-active").Status != StatusActive {
		t.Error("fresh active agent should stay active (within idle timeout)")
	}
}

// tailFileLocked: when no agent record exists for the ID, the function still
// reads/parses lines without panicking (agent == nil branches).
func TestWatcher_TailFileLockedNilAgent(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "proj", "sess", "subagents")
	os.MkdirAll(subDir, 0755)
	path := filepath.Join(subDir, "agent-noagent.jsonl")
	os.WriteFile(path, []byte(
		`{"type":"user","message":{"role":"user","content":"orphaned"}}`+"\n",
	), 0644)

	w := NewWatcher(context.Background(), dir)
	// Call tailFileLocked directly WITHOUT registering the agent first.
	w.mu.Lock()
	w.tailFileLocked(path, "noagent")
	w.mu.Unlock()

	// No agent was created (tail does not create records), and filePos advanced.
	if w.GetAgent("noagent") != nil {
		t.Error("tailFileLocked must not create an agent record")
	}
	w.mu.RLock()
	if w.filePos[path] == 0 {
		t.Error("expected filePos to advance past the consumed line")
	}
	w.mu.RUnlock()
}

// tailFileLocked: a file whose size equals the stored position has no new data
// and returns early.
func TestWatcher_TailFileNoNewData(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "proj", "sess", "subagents")
	os.MkdirAll(subDir, 0755)
	path := filepath.Join(subDir, "agent-nonew.jsonl")
	data := []byte(`{"type":"user","message":{"role":"user","content":"x"}}` + "\n")
	os.WriteFile(path, data, 0644)

	w := NewWatcher(context.Background(), dir)
	w.mu.Lock()
	w.agents["nonew"] = &SubagentInfo{AgentID: "nonew", FilePath: path, Status: StatusActive, StartedAt: time.Now()}
	// Pretend we already consumed the whole file.
	w.filePos[path] = int64(len(data))
	w.mu.Unlock()

	w.TailFile(path, "nonew")

	agent := w.GetAgent("nonew")
	if agent.EntryCount != 0 {
		t.Errorf("expected no entries read when size<=pos, got %d", agent.EntryCount)
	}
}

// tailFileLocked: a line that fails to parse (invalid JSON) is skipped but
// still advances filePos; subsequent valid lines are processed.
func TestWatcher_TailFileSkipsInvalidLine(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "proj", "sess", "subagents")
	os.MkdirAll(subDir, 0755)
	path := filepath.Join(subDir, "agent-badline.jsonl")
	os.WriteFile(path, []byte(
		"this is not json\n"+
			`{"type":"user","message":{"role":"user","content":"valid"}}`+"\n",
	), 0644)

	w := NewWatcher(context.Background(), dir)
	w.scanForAgents()

	agent := w.GetAgent("badline")
	if agent == nil {
		t.Fatal("agent missing")
	}
	// Both lines increment EntryCount (count happens before parse), but only
	// the valid line yields a description.
	if agent.Description == "" {
		t.Error("expected description from the valid line after skipping invalid")
	}
}

// scanForAgents: a file whose basename does not match the agent-*.jsonl
// pattern is ignored. The glob pattern enforces agent-*.jsonl, so to exercise
// the extractAgentID == "" continue we place an "agent-.jsonl" file (empty ID).
func TestWatcher_ScanIgnoresEmptyAgentID(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "proj", "sess", "subagents")
	os.MkdirAll(subDir, 0755)
	// "agent-.jsonl" matches the glob agent-*.jsonl but extractAgentID → "".
	os.WriteFile(filepath.Join(subDir, "agent-.jsonl"), []byte(`{"type":"x"}`+"\n"), 0644)
	// A valid one alongside it.
	os.WriteFile(filepath.Join(subDir, "agent-valid.jsonl"), []byte(`{"type":"x"}`+"\n"), 0644)

	w := NewWatcher(context.Background(), dir)
	w.scanForAgents()

	if w.GetAgent("") != nil {
		t.Error("empty-ID agent must not be tracked")
	}
	if w.GetAgent("valid") == nil {
		t.Error("valid agent should be tracked")
	}
	if w.AgentCount() != 1 {
		t.Errorf("expected exactly 1 tracked agent, got %d", w.AgentCount())
	}
}
