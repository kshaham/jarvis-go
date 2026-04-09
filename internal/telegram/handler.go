package telegram

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
)

// ---------------------------------------------------------------------------
// Handler -- main Telegram integration handler
// ---------------------------------------------------------------------------

// Handler manages the full lifecycle of Telegram bot integration: polling or
// webhook for incoming updates, command routing, callback query handling,
// output streaming, and notification delivery.
type Handler struct {
	lifecycleMu sync.Mutex   // protects Start/Stop transitions only
	mu          sync.RWMutex // protects runtime state reads

	bot      *Bot
	settings *SettingsStore
	server   ServerBridge

	officeBridge OfficeBridge // optional, set via SetOfficeBridge

	queue    *MessageQueue
	streamer *OutputStreamer
	commands *CommandRouter

	// Pending interactive prompts (permission requests, plan approvals) awaiting
	// user response via inline keyboard callback.
	pendingMu           sync.Mutex
	pendingInteractions map[string]*PendingInteraction // shortID -> interaction

	// Runtime-only linked sessions. Not persisted -- cleared on restart.
	linkedSessions map[string]string // chatID -> sessionID
	unsubscribers  map[string]func() // chatID -> unsubscribe output observer

	// Long-poll HTTP client (reused across getUpdates calls)
	longPollClient *http.Client

	// Lifecycle state
	started    atomic.Bool
	offset     int64
	cancelPoll context.CancelFunc

	// Error tracking for degraded mode
	consecutiveErrors int
	degradedMode      bool
}

// NewHandler creates a Handler with all sub-components wired together.
// Call Start() to begin polling or serving webhooks.
func NewHandler(bot *Bot, settingsStore *SettingsStore, server ServerBridge) *Handler {
	settings := settingsStore.Get()

	client := &http.Client{Timeout: 15 * time.Second}
	queue := NewMessageQueue(settings.BotToken, client, settings.RateLimitPerMinute)
	streamer := NewOutputStreamer(queue)

	h := &Handler{
		bot:                 bot,
		settings:            settingsStore,
		server:              server,
		queue:               queue,
		streamer:            streamer,
		longPollClient:      &http.Client{Timeout: 35 * time.Second},
		pendingInteractions: make(map[string]*PendingInteraction),
		linkedSessions:      make(map[string]string),
		unsubscribers:       make(map[string]func()),
	}

	h.commands = NewCommandRouter(h, server)
	return h
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

// Start begins the handler. Idempotent -- calling twice is a no-op.
// In polling mode, launches a background goroutine for long-polling.
// Starts the message queue and registers bot commands with Telegram.
func (h *Handler) Start() {
	h.lifecycleMu.Lock()
	defer h.lifecycleMu.Unlock()

	if h.started.Load() {
		return
	}

	settings := h.settings.Get()
	if !settings.Enabled || settings.BotToken == "" {
		slog.Warn("[telegram/handler] not starting: disabled or missing bot token")
		return
	}

	// Configure the bot with current credentials.
	h.bot.Configure(settings.BotToken, settings.ChatID)

	// Start the message queue processing goroutine.
	h.queue.Start()

	h.started.Store(true)
	slog.Info("[telegram/handler] started", "mode", settings.Mode, "chatID", settings.ChatID)

	// Register bot commands with Telegram (fire and forget).
	go h.registerBotCommands()

	// Start interaction cleanup goroutine.
	ctx, cancel := context.WithCancel(context.Background())
	h.cancelPoll = cancel
	go h.interactionCleanupLoop(ctx)

	// Launch polling if configured.
	if settings.Mode == "polling" {
		go h.pollLoop(ctx)
	}
}

// Stop shuts down the handler. Idempotent -- calling twice is a no-op.
func (h *Handler) Stop() {
	h.lifecycleMu.Lock()
	defer h.lifecycleMu.Unlock()

	if !h.started.Load() {
		return
	}

	slog.Info("[telegram/handler] stopping")

	// Cancel polling context (also stops interaction cleanup).
	if h.cancelPoll != nil {
		h.cancelPoll()
		h.cancelPoll = nil
	}

	// Stop the message queue (drains remaining messages).
	h.queue.Stop()

	// Flush any buffered output.
	h.streamer.FlushAll()

	// Unlink all sessions.
	h.mu.Lock()
	for chatID, unsub := range h.unsubscribers {
		unsub()
		delete(h.unsubscribers, chatID)
	}
	h.linkedSessions = make(map[string]string)
	h.mu.Unlock()

	h.started.Store(false)
	slog.Info("[telegram/handler] stopped")
}

// SetOfficeBridge sets the office bridge for leader reply routing.
// Called after office.Manager is created, since the office depends on the handler.
func (h *Handler) SetOfficeBridge(bridge OfficeBridge) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.officeBridge = bridge
}

// IsConnected returns true if the handler is started and the bot is connected.
func (h *Handler) IsConnected() bool {
	return h.started.Load() && h.bot.IsConnected()
}

// Settings returns the settings store for reading Telegram configuration.
func (h *Handler) Settings() *SettingsStore {
	return h.settings
}

// ---------------------------------------------------------------------------
// Polling
// ---------------------------------------------------------------------------

// pollLoop long-polls the Telegram getUpdates API. Runs until ctx is cancelled.
func (h *Handler) pollLoop(ctx context.Context) {
	slog.Info("[telegram/handler] polling loop started")

	for {
		select {
		case <-ctx.Done():
			slog.Info("[telegram/handler] polling loop stopped")
			return
		default:
		}

		updates, err := h.getUpdates(ctx, h.offset, 30)
		if err != nil {
			if ctx.Err() != nil {
				return // context cancelled, clean exit
			}

			h.consecutiveErrors++
			backoff := h.calculateBackoff()

			if h.consecutiveErrors >= 10 && !h.degradedMode {
				h.degradedMode = true
				slog.Warn("[telegram/handler] entering degraded mode after consecutive errors",
					"errors", h.consecutiveErrors)
			}

			slog.Error("[telegram/handler] getUpdates failed",
				"error", err,
				"consecutiveErrors", h.consecutiveErrors,
				"backoff", backoff,
				"degradedMode", h.degradedMode)

			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return
			}
			continue
		}

		// Success -- reset error tracking.
		if h.consecutiveErrors > 0 {
			if h.degradedMode {
				slog.Info("[telegram/handler] exiting degraded mode")
			}
			h.consecutiveErrors = 0
			h.degradedMode = false
		}

		for _, update := range updates {
			h.handleUpdate(update)

			if update.UpdateID >= h.offset {
				h.offset = update.UpdateID + 1
			}
		}
	}
}

// calculateBackoff returns the backoff duration based on consecutive error count.
// Exponential backoff from 1s to 60s cap.
func (h *Handler) calculateBackoff() time.Duration {
	if h.degradedMode {
		return 60 * time.Second
	}
	seconds := math.Pow(2, float64(h.consecutiveErrors-1))
	if seconds > 60 {
		seconds = 60
	}
	return time.Duration(seconds) * time.Second
}

// getUpdates calls the Telegram Bot API getUpdates endpoint.
func (h *Handler) getUpdates(ctx context.Context, offset int64, timeout int) ([]Update, error) {
	token, _ := h.bot.GetConfig()
	url := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates", token)

	payload := map[string]interface{}{
		"offset":  offset,
		"timeout": timeout,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal getUpdates payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create getUpdates request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Reuse long-poll HTTP client for connection pooling.
	resp, err := h.longPollClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("getUpdates request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read getUpdates response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var apiErr APIErrorResponse
		if json.Unmarshal(data, &apiErr) == nil && !apiErr.OK {
			return nil, fmt.Errorf("getUpdates API error %d: %s", apiErr.ErrorCode, apiErr.Description)
		}
		return nil, fmt.Errorf("getUpdates HTTP %d: %s", resp.StatusCode, string(data))
	}

	var result GetUpdatesResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse getUpdates response: %w", err)
	}

	if !result.OK {
		return nil, fmt.Errorf("getUpdates returned ok=false")
	}

	return result.Result, nil
}

// ---------------------------------------------------------------------------
// Update routing
// ---------------------------------------------------------------------------

// handleUpdate routes an incoming update to the appropriate handler.
func (h *Handler) handleUpdate(update Update) {
	if update.Message != nil {
		h.handleMessage(update.Message)
	} else if update.CallbackQuery != nil {
		h.handleCallbackQuery(update.CallbackQuery)
	}
}

// handleMessage processes an incoming text message.
func (h *Handler) handleMessage(msg *Message) {
	chatID := strconv.FormatInt(msg.Chat.ID, 10)

	// Chat ID verification: only process messages from the configured chat.
	settings := h.settings.Get()
	if settings.RequireChatIDVerify && settings.ChatID != "" && chatID != settings.ChatID {
		slog.Warn("[telegram/handler] message from unauthorized chat",
			"chatID", chatID, "expectedChatID", settings.ChatID)
		h.queue.Enqueue(chatID,
			"Not authorized. This bot is configured for a specific chat. Check your Telegram settings in Jarvis.",
			nil)
		return
	}

	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return
	}

	// Route: bot command
	if strings.HasPrefix(text, "/") {
		h.commands.HandleCommand(chatID, text)
		return
	}

	// Route: linked session -- forward text as input to the session
	h.mu.RLock()
	sessionID, isLinked := h.linkedSessions[chatID]
	h.mu.RUnlock()

	if isLinked {
		if h.server.SendToSession(sessionID, text) {
			slog.Debug("[telegram/handler] forwarded message to linked session",
				"chatID", chatID, "sessionID", ShortID(sessionID))
		} else {
			h.queue.Enqueue(chatID, "Failed to send message to linked session. It may have ended.", nil)
		}
		return
	}

	// Route: office leader pending query
	h.mu.RLock()
	bridge := h.officeBridge
	h.mu.RUnlock()

	if bridge != nil && bridge.IsTeamActive() && bridge.HasPendingLeaderQuery() {
		leaderSessionID := bridge.GetPendingLeaderSessionID()
		if leaderSessionID != "" {
			if h.server.SendToSession(leaderSessionID, text) {
				bridge.ClearPendingLeaderQuery()
				h.queue.Enqueue(chatID, "Reply sent to team leader.", nil)
			} else {
				h.queue.Enqueue(chatID, "Failed to deliver reply to team leader.", nil)
			}
			return
		}
	}

	// No route matched -- show help hint.
	h.queue.Enqueue(chatID, "No active session linked. Use /link to connect to a session, or /help for commands.", nil)
}

// handleCallbackQuery processes an inline keyboard button press.
func (h *Handler) handleCallbackQuery(query *CallbackQuery) {
	action, params := ParseCallbackData(query.Data)
	if action == "" {
		h.answerCallbackQuery(query.ID, "Invalid callback data")
		return
	}

	if len(params) == 0 {
		h.answerCallbackQuery(query.ID, "Missing session identifier")
		return
	}

	shortID := params[0]

	// Look up pending interaction.
	h.pendingMu.Lock()
	interaction, exists := h.pendingInteractions[shortID]
	if exists {
		delete(h.pendingInteractions, shortID)
	}
	h.pendingMu.Unlock()

	if !exists {
		h.answerCallbackQuery(query.ID, "This prompt has expired or already been answered.")
		return
	}

	// Execute action based on type.
	var responseText string
	switch action {
	case ActionPermissionApprove:
		h.server.SendToSession(interaction.SessionID, "yes")
		responseText = "Permission approved"
	case ActionPermissionDeny:
		h.server.SendToSession(interaction.SessionID, "no")
		responseText = "Permission denied"
	case ActionPermissionTrust:
		h.server.SendToSession(interaction.SessionID, "trust")
		responseText = "Permission trusted for session"
	case ActionPlanApprove:
		h.server.SendToSession(interaction.SessionID, "yes")
		responseText = "Plan approved"
	case ActionPlanDecline:
		h.server.SendToSession(interaction.SessionID, "no")
		responseText = "Plan declined"
	default:
		responseText = "Unknown action"
	}

	h.answerCallbackQuery(query.ID, responseText)
}

// ---------------------------------------------------------------------------
// Webhook
// ---------------------------------------------------------------------------

// WebhookHandler is an HTTP handler for Telegram webhook mode.
// It should be registered as a route like: POST /api/telegram/webhook/{secret}
// The secret in the URL path is validated against the configured webhook secret.
func (h *Handler) WebhookHandler(w http.ResponseWriter, r *http.Request) {
	if !h.started.Load() {
		http.Error(w, "handler not started", http.StatusServiceUnavailable)
		return
	}

	// Validate webhook secret from URL path via chi route parameter.
	urlSecret := chi.URLParam(r, "secret")

	settings := h.settings.Get()
	if settings.WebhookSecret == "" {
		http.Error(w, "webhook not configured", http.StatusForbidden)
		return
	}

	if subtle.ConstantTimeCompare([]byte(urlSecret), []byte(settings.WebhookSecret)) != 1 {
		http.Error(w, "invalid secret", http.StatusForbidden)
		return
	}

	// Parse the update.
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	var update Update
	if err := json.Unmarshal(body, &update); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// Route the update. Telegram expects a 200 OK response quickly.
	go h.handleUpdate(update)

	w.WriteHeader(http.StatusOK)
}

// ---------------------------------------------------------------------------
// Telegram API helpers
// ---------------------------------------------------------------------------

// answerCallbackQuery acknowledges an inline keyboard callback to Telegram.
// This removes the loading spinner from the pressed button.
func (h *Handler) answerCallbackQuery(queryID, text string) {
	token, _ := h.bot.GetConfig()
	url := fmt.Sprintf("https://api.telegram.org/bot%s/answerCallbackQuery", token)

	payload := map[string]interface{}{
		"callback_query_id": queryID,
	}
	if text != "" {
		payload["text"] = text
	}

	body, err := json.Marshal(payload)
	if err != nil {
		slog.Error("[telegram/handler] marshal answerCallbackQuery", "error", err)
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		slog.Error("[telegram/handler] answerCallbackQuery request failed", "error", err)
		return
	}
	resp.Body.Close()
}

// registerBotCommands calls the Telegram setMyCommands API to register command
// autocomplete hints. Fire and forget -- errors are logged but not fatal.
func (h *Handler) registerBotCommands() {
	token, _ := h.bot.GetConfig()
	url := fmt.Sprintf("https://api.telegram.org/bot%s/setMyCommands", token)

	commands := []map[string]string{
		{"command": "help", "description": "Show available commands"},
		{"command": "status", "description": "System status overview"},
		{"command": "sessions", "description": "List active sessions"},
		{"command": "list_agents", "description": "List spawned agents"},
		{"command": "send", "description": "Send message to session"},
		{"command": "new", "description": "Create new session"},
		{"command": "link", "description": "Link chat to a session"},
		{"command": "unlink", "description": "Disconnect linked session"},
		{"command": "linked", "description": "Show link status"},
		{"command": "output", "description": "Flush output buffer"},
		{"command": "stop", "description": "Stop linked session"},
		{"command": "spawn_agent", "description": "Spawn a new agent"},
		{"command": "stop_agent", "description": "Stop an agent"},
	}

	payload := map[string]interface{}{
		"commands": commands,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		slog.Error("[telegram/handler] marshal setMyCommands", "error", err)
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		slog.Error("[telegram/handler] setMyCommands request failed", "error", err)
		return
	}
	resp.Body.Close()

	slog.Debug("[telegram/handler] bot commands registered")
}

// ---------------------------------------------------------------------------
// Session linking
// ---------------------------------------------------------------------------

// linkSession links a Telegram chat to a session for output forwarding.
func (h *Handler) linkSession(chatID, sessionID string) {
	// Unlink any existing session for this chat first.
	h.unlinkSession(chatID)

	// Subscribe to session output.
	unsub := h.server.SubscribeToSessionOutput(sessionID, func(output string) {
		h.streamer.HandleOutput(chatID, output)
	})

	h.mu.Lock()
	h.linkedSessions[chatID] = sessionID
	h.unsubscribers[chatID] = unsub
	h.mu.Unlock()

	slog.Info("[telegram/handler] linked session",
		"chatID", chatID, "sessionID", ShortID(sessionID))
}

// unlinkSession removes the link between a chat and its session.
func (h *Handler) unlinkSession(chatID string) {
	h.mu.Lock()
	unsub, hasUnsub := h.unsubscribers[chatID]
	_, isLinked := h.linkedSessions[chatID]
	if isLinked {
		delete(h.linkedSessions, chatID)
	}
	if hasUnsub {
		delete(h.unsubscribers, chatID)
	}
	h.mu.Unlock()

	if hasUnsub {
		unsub()
	}

	// Flush any remaining buffered output for this chat.
	h.streamer.RemoveChat(chatID)

	if isLinked {
		slog.Info("[telegram/handler] unlinked session", "chatID", chatID)
	}
}

// ---------------------------------------------------------------------------
// Interaction cleanup
// ---------------------------------------------------------------------------

// interactionCleanupLoop periodically removes expired pending interactions.
func (h *Handler) interactionCleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(InteractionCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.cleanupExpiredInteractions()
		}
	}
}

// cleanupExpiredInteractions removes interactions past their expiry time.
func (h *Handler) cleanupExpiredInteractions() {
	now := time.Now()
	h.pendingMu.Lock()
	defer h.pendingMu.Unlock()

	for shortID, interaction := range h.pendingInteractions {
		if now.After(interaction.ExpiresAt) {
			slog.Debug("[telegram/handler] expired pending interaction",
				"shortID", shortID, "type", interaction.Type)
			delete(h.pendingInteractions, shortID)
		}
	}
}

// ---------------------------------------------------------------------------
// Notification methods -- send notifications to configured Telegram chat
// ---------------------------------------------------------------------------

// NotifySessionStart sends a session start notification to the configured chat.
func (h *Handler) NotifySessionStart(sessionID, name, workingDir string) {
	if !h.started.Load() {
		return
	}
	settings := h.settings.Get()
	if !settings.Notifications.SessionStart {
		return
	}
	text := FormatSessionStart(name, workingDir)
	h.queue.Enqueue(settings.ChatID, text, nil)
}

// NotifySessionComplete sends a session completion notification.
func (h *Handler) NotifySessionComplete(sessionID, name string, duration time.Duration) {
	if !h.started.Load() {
		return
	}
	settings := h.settings.Get()
	if !settings.Notifications.SessionComplete {
		return
	}
	text := FormatSessionComplete(name, duration)
	h.queue.Enqueue(settings.ChatID, text, nil)
}

// NotifySessionError sends a session error notification.
func (h *Handler) NotifySessionError(sessionID, name, errMsg string) {
	if !h.started.Load() {
		return
	}
	settings := h.settings.Get()
	if !settings.Notifications.SessionError {
		return
	}
	text := FormatSessionError(name, errMsg)
	h.queue.Enqueue(settings.ChatID, text, nil)
}

// NotifyPermissionPrompt sends a permission prompt notification with an inline keyboard.
func (h *Handler) NotifyPermissionPrompt(sessionID string, data map[string]interface{}) {
	if !h.started.Load() {
		return
	}
	settings := h.settings.Get()
	if !settings.Notifications.PermissionPrompt {
		return
	}

	shortID := ShortID(sessionID)
	toolName, _ := data["tool"].(string)

	// Register as a pending interaction.
	now := time.Now()
	h.pendingMu.Lock()
	h.pendingInteractions[shortID] = &PendingInteraction{
		SessionID: sessionID,
		ShortID:   shortID,
		Type:      InteractionTypePermission,
		ToolName:  toolName,
		CreatedAt: now,
		ExpiresAt: now.Add(DefaultInteractionTimeout),
	}
	h.pendingMu.Unlock()

	text := FormatPermissionPrompt(shortID, toolName, data)
	keyboard := PermissionKeyboard(sessionID)
	h.queue.Enqueue(settings.ChatID, text, keyboard)
}

// NotifyElicitation sends an elicitation dialog notification.
func (h *Handler) NotifyElicitation(sessionID string, data map[string]interface{}) {
	if !h.started.Load() {
		return
	}
	settings := h.settings.Get()
	if !settings.Notifications.Elicitation {
		return
	}

	shortID := ShortID(sessionID)
	question, _ := data["question"].(string)
	text := FormatElicitation(shortID, question)
	h.queue.Enqueue(settings.ChatID, text, nil)
}

// NotifyStuckState sends a stuck state warning notification.
func (h *Handler) NotifyStuckState(sessionID, state string, durationMs int64) {
	if !h.started.Load() {
		return
	}
	settings := h.settings.Get()
	if !settings.Notifications.StuckState {
		return
	}

	shortID := ShortID(sessionID)
	text := FormatStuckWarning(shortID, state, durationMs)
	h.queue.Enqueue(settings.ChatID, text, nil)
}

// NotifyRespawnBlocked sends a respawn blocked notification.
func (h *Handler) NotifyRespawnBlocked(sessionID, reason, details string) {
	if !h.started.Load() {
		return
	}
	settings := h.settings.Get()
	if !settings.Notifications.RespawnBlocked {
		return
	}

	shortID := ShortID(sessionID)
	text := FormatRespawnBlocked(shortID, reason, details)
	h.queue.Enqueue(settings.ChatID, text, nil)
}

// NotifyPlanGenerated sends a plan generated notification with an approval keyboard.
func (h *Handler) NotifyPlanGenerated(orchestratorID, task string, itemCount int) {
	if !h.started.Load() {
		return
	}
	settings := h.settings.Get()
	if !settings.Notifications.PlanApproval {
		return
	}

	shortID := ShortID(orchestratorID)

	// Register as a pending interaction.
	now := time.Now()
	h.pendingMu.Lock()
	h.pendingInteractions[shortID] = &PendingInteraction{
		SessionID: orchestratorID,
		ShortID:   shortID,
		Type:      InteractionTypePlan,
		CreatedAt: now,
		ExpiresAt: now.Add(DefaultInteractionTimeout),
	}
	h.pendingMu.Unlock()

	text := FormatPlanGenerated(task, itemCount)
	keyboard := PlanApprovalKeyboard(orchestratorID)
	h.queue.Enqueue(settings.ChatID, text, keyboard)
}

// ---------------------------------------------------------------------------
// Public API for office integration
// ---------------------------------------------------------------------------

// SendFormattedMessage sends a message to a specific chat with an optional
// inline keyboard. Used by office integration to send leader queries and
// team status updates.
func (h *Handler) SendFormattedMessage(chatID, text string, keyboard *InlineKeyboardMarkup) {
	if !h.started.Load() {
		return
	}
	h.queue.Enqueue(chatID, text, keyboard)
}
