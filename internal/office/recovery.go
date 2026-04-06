package office

import (
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"time"
)

// AgentReplenishmentEvent is emitted when a self-replenishment prompt is sent.
type AgentReplenishmentEvent struct {
	SessionID string
	AgentName string
	Type      string // "brainstorm" or "self-replenishment"
}

func (AgentReplenishmentEvent) officeEventType() string { return "agentReplenishment" }

// RecoveryTier represents the escalation level for stuck agent recovery.
type RecoveryTier int

const (
	RecoveryTier1 RecoveryTier = 1 // Enter key
	RecoveryTier2 RecoveryTier = 2 // Ctrl+C + recovery prompt
	RecoveryTier3 RecoveryTier = 3 // Ctrl+C + diagnostic prompt
)

// AgentRecoveryEvent is emitted when a stuck agent recovery is attempted.
type AgentRecoveryEvent struct {
	SessionID          string
	AgentName          string
	Attempt            int
	MaxAttempts        int
	Tier               RecoveryTier
	InactiveDurationMs int64
}

func (AgentRecoveryEvent) officeEventType() string { return "agentRecovery" }

// AgentStuckEvent is emitted when all recovery tiers are exhausted.
type AgentStuckEvent struct {
	SessionID          string
	AgentName          string
	InactiveDurationMs int64
	Attempts           int
}

func (AgentStuckEvent) officeEventType() string { return "agentStuck" }

// StartIdleMonitoring starts a background goroutine that periodically checks
// for stuck agents. It also starts the auto-standup timer if enabled.
func (m *Manager) StartIdleMonitoring() {
	m.mu.Lock()
	if m.idleTicker != nil {
		m.mu.Unlock()
		return
	}
	m.idleTicker = time.NewTicker(m.config.IdleCheckInterval)
	ticker := m.idleTicker
	m.mu.Unlock()

	go func() {
		tickCount := 0
		for range ticker.C {
			tickCount++
			m.checkSessionHealth()
			m.CleanupExpiredLocks()
			m.SweepBugTasks()
			m.autoCompleteIdleAgentTasks()
			m.checkUnacknowledgedTasks()
			m.checkStaleInProgressTasks()
			m.checkForStuckAgents()
			m.CheckForIdleAgents()
			m.checkForCuriousAgents()
			m.checkForReflectionOpportunity()
			m.checkBlockedTasksForLeaderTriage()
			m.PersistMetrics()
			m.maybeTriggerWaterCoolerChat()

			if tickCount%5 == 0 {
				roster := m.GetRoster()
				active := 0
				for _, a := range roster {
					if a.ActiveTask() != nil {
						active++
					}
				}
				m.broadcast("office:tickerMessage", map[string]interface{}{
					"message":   fmt.Sprintf("Team running: %d/%d agents active", active, len(roster)),
					"timestamp": time.Now().UnixMilli(),
				})
			}
		}
	}()

	m.startAutoStandup()

	slog.Info("Idle monitoring started", "interval", m.config.IdleCheckInterval)
}

// StopIdleMonitoring stops the idle monitoring goroutine and auto-standup timer.
func (m *Manager) StopIdleMonitoring() {
	m.mu.Lock()
	if m.idleTicker != nil {
		m.idleTicker.Stop()
		m.idleTicker = nil
	}
	m.mu.Unlock()

	m.stopAutoStandup()

	slog.Info("Idle monitoring stopped")
}

// startAutoStandup sets up the recurring standup timer.
func (m *Manager) startAutoStandup() {
	m.mu.Lock()
	if m.standupTimer != nil {
		m.mu.Unlock()
		return
	}
	if !m.autoStandupEnabled {
		m.mu.Unlock()
		return
	}

	m.standupTimer = time.NewTicker(m.config.AutoStandupInterval)
	ticker := m.standupTimer
	m.mu.Unlock()

	go func() {
		for range ticker.C {
			m.mu.RLock()
			enabled := m.autoStandupEnabled
			standupActive := m.standup != nil && m.standup.Active
			m.mu.RUnlock()

			if !enabled || standupActive {
				continue
			}

			// Check for active brainstorm meetings.
			hasBrainstorm := false
			m.mu.RLock()
			for _, mtg := range m.meetings {
				if mtg.Active && mtg.Type == "brainstorm" {
					hasBrainstorm = true
					break
				}
			}
			m.mu.RUnlock()

			if hasBrainstorm {
				slog.Info("Auto-standup skipped: brainstorm meeting in progress")
				continue
			}

			slog.Info("Auto-standup triggered")
			m.StartStandup(nil)
		}
	}()

	m.broadcast("office:autoStandupScheduled", map[string]interface{}{
		"intervalMs": m.config.AutoStandupInterval.Milliseconds(),
		"enabled":    true,
	})

	slog.Info("Auto-standup started", "interval", m.config.AutoStandupInterval)
}

// stopAutoStandup stops the auto-standup timer.
func (m *Manager) stopAutoStandup() {
	m.mu.Lock()
	if m.standupTimer != nil {
		m.standupTimer.Stop()
		m.standupTimer = nil
	}
	m.mu.Unlock()
}

// checkUnacknowledgedTasks nudges agents that have pending tasks older than
// TaskAckTimeout. After TaskAckMaxNudges nudges, the task is auto-failed.
func (m *Manager) checkUnacknowledgedTasks() {
	now := time.Now()

	m.mu.RLock()
	agents := make([]*AgentState, 0, len(m.agents))
	for _, a := range m.agents {
		agents = append(agents, a)
	}
	sessions := m.sessions
	cfg := m.config
	m.mu.RUnlock()

	for _, agent := range agents {
		if agent.Config == nil {
			continue
		}
		tasks := agent.GetTasks()

		// Collect pending tasks that are overdue for acknowledgment.
		var overdueTasks []*Task
		for _, t := range tasks {
			if t.Status == TaskPending && !t.AssignedAt.IsZero() && now.Sub(t.AssignedAt) > cfg.TaskAckTimeout {
				overdueTasks = append(overdueTasks, t)
			}
		}
		if len(overdueTasks) == 0 {
			continue
		}

		agentName := agent.SessionID
		if agent.Config != nil {
			agentName = agent.Config.Name
		}

		// Separate tasks into nudge-able and auto-fail.
		var nudgeTasks []*Task
		var failTasks []*Task

		agent.mu.Lock()
		for _, t := range overdueTasks {
			state, ok := agent.AckNudges[t.ID]
			if !ok {
				state = &AckNudgeState{}
				agent.AckNudges[t.ID] = state
			}
			if state.Count >= cfg.TaskAckMaxNudges {
				failTasks = append(failTasks, t)
			} else {
				state.Count++
				state.LastNudgeAt = now
				nudgeTasks = append(nudgeTasks, t)
			}
		}
		agent.mu.Unlock()

		// Send nudge prompt for tasks that still have nudge budget.
		if len(nudgeTasks) > 0 && sessions != nil {
			var lines []string
			lines = append(lines, "[TASK REMINDER] You have unacknowledged tasks that need your attention:")
			for _, t := range nudgeTasks {
				lines = append(lines, fmt.Sprintf("- %s (id: %s)", t.Title, t.ID))
			}
			lines = append(lines, "Acknowledge each task NOW: cm task ack <taskId>")
			lines = append(lines, "Then start working: cm task start <taskId>")
			nudgePrompt := strings.Join(lines, "\n")
			sessions.WriteInkPrompt(agent.SessionID, nudgePrompt)

			slog.Info("Sent task ack nudge", "agent", agentName, "taskCount", len(nudgeTasks))
		}

		// Auto-fail tasks that exceeded max nudges.
		for _, t := range failTasks {
			agent.UpdateTaskStatus(t.ID, TaskFailed)

			slog.Warn("Auto-failed unacknowledged task",
				"agent", agentName,
				"taskId", t.ID,
				"title", t.Title,
				"nudges", cfg.TaskAckMaxNudges)

			m.broadcast("office:taskUpdated", map[string]interface{}{
				"sessionId": agent.SessionID,
				"taskId":    t.ID,
				"status":    string(TaskFailed),
				"reason":    "exceeded max ack nudges",
			})

			// Clean up nudge state for failed task.
			agent.mu.Lock()
			delete(agent.AckNudges, t.ID)
			agent.mu.Unlock()
		}
	}
}

// autoCompleteIdleAgentTasks auto-completes in_progress tasks that appear
// stale (>5 min with no progress updates) and starts the next pending task.
func (m *Manager) autoCompleteIdleAgentTasks() {
	now := time.Now()
	const autoCompleteThreshold = 5 * time.Minute

	m.mu.RLock()
	agents := make([]*AgentState, 0, len(m.agents))
	for _, a := range m.agents {
		agents = append(agents, a)
	}
	sessions := m.sessions
	m.mu.RUnlock()

	for _, agent := range agents {
		if agent.Config == nil {
			continue
		}
		active := agent.ActiveTask()
		if active == nil {
			continue
		}

		// Check if the agent has been idle (LastPromptSentAt indicates
		// last time we sent something -- if it was long ago and the task
		// has been running for a while, the agent is likely done).
		agent.mu.Lock()
		lastPrompt := agent.LastPromptSentAt
		lastCheck, hasCheck := agent.ProgressChecks[active.ID]
		agent.mu.Unlock()

		// Task must have been in_progress for at least the threshold.
		taskAge := now.Sub(active.AssignedAt)
		if taskAge < autoCompleteThreshold {
			continue
		}

		// The agent should appear idle: last prompt was sent more than
		// 30s ago (meaning no recent interaction).
		if !lastPrompt.IsZero() && now.Sub(lastPrompt) < 30*time.Second {
			continue
		}

		// If we recently did a progress check and got no response, that
		// is a stronger signal. But if there has been no progress check at
		// all, be conservative and skip.
		if !hasCheck {
			continue
		}

		// Only auto-complete if the last progress check was at least 2 min
		// ago (giving the agent time to respond).
		if now.Sub(lastCheck) < 2*time.Minute {
			continue
		}

		agentName := agent.SessionID
		if agent.Config != nil {
			agentName = agent.Config.Name
		}

		// Auto-complete the current task.
		agent.UpdateTaskStatus(active.ID, TaskCompleted)
		slog.Info("Auto-completed idle agent task",
			"agent", agentName,
			"taskId", active.ID,
			"title", active.Title,
			"taskAge", taskAge)

		m.broadcast("office:taskUpdated", map[string]interface{}{
			"sessionId": agent.SessionID,
			"taskId":    active.ID,
			"status":    string(TaskCompleted),
			"reason":    "auto-completed (idle agent)",
		})

		// Clean up progress check state for completed task.
		agent.mu.Lock()
		delete(agent.ProgressChecks, active.ID)
		agent.mu.Unlock()

		// Start the next pending task if available.
		if sessions != nil {
			tasks := agent.GetTasks()
			for _, t := range tasks {
				if t.Status == TaskPending || t.Status == TaskAcknowledged {
					startPrompt := fmt.Sprintf("[AUTO-START] Your previous task was completed. Starting next task:\ncm task start %s", t.ID)
					sessions.WriteInkPrompt(agent.SessionID, startPrompt)
					slog.Info("Auto-started next task", "agent", agentName, "taskId", t.ID, "title", t.Title)
					break
				}
			}
		}
	}
}

// checkStaleInProgressTasks sends gentle and urgent progress check prompts
// to agents whose in_progress tasks have been running too long.
func (m *Manager) checkStaleInProgressTasks() {
	now := time.Now()
	const gentleThreshold = 5 * time.Minute
	const urgentThreshold = 15 * time.Minute

	m.mu.RLock()
	agents := make([]*AgentState, 0, len(m.agents))
	for _, a := range m.agents {
		agents = append(agents, a)
	}
	sessions := m.sessions
	m.mu.RUnlock()

	for _, agent := range agents {
		if agent.Config == nil {
			continue
		}
		active := agent.ActiveTask()
		if active == nil {
			continue
		}

		taskAge := now.Sub(active.AssignedAt)
		if taskAge < gentleThreshold {
			continue
		}

		agent.mu.Lock()
		lastCheck, hasCheck := agent.ProgressChecks[active.ID]
		agent.mu.Unlock()

		agentName := agent.SessionID
		if agent.Config != nil {
			agentName = agent.Config.Name
		}

		if taskAge >= urgentThreshold && hasCheck && now.Sub(lastCheck) > gentleThreshold {
			// Urgent prompt: task has been running >15 min and last check was >5 min ago.
			duration := taskAge.Round(time.Second).String()
			urgentPrompt := strings.Join([]string{
				fmt.Sprintf(`[URGENT] Task "%s" has been in progress for %s.`, active.Title, duration),
				fmt.Sprintf(`Are you blocked? Use: cm task blocked %s "<reason>"`, active.ID),
				fmt.Sprintf(`Or complete it: cm task complete %s`, active.ID),
			}, "\n")

			if sessions != nil {
				sessions.WriteInkPrompt(agent.SessionID, urgentPrompt)
			}

			agent.mu.Lock()
			agent.ProgressChecks[active.ID] = now
			agent.mu.Unlock()

			slog.Warn("Sent urgent progress check",
				"agent", agentName,
				"taskId", active.ID,
				"title", active.Title,
				"taskAge", taskAge)

		} else if !hasCheck || now.Sub(lastCheck) > gentleThreshold {
			// Gentle prompt: task has been running >5 min and no recent check.
			gentlePrompt := strings.Join([]string{
				fmt.Sprintf(`[PROGRESS CHECK] How's your progress on "%s"?`, active.Title),
				fmt.Sprintf(`Report: cm task progress %s "<brief update>"`, active.ID),
			}, "\n")

			if sessions != nil {
				sessions.WriteInkPrompt(agent.SessionID, gentlePrompt)
			}

			agent.mu.Lock()
			agent.ProgressChecks[active.ID] = now
			agent.mu.Unlock()

			slog.Info("Sent gentle progress check",
				"agent", agentName,
				"taskId", active.ID,
				"title", active.Title,
				"taskAge", taskAge)
		}
	}
}

// checkSessionHealth compares registered agents against alive sessions.
// Any agent whose session is no longer alive gets auto-cleaned via HandleAgentDeath.
func (m *Manager) checkSessionHealth() {
	m.mu.RLock()
	sessions := m.sessions
	agentIDs := make([]string, 0, len(m.agents))
	for sid := range m.agents {
		agentIDs = append(agentIDs, sid)
	}
	m.mu.RUnlock()

	if sessions == nil {
		return
	}

	for _, sid := range agentIDs {
		if !sessions.IsSessionAlive(sid) {
			slog.Warn("Agent session no longer alive, cleaning up", "session", sid)
			m.HandleAgentDeath(sid)
		}
	}
}

// checkForStuckAgents iterates over all registered agents and attempts
// recovery for those that have been inactive too long after receiving a prompt.
func (m *Manager) checkForStuckAgents() {
	now := time.Now()

	m.mu.RLock()
	agents := make([]*AgentState, 0, len(m.agents))
	for _, a := range m.agents {
		agents = append(agents, a)
	}
	sessions := m.sessions
	cfg := m.config
	m.mu.RUnlock()

	for _, agent := range agents {
		if agent.Config == nil {
			continue
		}
		agent.mu.Lock()
		lastPromptAt := agent.LastPromptSentAt
		agent.mu.Unlock()

		if lastPromptAt.IsZero() {
			continue
		}

		// Check inactivity duration against threshold.
		inactiveDuration := now.Sub(lastPromptAt)
		if inactiveDuration < cfg.StuckThreshold {
			continue
		}

		// Check recovery attempt limits.
		agent.mu.Lock()
		if !agent.RecoveryWindowStart.IsZero() && now.Sub(agent.RecoveryWindowStart) > cfg.RecoveryWindowDuration {
			// Reset window.
			agent.RecoveryAttempts = 0
			agent.RecoveryWindowStart = now
		}
		if agent.RecoveryWindowStart.IsZero() {
			agent.RecoveryWindowStart = now
		}
		if agent.RecoveryAttempts >= cfg.RecoveryMaxAttempts {
			agent.mu.Unlock()
			continue
		}
		agent.RecoveryAttempts++
		attemptCount := agent.RecoveryAttempts
		agent.mu.Unlock()

		agentName := agent.SessionID
		if agent.Config != nil {
			agentName = agent.Config.Name
		}

		tier := getRecoveryTier(attemptCount)
		slog.Warn("Stuck agent recovery",
			"tier", tier,
			"attempt", attemptCount,
			"max", cfg.RecoveryMaxAttempts,
			"agent", agentName,
			"inactive", inactiveDuration)

		m.executeRecoveryTier(sessions, agent.SessionID, tier)

		if tier == RecoveryTier3 {
			m.broadcast("office:agentRestarted", map[string]interface{}{
				"sessionId": agent.SessionID,
				"name":      agentName,
				"tier":      int(tier),
			})
		}

		m.broadcast("office:agentRecovery", map[string]interface{}{
			"sessionId":          agent.SessionID,
			"agentName":          agentName,
			"attempt":            attemptCount,
			"maxAttempts":        cfg.RecoveryMaxAttempts,
			"tier":               tier,
			"inactiveDurationMs": inactiveDuration.Milliseconds(),
		})
		m.emitEvent(AgentRecoveryEvent{
			SessionID:          agent.SessionID,
			AgentName:          agentName,
			Attempt:            attemptCount,
			MaxAttempts:        cfg.RecoveryMaxAttempts,
			Tier:               tier,
			InactiveDurationMs: inactiveDuration.Milliseconds(),
		})

		// If all tiers exhausted, broadcast stuck alert and notify.
		if attemptCount >= cfg.RecoveryMaxAttempts {
			m.broadcastStuckAlert(agent.SessionID, agentName, inactiveDuration)
		}
	}
}

// getRecoveryTier determines recovery tier from attempt count.
func getRecoveryTier(attemptCount int) RecoveryTier {
	if attemptCount <= 1 {
		return RecoveryTier1
	}
	if attemptCount <= 2 {
		return RecoveryTier2
	}
	return RecoveryTier3
}

// executeRecoveryTier performs the recovery action for the given tier.
func (m *Manager) executeRecoveryTier(sessions SessionStore, sessionID string, tier RecoveryTier) {
	switch tier {
	case RecoveryTier1:
		// Tier 1: Send Enter key.
		sessions.WriteViaScreen(sessionID, "\r")

	case RecoveryTier2:
		// Tier 2: Ctrl+C, wait 3s, send recovery prompt.
		sessions.WriteViaScreen(sessionID, "\x03")
		go func() {
			time.Sleep(3 * time.Second)
			recoveryPrompt := strings.Join([]string{
				"[RECOVERY] You appear to be stuck. Please resume your current task.",
				"If you were waiting for input, check your task list:",
				"",
				`Report your status: cm standup respond "<brief status>"`,
			}, "\n")
			sessions.WriteViaScreen(sessionID, recoveryPrompt+"\r")
		}()

	case RecoveryTier3:
		// Tier 3: Ctrl+C, wait 3s, send diagnostic prompt.
		sessions.WriteViaScreen(sessionID, "\x03")
		go func() {
			time.Sleep(3 * time.Second)
			diagnosticPrompt := strings.Join([]string{
				"[DIAGNOSTIC] Recovery check - please report your current status.",
				"1. What were you last working on?",
				"2. Are you blocked on anything?",
				"3. Do you need help from another team member?",
				"",
				`Reply via: cm standup respond "<your status report>"`,
			}, "\n")
			sessions.WriteViaScreen(sessionID, diagnosticPrompt+"\r")
		}()
	}
}

// broadcastStuckAlert emits a stuck alert after all recovery tiers exhausted.
func (m *Manager) broadcastStuckAlert(sessionID, agentName string, inactiveDuration time.Duration) {
	m.broadcast("office:agentStuck", map[string]interface{}{
		"sessionId":          sessionID,
		"agentName":          agentName,
		"inactiveDurationMs": inactiveDuration.Milliseconds(),
		"timestamp":          time.Now().UnixMilli(),
	})
	m.emitEvent(AgentStuckEvent{
		SessionID:          sessionID,
		AgentName:          agentName,
		InactiveDurationMs: inactiveDuration.Milliseconds(),
	})

	// Notify via Telegram if available.
	m.NotifyOfficeEvent("Agent Stuck",
		fmt.Sprintf("%s has been inactive for %s after %d recovery attempts",
			agentName,
			inactiveDuration.Round(time.Second),
			m.config.RecoveryMaxAttempts))

	slog.Warn("All recovery tiers exhausted", "agent", agentName)
}

// CheckForIdleAgents looks for agents with empty task queues and no active work,
// then attempts to find them new work via brainstorm or self-replenishment.
func (m *Manager) CheckForIdleAgents() {
	roster := m.GetRoster()
	for _, agent := range roster {
		if agent.Config == nil {
			continue
		}
		// Skip if agent still has enough work queued (more than 2 remaining).
		pendingCount := agent.PendingTaskCount()
		totalWork := pendingCount
		if agent.ActiveTask() != nil {
			totalWork++
		}
		if totalWork > 2 {
			continue // Still has enough work queued
		}

		// Check replenishment cooldown per agent.
		agent.mu.Lock()
		lastReplenish := agent.LastReplenishmentAt
		agent.mu.Unlock()

		m.mu.RLock()
		cooldown := m.config.ReplenishCooldown
		m.mu.RUnlock()

		if !lastReplenish.IsZero() && time.Since(lastReplenish) < cooldown {
			continue
		}

		// Record this replenishment attempt.
		agent.mu.Lock()
		agent.LastReplenishmentAt = time.Now()
		agent.mu.Unlock()

		agentName := agent.SessionID
		if agent.Config != nil {
			agentName = agent.Config.Name
		}
		slog.Info("Agent running low on tasks, finding work", "name", agentName, "remaining", totalWork)

		// Strategy 1: Brainstorm with a strategic-tier (captain/leader) agent.
		// Only pick leaders that aren't busy with their own tasks.
		leader := m.findStrategicAgent(agent.SessionID)
		if leader != nil && leader.ActiveTask() == nil && leader.PendingTaskCount() == 0 {
			meeting := m.StartBrainstorm([]string{agent.SessionID, leader.SessionID})
			if meeting != nil {
				leaderName := leader.SessionID
				if leader.Config != nil {
					leaderName = leader.Config.Name
				}
				slog.Info("Started brainstorm for idle agent with leader",
					"agent", agentName, "leader", leaderName)
				continue
			}
		}

		// Strategy 2: Brainstorm with any available (non-busy) agent.
		partner := m.findAvailableAgent(agent.SessionID)
		if partner != nil {
			meeting := m.StartBrainstorm([]string{agent.SessionID, partner.SessionID})
			if meeting != nil {
				partnerName := partner.SessionID
				if partner.Config != nil {
					partnerName = partner.Config.Name
				}
				slog.Info("Started brainstorm for idle agent with partner",
					"agent", agentName, "partner", partnerName)
				continue
			}
		}

		// Strategy 3: Self-replenishment prompt sent directly to the idle agent.
		m.sendSelfReplenishmentPrompt(agent)
	}
}

// findStrategicAgent returns the first strategic-tier agent that is not the
// given session.
func (m *Manager) findStrategicAgent(excludeSessionID string) *AgentState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, agent := range m.agents {
		if agent.SessionID != excludeSessionID && agent.Config != nil && agent.Config.Tier == "strategic" {
			return agent
		}
	}
	return nil
}

// findAvailableAgent returns an agent that has no active task and is not the
// given session. Prefers role-affinity matches when the requesting agent has
// a known role in the roleAffinity map.
func (m *Manager) findAvailableAgent(excludeSessionID string) *AgentState {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Look up the requesting agent's role for affinity matching.
	var requestingRole string
	if requester, ok := m.agents[excludeSessionID]; ok && requester.Config != nil {
		requestingRole = requester.Config.Role
	}

	preferredRoles := RoleAffinityPartners(requestingRole)

	// Helper: check if agent is truly idle (no active or pending tasks).
	isIdle := func(a *AgentState) bool {
		return a.ActiveTask() == nil && a.PendingTaskCount() <= 1
	}

	// First pass: find an affinity-matched available agent.
	if len(preferredRoles) > 0 {
		for _, preferredRole := range preferredRoles {
			for _, agent := range m.agents {
				if agent.SessionID != excludeSessionID &&
					isIdle(agent) &&
					agent.Config != nil &&
					agent.Config.Role == preferredRole {
					return agent
				}
			}
		}
	}

	// Fallback: any available agent.
	for _, agent := range m.agents {
		if agent.SessionID != excludeSessionID && isIdle(agent) {
			return agent
		}
	}
	return nil
}

// sendSelfReplenishmentPrompt sends an idle agent a prompt to find more work
// on their own when no brainstorm partner is available.
func (m *Manager) sendSelfReplenishmentPrompt(agent *AgentState) {
	mission := m.GetTeamMission()
	missionLine := ""
	if mission != "" {
		missionLine = fmt.Sprintf("\nTeam Mission: %s\n", mission)
	}

	prompt := fmt.Sprintf(`[FIND MORE WORK] You've completed all assigned tasks. Great work!
%s
Look for more ways to contribute to the project:
1. Review your teammates' recent commits for issues
2. Look for TODOs, FIXMEs, or technical debt in the codebase
3. Write tests for untested code paths
4. Improve documentation
5. Check for security issues or performance optimizations
6. Refactor code that could be cleaner or more efficient

Use 'cm task self-add "<title>" "<description>"' to add tasks to your queue.
Use 'cm message <sessionId> "<content>"' to coordinate with teammates.
Use 'cm roster' to see your team.`, missionLine)

	m.mu.RLock()
	sessions := m.sessions
	m.mu.RUnlock()

	if sessions != nil {
		sessions.WriteViaScreen(agent.SessionID, prompt+"\r")
	}

	agentName := agent.SessionID
	if agent.Config != nil {
		agentName = agent.Config.Name
	}

	m.broadcast("office:agentReplenishment", map[string]interface{}{
		"sessionId": agent.SessionID,
		"name":      agentName,
		"type":      "self-replenishment",
	})
	m.emitEvent(AgentReplenishmentEvent{
		SessionID: agent.SessionID,
		AgentName: agentName,
		Type:      "self-replenishment",
	})

	slog.Info("Sent self-replenishment prompt", "name", agentName)
}

// ---------------------------------------------------------------------------
// Tier 2: Blocked Task Triage
// ---------------------------------------------------------------------------

// checkBlockedTasksForLeaderTriage finds blocked tasks and asks a strategic-tier
// agent (leader) to triage them. After BlockedTriageMaxAttempts failed attempts,
// the task is auto-failed.
func (m *Manager) checkBlockedTasksForLeaderTriage() {
	roster := m.GetRoster()

	m.mu.RLock()
	sessions := m.sessions
	maxAttempts := m.config.BlockedTriageMaxAttempts
	m.mu.RUnlock()

	if sessions == nil {
		return
	}

	for _, agent := range roster {
		if agent.Config == nil {
			continue
		}
		agentName := agent.Config.Name

		tasks := agent.GetTasks()
		for _, task := range tasks {
			if task.Status != TaskBlocked {
				continue
			}

			// Check triage attempt count.
			m.mu.Lock()
			attempts := m.triageAttempts[task.ID]
			m.mu.Unlock()

			if attempts >= maxAttempts {
				// Auto-fail the task after max triage attempts.
				agent.UpdateTaskStatus(task.ID, TaskFailed)
				m.mu.Lock()
				delete(m.triageAttempts, task.ID)
				m.mu.Unlock()

				slog.Warn("Auto-failed blocked task after max triage attempts",
					"taskId", task.ID, "title", task.Title, "agent", agentName,
					"attempts", attempts)

				m.broadcast("office:taskAutoFailed", map[string]interface{}{
					"taskId":    task.ID,
					"title":     task.Title,
					"sessionId": agent.SessionID,
					"reason":    "max triage attempts exceeded",
				})
				continue
			}

			// Find a strategic-tier agent to triage.
			leader := m.findStrategicAgent(agent.SessionID)
			if leader == nil {
				continue
			}

			leaderName := leader.SessionID
			if leader.Config != nil {
				leaderName = leader.Config.Name
			}

			triagePrompt := fmt.Sprintf(
				"[BLOCKED TASK TRIAGE] %s is blocked on '%s': %s. "+
					"Reassign: `cm task triage %s %s <reason>`",
				agentName, task.Title, task.Description,
				task.ID, agent.SessionID)

			sessions.WriteInkPrompt(leader.SessionID, triagePrompt)

			m.mu.Lock()
			m.triageAttempts[task.ID] = attempts + 1
			m.mu.Unlock()

			slog.Info("Sent blocked task triage to leader",
				"taskId", task.ID, "title", task.Title,
				"agent", agentName, "leader", leaderName,
				"attempt", attempts+1)
		}
	}
}

// ---------------------------------------------------------------------------
// Tier 2: Curiosity Prompts
// ---------------------------------------------------------------------------

// CuriosityEvent is emitted when a curiosity prompt is sent to an agent.
type CuriosityEvent struct {
	SessionID string
	AgentName string
}

func (CuriosityEvent) officeEventType() string { return "curiosityTriggered" }

// checkForCuriousAgents sends curiosity prompts to idle agents with no tasks.
func (m *Manager) checkForCuriousAgents() {
	m.mu.RLock()
	enabled := m.curiosityEnabled
	cooldown := m.config.CuriosityCooldown
	sessions := m.sessions
	m.mu.RUnlock()

	if !enabled || sessions == nil {
		return
	}

	roster := m.GetRoster()
	for _, agent := range roster {
		if agent.Config == nil {
			continue
		}
		// Only target agents with no active task and no pending tasks.
		if agent.ActiveTask() != nil {
			continue
		}
		if agent.PendingTaskCount() > 0 {
			continue
		}

		// Check curiosity cooldown.
		agent.mu.Lock()
		lastCuriosity := agent.LastCuriosityAt
		agent.mu.Unlock()

		if !lastCuriosity.IsZero() && time.Since(lastCuriosity) < cooldown {
			continue
		}

		agentName := agent.SessionID
		if agent.Config != nil {
			agentName = agent.Config.Name
		}

		curiosityPrompt := strings.Join([]string{
			"[CURIOSITY] You have no tasks queued. Take this time to explore the codebase and find improvements:",
			"1. Look for performance bottlenecks or inefficiencies",
			"2. Check for security vulnerabilities or missing input validation",
			"3. Find missing tests or untested edge cases",
			"4. Identify code smells, duplication, or technical debt",
			"5. Review recent commits for potential issues",
			"",
			`Report findings: cm discover "<summary>" --findings "<item1>,<item2>"`,
			`Add tasks for yourself: cm task self-add "<title>" "<description>"`,
		}, "\n")

		sessions.WriteInkPrompt(agent.SessionID, curiosityPrompt)

		agent.mu.Lock()
		agent.LastCuriosityAt = time.Now()
		agent.mu.Unlock()

		m.metrics.TotalCuriosity.Add(1)

		m.broadcast("office:curiosityTriggered", map[string]interface{}{
			"sessionId": agent.SessionID,
			"agentName": agentName,
		})
		m.emitEvent(CuriosityEvent{
			SessionID: agent.SessionID,
			AgentName: agentName,
		})

		slog.Info("Sent curiosity prompt", "agent", agentName)
	}
}

// ---------------------------------------------------------------------------
// Tier 3: Water Cooler Chat
// ---------------------------------------------------------------------------

// WaterCoolerEvent is emitted when a water cooler chat is triggered.
type WaterCoolerEvent struct {
	SessionIDA string
	SessionIDB string
}

func (WaterCoolerEvent) officeEventType() string { return "waterCoolerChat" }

// maybeTriggerWaterCoolerChat has a 30% chance of pairing two idle agents
// for an informal chat about the codebase. Respects a cooldown period.
func (m *Manager) maybeTriggerWaterCoolerChat() {
	// 30% probability.
	if rand.Float64() > 0.3 {
		return
	}

	m.mu.RLock()
	lastChat := m.lastWaterCoolerAt
	cooldown := m.config.WaterCoolerCooldown
	sessions := m.sessions
	m.mu.RUnlock()

	// Check cooldown.
	if !lastChat.IsZero() && time.Since(lastChat) < cooldown {
		return
	}

	if sessions == nil {
		return
	}

	// Find idle agents (no active task, no pending tasks).
	roster := m.GetRoster()
	var idle []*AgentState
	for _, agent := range roster {
		if agent.ActiveTask() == nil && agent.PendingTaskCount() == 0 {
			idle = append(idle, agent)
		}
	}

	if len(idle) < 2 {
		return
	}

	// Pick 2 distinct random agents.
	idxA := rand.Intn(len(idle))
	idxB := rand.Intn(len(idle) - 1)
	if idxB >= idxA {
		idxB++
	}
	agentA := idle[idxA]
	agentB := idle[idxB]

	nameA := agentA.SessionID
	if agentA.Config != nil {
		nameA = agentA.Config.Name
	}
	nameB := agentB.SessionID
	if agentB.Config != nil {
		nameB = agentB.Config.Name
	}

	promptA := fmt.Sprintf(
		"[WATER COOLER] Quick chat with %s! Share something interesting you've noticed in the codebase or suggest an improvement.",
		nameB)
	promptB := fmt.Sprintf(
		"[WATER COOLER] Quick chat with %s! Share something interesting you've noticed in the codebase or suggest an improvement.",
		nameA)

	sessions.WriteInkPrompt(agentA.SessionID, promptA)
	sessions.WriteInkPrompt(agentB.SessionID, promptB)

	m.mu.Lock()
	m.lastWaterCoolerAt = time.Now()
	m.mu.Unlock()

	m.broadcast("office:waterCoolerChat", map[string]interface{}{
		"agentA": nameA,
		"agentB": nameB,
	})
	m.emitEvent(WaterCoolerEvent{
		SessionIDA: agentA.SessionID,
		SessionIDB: agentB.SessionID,
	})

	slog.Info("Water cooler chat triggered", "agentA", nameA, "agentB", nameB)
}

// ---------------------------------------------------------------------------
// Tier 3: Post-Task Reflection
// ---------------------------------------------------------------------------

// ReflectionEvent is emitted when a reflection prompt is sent to an agent.
type ReflectionEvent struct {
	SessionID string
	AgentName string
}

func (ReflectionEvent) officeEventType() string { return "reflectionTriggered" }

// checkForReflectionOpportunity sends a reflection prompt to agents that
// just completed all their tasks and haven't reflected recently.
func (m *Manager) checkForReflectionOpportunity() {
	now := time.Now()

	m.mu.RLock()
	sessions := m.sessions
	cooldown := m.config.ReflectionCooldown
	m.mu.RUnlock()

	if sessions == nil {
		return
	}

	roster := m.GetRoster()
	for _, agent := range roster {
		if agent.Config == nil {
			continue
		}

		// Skip agents that still have active or pending work.
		if agent.ActiveTask() != nil {
			continue
		}
		if agent.PendingTaskCount() > 0 {
			continue
		}

		tasks := agent.GetTasks()
		if len(tasks) == 0 {
			continue
		}

		// Check whether any task was completed within the last 2 minutes.
		recentlyCompleted := false
		for _, t := range tasks {
			if t.Status == TaskCompleted && !t.CompletedAt.IsZero() &&
				now.Sub(t.CompletedAt) <= 2*time.Minute {
				recentlyCompleted = true
				break
			}
		}
		if !recentlyCompleted {
			continue
		}

		// Check reflection cooldown.
		agent.mu.Lock()
		lastReflection := agent.LastReflectionAt
		agent.mu.Unlock()

		if !lastReflection.IsZero() && now.Sub(lastReflection) < cooldown {
			continue
		}

		agentName := agent.SessionID
		if agent.Config != nil {
			agentName = agent.Config.Name
		}

		reflectionPrompt := strings.Join([]string{
			"[REFLECTION] You just completed your tasks. Take a moment to reflect:",
			"1. What technique or approach worked well?",
			"2. What pitfalls did you encounter?",
			"3. What patterns did you notice in the codebase?",
			"4. Did you discover any reusable techniques worth sharing?",
			"",
			`Report: cm reflect "<your learning>" --category <category>`,
			"Categories: technique, pitfall, pattern, tool_usage, codebase_insight, collaboration",
			"",
			"If you discovered a reusable approach, create a skill:",
			`cm skill create "<skill-name>" "<detailed description of the technique>"`,
		}, "\n")

		sessions.WriteInkPrompt(agent.SessionID, reflectionPrompt)

		agent.mu.Lock()
		agent.LastReflectionAt = now
		agent.mu.Unlock()

		m.broadcast("office:reflectionTriggered", map[string]interface{}{
			"sessionId": agent.SessionID,
			"agentName": agentName,
		})
		m.emitEvent(ReflectionEvent{
			SessionID: agent.SessionID,
			AgentName: agentName,
		})

		slog.Info("Sent reflection prompt", "agent", agentName)
	}
}
