package office

import (
	"fmt"
	"html"
	"strings"
	"time"
)

// TelegramNotifier abstracts Telegram message sending for the office manager.
type TelegramNotifier interface {
	// SendOfficeUpdate sends a formatted HTML message to the configured chat.
	SendOfficeUpdate(text string)
	// IsConnected returns whether Telegram is configured and active.
	IsConnected() bool
}

// PendingLeaderQuery tracks a leader clarification request waiting for a
// Telegram reply. It expires after ExpiresAt to prevent stale queries from
// capturing unrelated messages.
type PendingLeaderQuery struct {
	SessionID string
	Question  string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// SetTelegramNotifier sets the notifier used to send office updates to Telegram.
func (m *Manager) SetTelegramNotifier(notifier TelegramNotifier) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.telegramNotifier = notifier
}

// SetTelegramConfig enables or disables periodic Telegram updates and sets
// the update interval. If enabled and a team is spawned, it starts the
// periodic update goroutine. If disabled, it stops any running goroutine.
func (m *Manager) SetTelegramConfig(enabled bool, intervalMin int) {
	m.mu.Lock()
	wasEnabled := m.telegramEnabled
	m.telegramEnabled = enabled
	interval := time.Duration(intervalMin) * time.Minute
	if interval < 10*time.Minute {
		interval = 10 * time.Minute
	}
	m.updateInterval = interval
	teamSpawned := m.teamSpawned
	m.mu.Unlock()

	if enabled && !wasEnabled && teamSpawned {
		m.startPeriodicUpdates()
	} else if !enabled && wasEnabled {
		m.StopTelegramUpdates()
	} else if enabled && wasEnabled {
		// Interval changed -- restart ticker
		m.StopTelegramUpdates()
		m.startPeriodicUpdates()
	}
}

// GetTelegramConfig returns the current Telegram update configuration.
func (m *Manager) GetTelegramConfig() (enabled bool, intervalMinutes int, lastUpdate time.Time) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.telegramEnabled, int(m.updateInterval.Minutes()), m.lastTelegramUpdate
}

// StopTelegramUpdates stops the periodic update goroutine if running.
func (m *Manager) StopTelegramUpdates() {
	m.mu.Lock()
	stopCh := m.updateStopCh
	m.updateStopCh = nil
	m.updateTicker = nil
	m.mu.Unlock()

	if stopCh != nil {
		close(stopCh)
	}
}

// startPeriodicUpdates launches a goroutine that sends sprint summaries at
// the configured interval via the TelegramNotifier.
func (m *Manager) startPeriodicUpdates() {
	m.mu.Lock()
	if m.telegramNotifier == nil || !m.telegramNotifier.IsConnected() {
		m.mu.Unlock()
		return
	}
	m.updateTicker = time.NewTicker(m.updateInterval)
	m.updateStopCh = make(chan struct{})
	ticker := m.updateTicker
	stopCh := m.updateStopCh
	m.mu.Unlock()

	go func() {
		for {
			select {
			case <-ticker.C:
				summary := m.buildSprintSummary()
				m.mu.RLock()
				notifier := m.telegramNotifier
				m.mu.RUnlock()
				if notifier != nil {
					notifier.SendOfficeUpdate(summary)
				}
				m.mu.Lock()
				m.lastTelegramUpdate = time.Now()
				m.mu.Unlock()
			case <-stopCh:
				ticker.Stop()
				return
			}
		}
	}()
}

// buildSprintSummary generates an HTML-formatted sprint summary from the
// current agent state, tasks, bug tasks, and meetings.
func (m *Manager) buildSprintSummary() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var b strings.Builder
	b.WriteString("<b>Office Sprint Update</b>\n")

	// Uptime
	if !m.teamSpawnedAt.IsZero() {
		uptime := time.Since(m.teamSpawnedAt)
		hours := int(uptime.Hours())
		minutes := int(uptime.Minutes()) % 60
		if hours > 0 {
			fmt.Fprintf(&b, "Uptime: %dh %dm\n", hours, minutes)
		} else {
			fmt.Fprintf(&b, "Uptime: %dm\n", minutes)
		}
	}

	// Team status
	b.WriteString("\n<b>Team Status</b>\n")
	if len(m.agents) == 0 {
		b.WriteString("- No agents registered\n")
	}
	for _, agent := range m.agents {
		if agent.Config == nil {
			continue
		}
		name := agent.Config.Name
		role := agent.Config.Role
		active := agent.ActiveTask()
		if active != nil {
			fmt.Fprintf(&b, "- %s (%s): working -- %s\n",
				html.EscapeString(name),
				html.EscapeString(role),
				html.EscapeString(active.Title))
		} else {
			fmt.Fprintf(&b, "- %s (%s): idle\n",
				html.EscapeString(name),
				html.EscapeString(role))
		}
	}

	// Completed tasks
	completedCount := 0
	inProgressTasks := make([]string, 0)
	for _, agent := range m.agents {
		if agent.Config == nil {
			continue
		}
		tasks := agent.GetTasks()
		for _, t := range tasks {
			if t.Status == TaskCompleted {
				completedCount++
			}
			if t.Status == TaskInProgress {
				inProgressTasks = append(inProgressTasks, fmt.Sprintf("%s (%s)",
					html.EscapeString(t.Title),
					html.EscapeString(agent.Config.Name)))
			}
		}
	}

	fmt.Fprintf(&b, "\n<b>Completed</b>\n- %d tasks completed\n", completedCount)

	if len(inProgressTasks) > 0 {
		b.WriteString("\n<b>In Progress</b>\n")
		for _, t := range inProgressTasks {
			fmt.Fprintf(&b, "- %s\n", t)
		}
	}

	// Bug tasks
	openBugs := 0
	resolvedBugs := 0
	for _, bt := range m.bugTasks {
		if bt.Status == "open" {
			openBugs++
		} else {
			resolvedBugs++
		}
	}
	if openBugs > 0 || resolvedBugs > 0 {
		fmt.Fprintf(&b, "\n<b>Bug Tasks</b>\n- %d open, %d resolved\n", openBugs, resolvedBugs)
	}

	// Active meetings
	activeMeetings := 0
	for _, mtg := range m.meetings {
		if mtg.Active {
			activeMeetings++
		}
	}
	if activeMeetings > 0 {
		fmt.Fprintf(&b, "\n<b>Active Meetings</b>: %d\n", activeMeetings)
	}

	result := b.String()
	// Cap at 3900 bytes (Telegram message limit with some margin).
	// Truncate at rune boundary to avoid splitting multi-byte UTF-8 characters.
	if len(result) > 3900 {
		runes := []rune(result)
		truncated := string(runes)
		for len(truncated) > 3900 {
			runes = runes[:len(runes)-1]
			truncated = string(runes)
		}
		result = truncated
	}
	return result
}

// NotifyOfficeEvent sends a brief event notification via TelegramNotifier
// if Telegram updates are enabled.
func (m *Manager) NotifyOfficeEvent(event, detail string) {
	m.mu.RLock()
	enabled := m.telegramEnabled
	notifier := m.telegramNotifier
	m.mu.RUnlock()

	if !enabled || notifier == nil || !notifier.IsConnected() {
		return
	}

	text := fmt.Sprintf("<b>%s</b>", html.EscapeString(event))
	if detail != "" {
		text += "\n" + html.EscapeString(detail)
	}
	notifier.SendOfficeUpdate(text)
}

// SetPendingLeaderQuery sets a pending clarification query from the team
// leader, typically forwarded to Telegram for user response.
func (m *Manager) SetPendingLeaderQuery(sessionID, question string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pendingLeaderQuery = &PendingLeaderQuery{
		SessionID: sessionID,
		Question:  question,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(10 * time.Minute),
	}
}

// HasPendingLeaderQuery returns true if the team leader has an outstanding
// clarification request that has not expired.
func (m *Manager) HasPendingLeaderQuery() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.pendingLeaderQuery == nil {
		return false
	}
	if time.Now().After(m.pendingLeaderQuery.ExpiresAt) {
		return false
	}
	return true
}

// GetPendingLeaderSessionID returns the session ID of the leader agent
// that issued the pending query. Returns "" if no query is pending.
func (m *Manager) GetPendingLeaderSessionID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.pendingLeaderQuery == nil {
		return ""
	}
	return m.pendingLeaderQuery.SessionID
}

// ClearPendingLeaderQuery removes the pending leader query, typically
// called after the user has replied via Telegram.
func (m *Manager) ClearPendingLeaderQuery() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pendingLeaderQuery = nil
}

// IsTeamActive returns true if a team is currently spawned and running.
// Implements the OfficeBridge interface for the Telegram handler.
func (m *Manager) IsTeamActive() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.teamSpawned
}
