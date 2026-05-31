package telegram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// Bot manages a Telegram Bot API connection.
type Bot struct {
	mu      sync.Mutex
	token   string
	chatID  string
	enabled bool
	client  *http.Client
}

// New returns a new unconfigured Bot.
func New() *Bot {
	// R28 · Bounded timeout so a slow/hung Telegram API can't block the
	// operator's request goroutine indefinitely. Matches the queue/long-poll
	// clients already used elsewhere in this package.
	return &Bot{client: &http.Client{Timeout: 15 * time.Second}}
}

// Configure sets the bot token and chat ID, enabling the bot when both are non-empty.
func (b *Bot) Configure(token, chatID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.token = token
	b.chatID = chatID
	b.enabled = token != "" && chatID != ""
}

// IsConnected returns whether the bot is configured and enabled.
func (b *Bot) IsConnected() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.enabled
}

// GetConfig returns the current token and chatID.
func (b *Bot) GetConfig() (token, chatID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.token, b.chatID
}

// SendMessage sends a text message to the configured chat via the Telegram Bot API.
func (b *Bot) SendMessage(text string) error {
	b.mu.Lock()
	token := b.token
	chatID := b.chatID
	enabled := b.enabled
	b.mu.Unlock()

	if !enabled {
		return fmt.Errorf("telegram not configured")
	}

	url := fmt.Sprintf("%s/bot%s/sendMessage", apiBaseURL(), token)
	body, _ := json.Marshal(map[string]interface{}{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "HTML",
	})

	resp, err := b.client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		// Strip the token before the error bubbles out — net/http wraps
		// this as *url.Error{URL: "https://.../bot<TOKEN>/..."}.
		return redactTokenErr(err, token)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram API error %d: %s", resp.StatusCode, string(data))
	}
	return nil
}

// Disconnect disables the bot without clearing stored credentials.
func (b *Bot) Disconnect() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.enabled = false
}
