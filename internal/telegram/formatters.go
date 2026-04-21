package telegram

import (
	"fmt"
	"html"
	"strings"
	"time"
)

// FormatSessionStart formats a session start notification.
func FormatSessionStart(name, workingDir string) string {
	return fmt.Sprintf(
		"<b>Session Started</b>\n<code>%s</code> in <code>%s</code>",
		html.EscapeString(name),
		html.EscapeString(workingDir),
	)
}

// FormatSessionComplete formats a session completion notification.
func FormatSessionComplete(name string, duration time.Duration) string {
	return fmt.Sprintf(
		"<b>Session Completed</b>\n<code>%s</code> after %s",
		html.EscapeString(name),
		humanDuration(duration),
	)
}

// FormatSessionError formats a session error notification.
func FormatSessionError(name, errMsg string) string {
	return fmt.Sprintf(
		"<b>Session Error</b>\n<code>%s</code>\n\n%s",
		html.EscapeString(name),
		html.EscapeString(errMsg),
	)
}

// FormatPermissionPrompt formats a permission prompt notification.
func FormatPermissionPrompt(sessionShortID, tool string, args map[string]interface{}) string {
	var sb strings.Builder
	sb.WriteString("<b>Permission Needed</b>\n")
	sb.WriteString(fmt.Sprintf("Session <code>%s</code>\n\n", html.EscapeString(sessionShortID)))
	sb.WriteString(fmt.Sprintf("Tool: <b>%s</b>", html.EscapeString(tool)))
	if len(args) > 0 {
		sb.WriteString("\n")
		for k, v := range args {
			valStr := fmt.Sprintf("%v", v)
			if len(valStr) > 200 {
				valStr = valStr[:200] + "..."
			}
			sb.WriteString(fmt.Sprintf("\n<code>%s</code>: %s", html.EscapeString(k), html.EscapeString(valStr)))
		}
	}
	return sb.String()
}

// FormatElicitation formats an elicitation dialog notification.
func FormatElicitation(sessionShortID, question string) string {
	return fmt.Sprintf(
		"<b>Input Requested</b>\nSession <code>%s</code>\n\n%s",
		html.EscapeString(sessionShortID),
		html.EscapeString(question),
	)
}

// FormatStuckWarning formats a stuck state warning.
func FormatStuckWarning(sessionShortID, state string, durationMs int64) string {
	dur := time.Duration(durationMs) * time.Millisecond
	return fmt.Sprintf(
		"<b>Stuck State Warning</b>\nSession <code>%s</code> stuck in <b>%s</b> for %s",
		html.EscapeString(sessionShortID),
		html.EscapeString(state),
		humanDuration(dur),
	)
}

// FormatRespawnBlocked formats a respawn blocked notification.
func FormatRespawnBlocked(sessionShortID, reason, details string) string {
	msg := fmt.Sprintf(
		"<b>Respawn Blocked</b>\nSession <code>%s</code>\n\nReason: %s",
		html.EscapeString(sessionShortID),
		html.EscapeString(reason),
	)
	if details != "" {
		msg += fmt.Sprintf("\n\n%s", html.EscapeString(details))
	}
	return msg
}

// FormatPlanGenerated formats a plan generated notification.
func FormatPlanGenerated(task string, itemCount int) string {
	return fmt.Sprintf(
		"<b>Plan Ready for Review</b>\n\nTask: %s\nSteps: %d",
		html.EscapeString(task),
		itemCount,
	)
}

// FormatSystemStatus formats system status for /status command.
func FormatSystemStatus(status SystemStatus) string {
	return fmt.Sprintf(
		"<b>Jarvis Status</b>\n\nSessions: <b>%d</b>\nAgents: <b>%d</b>\nUptime: %s\nVersion: %s",
		status.SessionCount,
		status.AgentCount,
		html.EscapeString(status.Uptime),
		html.EscapeString(status.Version),
	)
}

// FormatSessionList formats a list of sessions for /sessions command.
func FormatSessionList(sessions []SessionInfo) string {
	if len(sessions) == 0 {
		return "No active sessions."
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>Sessions</b> (%d)\n", len(sessions)))
	for _, s := range sessions {
		status := "idle"
		if s.IsWorking {
			status = "working"
		}
		sb.WriteString(fmt.Sprintf("\n<code>%s</code> %s [%s] (%s)",
			html.EscapeString(s.ShortID),
			html.EscapeString(s.Name),
			html.EscapeString(s.Status),
			status,
		))
	}
	return sb.String()
}

// FormatAgentList formats a list of agents for /list_agents command.
func FormatAgentList(agents []AgentInfo) string {
	if len(agents) == 0 {
		return "No active agents."
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>Agents</b> (%d)\n", len(agents)))
	for _, a := range agents {
		sb.WriteString(fmt.Sprintf("\n<code>%s</code> %s [%s]",
			html.EscapeString(a.ID),
			html.EscapeString(a.Name),
			html.EscapeString(a.Status),
		))
	}
	return sb.String()
}

// FormatHelpMessage returns the /help command response.
func FormatHelpMessage() string {
	return `<b>Jarvis Bot Commands</b>

/help - Show this help message
/status - System status overview
/sessions - List active sessions
/list_agents - List spawned agents
/send &lt;id&gt; &lt;msg&gt; - Send message to session
/new [name] - Create new session
/link [id] - Link this chat to a session
/unlink - Disconnect linked session
/linked - Show link status
/output - Flush output buffer
/stop - Stop linked session
/spawn_agent &lt;task&gt; - Spawn new agent
/stop_agent &lt;id&gt; - Stop an agent

<b>Team lead (office)</b>
/lead - Show the lead agent's status
/lead &lt;msg&gt; - Send prompt to the lead (reply auto-forwarded)
/ask &lt;msg&gt; - Alias of /lead &lt;msg&gt;
/lead_status - Always show status (args ignored)`
}

// humanDuration formats a duration in a human-readable way.
func humanDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	hours := int(d.Hours())
	mins := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh%dm", hours, mins)
}

// ShortID returns the first 8 characters of an ID.
func ShortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}
