package office

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// StandupMessage represents one entry in the standup transcript.
type StandupMessage struct {
	SessionID   string `json:"sessionId"`
	SpeakerName string `json:"speakerName"`
	Content     string `json:"content"`
	Type        string `json:"type"` // "opening", "status_report", "closing", "takeaway"
	Timestamp   int64  `json:"timestamp"`
}

// PersistedStandup is the on-disk representation of a completed standup.
type PersistedStandup struct {
	ID               string           `json:"id"`
	Participants     []string         `json:"participants"`
	Transcript       []StandupMessage `json:"transcript"`
	StartedAt        int64            `json:"startedAt"`
	CompletedAt      int64            `json:"completedAt"`
	RespondedCount   int              `json:"respondedCount"`
	TimedOutCount    int              `json:"timedOutCount"`
	ParticipantCount int              `json:"participantCount"`
}

// StandupStartedEvent is emitted when a standup begins (already defined in events.go).
// StandupEndedEvent is emitted when a standup concludes (already defined in events.go).

// standupDir returns the directory for persisted standups.
func standupDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".jarvis", "office-memory", "standups")
}

// StartStandup begins a standup meeting. Sends the standup prompt to each
// participant via WriteViaScreen and starts response collection. If a standup
// is already active, returns the current state unchanged.
func (m *Manager) StartStandup(participantIDs []string) *StandupState {
	m.mu.Lock()
	if m.wrappingUp {
		m.mu.Unlock()
		slog.Info("Standup blocked: team is wrapping up")
		return nil
	}
	if m.standup != nil && m.standup.Active {
		st := m.standup
		m.mu.Unlock()
		return st
	}

	// Resolve participants: use provided IDs or all registered agents.
	participants := participantIDs
	if len(participants) == 0 {
		for sessionID := range m.agents {
			participants = append(participants, sessionID)
		}
	}

	// Filter to only registered agents.
	active := make([]string, 0, len(participants))
	for _, id := range participants {
		if _, ok := m.agents[id]; ok {
			active = append(active, id)
		}
	}

	if len(active) == 0 {
		m.mu.Unlock()
		slog.Error("Cannot start standup: no active participants")
		return nil
	}

	standupID := uuid.New().String()
	m.standup = &StandupState{
		Active:       true,
		Participants: active,
		Responses:    make(map[string]string),
		Transcript:   make([]StandupMessage, 0, len(active)+2),
		StartedAt:    time.Now(),
		CurrentIndex: 0,
	}
	sessions := m.sessions
	cfg := m.config
	standupResult := m.standup // capture before unlock
	m.mu.Unlock()

	m.broadcast("office:standupStarted", map[string]interface{}{
		"id":           standupID,
		"participants": active,
		"startedAt":    time.Now().UnixMilli(),
	})
	m.emitEvent(StandupStartedEvent{Participants: active})
	m.metrics.TotalStandups.Add(1)
	m.auditLog("office:standupStarted", "", "system", map[string]interface{}{
		"participants": len(active),
	})

	slog.Info("Standup started", "participants", len(active), "id", standupID)

	// Run round-robin async.
	go m.runStandupRoundRobin(standupID, active, sessions, cfg)

	return standupResult
}

// runStandupRoundRobin iterates through participants, sending prompts and
// waiting for responses with nudge/timeout logic.
func (m *Manager) runStandupRoundRobin(standupID string, participants []string, sessions SessionStore, cfg *Config) {
	openingMsg := StandupMessage{
		SessionID:   "system",
		SpeakerName: "Standup",
		Content:     fmt.Sprintf("Daily standup started with %d participants.", len(participants)),
		Type:        "opening",
		Timestamp:   time.Now().UnixMilli(),
	}
	transcript := []StandupMessage{openingMsg}

	// Also store the opening on the shared standup state so GetOfficeState returns it.
	m.mu.Lock()
	if m.standup != nil {
		m.standup.Transcript = append(m.standup.Transcript, openingMsg)
	}
	m.mu.Unlock()

	// Broadcast the opening message so frontend SSE listeners see it.
	m.broadcast("office:standupMessage", openingMsg)

	for i, sessionID := range participants {
		// Check if standup was cancelled.
		m.mu.RLock()
		active := m.standup != nil && m.standup.Active
		m.mu.RUnlock()
		if !active {
			break
		}

		m.mu.Lock()
		if m.standup != nil {
			m.standup.CurrentIndex = i
		}
		m.mu.Unlock()

		agent := m.GetAgent(sessionID)
		speakerName := sessionID
		if agent != nil && agent.Config != nil {
			speakerName = agent.Config.Name
		}

		m.broadcast("office:standupSpeaker", map[string]interface{}{
			"sessionId":   sessionID,
			"speakerName": speakerName,
			"index":       i,
		})
		slog.Info("Standup: agent turn", "name", speakerName, "index", i)

		// Build and send the standup prompt.
		teamMission := m.GetTeamMission()
		missionLine := ""
		if teamMission != "" {
			missionLine = fmt.Sprintf("Team Mission: %s\nConsider how your work aligns with this mission in your update.\n", teamMission)
		}

		crossContext := BuildCrossAgentContext(sessionID)
		crossContextLine := ""
		if crossContext != "" {
			crossContextLine = crossContext + "\n"
		}

		standupPrompt := strings.Join([]string{
			"[STANDUP] It's your turn to give a status update.",
			"What did you work on recently? What are you planning next? Any blockers?",
			missionLine,
			crossContextLine,
			"Reply by running this command (IMPORTANT - do this now):",
			`cm standup respond "<your 2-3 sentence status update>"`,
		}, "\n")

		if sessions == nil {
			slog.Warn("Standup: sessions unavailable, skipping prompt", "agent", speakerName)
		} else {
			sent := sessions.WriteInkPrompt(sessionID, standupPrompt)
			if !sent {
				slog.Warn("Standup prompt delivery failed", "agent", speakerName)
			}
		}

		if agent != nil {
			agent.mu.Lock()
			agent.LastPromptSentAt = time.Now()
			agent.mu.Unlock()
		}

		// Wait for response with nudge logic.
		response := m.waitForStandupResponse(sessionID, speakerName, sessions, cfg)

		msg := StandupMessage{
			SessionID:   sessionID,
			SpeakerName: speakerName,
			Content:     response,
			Type:        "status_report",
			Timestamp:   time.Now().UnixMilli(),
		}

		// Record response AND append to shared transcript so GetOfficeState can return it.
		m.mu.Lock()
		if m.standup != nil {
			m.standup.Responses[sessionID] = response
			m.standup.Transcript = append(m.standup.Transcript, msg)
		}
		m.mu.Unlock()

		transcript = append(transcript, msg)
		m.broadcast("office:standupMessage", msg)
	}

	// Complete the standup.
	m.mu.RLock()
	stillActive := m.standup != nil && m.standup.Active
	m.mu.RUnlock()

	if stillActive {
		closingMsg := StandupMessage{
			SessionID:   "system",
			SpeakerName: "Standup",
			Content:     fmt.Sprintf("Standup completed. %d team members reported.", len(participants)),
			Type:        "closing",
			Timestamp:   time.Now().UnixMilli(),
		}
		transcript = append(transcript, closingMsg)

		m.mu.Lock()
		var standupStartedAt time.Time
		if m.standup != nil {
			standupStartedAt = m.standup.StartedAt
			m.standup.Active = false
			// Keep transcript on state so GetOfficeState returns it after standup ends.
			m.standup.Transcript = append(m.standup.Transcript, closingMsg)
		}
		m.mu.Unlock()

		// Broadcast the closing message too so frontend SSE listeners see it.
		m.broadcast("office:standupMessage", closingMsg)

		m.broadcast("office:standupEnded", map[string]interface{}{
			"id":         standupID,
			"transcript": transcript,
		})
		m.emitEvent(StandupEndedEvent{Summary: closingMsg.Content})

		// Persist to disk.
		m.persistStandup(standupID, participants, transcript, standupStartedAt)
		m.metrics.TotalStandupsComplete.Add(1)

		// Trigger leader task assignment by notifying strategic-tier agents.
		m.triggerLeaderTaskAssignment(standupID, participants, transcript)

		slog.Info("Standup completed", "id", standupID, "participants", len(participants))
	}
}

// waitForStandupResponse blocks until the agent responds via SubmitStandupResponse
// or the timeout expires. Nudges are sent at regular intervals.
func (m *Manager) waitForStandupResponse(sessionID, speakerName string, sessions SessionStore, cfg *Config) string {
	agent := m.GetAgent(sessionID)
	if agent == nil {
		return fmt.Sprintf("%s was unreachable during standup.", speakerName)
	}

	// Always create a fresh channel to avoid consuming stale responses.
	agent.mu.Lock()
	agent.StandupCh = make(chan string, 1)
	ch := agent.StandupCh
	agent.mu.Unlock()

	timeout := time.NewTimer(cfg.StandupResponseTimeout)
	defer timeout.Stop()

	nudgeInterval := time.NewTicker(cfg.StandupNudgeInterval)
	defer nudgeInterval.Stop()

	nudgeCount := 0

	for {
		select {
		case response := <-ch:
			return response

		case <-nudgeInterval.C:
			nudgeCount++
			if nudgeCount > cfg.StandupMaxNudges {
				slog.Warn("Standup: max nudges reached, giving up", "agent", speakerName)
				return fmt.Sprintf("%s did not respond in time.", speakerName)
			}
			slog.Info("Standup: nudging agent", "name", speakerName, "nudge", nudgeCount, "max", cfg.StandupMaxNudges)

			// Send Ctrl+C then nudge prompt in a goroutine to keep select responsive.
			if sessions != nil {
				go func() {
					sessions.WriteViaScreen(sessionID, "\x03")
					time.Sleep(2 * time.Second)
					shortNudge := strings.Join([]string{
						"[STANDUP REMINDER] You haven't responded to the standup yet. This is URGENT - please respond NOW.",
						"Reply by running this command IMMEDIATELY:",
						`cm standup respond "<your 2-3 sentence status update>"`,
					}, "\n")
					sessions.WriteInkPrompt(sessionID, shortNudge)
				}()
			}

		case <-timeout.C:
			slog.Warn("Standup response timeout", "agent", speakerName)
			return fmt.Sprintf("%s did not respond in time.", speakerName)
		}
	}
}

// SubmitStandupResponse records a response from an agent during standup.
// Returns true if the response was accepted (standup active and agent is a participant).
func (m *Manager) SubmitStandupResponse(sessionID, content string) bool {
	m.mu.RLock()
	active := m.standup != nil && m.standup.Active
	m.mu.RUnlock()

	agent := m.GetAgent(sessionID)
	if agent == nil {
		return false
	}

	sanitized := sanitizeForTerminal(content)

	agent.mu.Lock()
	ch := agent.StandupCh
	agent.mu.Unlock()

	if ch == nil {
		if !active {
			slog.Warn("Late standup response, no active standup", "session", sessionID)
		}
		return false
	}

	select {
	case ch <- sanitized:
		slog.Info("Standup response received", "session", sessionID, "content_len", len(sanitized))
		return true
	default:
		// Channel full -- duplicate response.
		return false
	}
}

// StopStandup cancels an active standup.
func (m *Manager) StopStandup() {
	m.mu.Lock()
	if m.standup != nil {
		m.standup.Active = false
	}
	m.mu.Unlock()

	m.broadcast("office:standupEnded", map[string]interface{}{
		"cancelled": true,
	})
}

// CompleteStandup is an alias for StopStandup -- it marks the standup done.
func (m *Manager) CompleteStandup() {
	m.StopStandup()
}

// persistStandup writes the completed standup to disk as JSON.
func (m *Manager) persistStandup(standupID string, participants []string, transcript []StandupMessage, startedAt time.Time) {
	dir := standupDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Error("Failed to create standup dir", "err", err)
		return
	}

	// Count responded vs timed-out.
	responded := 0
	timedOut := 0
	for _, msg := range transcript {
		if msg.Type != "status_report" {
			continue
		}
		if strings.Contains(msg.Content, "did not respond in time") ||
			msg.Content == "(offline - skipped)" {
			timedOut++
		} else {
			responded++
		}
	}

	persisted := PersistedStandup{
		ID:               standupID,
		Participants:     participants,
		Transcript:       transcript,
		StartedAt:        startedAt.UnixMilli(),
		CompletedAt:      time.Now().UnixMilli(),
		RespondedCount:   responded,
		TimedOutCount:    timedOut,
		ParticipantCount: len(participants),
	}

	data, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		slog.Error("Failed to marshal standup", "err", err)
		return
	}

	filePath := filepath.Join(dir, standupID+".json")
	tmpPath := filePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		slog.Error("Failed to write standup", "err", err)
		return
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		slog.Error("Failed to rename standup file", "err", err)
		return
	}

	slog.Info("Standup persisted to disk", "id", standupID, "path", filePath)
}

// triggerLeaderTaskAssignment sends a task assignment prompt to strategic-tier
// agents with the standup transcript so they can assign tasks.
func (m *Manager) triggerLeaderTaskAssignment(standupID string, participants []string, transcript []StandupMessage) {
	m.mu.RLock()
	sessions := m.sessions
	agents := make([]*AgentState, 0)
	for _, a := range m.agents {
		agents = append(agents, a)
	}
	m.mu.RUnlock()

	// Build transcript summary.
	var summaryBuilder strings.Builder
	for _, msg := range transcript {
		if msg.Type == "status_report" {
			fmt.Fprintf(&summaryBuilder, "- %s: %s\n", msg.SpeakerName, msg.Content)
		}
	}
	transcriptSummary := summaryBuilder.String()

	// Read team mission for leader context.
	teamMission := m.GetTeamMission()

	// Build roster summary for leader reference (configId -> name).
	var rosterBuilder strings.Builder
	m.mu.RLock()
	for _, a := range m.agents {
		if a.Config != nil {
			fmt.Fprintf(&rosterBuilder, "  %s = %s (%s)\n", a.ConfigID, a.Config.Name, a.Config.Role)
		}
	}
	m.mu.RUnlock()
	rosterSummary := rosterBuilder.String()

	// Find strategic-tier agents and send task assignment prompt.
	for _, agent := range agents {
		if agent.Config == nil || agent.Config.Tier != "strategic" {
			continue
		}

		// Prepend mission context if available.
		missionPrefix := ""
		if teamMission != "" {
			missionPrefix = "[Team Mission] " + teamMission + "\nPrioritize tasks that align with this mission. Think about how the team can go above and beyond to deliver exceptional results.\n\n"
		}

		leaderPrompt := strings.Join([]string{
			missionPrefix + "[LEADER TASK ASSIGNMENT] Standup just completed. Here are the team reports:",
			"",
			transcriptSummary,
			"Team roster (use configId in TASKS_FOR):",
			rosterSummary,
			"Based on each agent's status, create 2-4 specific, actionable tasks for EACH team member.",
			`Respond using: cm leader-tasks respond "TASKS_FOR:agentId\n- [HIGH] title | description\n..."`,
		}, "\n")

		// Reset leader task channel for this standup round.
		agent.mu.Lock()
		agent.LeaderTaskCh = make(chan string, 1)
		ch := agent.LeaderTaskCh
		agent.mu.Unlock()

		if sessions == nil {
			slog.Warn("Leader task assignment: sessions unavailable", "agent", agent.Config.Name)
			continue
		}
		sent := sessions.WriteInkPrompt(agent.SessionID, leaderPrompt)
		if sent {
			slog.Info("Leader task assignment prompt sent", "leader", agent.Config.Name)
		}

		// Wait for leader response and process task assignments (Gap 3+4+5).
		go m.processLeaderTaskResponse(ch, agent, agents, sessions, teamMission)
	}
}

// processLeaderTaskResponse waits for the leader's task assignment response,
// parses it into per-agent tasks, injects task queues, and sends a summary.
func (m *Manager) processLeaderTaskResponse(ch chan string, leader *AgentState, allAgents []*AgentState, sessions SessionStore, teamMission string) {
	// Wait up to 5 minutes for the leader to respond.
	timeout := time.NewTimer(5 * time.Minute)
	defer timeout.Stop()

	var response string
	select {
	case response = <-ch:
		slog.Info("Leader task response received", "leader", leader.Config.Name, "len", len(response))
	case <-timeout.C:
		slog.Warn("Leader task response timeout", "leader", leader.Config.Name)
		return
	}

	// Parse the response for TASKS_FOR:{configId} blocks.
	taskAssignments := parseLeaderTaskResponse(response)
	if len(taskAssignments) == 0 {
		slog.Warn("No task assignments parsed from leader response", "leader", leader.Config.Name)
		return
	}

	// Create tasks and inject queues for each agent.
	var summaryBuilder strings.Builder
	for configID, taskLines := range taskAssignments {
		agent := m.GetAgentByConfig(configID)
		if agent == nil {
			slog.Warn("Task assignment for unknown agent", "configId", configID)
			continue
		}

		// Create tasks from parsed lines.
		createdTasks := make([]*Task, 0, len(taskLines))
		for _, line := range taskLines {
			task := &Task{
				ID:          uuid.New().String(),
				Title:       line.Title,
				Description: line.Description,
				Priority:    line.Priority,
				Status:      TaskPending,
				AssignedAt:  time.Now(),
				Source:      "leader",
			}
			agent.AddTask(task)
			createdTasks = append(createdTasks, task)
		}

		// Build and inject task queue prompt.
		m.injectTaskQueue(agent, createdTasks, sessions, teamMission)

		// Build summary line for leader.
		titles := make([]string, 0, len(createdTasks))
		for _, t := range createdTasks {
			titles = append(titles, t.Title)
		}
		fmt.Fprintf(&summaryBuilder, "- %s: %d tasks (%s)\n", agent.Config.Name, len(createdTasks), strings.Join(titles, ", "))
	}

	// Gap 5: Send post-standup summary to the leader.
	if summaryBuilder.Len() > 0 {
		leaderSummary := strings.Join([]string{
			"[Post-Standup Summary]",
			"Tasks assigned:",
			summaryBuilder.String(),
			"Monitor your team's progress. If any agent is blocked, triage their task to another agent using `cm task triage`.",
		}, "\n")
		if sessions != nil {
			sessions.WriteInkPrompt(leader.SessionID, leaderSummary)
		}
		slog.Info("Post-standup summary sent to leader", "leader", leader.Config.Name)
	}

	// AutoAgent: analyze team performance and inject optimization hints to bottom performers.
	opts := m.AnalyzeAndOptimize()
	if len(opts) > 0 {
		m.InjectOptimizationSuggestions(opts)
		slog.Info("Optimization suggestions injected", "count", len(opts))
	}

	// AutoAgent: append a standup insight to the living team mission document.
	if summaryBuilder.Len() > 0 {
		missionInsight := fmt.Sprintf("After standup (%s), %d agents received new tasks. Leader: %s.",
			time.Now().Format("2006-01-02 15:04"), len(taskAssignments), leader.Config.Name)
		m.AppendMissionInsight(missionInsight)
	}
}

// parsedTaskLine holds a single task parsed from the leader's response.
type parsedTaskLine struct {
	Title       string
	Description string
	Priority    string
}

// parseLeaderTaskResponse parses TASKS_FOR:{configId} blocks from the leader response.
// Expected format:
//
//	TASKS_FOR:configId
//	- [HIGH] title | description
//	- [MEDIUM] title | description
func parseLeaderTaskResponse(response string) map[string][]parsedTaskLine {
	result := make(map[string][]parsedTaskLine)
	lines := strings.Split(response, "\n")
	currentConfigID := ""

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Check for TASKS_FOR:{configId} marker.
		if strings.HasPrefix(trimmed, "TASKS_FOR:") {
			currentConfigID = strings.TrimSpace(strings.TrimPrefix(trimmed, "TASKS_FOR:"))
			if _, exists := result[currentConfigID]; !exists {
				result[currentConfigID] = make([]parsedTaskLine, 0)
			}
			continue
		}

		// Parse task lines like "- [HIGH] title | description".
		if currentConfigID != "" && strings.HasPrefix(trimmed, "- [") {
			parsed := parseTaskLine(trimmed)
			if parsed != nil {
				result[currentConfigID] = append(result[currentConfigID], *parsed)
			}
		}
	}

	return result
}

// parseTaskLine parses a single task line like "- [HIGH] title | description".
func parseTaskLine(line string) *parsedTaskLine {
	// Remove leading "- "
	line = strings.TrimPrefix(line, "- ")

	// Extract priority from [PRIORITY]
	priority := "medium"
	if strings.HasPrefix(line, "[") {
		endBracket := strings.Index(line, "]")
		if endBracket > 0 {
			priority = strings.ToLower(strings.TrimSpace(line[1:endBracket]))
			line = strings.TrimSpace(line[endBracket+1:])
		}
	}

	// Split on " | " for title and description
	parts := strings.SplitN(line, " | ", 2)
	title := strings.TrimSpace(parts[0])
	description := ""
	if len(parts) > 1 {
		description = strings.TrimSpace(parts[1])
	}

	if title == "" {
		return nil
	}

	return &parsedTaskLine{
		Title:       title,
		Description: description,
		Priority:    priority,
	}
}

// injectTaskQueue sends a task queue prompt to an agent's session.
func (m *Manager) injectTaskQueue(agent *AgentState, tasks []*Task, sessions SessionStore, teamMission string) {
	var b strings.Builder
	b.WriteString("[Task Queue] Your tasks for this sprint:\n")
	if teamMission != "" {
		b.WriteString("[Team Mission] ")
		b.WriteString(teamMission)
		b.WriteString("\n")
	}
	for i, task := range tasks {
		fmt.Fprintf(&b, "%d. [%s] %s", i+1, strings.ToUpper(task.Priority), task.Title)
		if task.Description != "" {
			fmt.Fprintf(&b, " | %s", task.Description)
		}
		b.WriteString("\n")
	}
	b.WriteString("\nUse `cm task ack <id>` to acknowledge, `cm task start <id>` to begin.")

	if sessions == nil {
		slog.Warn("injectTaskQueue: sessions unavailable", "agent", agent.Config.Name)
		return
	}
	sent := sessions.WriteInkPrompt(agent.SessionID, b.String())
	if sent {
		slog.Info("Task queue injected", "agent", agent.Config.Name, "tasks", len(tasks))
	}

	m.broadcast("office:taskQueueGenerated", map[string]interface{}{
		"sessionId": agent.SessionID,
		"agentName": agent.Config.Name,
		"taskCount": len(tasks),
	})
}
