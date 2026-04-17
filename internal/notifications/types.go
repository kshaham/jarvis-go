// Package notifications routes SSE events to user-defined destinations
// (Telegram chats, HTTP webhooks, log sinks) based on match rules.
//
// The router subscribes to the SSE broker via Broker.AddPostBroadcastHook;
// every broadcast flows through the Routes list and is dispatched to
// matching channels asynchronously. Matching uses a simple event-pattern
// language ("session:idle", "office:*", "*").
package notifications

import "time"

// ChannelType enumerates the built-in destination kinds.
const (
	ChannelTelegram = "telegram"
	ChannelWebhook  = "webhook"
	ChannelLog      = "log"
)

// MatchRule decides whether an event should fire a route. EventPattern is
// either the exact event name or a trailing-wildcard pattern ("office:*").
// SessionIDs, when non-empty, restricts matching to events carrying one of
// the listed session IDs (payload key "id" or "sessionId").
type MatchRule struct {
	EventPattern string   `json:"eventPattern"`
	SessionIDs   []string `json:"sessionIds,omitempty"`
}

// ChannelSpec describes one destination for a matched event.
// Target meaning depends on Type:
//   - telegram: chat ID (numeric string)
//   - webhook:  full URL, POSTed as JSON with optional Headers
//   - log:      label prepended to the log line
type ChannelSpec struct {
	Type    string            `json:"type"`
	Target  string            `json:"target"`
	Headers map[string]string `json:"headers,omitempty"`
}

// Route is a named, persistently-stored routing rule.
type Route struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Enabled   bool          `json:"enabled"`
	Match     MatchRule     `json:"match"`
	Channels  []ChannelSpec `json:"channels"`
	CreatedAt time.Time     `json:"createdAt"`
	UpdatedAt time.Time     `json:"updatedAt"`
}
