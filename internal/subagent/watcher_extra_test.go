package subagent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeAgentFile creates the projects/<proj>/<sess>/subagents/agent-<id>.jsonl
// fixture inside dir and returns its full path. Distinct from helpers in other
// test files (which inline this) to keep a single reusable path builder here.
func writeAgentFixture(t *testing.T, dir, agentID string, lines []string) string {
	t.Helper()
	subDir := filepath.Join(dir, "proj", "sess", "subagents")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(subDir, "agent-"+agentID+".jsonl")
	var sb strings.Builder
	for _, l := range lines {
		sb.WriteString(l)
		sb.WriteString("\n")
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

// drainEvents consumes all currently-queued events until idle for `quiet`.
func drainEvents(w *Watcher, quiet time.Duration) []SubagentEvent {
	var evs []SubagentEvent
	timer := time.NewTimer(quiet)
	defer timer.Stop()
	for {
		select {
		case ev := <-w.Events():
			evs = append(evs, ev)
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(quiet)
		case <-timer.C:
			return evs
		}
	}
}

// --- Start / scanLoop / cleanupLoop via short intervals would require const
// overrides; instead drive scanForAgents + cleanup directly AND exercise the
// goroutine lifecycle via Start/Stop to cover the loop scaffolding. ---

func TestWatcher_StartStopLifecycle(t *testing.T) {
	dir := t.TempDir()
	writeAgentFixture(t, dir, "life1", []string{`{"type":"user","message":{"role":"user","content":"hello"}}`})

	w := NewWatcher(context.Background(), dir)
	w.Start() // launches scanLoop + cleanupLoop; initial scan runs synchronously inside scanLoop

	// The initial scan inside scanLoop should discover the agent shortly.
	deadline := time.After(2 * time.Second)
	for {
		if w.AgentCount() == 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("agent not discovered by scanLoop initial scan, count=%d", w.AgentCount())
		case <-time.After(10 * time.Millisecond):
		}
	}

	w.Stop()
	// After Stop, the events channel is closed; a receive should not block.
	select {
	case _, ok := <-w.Events():
		_ = ok // may be a buffered event or closed; either is fine
	case <-time.After(200 * time.Millisecond):
		t.Error("expected events channel to be readable/closed after Stop")
	}
}

func TestWatcher_StopIsIdempotent(t *testing.T) {
	w := NewWatcher(context.Background(), t.TempDir())
	w.Start()
	w.Stop()
	// Second Stop must not panic (closeOnce guards the channel close).
	w.Stop()
}

func TestWatcher_StopViaParentContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	w := NewWatcher(ctx, t.TempDir())
	w.Start()
	// Cancel the PARENT context; the loops watch the derived child ctx.
	cancel()
	// Stop still needs to be called to wait + close, but the loops should
	// already be returning due to ctx.Done.
	done := make(chan struct{})
	go func() { w.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return after parent context cancel")
	}
}

// --- TailFile public wrapper (was 0%) ---

func TestWatcher_TailFilePublic(t *testing.T) {
	dir := t.TempDir()
	path := writeAgentFixture(t, dir, "tail1", []string{
		`{"type":"user","message":{"role":"user","content":"do the thing"}}`,
	})

	w := NewWatcher(context.Background(), dir)
	// Pre-register the agent so tailFileLocked has an agent to mutate.
	w.mu.Lock()
	w.agents["tail1"] = &SubagentInfo{AgentID: "tail1", FilePath: path, Status: StatusActive, StartedAt: time.Now()}
	w.mu.Unlock()

	w.TailFile(path, "tail1")

	agent := w.GetAgent("tail1")
	if agent == nil {
		t.Fatal("agent missing")
	}
	if agent.EntryCount != 1 {
		t.Errorf("expected EntryCount 1, got %d", agent.EntryCount)
	}
	if agent.Description == "" {
		t.Error("expected description from first user message")
	}
}

func TestWatcher_TailFileMissingFile(t *testing.T) {
	w := NewWatcher(context.Background(), t.TempDir())
	// Should silently return (os.Open error path) without panicking.
	w.TailFile("/nonexistent/path/agent-x.jsonl", "x")
	if w.AgentCount() != 0 {
		t.Error("no agent should be created for missing file")
	}
}

// --- Incremental tailing: second call only reads appended bytes ---

func TestWatcher_IncrementalTail(t *testing.T) {
	dir := t.TempDir()
	path := writeAgentFixture(t, dir, "inc1", []string{
		`{"type":"user","message":{"role":"user","content":"first line"}}`,
	})

	w := NewWatcher(context.Background(), dir)
	w.scanForAgents()

	agent := w.GetAgent("inc1")
	if agent == nil {
		t.Fatal("agent missing after scan")
	}
	if agent.EntryCount != 1 {
		t.Fatalf("expected 1 entry after first scan, got %d", agent.EntryCount)
	}

	// Append a second line.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("open append: %v", err)
	}
	f.WriteString(`{"type":"assistant","message":{"role":"assistant","model":"claude-sonnet-4-x","usage":{"input_tokens":7,"output_tokens":3},"content":"second"}}` + "\n")
	f.Close()

	w.scanForAgents() // tails appended content
	agent = w.GetAgent("inc1")
	if agent.EntryCount != 2 {
		t.Errorf("expected 2 entries after append, got %d", agent.EntryCount)
	}
	if agent.InputTokens != 7 || agent.OutputTokens != 3 {
		t.Errorf("expected tokens aggregated, got in=%d out=%d", agent.InputTokens, agent.OutputTokens)
	}
	if agent.TokensUsed != 10 {
		t.Errorf("expected TokensUsed 10, got %d", agent.TokensUsed)
	}
}

// --- Partial trailing line is NOT consumed until newline-terminated ---

func TestWatcher_PartialTrailingLineDeferred(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "proj", "sess", "subagents")
	os.MkdirAll(subDir, 0755)
	path := filepath.Join(subDir, "agent-partial.jsonl")

	// Write one complete line + a partial line with no trailing newline.
	complete := `{"type":"user","message":{"role":"user","content":"complete"}}` + "\n"
	partial := `{"type":"assistant","message":{"role":"assistant","content":"incomp`
	os.WriteFile(path, []byte(complete+partial), 0644)

	w := NewWatcher(context.Background(), dir)
	w.scanForAgents()
	agent := w.GetAgent("partial")
	if agent == nil {
		t.Fatal("agent missing")
	}
	if agent.EntryCount != 1 {
		t.Fatalf("expected only the complete line counted, got %d", agent.EntryCount)
	}

	// Now finish the partial line.
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	f.WriteString(`lete"}}` + "\n")
	f.Close()

	w.scanForAgents()
	agent = w.GetAgent("partial")
	if agent.EntryCount != 2 {
		t.Errorf("expected 2 entries after completing the partial line, got %d", agent.EntryCount)
	}
}

// --- Idle agent reactivates when new content arrives ---

func TestWatcher_IdleReactivatesOnNewContent(t *testing.T) {
	dir := t.TempDir()
	path := writeAgentFixture(t, dir, "react1", []string{
		`{"type":"user","message":{"role":"user","content":"start"}}`,
	})

	w := NewWatcher(context.Background(), dir)
	w.scanForAgents()

	// Force the agent to idle.
	w.mu.Lock()
	w.agents["react1"].Status = StatusIdle
	w.mu.Unlock()

	// Append content and tail again — should flip back to active.
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	f.WriteString(`{"type":"assistant","message":{"role":"assistant","content":"more"}}` + "\n")
	f.Close()

	w.scanForAgents()
	agent := w.GetAgent("react1")
	if agent.Status != StatusActive {
		t.Errorf("expected agent to reactivate to active, got %s", agent.Status)
	}
}

// --- scanForAgents backfills FilePath for hook-registered agents ---

func TestWatcher_ScanBackfillsFilePathForHookAgent(t *testing.T) {
	dir := t.TempDir()
	path := writeAgentFixture(t, dir, "hookbf", []string{
		`{"type":"user","message":{"role":"user","content":"hi"}}`,
	})

	w := NewWatcher(context.Background(), dir)
	// Register via hook with NO file path; agentID must match extractAgentID(path)="hookbf".
	w.OnHookStart("sess", "Explore", "hookbf")
	before := w.GetAgent("hookbf")
	if before.FilePath != "" {
		t.Fatalf("precondition: hook agent should have empty FilePath, got %q", before.FilePath)
	}

	w.scanForAgents()
	after := w.GetAgent("hookbf")
	if after.FilePath != path {
		t.Errorf("expected FilePath backfilled to %q, got %q", path, after.FilePath)
	}
	// Description from the hook (Explore) should be preserved (not overwritten),
	// because content parsing only sets Description when it's empty.
	if after.Description != "Explore" {
		t.Errorf("expected hook description preserved, got %q", after.Description)
	}
}

// --- sweepPendingTools (was 0%) ---

func TestWatcher_SweepPendingTools(t *testing.T) {
	w := NewWatcher(context.Background(), t.TempDir())

	old := time.Now().Add(-2 * time.Hour) // older than 1h cutoff
	recent := time.Now().Add(-1 * time.Minute)

	w.mu.Lock()
	w.pendingTools["ag1"] = map[string]string{"old_tu": "Bash", "new_tu": "Read"}
	w.pendingToolTimes["ag1"] = map[string]time.Time{"old_tu": old, "new_tu": recent}
	w.mu.Unlock()

	w.sweepPendingTools()

	w.mu.RLock()
	defer w.mu.RUnlock()
	if _, ok := w.pendingTools["ag1"]["old_tu"]; ok {
		t.Error("expected old pending tool to be swept")
	}
	if _, ok := w.pendingToolTimes["ag1"]["old_tu"]; ok {
		t.Error("expected old pending tool time to be swept")
	}
	if _, ok := w.pendingTools["ag1"]["new_tu"]; !ok {
		t.Error("expected recent pending tool to remain")
	}
}

// --- ForceCleanup (was 0%) ---

func TestWatcher_ForceCleanup(t *testing.T) {
	w := NewWatcher(context.Background(), t.TempDir())

	now := time.Now()
	w.mu.Lock()
	// Stale idle (older than staleIdleMaxAge=4h) → removed
	w.agents["stale-idle"] = &SubagentInfo{AgentID: "stale-idle", Status: StatusIdle, FilePath: "/p1", LastActivityAt: now.Add(-5 * time.Hour)}
	// Old completed (older than 1h) → removed
	w.agents["old-done"] = &SubagentInfo{AgentID: "old-done", Status: StatusCompleted, FilePath: "/p2", LastActivityAt: now.Add(-2 * time.Hour)}
	// Recent idle → kept
	w.agents["fresh-idle"] = &SubagentInfo{AgentID: "fresh-idle", Status: StatusIdle, FilePath: "/p3", LastActivityAt: now.Add(-1 * time.Minute)}
	// Active → kept (not subject to cleanup)
	w.agents["active"] = &SubagentInfo{AgentID: "active", Status: StatusActive, FilePath: "/p4", LastActivityAt: now.Add(-10 * time.Hour)}
	for id := range w.agents {
		w.accessOrder[id] = now
		w.filePos[w.agents[id].FilePath] = 100
	}
	w.mu.Unlock()

	removed := w.ForceCleanup()
	if removed != 2 {
		t.Errorf("expected 2 removed, got %d", removed)
	}
	if w.GetAgent("stale-idle") != nil || w.GetAgent("old-done") != nil {
		t.Error("expected stale agents removed")
	}
	if w.GetAgent("fresh-idle") == nil || w.GetAgent("active") == nil {
		t.Error("expected fresh/active agents retained")
	}
	// filePos for removed agents should be gone.
	w.mu.RLock()
	if _, ok := w.filePos["/p1"]; ok {
		t.Error("expected filePos for removed stale-idle to be cleared")
	}
	w.mu.RUnlock()
}

func TestWatcher_ForceCleanupNoStale(t *testing.T) {
	w := NewWatcher(context.Background(), t.TempDir())
	w.mu.Lock()
	w.agents["a"] = &SubagentInfo{AgentID: "a", Status: StatusActive, LastActivityAt: time.Now()}
	w.mu.Unlock()
	if removed := w.ForceCleanup(); removed != 0 {
		t.Errorf("expected 0 removed, got %d", removed)
	}
}

// --- ClearAll (was 0%) ---

func TestWatcher_ClearAll(t *testing.T) {
	dir := t.TempDir()
	writeAgentFixture(t, dir, "clr1", []string{
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","id":"tu1","input":{"command":"ls"}}]}}`,
	})
	w := NewWatcher(context.Background(), dir)
	w.scanForAgents()

	if w.AgentCount() == 0 {
		t.Fatal("expected at least one agent before ClearAll")
	}

	count := w.ClearAll()
	if count < 1 {
		t.Errorf("expected ClearAll to report >=1 cleared, got %d", count)
	}
	if w.AgentCount() != 0 {
		t.Errorf("expected 0 agents after ClearAll, got %d", w.AgentCount())
	}
	// Internal maps must be reset (not nil) so the watcher keeps working.
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.agents == nil || w.filePos == nil || w.pendingTools == nil ||
		w.pendingToolTimes == nil || w.accessOrder == nil {
		t.Error("ClearAll must re-initialize internal maps, not nil them")
	}
	if len(w.pendingTools) != 0 || len(w.pendingToolTimes) != 0 {
		t.Error("expected pending maps emptied")
	}
}

func TestWatcher_ClearAllEmpty(t *testing.T) {
	w := NewWatcher(context.Background(), t.TempDir())
	if count := w.ClearAll(); count != 0 {
		t.Errorf("expected 0 cleared on empty watcher, got %d", count)
	}
}

// --- MarkCompleted: already-completed and missing agents are no-ops ---

func TestWatcher_MarkCompletedNoOpCases(t *testing.T) {
	w := NewWatcher(context.Background(), t.TempDir())

	// Missing agent → no event, no panic.
	w.MarkCompleted("ghost")

	w.mu.Lock()
	w.agents["done"] = &SubagentInfo{AgentID: "done", Status: StatusCompleted, StartedAt: time.Now()}
	w.mu.Unlock()
	// Already completed → no new event emitted.
	w.MarkCompleted("done")

	select {
	case ev := <-w.Events():
		t.Errorf("expected no event for no-op MarkCompleted, got %T", ev)
	case <-time.After(100 * time.Millisecond):
		// expected
	}
}

// --- scanForAgents glob error path (bad pattern dir) is hard to trigger; instead
// assert empty projectsDir yields no agents and no panic. ---

func TestWatcher_ScanEmptyDir(t *testing.T) {
	w := NewWatcher(context.Background(), t.TempDir())
	w.scanForAgents()
	if w.AgentCount() != 0 {
		t.Errorf("expected 0 agents in empty dir, got %d", w.AgentCount())
	}
}

// --- cleanupStaleAgents removes old completed agents ---

func TestWatcher_CleanupStaleCompleted(t *testing.T) {
	w := NewWatcher(context.Background(), t.TempDir())
	w.mu.Lock()
	w.agents["old-completed"] = &SubagentInfo{
		AgentID: "old-completed", Status: StatusCompleted,
		FilePath: "/tmp/x.jsonl", LastActivityAt: time.Now().Add(-2 * time.Hour),
	}
	w.agents["recent-completed"] = &SubagentInfo{
		AgentID: "recent-completed", Status: StatusCompleted,
		FilePath: "/tmp/y.jsonl", LastActivityAt: time.Now().Add(-1 * time.Minute),
	}
	w.mu.Unlock()

	w.cleanupStaleAgents()

	if w.GetAgent("old-completed") != nil {
		t.Error("expected old completed agent to be cleaned up")
	}
	if w.GetAgent("recent-completed") == nil {
		t.Error("expected recent completed agent to remain")
	}
}

// --- emit drops events when channel is full (cover default branch) ---

func TestWatcher_EmitDropsWhenFull(t *testing.T) {
	w := NewWatcher(context.Background(), t.TempDir())
	// Fill the 64-capacity buffer.
	for i := 0; i < 64; i++ {
		w.emit(SubagentProgress{AgentID: "a", Summary: fmt.Sprintf("%d", i)})
	}
	// This emit must hit the default (drop) branch without blocking.
	done := make(chan struct{})
	go func() {
		w.emit(SubagentProgress{AgentID: "a", Summary: "overflow"})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("emit blocked when channel was full")
	}
}

// --- processEntry: progress + message events emitted ---

func TestWatcher_ProcessEntryEmitsMessageAndProgress(t *testing.T) {
	dir := t.TempDir()
	writeAgentFixture(t, dir, "proc1", []string{
		`{"type":"user","message":{"role":"user","content":"first user msg"}}`,
		`{"type":"progress","data":{"query":"searching"}}`,
	})
	w := NewWatcher(context.Background(), dir)
	w.scanForAgents()

	evs := drainEvents(w, 200*time.Millisecond)
	var sawMessage, sawProgress, sawDiscovered bool
	for _, ev := range evs {
		switch e := ev.(type) {
		case SubagentMessage:
			if e.Role == "user" && e.Content == "first user msg" {
				sawMessage = true
			}
		case SubagentProgress:
			if e.Summary == "Query: searching" {
				sawProgress = true
			}
		case SubagentDiscovered:
			sawDiscovered = true
		}
	}
	if !sawDiscovered {
		t.Error("expected SubagentDiscovered event")
	}
	if !sawMessage {
		t.Error("expected SubagentMessage event for user message")
	}
	if !sawProgress {
		t.Error("expected SubagentProgress event")
	}
}

// --- tool_result without a preceding tool_use: toolName resolves empty ---

func TestWatcher_ToolResultWithoutPendingUse(t *testing.T) {
	dir := t.TempDir()
	writeAgentFixture(t, dir, "orphan1", []string{
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"missing_tu","content":"orphan result"}]}}`,
	})
	w := NewWatcher(context.Background(), dir)
	w.scanForAgents()

	evs := drainEvents(w, 200*time.Millisecond)
	var found bool
	for _, ev := range evs {
		if te, ok := ev.(SubagentTool); ok && te.Status == "completed" {
			found = true
			if te.ToolName != "" {
				t.Errorf("expected empty toolName for orphan result, got %q", te.ToolName)
			}
		}
	}
	if !found {
		t.Error("expected a completed tool event for the orphan tool_result")
	}
}

// --- tool_result with error status linked to a pending tool_use ---

func TestWatcher_ToolResultErrorStatus(t *testing.T) {
	dir := t.TempDir()
	writeAgentFixture(t, dir, "err1", []string{
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","id":"tu_e","input":{"command":"bad"}}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_e","content":"boom","is_error":true}]}}`,
	})
	w := NewWatcher(context.Background(), dir)
	w.scanForAgents()

	evs := drainEvents(w, 200*time.Millisecond)
	var sawError bool
	for _, ev := range evs {
		if te, ok := ev.(SubagentTool); ok && te.Status == "error" {
			sawError = true
			if te.ToolName != "Bash" {
				t.Errorf("expected error result linked to Bash, got %q", te.ToolName)
			}
		}
	}
	if !sawError {
		t.Error("expected an error-status tool event")
	}

	// After the result is consumed, the pending entry must be cleared.
	w.mu.RLock()
	if pend, ok := w.pendingTools["err1"]; ok {
		if _, still := pend["tu_e"]; still {
			t.Error("expected pending tool entry cleared after result")
		}
	}
	w.mu.RUnlock()
}

// --- Description extraction is capped at 200 chars + ellipsis ---

func TestWatcher_LongUserDescriptionTruncated(t *testing.T) {
	dir := t.TempDir()
	longMsg := strings.Repeat("w", 300)
	writeAgentFixture(t, dir, "longdesc", []string{
		`{"type":"user","message":{"role":"user","content":"` + longMsg + `"}}`,
	})
	w := NewWatcher(context.Background(), dir)
	w.scanForAgents()

	agent := w.GetAgent("longdesc")
	if agent == nil {
		t.Fatal("agent missing")
	}
	// Description is taken from entry.Content, which itself was truncated to
	// textPreviewLength (500) during parsing, then capped again at 200 here.
	if len(agent.Description) != 203 { // 200 + "..."
		t.Errorf("expected description length 203, got %d", len(agent.Description))
	}
	if !strings.HasSuffix(agent.Description, "...") {
		t.Error("expected description to end with ...")
	}
}

// --- evictOldestLocked picks the least-recently-accessed agent ---

func TestWatcher_EvictOldest(t *testing.T) {
	w := NewWatcher(context.Background(), t.TempDir())
	now := time.Now()
	w.mu.Lock()
	w.agents["oldest"] = &SubagentInfo{AgentID: "oldest", FilePath: "/o"}
	w.agents["middle"] = &SubagentInfo{AgentID: "middle", FilePath: "/m"}
	w.agents["newest"] = &SubagentInfo{AgentID: "newest", FilePath: "/n"}
	w.accessOrder["oldest"] = now.Add(-10 * time.Minute)
	w.accessOrder["middle"] = now.Add(-5 * time.Minute)
	w.accessOrder["newest"] = now
	w.filePos["/o"] = 1
	w.evictOldestLocked()
	w.mu.Unlock()

	if w.GetAgent("oldest") != nil {
		t.Error("expected oldest agent evicted")
	}
	if w.GetAgent("middle") == nil || w.GetAgent("newest") == nil {
		t.Error("expected newer agents retained")
	}
	w.mu.RLock()
	if _, ok := w.filePos["/o"]; ok {
		t.Error("expected evicted agent filePos cleared")
	}
	w.mu.RUnlock()
}

func TestWatcher_EvictOldestEmpty(t *testing.T) {
	w := NewWatcher(context.Background(), t.TempDir())
	w.mu.Lock()
	w.evictOldestLocked() // no agents → early return, no panic
	w.mu.Unlock()
}

// --- Concurrency: hooks + scans + reads under -race ---

func TestWatcher_ConcurrentAccessRace(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		subDir := filepath.Join(dir, fmt.Sprintf("p%d", i), "sess", "subagents")
		os.MkdirAll(subDir, 0755)
		os.WriteFile(
			filepath.Join(subDir, fmt.Sprintf("agent-race%d.jsonl", i)),
			[]byte(`{"type":"user","message":{"role":"user","content":"hi"}}`+"\n"),
			0644,
		)
	}

	w := NewWatcher(context.Background(), dir)
	w.Start()
	defer w.Stop()

	// Drain events so the buffer doesn't fill and block emitters.
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-w.Events():
			}
		}
	}()

	done := make(chan struct{})
	for g := 0; g < 4; g++ {
		go func(n int) {
			for i := 0; i < 50; i++ {
				w.OnHookStart("s", "Explore", fmt.Sprintf("h-%d-%d", n, i))
				w.GetAgents()
				w.AgentCount()
				w.scanForAgents()
				w.AgentsByLastActivity()
				w.OnHookStop("s", fmt.Sprintf("h-%d-%d", n, i))
				w.GetAgent(fmt.Sprintf("h-%d-%d", n, i))
			}
			done <- struct{}{}
		}(g)
	}
	for g := 0; g < 4; g++ {
		<-done
	}
	close(stop)
	_ = w.AgentCount()
}
