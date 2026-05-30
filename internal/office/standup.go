package office

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/kshaham/jarvis/internal/safego"
)

// maxStandupFileBytes caps how much data LoadPersistedStandup will read from a
// single standup JSON file. A 10 MiB standup file is already unrealistically
// large; anything bigger is most likely corruption or a write-gone-wrong.
const maxStandupFileBytes = 10 << 20

// StandupMessage represents one entry in the standup transcript.
type StandupMessage struct {
	SessionID   string `json:"sessionId"`
	SpeakerName string `json:"speakerName"`
	Content     string `json:"content"`
	Type        string `json:"type"` // "opening", "status_report", "closing", "takeaway"
	Timestamp   int64  `json:"timestamp"`
	// TimedOut marks a status_report that is a timeout/unreachable fallback
	// (not a real agent response). Set at the source so persistStandup's
	// responded-vs-timedout tally doesn't have to substring-match the Content
	// (which miscounts a real update that quotes the timeout phrase).
	TimedOut bool `json:"timedOut,omitempty"`
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
		ID:           standupID,
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

	// Run round-robin async. R19 · wrap in safeRun so a panic in the
	// round-robin (e.g. a nil sessions pointer or a malformed config)
	// surfaces as an office:panicRecovered broadcast instead of
	// silently killing the standup subsystem until restart.
	go safeRun("standup-round-robin", m.broadcast, func() {
		m.runStandupRoundRobin(standupID, active, sessions, cfg)
	})

	return standupResult
}

// runStandupRoundRobin iterates through participants, sending prompts and
// waiting for responses with nudge/timeout logic.
func (m *Manager) runStandupRoundRobin(standupID string, participants []string, sessions SessionStore, cfg *Config) {
	// Capture the StandupState THIS goroutine owns. All shared-state mutations
	// below are guarded by pointer identity (m.standup == st), not just
	// "m.standup != nil": if this standup is stopped and a NEW one starts (or
	// the auto-standup tick fires) while this goroutine is parked in
	// waitForStandupResponse, the live m.standup becomes a different object —
	// without the identity check this goroutine would corrupt the new standup's
	// transcript/responses and prematurely mark it complete.
	m.mu.RLock()
	st := m.standup
	m.mu.RUnlock()
	if st == nil {
		return
	}

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
	if m.standup == st {
		m.standup.Transcript = append(m.standup.Transcript, openingMsg)
	}
	m.mu.Unlock()

	// Broadcast the opening message so frontend SSE listeners see it.
	m.broadcast("office:standupMessage", openingMsg)

	for i, sessionID := range participants {
		// Check if standup was cancelled OR replaced by a newer standup.
		m.mu.RLock()
		active := m.standup == st && m.standup.Active
		m.mu.RUnlock()
		if !active {
			break
		}

		m.mu.Lock()
		if m.standup == st {
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
		response, timedOut := m.waitForStandupResponse(sessionID, speakerName, sessions, cfg)

		msg := StandupMessage{
			SessionID:   sessionID,
			SpeakerName: speakerName,
			Content:     response,
			Type:        "status_report",
			Timestamp:   time.Now().UnixMilli(),
			TimedOut:    timedOut,
		}

		// Record response AND append to shared transcript so GetOfficeState can
		// return it -- but only if THIS standup is still the live one.
		m.mu.Lock()
		if m.standup == st {
			m.standup.Responses[sessionID] = response
			m.standup.Transcript = append(m.standup.Transcript, msg)
		}
		m.mu.Unlock()

		transcript = append(transcript, msg)
		m.broadcast("office:standupMessage", msg)
	}

	// Complete the standup -- only if THIS standup is still the live, active one.
	m.mu.RLock()
	stillActive := m.standup == st && m.standup.Active
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
		if m.standup == st {
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

		// R35 · Push one-line summary to Telegram so operators see
		// the progress beat without opening the dashboard. Respects
		// the StandupComplete notification preference inside the
		// notifier implementation; this call is a cheap no-op when
		// disconnected.
		if m.telegramNotifier != nil && m.telegramNotifier.IsConnected() {
			m.telegramNotifier.NotifyStandupComplete(buildTelegramStandupSummary(standupID, participants, transcript))
		}

		// Trigger leader task assignment by notifying strategic-tier agents.
		m.triggerLeaderTaskAssignment(standupID, participants, transcript)

		slog.Info("Standup completed", "id", standupID, "participants", len(participants))
	}
}

// waitForStandupResponse blocks until the agent responds via SubmitStandupResponse
// or the timeout expires. Nudges are sent at regular intervals.
// waitForStandupResponse returns (content, timedOut). timedOut is true when the
// content is a fallback (unreachable / aborted / nudges exhausted / timeout)
// rather than a real agent response.
func (m *Manager) waitForStandupResponse(sessionID, speakerName string, sessions SessionStore, cfg *Config) (string, bool) {
	agent := m.GetAgent(sessionID)
	if agent == nil {
		return fmt.Sprintf("%s was unreachable during standup.", speakerName), true
	}

	// R19 · Fresh waitSlot so late replies from a prior round can't
	// pollute this one. The defer finish() below is what makes
	// post-return submissions safe to drop.
	agent.mu.Lock()
	slot := newWaitSlot()
	agent.StandupSlot = slot
	agent.mu.Unlock()
	defer slot.finish()
	ch := slot.ch()

	timeout := time.NewTimer(cfg.StandupResponseTimeout)
	defer timeout.Stop()

	nudgeInterval := time.NewTicker(cfg.StandupNudgeInterval)
	defer nudgeInterval.Stop()

	nudgeCount := 0

	for {
		select {
		case response := <-ch:
			// R19 · Sentinel from abortMeetingsFacilitatedBy means the
			// standup leader died; return a clear fallback.
			if response == meetingAbortedSentinel {
				return "[standup aborted: facilitator unavailable]", true
			}
			return response, false

		case <-nudgeInterval.C:
			nudgeCount++
			if nudgeCount > cfg.StandupMaxNudges {
				slog.Warn("Standup: max nudges reached, giving up", "agent", speakerName)
				return fmt.Sprintf("%s did not respond in time.", speakerName), true
			}
			slog.Info("Standup: nudging agent", "name", speakerName, "nudge", nudgeCount, "max", cfg.StandupMaxNudges)

			// Send Ctrl+C then nudge prompt in a goroutine to keep select responsive.
			if sessions != nil {
				go safeRun("standup-nudge", m.broadcast, func() {
					sessions.WriteViaScreen(sessionID, "\x03")
					time.Sleep(2 * time.Second)
					shortNudge := strings.Join([]string{
						"[STANDUP REMINDER] You haven't responded to the standup yet. This is URGENT - please respond NOW.",
						"Reply by running this command IMMEDIATELY:",
						`cm standup respond "<your 2-3 sentence status update>"`,
					}, "\n")
					sessions.WriteInkPrompt(sessionID, shortNudge)
				})
			}

		case <-timeout.C:
			slog.Warn("Standup response timeout", "agent", speakerName)
			return fmt.Sprintf("%s did not respond in time.", speakerName), true
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
	// Even a late / duplicate / rejected submission is evidence the
	// agent is alive -- counts for the stuck detector.
	agent.TouchAgentAPI()

	sanitized := sanitizeForTerminal(content)

	agent.mu.Lock()
	slot := agent.StandupSlot
	agent.mu.Unlock()

	if slot == nil {
		if !active {
			slog.Warn("Late standup response, no active standup", "session", sessionID)
		}
		return false
	}

	if slot.deliver(sanitized) {
		slog.Info("Standup response received", "session", sessionID, "content_len", len(sanitized))
		return true
	}
	// Either the slot was finished (late reply) or the buffer was
	// full (duplicate). Count only the finished case since duplicates
	// are a distinct, less-interesting failure mode.
	if slot != nil && slot.closed.Load() {
		recordLateResponse("standup", sessionID)
	}
	return false
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
		// Use the structured flag set at the source, not substring matching the
		// free-text content (which miscounts a real update quoting the phrase).
		// Keep the legacy "(offline - skipped)" check for any older persisted data.
		if msg.TimedOut || msg.Content == "(offline - skipped)" {
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
		os.Remove(tmpPath) //nolint:errcheck
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

		// R19 · Fresh waitSlot per standup round — the goroutine below
		// defers finish() so late leader replies drop safely.
		agent.mu.Lock()
		slot := newWaitSlot()
		agent.LeaderTaskSlot = slot
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
		safego.Go("office.processLeaderTaskResponse", func() {
			m.processLeaderTaskResponse(slot, agent, agents, sessions, teamMission)
		})
	}
}

// processLeaderTaskResponse waits for the leader's task assignment response,
// parses it into per-agent tasks, injects task queues, and sends a summary.
func (m *Manager) processLeaderTaskResponse(slot *waitSlot, leader *AgentState, allAgents []*AgentState, sessions SessionStore, teamMission string) {
	// R19 · finish() drops late replies at the sender.
	defer slot.finish()

	// Wait up to 5 minutes for the leader to respond.
	timeout := time.NewTimer(5 * time.Minute)
	defer timeout.Stop()

	var response string
	select {
	case response = <-slot.ch():
		slog.Info("Leader task response received", "leader", leader.Config.Name, "len", len(response))
	case <-timeout.C:
		// 12h-unattended fallback: pre-fix, a leader that failed to
		// respond left every other agent idle until the next standup
		// (which itself could fail). Broadcast a "self-direct"
		// prompt to every non-leader agent so they keep working
		// against the team mission instead of sitting silent.
		slog.Warn("Leader task response timeout — broadcasting self-direct fallback",
			"leader", leader.Config.Name)
		m.broadcastSelfDirectFallback(allAgents, leader, sessions, teamMission)
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

// broadcastSelfDirectFallback runs when processLeaderTaskResponse
// hits its 5-min timeout without a parseable leader response. Sends
// each non-leader agent a "self-direct" prompt so they keep working
// against the team mission instead of idling for the rest of the
// standup interval.
//
// Pre-fix, a single unresponsive leader could waste the entire
// remaining 12h window — agents waited for tasks that never arrived
// and the next standup might fail the same way. This fallback keeps
// the team productive degraded rather than blocked.
//
// Broadcasts office:standupLeaderTimeout so operators see the
// degraded mode on the dashboard.
func (m *Manager) broadcastSelfDirectFallback(allAgents []*AgentState, leader *AgentState, sessions SessionStore, teamMission string) {
	if sessions == nil {
		return
	}
	missionLine := ""
	if teamMission != "" {
		missionLine = "\n[Team Mission] " + teamMission
	}
	prompt := "[SELF-DIRECT FALLBACK] The team lead didn't respond to the standup task-assignment prompt within 5 minutes. " +
		"To keep the team moving, pick the highest-value task from your own backlog " +
		"(use `cm task list` to inspect) and resume work. If you have nothing queued, " +
		"propose one concrete next step toward the team mission and start on it." + missionLine

	notified := 0
	for _, agent := range allAgents {
		if agent == nil || leader != nil && agent.SessionID == leader.SessionID {
			continue
		}
		if sessions.WriteInkPrompt(agent.SessionID, prompt) {
			notified++
		}
	}

	leaderName := ""
	if leader != nil && leader.Config != nil {
		leaderName = leader.Config.Name
	}
	m.broadcast("office:standupLeaderTimeout", map[string]interface{}{
		"leader":           leaderName,
		"selfDirectedAgents": notified,
		"timestamp":        time.Now().UnixMilli(),
	})
	slog.Info("Self-direct fallback broadcast complete",
		"leader", leaderName, "agentsNotified", notified)
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

// buildTelegramStandupSummary renders the end-of-standup summary
// pushed to Telegram after a standup finalises. Kept compact --
// header + one line per agent truncated to 200 chars of their
// response -- so it fits comfortably under Telegram's 4096 char
// cap even for 10-agent teams. R35.
func buildTelegramStandupSummary(standupID string, participants []string, transcript []StandupMessage) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<b>📋 Standup complete</b> (%d agents)\n",
		len(participants))

	// One-liner per agent from their status_report message. Fallback
	// "(no response)" when the agent timed out.
	reported := map[string]string{}
	for _, msg := range transcript {
		if msg.Type != "status_report" {
			continue
		}
		if msg.SpeakerName == "" {
			continue
		}
		// First line of the response, truncated.
		line := msg.Content
		if idx := strings.IndexByte(line, '\n'); idx > 0 {
			line = line[:idx]
		}
		if len(line) > 200 {
			line = line[:199] + "…"
		}
		reported[msg.SpeakerName] = strings.TrimSpace(line)
	}

	// Participants are session IDs in the transcript record; derive
	// speaker names by walking the transcript too, since the sessionID
	// list doesn't carry names. Collect unique ordered speakers.
	seen := map[string]bool{}
	for _, msg := range transcript {
		if msg.Type != "status_report" {
			continue
		}
		if seen[msg.SpeakerName] {
			continue
		}
		seen[msg.SpeakerName] = true
		line := reported[msg.SpeakerName]
		if line == "" {
			line = "(no response)"
		}
		fmt.Fprintf(&b, "• <b>%s</b>: %s\n",
			html.EscapeString(msg.SpeakerName),
			html.EscapeString(line))
	}
	return strings.TrimRight(b.String(), "\n")
}

// StandupDir is the exported directory of persisted standups.
func StandupDir() string { return standupDir() }

// StandupAudioPath returns the cache path for a standup's generated MP3.
func StandupAudioPath(id string) string {
	return filepath.Join(standupDir(), id+".mp3")
}

// LoadPersistedStandup reads one persisted standup by ID.
// The read is bounded to maxStandupFileBytes to prevent unbounded memory
// growth if a file is accidentally very large or corrupted (Fix 6b).
func LoadPersistedStandup(id string) (*PersistedStandup, error) {
	path := filepath.Join(standupDir(), id+".json")
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	lr := io.LimitReader(f, maxStandupFileBytes+1)
	data, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if len(data) > maxStandupFileBytes {
		return nil, errors.New("standup file exceeds size limit")
	}
	var ps PersistedStandup
	if err := json.Unmarshal(data, &ps); err != nil {
		return nil, err
	}
	return &ps, nil
}

// LatestCompletedStandupID returns the ID of the most-recently completed
// standup, or os.ErrNotExist if none exist.
//
// Implementation: sort directory entries by mtime descending (standups are
// written once at completion so mtime approximates CompletedAt), then parse
// only the newest file. If that file is corrupt, fall through to the next-
// newest. This parses 1 file in the common case instead of N (Fix 5a).
func LatestCompletedStandupID() (string, error) {
	entries, err := os.ReadDir(standupDir())
	if err != nil {
		return "", err
	}

	// Collect .json entries with their mtimes.
	type jsonEntry struct {
		id    string
		mtime int64
	}
	var candidates []jsonEntry
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		candidates = append(candidates, jsonEntry{
			id:    strings.TrimSuffix(e.Name(), ".json"),
			mtime: info.ModTime().UnixNano(),
		})
	}

	// Sort newest-mtime first.
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].mtime > candidates[j].mtime
	})

	// Parse candidates until one loads successfully.
	for _, c := range candidates {
		if _, err := LoadPersistedStandup(c.id); err == nil {
			return c.id, nil
		}
	}
	return "", os.ErrNotExist
}
