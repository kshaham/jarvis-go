package telegram

import "time"

// ---------------------------------------------------------------------------
// Telegram Bot API types
// ---------------------------------------------------------------------------

// Update represents an incoming update from the Telegram Bot API.
// At most one of Message or CallbackQuery will be set.
type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *Message       `json:"message,omitempty"`
	CallbackQuery *CallbackQuery `json:"callback_query,omitempty"`
}

// GetUpdatesResponse is the envelope returned by the getUpdates endpoint.
type GetUpdatesResponse struct {
	OK     bool     `json:"ok"`
	Result []Update `json:"result"`
}

// APIErrorResponse is the error envelope returned by Telegram on failure.
// Used to distinguish retryable errors (429, 5xx) from permanent failures.
type APIErrorResponse struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
	ErrorCode   int    `json:"error_code"`
}

// Message represents a Telegram message.
type Message struct {
	MessageID int64  `json:"message_id"`
	Chat      Chat   `json:"chat"`
	From      *User  `json:"from,omitempty"`
	Text      string `json:"text"`
	Date      int64  `json:"date"`
}

// Chat represents a Telegram chat (private, group, or supergroup).
type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"` // "private", "group", "supergroup"
}

// User represents a Telegram user.
type User struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	Username  string `json:"username,omitempty"`
}

// CallbackQuery represents an incoming callback query from a callback button
// in an inline keyboard.
type CallbackQuery struct {
	ID      string   `json:"id"`
	From    User     `json:"from"`
	Message *Message `json:"message,omitempty"`
	Data    string   `json:"data"`
}

// InlineKeyboardMarkup represents an inline keyboard attached to a message.
type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

// InlineKeyboardButton represents one button of an inline keyboard.
type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
}

// SendMessageRequest is the request body for the sendMessage API call.
type SendMessageRequest struct {
	ChatID      string                `json:"chat_id"`
	Text        string                `json:"text"`
	ParseMode   string                `json:"parse_mode,omitempty"`
	ReplyMarkup *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
}

// SendMessageResponse is the envelope returned by the sendMessage endpoint.
type SendMessageResponse struct {
	OK     bool     `json:"ok"`
	Result *Message `json:"result,omitempty"`
}

// ---------------------------------------------------------------------------
// ServerBridge -- abstracts the web server for the Telegram handler
// ---------------------------------------------------------------------------

// ServerBridge provides the Telegram handler with access to session management,
// system status, and output streaming without importing the web layer.
// Implemented by the web server and passed to the handler on creation.
type ServerBridge interface {
	// GetSystemStatus returns high-level system metrics.
	GetSystemStatus() SystemStatus

	// GetSessionsList returns information about all active sessions.
	GetSessionsList() []SessionInfo

	// GetAgentsList returns information about all active agents.
	GetAgentsList() []AgentInfo

	// SendToSession sends a text message to a running session.
	// Returns true if the message was delivered.
	SendToSession(sessionID, message string) bool

	// CreateSession creates a new session with the given name and returns its ID.
	CreateSession(name string) (sessionID string, err error)

	// StopSession terminates the session identified by sessionID.
	StopSession(sessionID string) error

	// SubscribeToSessionOutput registers a callback that is invoked with
	// terminal output from the given session. Returns an unsubscribe function
	// that removes the callback. Uses an observer pattern so the single-consumer
	// event channel constraint is preserved.
	SubscribeToSessionOutput(sessionID string, callback func(output string)) (unsubscribe func())

	// GetDetailedStatus returns a human-readable detailed status string for
	// the entity (session or agent) identified by entityID.
	GetDetailedStatus(entityID string) string
}

// SystemStatus holds high-level system metrics returned by ServerBridge.
type SystemStatus struct {
	SessionCount int    `json:"sessionCount"`
	AgentCount   int    `json:"agentCount"`
	Uptime       string `json:"uptime"`
	Version      string `json:"version"`
}

// SessionInfo describes a single session for listing purposes.
type SessionInfo struct {
	ID        string `json:"id"`
	ShortID   string `json:"shortId"`   // first 8 chars of ID
	Name      string `json:"name"`
	Status    string `json:"status"`
	Mode      string `json:"mode"`
	IsWorking bool   `json:"isWorking"`
}

// AgentInfo describes a single agent for listing purposes.
type AgentInfo struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// ---------------------------------------------------------------------------
// OfficeBridge -- abstracts the office manager for leader reply routing
// ---------------------------------------------------------------------------

// OfficeBridge provides the Telegram handler with access to the Virtual Office
// team leader query state without importing the office package.
// Implemented by office.Manager and set via Handler.SetOfficeBridge.
type OfficeBridge interface {
	// HasPendingLeaderQuery returns true if the team leader has an outstanding
	// clarification request that has not expired.
	HasPendingLeaderQuery() bool

	// GetPendingLeaderSessionID returns the session ID of the leader agent
	// that issued the pending query. Returns "" if no query is pending.
	GetPendingLeaderSessionID() string

	// ClearPendingLeaderQuery removes the pending leader query, typically
	// called after the user has replied via Telegram.
	ClearPendingLeaderQuery()

	// IsTeamActive returns true if a team is currently spawned and running.
	IsTeamActive() bool
}

// ---------------------------------------------------------------------------
// PendingInteraction -- tracks permission / plan approval prompts
// ---------------------------------------------------------------------------

// PendingInteraction tracks an interactive prompt (permission request or plan
// approval) that was forwarded to Telegram and is awaiting a user response
// via inline keyboard callback. Interactions expire after Timeout and are
// cleaned up by a periodic sweep goroutine.
type PendingInteraction struct {
	// SessionID is the full session UUID that owns this interaction.
	SessionID string

	// ShortID is the first 8 characters of SessionID, used in callback data
	// to fit within Telegram's 64-byte callback_data limit.
	ShortID string

	// Type distinguishes the kind of interaction ("permission" or "plan").
	Type string

	// ToolName is set for permission interactions to identify the tool
	// requesting approval (empty for plan interactions).
	ToolName string

	// CreatedAt records when the interaction was created.
	CreatedAt time.Time

	// ExpiresAt is CreatedAt + Timeout. Interactions past this time are
	// considered stale and are removed by the cleanup sweep.
	ExpiresAt time.Time
}

// PendingInteraction type constants.
const (
	InteractionTypePermission = "permission"
	InteractionTypePlan       = "plan"
)

// DefaultInteractionTimeout is the duration after which a pending interaction
// expires if the user has not responded.
const DefaultInteractionTimeout = 5 * time.Minute

// InteractionCleanupInterval is how often the cleanup goroutine sweeps for
// expired pending interactions.
const InteractionCleanupInterval = 60 * time.Second
