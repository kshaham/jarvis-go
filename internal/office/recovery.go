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
	m.monitorDone = make(chan struct{})
	ticker := m.idleTicker
	done := m.monitorDone
	m.mu.Unlock()

	go func() {
		tickCount := 0
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
			}

			tickCount++

			// Recover from panics in any monitoring function so the ticker
			// goroutine never dies. A crashed monitoring tick is logged and
			// the next tick runs normally.
			func() {
				defer func() {
					if r := recover(); r != nil {
						slog.Error("Panic in idle monitoring tick", "panic", r, "tick", tickCount)
					}
				}()

				m.checkSessionHealth()
				m.CleanupExpiredLocks()
				m.SweepBugTasks()
				m.SweepMeetings()
				// R20 · Force-end meetings still Active past 2h +
				// clear dangling bug assignments. Covers ghost state
				// from pre-R19 crashes and hard restarts.
				m.sweepStaleMeetings()
				m.sweepOrphanedBugs()
				m.autoCompleteIdleAgentTasks()
				m.checkUnacknowledgedTasks()
				m.checkStaleInProgressTasks()
				m.checkForStuckAgents()
				m.checkForWedgedWorkingAgents()
				m.CheckForIdleAgents()
				m.checkForCuriousAgents()
				m.checkForReflectionOpportunity()
				m.checkBlockedTasksForLeaderTriage()
				// R29 · Auto-reject pending handoffs whose receiver has
				// left the roster. Threshold can be overridden via
				// ~/.jarvis/office-memory/handoff-stale-seconds.txt.
				m.SweepStaleHandoffs(loadHandoffStaleThreshold())
				m.PersistMetrics()
				m.maybeTriggerWaterCoolerChat()
				// Ping the announced dev URL; after 3 consecutive failures
				// the health checker auto-clears the slot so teammates
				// stop being pointed at a dead server.
				m.CheckDevInstanceHealth()
				// Knowledge-library self-improvement sweep. Gated by
				// memifyInterval (~6h) so the disk walk + workflow
				// writes only fire a few times a day. Auto-promotes
				// high-reuse workflows to ~/.claude/skills/ via the
				// existing WriteSharedSkill path.
				m.maybeRunMemify()
			}()

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
	if m.monitorDone != nil {
		close(m.monitorDone)
		m.monitorDone = nil
	}
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
	m.standupDone = make(chan struct{})
	ticker := m.standupTimer
	done := m.standupDone
	m.mu.Unlock()

	// R19 · Outer loop wrapped in safeRun so one panic doesn't kill
	// the timer. Per-tick work runs inside another safeRun so a panic
	// during StartStandup is logged + broadcast and the ticker keeps
	// scheduling future ticks.
	go safeRun("auto-standup-timer", m.broadcast, func() {
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
			}
			safeRun("auto-standup-tick", m.broadcast, func() {
				m.mu.RLock()
				enabled := m.autoStandupEnabled
				standupActive := m.standup != nil && m.standup.Active
				paused := m.paused
				m.mu.RUnlock()

				if !enabled || standupActive {
					return
				}
				// R25 · Auto-standup is gated on pause too.
				if paused {
					return
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
					return
				}

				slog.Info("Auto-standup triggered")
				m.StartStandup(nil)
			})
		}
	})

	m.broadcast("office:autoStandupScheduled", map[string]interface{}{
		"intervalMs": m.config.AutoStandupInterval.Milliseconds(),
		"enabled":    true,
	})

	slog.Info("Auto-standup started", "interval", m.config.AutoStandupInterval)
}

// stopAutoStandup stops the auto-standup timer.
func (m *Manager) stopAutoStandup() {
	m.mu.Lock()
	if m.standupDone != nil {
		close(m.standupDone)
		m.standupDone = nil
	}
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

		// If the agent is actively in a turn, skip this cycle entirely:
		// the nudge prompt would just queue behind the current turn, and
		// the count would keep advancing toward auto-fail before the
		// agent ever saw it. Production logs observed 5 consecutive
		// nudges during a single 20-minute tool-heavy turn, resulting
		// in every queued task being auto-failed unfairly. We'll try
		// again on the next tick.
		if sessions != nil && sessions.IsSessionWorking(agent.SessionID) {
			continue
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
			// Mark a prompt as sent so the stuck-detector knows we've been
			// actively talking to this agent.
			agent.mu.Lock()
			agent.LastPromptSentAt = time.Now()
			agent.mu.Unlock()

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

		// If the SDK reports this session is currently in a turn, the
		// agent is working on SOMETHING -- we have no way to know if
		// it's this task specifically, but auto-completing a task out
		// from under a working agent is much worse than missing the
		// auto-complete for another tick. Observed 2026-04-18 01:41-
		// 01:43 UTC: Captain/Shield/Dash all had their in-progress
		// tasks auto-completed while still mid-turn working on them
		// ("taskAge=7m+" but the agents were producing task-progress
		// calls seconds later).
		if sessions != nil && sessions.IsSessionWorking(agent.SessionID) {
			continue
		}

		// Check if the agent has been idle (LastPromptSentAt indicates
		// last time we sent something -- if it was long ago and the task
		// has been running for a while, the agent is likely done).
		agent.mu.Lock()
		lastPrompt := agent.LastPromptSentAt
		lastCheck, hasCheck := agent.ProgressChecks[active.ID]
		lastAPI := agent.LastAgentAPIAt
		lastTurn := agent.LastTurnAt
		agent.mu.Unlock()

		// Task must have been in_progress for at least the threshold.
		baseline := active.StartedAt
		if baseline.IsZero() {
			baseline = active.AssignedAt
		}
		taskAge := now.Sub(baseline)
		if taskAge < autoCompleteThreshold {
			continue
		}

		// Grace window after a turn ends: the agent needs a few seconds
		// to emit task-complete / task-progress via the `cm` CLI post-
		// turn. Observed 2026-04-18 03:53:41-44 UTC: Ops turn ended,
		// auto-complete fired 3 seconds later before the agent could
		// make any follow-up API call.
		if !lastTurn.IsZero() && now.Sub(lastTurn) < 30*time.Second {
			continue
		}

		// A recent API call from the agent (task-progress, self-add,
		// etc.) proves they're still engaged. Skip the auto-complete.
		if !lastAPI.IsZero() && now.Sub(lastAPI) < 2*time.Minute {
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

		// Don't poke an agent mid-turn. They can't read the progress
		// prompt until their current turn ends; sending it anyway just
		// piles another message on the SDK queue while the task-age
		// counter advances and eventually escalates to "urgent".
		// Observed 2026-04-18 03:23-03:33 UTC: Shield received 7
		// progress checks at 5-min intervals culminating in 3 urgent
		// checks (taskAge 15/20/25m) on a single in-progress task,
		// while actively working the whole time.
		if sessions != nil && sessions.IsSessionWorking(agent.SessionID) {
			continue
		}

		staleBaseline := active.StartedAt
		if staleBaseline.IsZero() {
			staleBaseline = active.AssignedAt
		}
		taskAge := now.Sub(staleBaseline)
		if taskAge < gentleThreshold {
			continue
		}

		agent.mu.Lock()
		lastCheck, hasCheck := agent.ProgressChecks[active.ID]
		lastAPI := agent.LastAgentAPIAt
		lastTurn := agent.LastTurnAt
		agent.mu.Unlock()

		// Grace window after a turn ends: the agent may be about to
		// emit task-progress or task-complete. Don't interrupt with a
		// progress-check prompt for 30s.
		if !lastTurn.IsZero() && now.Sub(lastTurn) < 30*time.Second {
			continue
		}

		// Recent API call from the agent (task-progress, task-ack,
		// self-add, etc.) is proof they're still engaged. Skip the
		// progress check; they'll report naturally.
		if !lastAPI.IsZero() && now.Sub(lastAPI) < 2*time.Minute {
			continue
		}

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

// checkForWedgedWorkingAgents stops SDK sessions that have been
// reporting "working" for longer than WedgedWorkingTimeout without
// emitting any tool activity or EventTokens. This is the complement
// of checkForStuckAgents — that one only looks at *non-working*
// agents (the SDK flag is trusted) and cannot catch a wedged subprocess
// that's stuck between turns. R18's turn cap doesn't help here either
// because NumTurns only ticks at turn boundaries.
//
// Rarely trips in healthy operation. When it does, the agent gets
// stopped the same way an operator-initiated kill does, and an
// office:agentIdleStopped event fires for the dashboard.
func (m *Manager) checkForWedgedWorkingAgents() {
	m.mu.RLock()
	cfg := m.config
	sessions := m.sessions
	teamSpawnedAt := m.teamSpawnedAt
	m.mu.RUnlock()

	if cfg.WedgedWorkingTimeout <= 0 || sessions == nil {
		return
	}
	now := time.Now()
	// Respect the same grace period as stuck-detection — a freshly
	// spawned team is legitimately silent for a minute or two.
	if !teamSpawnedAt.IsZero() && now.Sub(teamSpawnedAt) < cfg.StuckGracePeriod {
		return
	}

	for _, agent := range m.GetRoster() {
		if agent == nil || agent.Config == nil {
			continue
		}
		// Only act on sessions that self-report as "working". Non-
		// working agents are already covered by checkForStuckAgents.
		if !sessions.IsSessionWorking(agent.SessionID) {
			continue
		}
		agent.mu.Lock()
		lastTurn := agent.LastTurnAt
		lastAPI := agent.LastAgentAPIAt
		lastPrompt := agent.LastPromptSentAt
		agent.mu.Unlock()

		// lastActivity = most recent signal from the agent OR the
		// last prompt we sent it (the prompt itself proves it was
		// reachable at that moment).
		lastActivity := lastPrompt
		if lastTurn.After(lastActivity) {
			lastActivity = lastTurn
		}
		if lastAPI.After(lastActivity) {
			lastActivity = lastAPI
		}
		if lastActivity.IsZero() {
			// Agent spawned but hasn't made or received a turn yet —
			// fall back to the spawn timestamp so we don't infinitely
			// forgive a session that was born wedged.
			lastActivity = teamSpawnedAt
		}
		if lastActivity.IsZero() {
			continue
		}

		idle := now.Sub(lastActivity)
		if idle < cfg.WedgedWorkingTimeout {
			continue
		}

		agentName := agent.SessionID
		if agent.Config != nil {
			agentName = agent.Config.Name
		}
		slog.Warn("SDK session wedged in working state beyond timeout; stopping",
			"session", agent.SessionID, "name", agentName,
			"idle", idle.Round(time.Second).String(),
			"timeout", cfg.WedgedWorkingTimeout.String())

		if err := sessions.StopSession(agent.SessionID); err != nil {
			slog.Warn("failed to stop wedged session", "session", agent.SessionID, "err", err)
			// Fall through — still broadcast so the dashboard flags
			// the agent even if the stop didn't take.
		}
		// R21 code review (third pass): claim the stop-broadcast slot
		// so we don't double-toast if OfficeAgentKill or another
		// wedge-tick won this race. Ordering note: broadcast BEFORE
		// UnregisterAgent so the dashboard receives the flash+reason
		// event while the card is still on screen, then the card
		// removes on the subsequent office:memberLeft.
		if agent.ClaimStopBroadcast() {
			m.broadcast("office:agentIdleStopped", map[string]interface{}{
				"sessionId": agent.SessionID,
				"agentName": agentName,
				"idleMs":    idle.Milliseconds(),
				"timeoutMs": cfg.WedgedWorkingTimeout.Milliseconds(),
			})
		}
		// UnregisterAgent triggers the same HandleAgentDeath path used
		// by R18's operator kill, so task reassignment + dev-instance
		// cleanup still runs.
		m.UnregisterAgent(agent.SessionID)
	}
}

// checkForStuckAgents iterates over all registered agents and attempts
// recovery for those that have been inactive too long after receiving a prompt.
func (m *Manager) checkForStuckAgents() {
	// R25 · Paused team doesn't get recovery nudges. Agents that
	// look stuck mid-pause are just idling per the operator's
	// intent — not worth the recovery prompt (which would wake
	// them back up mid-pause).
	if m.IsPaused() {
		return
	}
	now := time.Now()

	m.mu.RLock()
	agents := make([]*AgentState, 0, len(m.agents))
	for _, a := range m.agents {
		agents = append(agents, a)
	}
	sessions := m.sessions
	cfg := m.config
	teamSpawnedAt := m.teamSpawnedAt
	m.mu.RUnlock()

	// Grace period: don't run stuck-detection for the first 3 minutes after
	// a team spawn. Agents are still processing their initial kickstart
	// and may take a minute or two of silent "thinking" before they emit
	// their first events. Firing recovery during that window was auto-
	// killing otherwise-healthy agents (observed 2026-04-17).
	if !teamSpawnedAt.IsZero() && now.Sub(teamSpawnedAt) < cfg.StuckGracePeriod {
		return
	}

	for _, agent := range agents {
		if agent.Config == nil {
			continue
		}
		// If the session reports it's currently executing a turn, the
		// agent is by definition not stuck -- it's just thinking. The
		// SDK's working flag is ground truth; trust it over any
		// heuristic timestamp. Observed 2026-04-18 00:53:48: Captain
		// was flagged "inactive=5m6s" after a successful standup
		// response, only because he was silently working through a
		// long multi-step turn between that response and the next
		// API call.
		if sessions != nil && sessions.IsSessionWorking(agent.SessionID) {
			continue
		}

		agent.mu.Lock()
		lastPromptAt := agent.LastPromptSentAt
		lastTurnAt := agent.LastTurnAt
		lastAPIAt := agent.LastAgentAPIAt
		agent.mu.Unlock()

		// "Last activity" is the most recent of:
		//   - LastPromptSentAt: last prompt WE sent to the agent
		//   - LastTurnAt: last turn the agent completed (EventTokens)
		//   - LastAgentAPIAt: last API call FROM the agent (task-ack,
		//     task-start, task-complete, task-progress, self-add, etc.)
		//
		// LastTurnAt alone was insufficient: a multi-step tool-heavy
		// turn doesn't emit EventTokens until it finishes, so an agent
		// making dozens of successful task-progress + task-complete
		// calls inside one long turn looked "inactive=5m0s" to the
		// stuck detector. Observed 2026-04-17 23:37:54: all 5 agents
		// flagged stuck seconds after POSTing task-complete.
		lastActive := lastPromptAt
		if lastTurnAt.After(lastActive) {
			lastActive = lastTurnAt
		}
		if lastAPIAt.After(lastActive) {
			lastActive = lastAPIAt
		}

		if lastActive.IsZero() {
			continue
		}

		// Check inactivity duration against threshold.
		inactiveDuration := now.Sub(lastActive)
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

		// Backoff: suppress recovery if we already sent one recently for this
		// agent. Tier 2 requires 30s since last recovery; Tier 3 requires 60s.
		// Prevents spamming a persistently stuck agent on every idle tick.
		//
		// R27 concurrency review · Previously this did RLock → read →
		// Unlock → executeRecoveryTier → Lock → write. Under the single
		// idle-ticker goroutine that's safe, but if a second caller is
		// ever added (manual API, test hook) the window between check
		// and write opens a TOCTOU where two callers both pass the
		// backoff gate and double-recover the same agent. Now we take
		// a single Lock, test, and immediately mark lastRecoveryAt so
		// any concurrent caller sees the recovery as in-flight before
		// we even send the prompt.
		m.mu.Lock()
		lastRecovery := m.lastRecoveryAt[agent.SessionID]
		var backoff time.Duration
		switch tier {
		case RecoveryTier2:
			backoff = 30 * time.Second
		case RecoveryTier3:
			backoff = 60 * time.Second
		}
		if !lastRecovery.IsZero() && backoff > 0 && time.Since(lastRecovery) < backoff {
			m.mu.Unlock()
			// Roll back the attempt speculatively consumed above: a
			// backoff-suppressed tick must be a true no-op, otherwise the
			// escalation counter is exhausted on backoff-skipped ticks and the
			// Tier-2/Tier-3 recovery prompts (and the stuck/auto-kill alert)
			// never fire for a genuinely stuck agent.
			agent.mu.Lock()
			if agent.RecoveryAttempts > 0 {
				agent.RecoveryAttempts--
			}
			agent.mu.Unlock()
			continue
		}
		m.lastRecoveryAt[agent.SessionID] = time.Now()
		m.mu.Unlock()

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
		// Tier 2: Ctrl+C, wait 3s, send recovery prompt. Exit early if monitor stops.
		sessions.WriteViaScreen(sessionID, "\x03")
		m.mu.RLock()
		monitorDone := m.monitorDone
		m.mu.RUnlock()
		// R21 code review: wrap in safeRun so a panic inside
		// WriteViaScreen (e.g., nil PTY after session tear-down race)
		// broadcasts office:panicRecovered instead of taking the whole
		// process down.
		go safeRun("recovery-tier2", m.broadcast, func() {
			select {
			case <-time.After(3 * time.Second):
			case <-monitorDone:
				return
			}
			if sessions == nil || !sessions.IsSessionAlive(sessionID) {
				return
			}
			recoveryPrompt := strings.Join([]string{
				"[RECOVERY] You appear to be stuck. Please resume your current task.",
				"If you were waiting for input, check your task list:",
				"",
				`Report your status: cm standup respond "<brief status>"`,
			}, "\n")
			sessions.WriteViaScreen(sessionID, recoveryPrompt+"\r")
		})

	case RecoveryTier3:
		// Tier 3: Ctrl+C, wait 3s, send diagnostic prompt. Exit early if monitor stops.
		sessions.WriteViaScreen(sessionID, "\x03")
		m.mu.RLock()
		monitorDone := m.monitorDone
		m.mu.RUnlock()
		// R21 code review: same panic-recovery wrap as Tier 2.
		go safeRun("recovery-tier3", m.broadcast, func() {
			select {
			case <-time.After(3 * time.Second):
			case <-monitorDone:
				return
			}
			if sessions == nil || !sessions.IsSessionAlive(sessionID) {
				return
			}
			diagnosticPrompt := strings.Join([]string{
				"[DIAGNOSTIC] Recovery check - please report your current status.",
				"1. What were you last working on?",
				"2. Are you blocked on anything?",
				"3. Do you need help from another team member?",
				"",
				`Reply via: cm standup respond "<your status report>"`,
			}, "\n")
			sessions.WriteViaScreen(sessionID, diagnosticPrompt+"\r")
		})
	}
}

// broadcastStuckAlert emits a stuck alert after all recovery tiers exhausted.
// If AutoKillAfterStuck is enabled (default), the session is also stopped so
// it doesn't sit idle burning quota while the operator is away.
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

	// Auto-kill to stop burning tokens. Honors the config toggle so ops
	// users can opt out when they need to post-mortem a stuck agent.
	if m.config.AutoKillAfterStuck {
		m.broadcast("office:agentKilled", map[string]interface{}{
			"sessionId": sessionID,
			"agentName": agentName,
			"reason":    "stuck after all recovery tiers",
		})
		// HandleAgentDeath cleans up the agent state and reassigns its tasks.
		// The pty session itself is stopped through its normal exit path when
		// the respawn controller stops driving it.
		m.HandleAgentDeath(sessionID)
		slog.Warn("Auto-killed stuck agent", "agent", agentName, "session", sessionID)
	}
}

// CheckForIdleAgents looks for agents with empty task queues and no active work,
// then attempts to find them new work via brainstorm or self-replenishment.
func (m *Manager) CheckForIdleAgents() {
	if m.IsWrappingUp() {
		return // Don't assign new work during wrap-up
	}
	if m.IsPaused() {
		return // R25 · Paused team doesn't get replenishment prompts.
	}
	if !m.IsPerpetualMode() {
		return // Goal mode: don't force-replenish, agents stop when tasks are done
	}
	m.mu.RLock()
	sessions := m.sessions
	m.mu.RUnlock()
	roster := m.GetRoster()
	for _, agent := range roster {
		if agent.Config == nil {
			continue
		}
		// Skip if the agent is currently processing a turn. A
		// replenishment prompt sent mid-turn just queues behind the
		// active work, and before the agent reads it the next tick
		// fires another one on top (observed 2026-04-17 22:08-22:09:
		// Captain / Dash / Stack / Shield each got two replenishment
		// prompts 45s apart without being able to respond to the
		// first). Same class as the ack-nudge fix in 83c9377.
		if sessions != nil && sessions.IsSessionWorking(agent.SessionID) {
			continue
		}
		// Also skip if the agent is already in an active meeting.
		// Starting a second brainstorm for the same idle agent while
		// they have one pending in the SDK queue just doubles up and
		// both time out.
		if m.IsInActiveMeeting(agent.SessionID) {
			continue
		}
		// Skip if agent still has enough work queued (more than 2 remaining).
		pendingCount := agent.PendingTaskCount()
		totalWork := pendingCount
		if agent.ActiveTask() != nil {
			totalWork++
		}
		if totalWork > 1 {
			continue // Still has work queued
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
		// Only pick leaders that aren't busy with their own tasks AND
		// aren't currently in a turn (a prompt sent to a mid-turn
		// facilitator just queues; the brainstorm times out at 5 min
		// without a response. Observed 2026-04-18 04:02:59: Ops +
		// Stack brainstorm where Stack was working -> Ops ended up
		// facilitator -> timeout).
		leader := m.findStrategicAgent(agent.SessionID)
		if leader != nil && leader.ActiveTask() == nil && leader.PendingTaskCount() == 0 &&
			!sessions.IsSessionWorking(leader.SessionID) {
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
		// findAvailableAgent now also filters by IsSessionWorking.
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

// findAvailableAgent returns an agent that has no active task, is not
// currently in a turn, and is not the given session. Prefers
// role-affinity matches when the requesting agent has a known role in
// the roleAffinity map.
func (m *Manager) findAvailableAgent(excludeSessionID string) *AgentState {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sessions := m.sessions

	// Look up the requesting agent's role for affinity matching.
	var requestingRole string
	if requester, ok := m.agents[excludeSessionID]; ok && requester.Config != nil {
		requestingRole = requester.Config.Role
	}

	preferredRoles := RoleAffinityPartners(requestingRole)

	// Helper: check if agent is truly idle (no active or pending tasks
	// AND not currently executing a turn). Sending a brainstorm prompt
	// to a mid-turn agent queues behind their current work; they can't
	// respond, and the 5-min timeout is the only resolution.
	isIdle := func(a *AgentState) bool {
		if a.ActiveTask() != nil || a.PendingTaskCount() > 1 {
			return false
		}
		if sessions != nil && sessions.IsSessionWorking(a.SessionID) {
			return false
		}
		return true
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

	roleName := ""
	if agent.Config != nil {
		roleName = agent.Config.Role
	}

	prompt := fmt.Sprintf(`[URGENT: FIND MORE WORK NOW] Your task queue is empty. You must NOT be idle.
%s
As a %s, scan the codebase RIGHT NOW and create tasks for yourself:

1. Run 'git log --oneline -20' to see recent changes -- review for bugs or improvements
2. Search for TODOs/FIXMEs: grep -r "TODO\|FIXME" . --include="*.go" --include="*.js" --include="*.ts"
3. Run the test suite and fix any failures
4. Check 'cm roster' to see what teammates are doing -- avoid overlapping work
5. Look for code relevant to your role that could be improved

CREATE TASKS NOW using these commands:
  cm task self-add "Review recent commits for bugs" --priority high
  cm task self-add "Write tests for untested paths" --priority medium
  cm task self-add "Refactor and improve code quality" --priority medium

Then immediately start working: cm task ack <id> && cm task start <id>

DO NOT WAIT. Create at least 2 tasks and start one NOW.`, missionLine, roleName)

	m.mu.RLock()
	sessions := m.sessions
	m.mu.RUnlock()

	if sessions != nil {
		sessions.WriteInkPrompt(agent.SessionID, prompt)
		agent.mu.Lock()
		agent.LastPromptSentAt = time.Now()
		agent.mu.Unlock()
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

	// Collect the set of live task IDs across every agent so we can reclaim
	// triage-map entries for tasks that no longer exist (completed, failed,
	// or owned by a deleted agent). This is a defense in depth -- the
	// direct path in UpdateTaskStatus only fires when a known task changes
	// status, but drifts from agent deletions still happen.
	liveTaskIDs := make(map[string]struct{}, 32)
	for _, agent := range roster {
		for _, t := range agent.GetTasks() {
			// Only blocked tasks participate in triage; prune anything that
			// moved on.
			if t.Status == TaskBlocked {
				liveTaskIDs[t.ID] = struct{}{}
			}
		}
	}
	m.SweepTriageMaps(liveTaskIDs)

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

			// Check triage cooldown (5 min between attempts per task).
			m.mu.Lock()
			attempts := m.triageAttempts[task.ID]
			lastAt := m.lastTriageAt[task.ID]
			m.mu.Unlock()

			if !lastAt.IsZero() && time.Since(lastAt) < 5*time.Minute {
				continue
			}

			if attempts >= maxAttempts {
				// Auto-fail the task after max triage attempts.
				agent.UpdateTaskStatus(task.ID, TaskFailed)
				m.mu.Lock()
				delete(m.triageAttempts, task.ID)
				delete(m.lastTriageAt, task.ID)
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
			m.lastTriageAt[task.ID] = time.Now()
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
	if m.IsWrappingUp() {
		return
	}
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
		// Don't poke a session that's actively in a turn -- the
		// curiosity prompt would just queue behind the current work.
		if sessions.IsSessionWorking(agent.SessionID) {
			continue
		}
		// Also skip if the agent is already in an active meeting
		// (brainstorm). Observed 2026-04-18 03:42:29: one
		// idle-monitoring tick started a brainstorm for Ops (with
		// Captain as facilitator) AND immediately queued a curiosity
		// prompt to Ops, doubling up on an already-pending meeting
		// prompt in the SDK queue.
		if m.IsInActiveMeeting(agent.SessionID) {
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
		agent.LastPromptSentAt = time.Now()
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
	if m.IsWrappingUp() {
		return
	}
	// 30% probability.
	if rand.Float64() > 0.3 {
		return
	}

	m.mu.RLock()
	lastChat := m.lastWaterCoolerAt
	cooldown := m.config.WaterCoolerCooldown
	sessions := m.sessions
	// Don't start a water-cooler chat during an active standup: agents
	// who haven't taken their turn yet would get the chat prompt
	// queued in front of their standup prompt, delaying the whole
	// round-robin. Observed 2026-04-17 23:25:34 firing Shield<>Dash
	// while Captain (index 1) was still responding.
	standupActive := m.standup != nil && m.standup.Active
	m.mu.RUnlock()

	if standupActive {
		return
	}

	// Check cooldown.
	if !lastChat.IsZero() && time.Since(lastChat) < cooldown {
		return
	}

	if sessions == nil {
		return
	}

	// Find idle agents (no active task, no pending tasks, NOT mid-turn,
	// NOT already in a brainstorm or other active meeting).
	roster := m.GetRoster()
	var idle []*AgentState
	for _, agent := range roster {
		if agent.ActiveTask() != nil || agent.PendingTaskCount() != 0 {
			continue
		}
		if sessions.IsSessionWorking(agent.SessionID) {
			continue
		}
		if m.IsInActiveMeeting(agent.SessionID) {
			continue
		}
		idle = append(idle, agent)
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
	agentA.mu.Lock()
	agentA.LastPromptSentAt = time.Now()
	agentA.mu.Unlock()
	agentB.mu.Lock()
	agentB.LastPromptSentAt = time.Now()
	agentB.mu.Unlock()

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
	if m.IsWrappingUp() {
		return
	}
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
		// Skip agents mid-turn: a reflection prompt stacked behind an
		// in-flight turn just gets consumed as "another message" once
		// the turn completes, losing the contemplative context.
		if sessions.IsSessionWorking(agent.SessionID) {
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
			"If you discovered a reusable approach, create a shared skill:",
			`cm skill create "<skill-name>" "<detailed description of the technique>"`,
			"",
			"Save your current work context to memory for next session:",
			`cm memory write "<summary of what you worked on, key decisions, and next steps>"`,
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
