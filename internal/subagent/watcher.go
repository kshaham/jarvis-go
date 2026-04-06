package subagent

import (
	"bufio"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	maxTrackedAgents     = 200
	scanInterval         = 10 * time.Second
	idleTimeout          = 30 * time.Second
	staleIdleMaxAge      = 4 * time.Hour
	staleCleanupInterval = 5 * time.Minute
)

// Watcher monitors subagent JSONL files under ~/.claude/projects.
type Watcher struct {
	mu          sync.RWMutex
	agents      map[string]*SubagentInfo
	filePos     map[string]int64 // file path -> last read position
	projectsDir string
	events      chan SubagentEvent
	ctx         context.Context
	cancel      context.CancelFunc
	closeOnce   sync.Once

	// Pending tool calls: agentID -> toolUseID -> toolName
	pendingTools map[string]map[string]string

	// LRU tracking: agentID -> last access time
	accessOrder map[string]time.Time
}

// NewWatcher creates a new subagent watcher.
func NewWatcher(ctx context.Context, projectsDir string) *Watcher {
	childCtx, cancel := context.WithCancel(ctx)
	return &Watcher{
		agents:       make(map[string]*SubagentInfo),
		filePos:      make(map[string]int64),
		projectsDir:  projectsDir,
		events:       make(chan SubagentEvent, 64),
		ctx:          childCtx,
		cancel:       cancel,
		pendingTools: make(map[string]map[string]string),
		accessOrder:  make(map[string]time.Time),
	}
}

// Start begins scanning for subagent files.
func (w *Watcher) Start() {
	go w.scanLoop()
	go w.cleanupLoop()
}

// Stop halts the watcher.
func (w *Watcher) Stop() {
	w.cancel()
	w.closeOnce.Do(func() { close(w.events) })
}

// Events returns the event channel.
func (w *Watcher) Events() <-chan SubagentEvent {
	return w.events
}

// GetAgents returns all tracked agents.
func (w *Watcher) GetAgents() []SubagentInfo {
	w.mu.RLock()
	defer w.mu.RUnlock()
	result := make([]SubagentInfo, 0, len(w.agents))
	for _, a := range w.agents {
		result = append(result, *a)
	}
	return result
}

// GetAgent returns a specific agent by ID, or nil if not found.
func (w *Watcher) GetAgent(agentID string) *SubagentInfo {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if a, ok := w.agents[agentID]; ok {
		copy := *a
		return &copy
	}
	return nil
}

// AgentCount returns the number of tracked agents.
func (w *Watcher) AgentCount() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return len(w.agents)
}

// RemoveAgent removes an agent from tracking by its agent ID.
// Returns true if the agent was found and removed.
func (w *Watcher) RemoveAgent(agentID string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	agent, exists := w.agents[agentID]
	if !exists {
		return false
	}
	if agent.FilePath != "" {
		delete(w.filePos, agent.FilePath)
	}
	delete(w.agents, agentID)
	delete(w.pendingTools, agentID)
	delete(w.accessOrder, agentID)
	return true
}

func (w *Watcher) scanLoop() {
	ticker := time.NewTicker(scanInterval)
	defer ticker.Stop()

	// Initial scan
	w.scanForAgents()

	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			w.scanForAgents()
			w.checkIdleAgents()
		}
	}
}

// cleanupLoop periodically removes stale agents.
func (w *Watcher) cleanupLoop() {
	ticker := time.NewTicker(staleCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			w.cleanupStaleAgents()
		}
	}
}

func (w *Watcher) scanForAgents() {
	// Walk projectsDir looking for subagent JSONL files
	pattern := filepath.Join(w.projectsDir, "*", "*", "subagents", "agent-*.jsonl")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		slog.Debug("subagent scan glob error", "error", err)
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	for _, filePath := range matches {
		agentID := extractAgentID(filePath)
		if agentID == "" {
			continue
		}

		if _, exists := w.agents[agentID]; exists {
			// Already tracked; tail new content
			w.tailFileLocked(filePath, agentID)
			continue
		}

		// Enforce LRU eviction before adding new agent
		if len(w.agents) >= maxTrackedAgents {
			w.evictOldestLocked()
		}

		now := time.Now()
		info := &SubagentInfo{
			AgentID:        agentID,
			FilePath:       filePath,
			Status:         StatusActive,
			StartedAt:      now,
			LastActivityAt: now,
		}
		w.agents[agentID] = info
		w.accessOrder[agentID] = now

		w.emit(SubagentDiscovered{Info: *info})

		// Read initial content
		w.tailFileLocked(filePath, agentID)
	}
}

// checkIdleAgents transitions active agents to idle after inactivity timeout.
func (w *Watcher) checkIdleAgents() {
	w.mu.Lock()
	defer w.mu.Unlock()

	now := time.Now()
	for _, agent := range w.agents {
		if agent.Status != StatusActive {
			continue
		}
		if now.Sub(agent.LastActivityAt) > idleTimeout {
			agent.Status = StatusIdle
			w.emit(SubagentUpdated{Info: *agent})
		}
	}
}

// cleanupStaleAgents removes agents that have been idle for too long.
func (w *Watcher) cleanupStaleAgents() {
	w.mu.Lock()
	defer w.mu.Unlock()

	now := time.Now()
	var toRemove []string
	for agentID, agent := range w.agents {
		switch agent.Status {
		case StatusIdle:
			if now.Sub(agent.LastActivityAt) > staleIdleMaxAge {
				toRemove = append(toRemove, agentID)
			}
		case StatusCompleted:
			// Remove completed agents after 1 hour
			if now.Sub(agent.LastActivityAt) > time.Hour {
				toRemove = append(toRemove, agentID)
			}
		}
	}

	for _, agentID := range toRemove {
		agent := w.agents[agentID]
		delete(w.filePos, agent.FilePath)
		delete(w.agents, agentID)
		delete(w.pendingTools, agentID)
		delete(w.accessOrder, agentID)
		slog.Debug("cleaned up stale agent", "agentId", agentID, "status", agent.Status)
	}
}

// evictOldestLocked removes the least recently accessed agent to make room.
// Must be called with w.mu held.
func (w *Watcher) evictOldestLocked() {
	if len(w.agents) == 0 {
		return
	}

	var oldestID string
	var oldestTime time.Time
	first := true
	for agentID, accessTime := range w.accessOrder {
		if first || accessTime.Before(oldestTime) {
			oldestID = agentID
			oldestTime = accessTime
			first = false
		}
	}

	if oldestID != "" {
		agent := w.agents[oldestID]
		if agent != nil {
			delete(w.filePos, agent.FilePath)
		}
		delete(w.agents, oldestID)
		delete(w.pendingTools, oldestID)
		delete(w.accessOrder, oldestID)
		slog.Debug("evicted LRU agent", "agentId", oldestID)
	}
}

func extractAgentID(filePath string) string {
	base := filepath.Base(filePath)
	if !strings.HasPrefix(base, "agent-") || !strings.HasSuffix(base, ".jsonl") {
		return ""
	}
	return strings.TrimSuffix(strings.TrimPrefix(base, "agent-"), ".jsonl")
}

// TailFile reads new lines from a JSONL file starting from the last known position.
// This is the public version that acquires the lock.
func (w *Watcher) TailFile(filePath, agentID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.tailFileLocked(filePath, agentID)
}

// tailFileLocked reads new lines from a JSONL file starting from the last known
// position and processes each line. Must be called with w.mu held.
func (w *Watcher) tailFileLocked(filePath, agentID string) {
	pos := w.filePos[filePath]

	f, err := os.Open(filePath)
	if err != nil {
		return
	}
	defer f.Close()

	// Check file size first
	stat, err := f.Stat()
	if err != nil {
		return
	}
	if stat.Size() <= pos {
		return // No new data
	}

	if pos > 0 {
		f.Seek(pos, 0)
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024) // 1MB max line

	var bytesRead int64
	agent := w.agents[agentID]

	for scanner.Scan() {
		line := scanner.Text()
		bytesRead += int64(len(line)) + 1 // +1 for newline

		if strings.TrimSpace(line) == "" {
			continue
		}

		if agent != nil {
			agent.LastActivityAt = time.Now()
			agent.EntryCount++
			agent.FileSize = stat.Size()
			if agent.Status == StatusIdle {
				agent.Status = StatusActive
			}
		}

		// Update LRU access time
		w.accessOrder[agentID] = time.Now()

		// Parse the JSONL line
		entry, err := ParseJSONLLine(line)
		if err != nil {
			slog.Debug("failed to parse JSONL line", "agentId", agentID, "error", err)
			continue
		}
		if entry == nil {
			continue
		}

		// Process the parsed entry
		w.processEntry(agentID, entry)
	}

	// Update file position
	w.filePos[filePath] = pos + bytesRead

	// Update agent info and emit update event
	if agent != nil && bytesRead > 0 {
		w.emit(SubagentUpdated{Info: *agent})
	}
}

// processEntry handles a parsed JSONL entry and emits appropriate events.
// Must be called with w.mu held.
func (w *Watcher) processEntry(agentID string, entry *Entry) {
	agent := w.agents[agentID]

	// Aggregate tokens
	if agent != nil && (entry.InputTokens > 0 || entry.OutputTokens > 0) {
		agent.InputTokens += entry.InputTokens
		agent.OutputTokens += entry.OutputTokens
		agent.TokensUsed = agent.InputTokens + agent.OutputTokens
	}

	// Extract model info
	if agent != nil && entry.Model != "" && agent.Model == "" {
		agent.Model = entry.Model
		agent.ModelShort = extractModelShort(entry.Model)
	}

	switch entry.Type {
	case EntryToolUse:
		if agent != nil {
			agent.ToolCallCount++
		}
		// Track pending tool call for linking results
		if entry.ToolUseID != "" {
			if w.pendingTools[agentID] == nil {
				w.pendingTools[agentID] = make(map[string]string)
			}
			w.pendingTools[agentID][entry.ToolUseID] = entry.ToolName
		}

		w.emit(SubagentTool{
			AgentID:   agentID,
			ToolName:  entry.ToolName,
			ToolUseID: entry.ToolUseID,
			Status:    "started",
			Input:     entry.ToolInput,
		})

	case EntryToolResult:
		// Link to pending tool call
		toolName := ""
		if entry.ToolUseID != "" {
			if pending, ok := w.pendingTools[agentID]; ok {
				toolName = pending[entry.ToolUseID]
				delete(pending, entry.ToolUseID)
			}
		}

		status := "completed"
		if entry.IsError {
			status = "error"
		}

		w.emit(SubagentTool{
			AgentID:   agentID,
			ToolName:  toolName,
			ToolUseID: entry.ToolUseID,
			Status:    status,
		})

	case EntryUser, EntryAssistant:
		if entry.Content != "" {
			w.emit(SubagentMessage{
				AgentID: agentID,
				Role:    string(entry.Type),
				Content: entry.Content,
			})

			// Extract description from first user message
			if agent != nil && agent.Description == "" && entry.Type == EntryUser {
				desc := entry.Content
				if len(desc) > 200 {
					desc = desc[:200] + "..."
				}
				agent.Description = desc
			}
		}

	case EntryProgress:
		w.emit(SubagentProgress{
			AgentID: agentID,
			Summary: entry.Content,
		})
	}
}

// MarkCompleted transitions an agent to completed status and emits the event.
func (w *Watcher) MarkCompleted(agentID string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	agent, ok := w.agents[agentID]
	if !ok || agent.Status == StatusCompleted {
		return
	}

	agent.Status = StatusCompleted
	duration := time.Since(agent.StartedAt)

	w.emit(SubagentCompleted{
		Info:     *agent,
		Duration: duration,
	})
}

// emit sends an event without blocking.
func (w *Watcher) emit(event SubagentEvent) {
	select {
	case w.events <- event:
	default:
		// Channel full; drop event
	}
}

// extractModelShort maps a full model name to a short identifier.
func extractModelShort(model string) string {
	lower := strings.ToLower(model)
	switch {
	case strings.Contains(lower, "haiku"):
		return "haiku"
	case strings.Contains(lower, "sonnet"):
		return "sonnet"
	case strings.Contains(lower, "opus"):
		return "opus"
	default:
		return ""
	}
}

// ForceCleanup triggers an immediate cleanup of stale agents and returns the
// number of agents removed.
func (w *Watcher) ForceCleanup() int {
	w.mu.Lock()
	defer w.mu.Unlock()

	now := time.Now()
	var toRemove []string
	for agentID, agent := range w.agents {
		switch agent.Status {
		case StatusIdle:
			if now.Sub(agent.LastActivityAt) > staleIdleMaxAge {
				toRemove = append(toRemove, agentID)
			}
		case StatusCompleted:
			if now.Sub(agent.LastActivityAt) > time.Hour {
				toRemove = append(toRemove, agentID)
			}
		}
	}

	for _, agentID := range toRemove {
		agent := w.agents[agentID]
		delete(w.filePos, agent.FilePath)
		delete(w.agents, agentID)
		delete(w.pendingTools, agentID)
		delete(w.accessOrder, agentID)
		slog.Debug("force-cleaned stale agent", "agentId", agentID, "status", agent.Status)
	}

	return len(toRemove)
}

// ClearAll removes all tracked subagents and returns the count cleared.
func (w *Watcher) ClearAll() int {
	w.mu.Lock()
	defer w.mu.Unlock()

	count := len(w.agents)
	w.agents = make(map[string]*SubagentInfo)
	w.filePos = make(map[string]int64)
	w.pendingTools = make(map[string]map[string]string)
	w.accessOrder = make(map[string]time.Time)

	slog.Info("cleared all tracked subagents", "count", count)
	return count
}

// AgentsByLastActivity returns agents sorted by last activity (most recent first).
func (w *Watcher) AgentsByLastActivity() []SubagentInfo {
	agents := w.GetAgents()
	sort.Slice(agents, func(i, j int) bool {
		return agents[i].LastActivityAt.After(agents[j].LastActivityAt)
	})
	return agents
}
