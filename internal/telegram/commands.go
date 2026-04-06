package telegram

import (
	"fmt"
	"html"
	"log/slog"
	"strings"
)

// ---------------------------------------------------------------------------
// CommandRouter -- dispatches bot commands to handler methods
// ---------------------------------------------------------------------------

// CommandRouter parses and dispatches Telegram bot commands (messages starting
// with "/") to the appropriate handler method.
type CommandRouter struct {
	handler *Handler
	server  ServerBridge
}

// NewCommandRouter creates a CommandRouter wired to the given handler and server bridge.
func NewCommandRouter(handler *Handler, server ServerBridge) *CommandRouter {
	return &CommandRouter{
		handler: handler,
		server:  server,
	}
}

// HandleCommand parses a command string and dispatches to the appropriate method.
// text is the full message text (e.g., "/send abc123 hello world").
func (cr *CommandRouter) HandleCommand(chatID, text string) {
	// Strip the bot username suffix if present (e.g., "/status@MyBot" -> "/status").
	text = strings.TrimSpace(text)
	parts := strings.SplitN(text, " ", 2)
	command := strings.Split(parts[0], "@")[0] // remove @botname suffix
	command = strings.ToLower(command)

	args := ""
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}

	slog.Debug("[telegram/commands] dispatching", "command", command, "chatID", chatID)

	switch command {
	case "/help", "/start":
		cr.cmdHelp(chatID)
	case "/status":
		cr.cmdStatus(chatID)
	case "/sessions":
		cr.cmdSessions(chatID)
	case "/list_agents":
		cr.cmdListAgents(chatID)
	case "/send":
		cr.cmdSend(chatID, args)
	case "/new":
		cr.cmdNew(chatID, args)
	case "/link":
		cr.cmdLink(chatID, args)
	case "/unlink":
		cr.cmdUnlink(chatID)
	case "/linked":
		cr.cmdLinked(chatID)
	case "/output":
		cr.cmdOutput(chatID)
	case "/stop":
		cr.cmdStop(chatID)
	case "/spawn_agent":
		cr.cmdSpawnAgent(chatID, args)
	case "/stop_agent":
		cr.cmdStopAgent(chatID, args)
	default:
		cr.handler.queue.Enqueue(chatID,
			fmt.Sprintf("Unknown command: %s\nUse /help for available commands.", html.EscapeString(command)),
			nil)
	}
}

// ---------------------------------------------------------------------------
// Individual command implementations
// ---------------------------------------------------------------------------

// cmdHelp sends the formatted help message listing all available commands.
func (cr *CommandRouter) cmdHelp(chatID string) {
	cr.handler.queue.Enqueue(chatID, FormatHelpMessage(), nil)
}

// cmdStatus queries the server bridge for system status and sends a formatted response.
func (cr *CommandRouter) cmdStatus(chatID string) {
	status := cr.server.GetSystemStatus()
	text := FormatSystemStatus(status)
	cr.handler.queue.Enqueue(chatID, text, nil)
}

// cmdSessions lists all active sessions.
func (cr *CommandRouter) cmdSessions(chatID string) {
	sessions := cr.server.GetSessionsList()
	text := FormatSessionList(sessions)
	cr.handler.queue.Enqueue(chatID, text, nil)
}

// cmdListAgents lists all active agents.
func (cr *CommandRouter) cmdListAgents(chatID string) {
	agents := cr.server.GetAgentsList()
	text := FormatAgentList(agents)
	cr.handler.queue.Enqueue(chatID, text, nil)
}

// cmdSend sends a message to a session by short ID.
// Expected format: "/send <shortID> <message>"
func (cr *CommandRouter) cmdSend(chatID, args string) {
	if args == "" {
		cr.handler.queue.Enqueue(chatID, "Usage: /send &lt;sessionID&gt; &lt;message&gt;", nil)
		return
	}

	parts := strings.SplitN(args, " ", 2)
	if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
		cr.handler.queue.Enqueue(chatID, "Usage: /send &lt;sessionID&gt; &lt;message&gt;", nil)
		return
	}

	targetID := strings.TrimSpace(parts[0])
	message := strings.TrimSpace(parts[1])

	// Resolve short ID to full session ID by searching active sessions.
	fullID := cr.resolveSessionID(targetID)
	if fullID == "" {
		cr.handler.queue.Enqueue(chatID,
			fmt.Sprintf("Session <code>%s</code> not found.", html.EscapeString(targetID)),
			nil)
		return
	}

	if cr.server.SendToSession(fullID, message) {
		cr.handler.queue.Enqueue(chatID,
			fmt.Sprintf("Message sent to session <code>%s</code>.", html.EscapeString(ShortID(fullID))),
			nil)
	} else {
		cr.handler.queue.Enqueue(chatID,
			fmt.Sprintf("Failed to send message to session <code>%s</code>.", html.EscapeString(targetID)),
			nil)
	}
}

// cmdNew creates a new session with an optional name.
func (cr *CommandRouter) cmdNew(chatID, args string) {
	name := strings.TrimSpace(args)
	if name == "" {
		name = "telegram-session"
	}

	sessionID, err := cr.server.CreateSession(name)
	if err != nil {
		slog.Error("[telegram/commands] create session failed", "error", err)
		cr.handler.queue.Enqueue(chatID,
			fmt.Sprintf("Failed to create session: %s", html.EscapeString(err.Error())),
			nil)
		return
	}

	cr.handler.queue.Enqueue(chatID,
		fmt.Sprintf("Session created: <code>%s</code>\nUse /link %s to connect.",
			html.EscapeString(ShortID(sessionID)),
			html.EscapeString(ShortID(sessionID))),
		nil)
}

// cmdLink links the current chat to a session for bidirectional communication.
// If no session ID is provided, links to the most recently created session.
func (cr *CommandRouter) cmdLink(chatID, args string) {
	targetID := strings.TrimSpace(args)

	if targetID == "" {
		// Link to the most recent session.
		sessions := cr.server.GetSessionsList()
		if len(sessions) == 0 {
			cr.handler.queue.Enqueue(chatID, "No active sessions. Use /new to create one.", nil)
			return
		}
		// Use the first session in the list (most recent).
		targetID = sessions[0].ID
	} else {
		// Resolve short ID to full session ID.
		fullID := cr.resolveSessionID(targetID)
		if fullID == "" {
			cr.handler.queue.Enqueue(chatID,
				fmt.Sprintf("Session <code>%s</code> not found.", html.EscapeString(targetID)),
				nil)
			return
		}
		targetID = fullID
	}

	cr.handler.linkSession(chatID, targetID)

	settings := cr.handler.settings.Get()
	outputNote := ""
	if settings.ForwardOutputToChat {
		outputNote = "\nOutput forwarding is enabled."
	}

	cr.handler.queue.Enqueue(chatID,
		fmt.Sprintf("Linked to session <code>%s</code>. Messages you send will be forwarded.%s",
			html.EscapeString(ShortID(targetID)), outputNote),
		nil)
}

// cmdUnlink disconnects the current chat from its linked session.
func (cr *CommandRouter) cmdUnlink(chatID string) {
	cr.handler.mu.RLock()
	_, isLinked := cr.handler.linkedSessions[chatID]
	cr.handler.mu.RUnlock()

	if !isLinked {
		cr.handler.queue.Enqueue(chatID, "No session linked to this chat.", nil)
		return
	}

	cr.handler.unlinkSession(chatID)
	cr.handler.queue.Enqueue(chatID, "Session unlinked.", nil)
}

// cmdLinked shows the current link status for this chat.
func (cr *CommandRouter) cmdLinked(chatID string) {
	cr.handler.mu.RLock()
	sessionID, isLinked := cr.handler.linkedSessions[chatID]
	cr.handler.mu.RUnlock()

	if !isLinked {
		cr.handler.queue.Enqueue(chatID, "No session linked to this chat.\nUse /link to connect.", nil)
		return
	}

	text := fmt.Sprintf("Linked to session <code>%s</code>", html.EscapeString(ShortID(sessionID)))

	// Try to get detailed status.
	details := cr.server.GetDetailedStatus(sessionID)
	if details != "" {
		text += fmt.Sprintf("\n\n%s", html.EscapeString(details))
	}

	cr.handler.queue.Enqueue(chatID, text, nil)
}

// cmdOutput forces a flush of the output buffer for this chat.
func (cr *CommandRouter) cmdOutput(chatID string) {
	cr.handler.mu.RLock()
	_, isLinked := cr.handler.linkedSessions[chatID]
	cr.handler.mu.RUnlock()

	if !isLinked {
		cr.handler.queue.Enqueue(chatID, "No session linked. Use /link first.", nil)
		return
	}

	cr.handler.streamer.FlushChat(chatID)
	cr.handler.queue.Enqueue(chatID, "Output buffer flushed.", nil)
}

// cmdStop stops the session linked to this chat.
func (cr *CommandRouter) cmdStop(chatID string) {
	cr.handler.mu.RLock()
	sessionID, isLinked := cr.handler.linkedSessions[chatID]
	cr.handler.mu.RUnlock()

	if !isLinked {
		cr.handler.queue.Enqueue(chatID, "No session linked. Use /link first.", nil)
		return
	}

	if err := cr.server.StopSession(sessionID); err != nil {
		slog.Error("[telegram/commands] stop session failed", "error", err, "sessionID", ShortID(sessionID))
		cr.handler.queue.Enqueue(chatID,
			fmt.Sprintf("Failed to stop session: %s", html.EscapeString(err.Error())),
			nil)
		return
	}

	// Unlink after stopping.
	cr.handler.unlinkSession(chatID)
	cr.handler.queue.Enqueue(chatID,
		fmt.Sprintf("Session <code>%s</code> stopped and unlinked.", html.EscapeString(ShortID(sessionID))),
		nil)
}

// cmdSpawnAgent creates a new agent session with the given task description.
func (cr *CommandRouter) cmdSpawnAgent(chatID, args string) {
	task := strings.TrimSpace(args)
	if task == "" {
		cr.handler.queue.Enqueue(chatID, "Usage: /spawn_agent &lt;task description&gt;", nil)
		return
	}

	sessionID, err := cr.server.CreateSession(task)
	if err != nil {
		slog.Error("[telegram/commands] spawn agent failed", "error", err)
		cr.handler.queue.Enqueue(chatID,
			fmt.Sprintf("Failed to spawn agent: %s", html.EscapeString(err.Error())),
			nil)
		return
	}

	cr.handler.queue.Enqueue(chatID,
		fmt.Sprintf("Agent spawned: <code>%s</code>\nTask: %s",
			html.EscapeString(ShortID(sessionID)),
			html.EscapeString(task)),
		nil)
}

// cmdStopAgent stops an agent by its short ID.
func (cr *CommandRouter) cmdStopAgent(chatID, args string) {
	targetID := strings.TrimSpace(args)
	if targetID == "" {
		cr.handler.queue.Enqueue(chatID, "Usage: /stop_agent &lt;agentID&gt;", nil)
		return
	}

	fullID := cr.resolveSessionID(targetID)
	if fullID == "" {
		cr.handler.queue.Enqueue(chatID,
			fmt.Sprintf("Agent <code>%s</code> not found.", html.EscapeString(targetID)),
			nil)
		return
	}

	if err := cr.server.StopSession(fullID); err != nil {
		slog.Error("[telegram/commands] stop agent failed", "error", err, "agentID", targetID)
		cr.handler.queue.Enqueue(chatID,
			fmt.Sprintf("Failed to stop agent: %s", html.EscapeString(err.Error())),
			nil)
		return
	}

	cr.handler.queue.Enqueue(chatID,
		fmt.Sprintf("Agent <code>%s</code> stopped.", html.EscapeString(ShortID(fullID))),
		nil)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// resolveSessionID resolves a short ID or full ID to a full session ID by
// searching active sessions. Returns "" if not found.
func (cr *CommandRouter) resolveSessionID(id string) string {
	sessions := cr.server.GetSessionsList()
	for _, s := range sessions {
		if s.ID == id || s.ShortID == id {
			return s.ID
		}
		// Also match by prefix.
		if len(id) >= 4 && strings.HasPrefix(s.ID, id) {
			return s.ID
		}
	}

	// Also check agents.
	agents := cr.server.GetAgentsList()
	for _, a := range agents {
		if a.ID == id {
			return a.ID
		}
		if len(id) >= 4 && strings.HasPrefix(a.ID, id) {
			return a.ID
		}
	}

	return ""
}
