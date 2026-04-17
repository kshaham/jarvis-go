package web

import (
	"fmt"
	"time"

	"github.com/claudeman/jarvis-go/internal/pty"
	"github.com/claudeman/jarvis-go/internal/telegram"
)

// telegramBridge implements telegram.ServerBridge by delegating to the session manager.
type telegramBridge struct {
	sm      *pty.SessionManager
	startAt time.Time
}

func newTelegramBridge(sm *pty.SessionManager) *telegramBridge {
	return &telegramBridge{sm: sm, startAt: time.Now()}
}

func (b *telegramBridge) GetSystemStatus() telegram.SystemStatus {
	sessions := b.sm.GetAllSessions()
	uptime := time.Since(b.startAt)
	return telegram.SystemStatus{
		SessionCount: len(sessions),
		AgentCount:   0, // populated by subagent watcher if available
		Uptime:       telegram.ShortID(uptime.String()),
		Version:      "0.2.0-go",
	}
}

func (b *telegramBridge) GetSessionsList() []telegram.SessionInfo {
	sessions := b.sm.GetAllSessions()
	result := make([]telegram.SessionInfo, 0, len(sessions))
	for _, s := range sessions {
		result = append(result, telegram.SessionInfo{
			ID:        s.ID,
			ShortID:   telegram.ShortID(s.ID),
			Name:      s.Name(),
			Status:    string(s.Status()),
			Mode:      string(s.Mode),
			IsWorking: s.IsWorking(),
		})
	}
	return result
}

func (b *telegramBridge) GetAgentsList() []telegram.AgentInfo {
	// Subagent info is not directly accessible from the bridge;
	// return empty for now -- can be wired to SubagentWatcher later.
	return []telegram.AgentInfo{}
}

func (b *telegramBridge) SendToSession(sessionID, message string) bool {
	s, ok := b.sm.GetSession(sessionID)
	if !ok {
		return false
	}
	return s.WriteViaScreen(message)
}

func (b *telegramBridge) CreateSession(name string) (string, error) {
	// Session creation requires more context (workingDir, screenManager, etc.)
	// that the bridge doesn't have. Return an error for now -- full support
	// will be added when the web server passes a richer creation function.
	return "", fmt.Errorf("session creation via Telegram not yet implemented")
}

func (b *telegramBridge) StopSession(sessionID string) error {
	s, ok := b.sm.GetSession(sessionID)
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}
	s.Stop()
	return nil
}

func (b *telegramBridge) SubscribeToSessionOutput(sessionID string, callback func(output string)) func() {
	s, ok := b.sm.GetSession(sessionID)
	if !ok {
		return func() {} // no-op unsubscribe
	}
	return s.AddOutputObserver(callback)
}

func (b *telegramBridge) GetDetailedStatus(entityID string) string {
	s, ok := b.sm.GetSession(entityID)
	if !ok {
		return fmt.Sprintf("Entity %s not found", entityID)
	}
	input, output, total := s.Tokens()
	return fmt.Sprintf(
		"Session: %s\nName: %s\nStatus: %s\nMode: %s\nWorking: %v\nTokens: in=%d out=%d total=%d\nDir: %s",
		s.ID, s.Name(), s.Status(), s.Mode, s.IsWorking(),
		input, output, total, s.WorkingDir,
	)
}

// officeTelegramAdapter bridges office.TelegramNotifier to telegram.Handler,
// forwarding office update messages to the configured Telegram chat.
type officeTelegramAdapter struct {
	handler *telegram.Handler
	chatID  string
}

func (a *officeTelegramAdapter) SendOfficeUpdate(text string) {
	if !a.IsConnected() {
		return
	}
	a.handler.SendFormattedMessage(a.chatID, text, nil)
}

func (a *officeTelegramAdapter) IsConnected() bool {
	return a.handler != nil && a.handler.IsConnected()
}

// telegramNotifyAdapter bridges notifications.TelegramSender to the existing
// *telegram.Handler. Per-route chat IDs are configured on the ChannelSpec,
// not the adapter, so one telegram handler services any number of routes
// targeting different chats.
type telegramNotifyAdapter struct {
	handler *telegram.Handler
}

func (a *telegramNotifyAdapter) SendFormattedMessage(chatID, text string, _ interface{}) {
	if a.handler == nil {
		return
	}
	a.handler.SendFormattedMessage(chatID, text, nil)
}
