package telegram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Token bucket rate limiter
// ---------------------------------------------------------------------------

type tokenBucket struct {
	mu         sync.Mutex
	capacity   float64
	tokens     float64
	lastRefill time.Time
	rate       float64 // tokens per second
}

func newTokenBucket(perMinute int) *tokenBucket {
	rate := float64(perMinute) / 60.0
	return &tokenBucket{
		capacity:   float64(perMinute),
		tokens:     float64(perMinute), // start full
		lastRefill: time.Now(),
		rate:       rate,
	}
}

// refill adds tokens based on elapsed time since last refill.
func (tb *tokenBucket) refill() {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.refillLocked()
}

func (tb *tokenBucket) refillLocked() {
	now := time.Now()
	elapsed := now.Sub(tb.lastRefill).Seconds()
	tb.tokens += elapsed * tb.rate
	if tb.tokens > tb.capacity {
		tb.tokens = tb.capacity
	}
	tb.lastRefill = now
}

// tryConsume attempts to consume one token. Returns true if successful.
func (tb *tokenBucket) tryConsume() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.refillLocked()
	if tb.tokens >= 1.0 {
		tb.tokens -= 1.0
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Queued message
// ---------------------------------------------------------------------------

const (
	defaultMaxRetries = 3
	maxMessageLength  = 3900
	apiTimeout        = 15 * time.Second
	// maxQueueDepth bounds the backlog so a sustained outage (Telegram down,
	// or a broadcast storm) can't grow the slice without limit. When full we
	// drop the OLDEST queued message -- for notifications the newest matters
	// most, and an unbounded backlog is a memory leak.
	maxQueueDepth = 1000
	// drainDeadline caps total best-effort send time during shutdown.
	drainDeadline = 10 * time.Second
)

type queuedMessage struct {
	ChatID     string
	Text       string
	Keyboard   *InlineKeyboardMarkup
	Retries    int
	MaxRetries int
}

// ---------------------------------------------------------------------------
// MessageQueue
// ---------------------------------------------------------------------------

// MessageQueue provides a rate-limited, retrying message queue that sends
// messages to the Telegram Bot API. It calls the API directly using the
// provided bot token and http.Client, without going through the Bot struct.
type MessageQueue struct {
	mu      sync.Mutex
	token   string            // fallback / initial token
	tokenFn func() string     // live-token provider (set by the Handler); wins over token
	client  *http.Client
	queue   []queuedMessage
	limiter *tokenBucket
	notify  chan struct{}
	stopCh  chan struct{}
	stopped chan struct{}
}

// SetTokenProvider installs a function returning the CURRENT bot token, so the
// queue follows runtime token (re)configuration instead of using the value
// frozen at construction. Without this, setting/rotating the token at runtime
// silently broke all queued sends (the poll loop reads the live token, the
// queue didn't).
func (mq *MessageQueue) SetTokenProvider(fn func() string) {
	mq.mu.Lock()
	mq.tokenFn = fn
	mq.mu.Unlock()
}

// currentToken returns the live token from the provider when set + non-empty,
// else the construction-time token.
func (mq *MessageQueue) currentToken() string {
	mq.mu.Lock()
	fn, tok := mq.tokenFn, mq.token
	mq.mu.Unlock()
	if fn != nil {
		if live := fn(); live != "" {
			return live
		}
	}
	return tok
}

// NewMessageQueue creates a MessageQueue that sends via the Telegram Bot API.
// token is the bot token used for API calls. ratePerMinute controls how many
// messages per minute the queue will send.
func NewMessageQueue(token string, client *http.Client, ratePerMinute int) *MessageQueue {
	if client == nil {
		client = &http.Client{Timeout: apiTimeout}
	}
	return &MessageQueue{
		token:   token,
		client:  client,
		queue:   make([]queuedMessage, 0, 32),
		limiter: newTokenBucket(ratePerMinute),
		notify:  make(chan struct{}, 1),
		stopCh:  make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

// Enqueue adds a message to the queue and signals the processing goroutine.
// keyboard may be nil for plain text messages.
func (mq *MessageQueue) Enqueue(chatID, text string, keyboard *InlineKeyboardMarkup) {
	mq.mu.Lock()
	if len(mq.queue) >= maxQueueDepth {
		dropped := mq.queue[0]
		mq.queue = mq.queue[1:]
		log.Printf("[telegram/queue] backlog at cap (%d); dropping oldest message to chat %s",
			maxQueueDepth, dropped.ChatID)
	}
	mq.queue = append(mq.queue, queuedMessage{
		ChatID:     chatID,
		Text:       text,
		Keyboard:   keyboard,
		Retries:    0,
		MaxRetries: defaultMaxRetries,
	})
	mq.mu.Unlock()

	// Non-blocking signal so we never block the caller.
	select {
	case mq.notify <- struct{}{}:
	default:
	}
}

// Start launches the background processing goroutine.
func (mq *MessageQueue) Start() {
	go mq.processLoop()
}

// Stop signals the processing goroutine to exit and waits for it to finish.
func (mq *MessageQueue) Stop() {
	close(mq.stopCh)
	<-mq.stopped
}

// ---------------------------------------------------------------------------
// Processing loop
// ---------------------------------------------------------------------------

func (mq *MessageQueue) processLoop() {
	defer close(mq.stopped)

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-mq.stopCh:
			mq.drainOnShutdown()
			return

		case <-mq.notify:
			mq.processAvailable()

		case <-ticker.C:
			// Periodic refill and opportunistic send.
			mq.limiter.refill()
			mq.processAvailable()
		}
	}
}

// processAvailable sends as many queued messages as the rate limiter allows.
func (mq *MessageQueue) processAvailable() {
	for {
		if !mq.limiter.tryConsume() {
			return
		}
		msg, ok := mq.dequeue()
		if !ok {
			// Nothing in queue; "return" the token we consumed.
			// We cannot un-consume from the bucket easily, but since
			// the bucket refills continuously this is acceptable.
			return
		}
		mq.processOne(msg)
	}
}

// dequeue removes and returns the front message, or returns false if empty.
func (mq *MessageQueue) dequeue() (queuedMessage, bool) {
	mq.mu.Lock()
	defer mq.mu.Unlock()
	if len(mq.queue) == 0 {
		return queuedMessage{}, false
	}
	msg := mq.queue[0]
	mq.queue = mq.queue[1:]
	return msg, true
}

// processOne sends a single message. On failure it may re-enqueue for retry.
func (mq *MessageQueue) processOne(msg queuedMessage) {
	text := msg.Text
	if len(text) > maxMessageLength {
		text = text[:maxMessageLength]
	}

	err := mq.sendTelegram(msg.ChatID, text, msg.Keyboard)
	if err == nil {
		return
	}

	// Try to parse as a Telegram API error to decide retry strategy.
	apiErr, ok := err.(*telegramAPIError)
	if !ok {
		// Network or other non-API error -- always retry.
		mq.maybeRetry(msg, err)
		return
	}

	if apiErr.StatusCode == 429 || apiErr.StatusCode >= 500 {
		mq.maybeRetry(msg, err)
		return
	}

	// Other 4xx -- permanent failure, discard.
	log.Printf("[telegram/queue] discarding message to chat %s: %v", msg.ChatID, err)
}

// maybeRetry re-enqueues a message if it has retries remaining.
func (mq *MessageQueue) maybeRetry(msg queuedMessage, err error) {
	if msg.Retries >= msg.MaxRetries {
		log.Printf("[telegram/queue] giving up on message to chat %s after %d retries: %v",
			msg.ChatID, msg.Retries, err)
		return
	}

	msg.Retries++
	backoff := time.Duration(math.Pow(2, float64(msg.Retries-1))) * time.Second // 1s, 2s, 4s
	log.Printf("[telegram/queue] retrying message to chat %s in %v (attempt %d/%d): %v",
		msg.ChatID, backoff, msg.Retries, msg.MaxRetries, err)

	go func() {
		// R26 · Low panic risk here (just slice append + channel
		// send) but recover anyway so a malformed stopCh close or
		// anything else doesn't take the retry path down silently.
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[telegram/queue] retry goroutine panic: %v", rec)
			}
		}()
		select {
		case <-time.After(backoff):
			mq.mu.Lock()
			mq.queue = append(mq.queue, msg)
			mq.mu.Unlock()

			select {
			case mq.notify <- struct{}{}:
			default:
			}
		case <-mq.stopCh:
			// Shutting down, drop the retry.
		}
	}()
}

// drainOnShutdown attempts to send remaining queued messages best-effort,
// bounded by a wall-clock deadline so a slow/unreachable Telegram (each send
// can block up to apiTimeout) can't hang process shutdown indefinitely.
func (mq *MessageQueue) drainOnShutdown() {
	mq.mu.Lock()
	remaining := make([]queuedMessage, len(mq.queue))
	copy(remaining, mq.queue)
	mq.queue = mq.queue[:0]
	mq.mu.Unlock()

	deadline := time.Now().Add(drainDeadline)
	for i, msg := range remaining {
		if time.Now().After(deadline) {
			log.Printf("[telegram/queue] shutdown drain deadline reached; dropping %d undelivered message(s)",
				len(remaining)-i)
			return
		}
		text := msg.Text
		if len(text) > maxMessageLength {
			text = text[:maxMessageLength]
		}
		if err := mq.sendTelegram(msg.ChatID, text, msg.Keyboard); err != nil {
			log.Printf("[telegram/queue] failed to drain message to chat %s on shutdown: %v",
				msg.ChatID, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Telegram API call
// ---------------------------------------------------------------------------

type telegramAPIError struct {
	StatusCode  int
	ErrorCode   int
	Description string
}

func (e *telegramAPIError) Error() string {
	return fmt.Sprintf("telegram API error %d (code %d): %s",
		e.StatusCode, e.ErrorCode, e.Description)
}

// sendTelegram sends a message to the Telegram Bot API directly.
func (mq *MessageQueue) sendTelegram(chatID, text string, keyboard *InlineKeyboardMarkup) error {
	// Capture the token ONCE so the redaction below scrubs the exact token the
	// URL was built from. Using mq.token (the construction-time field) here while
	// the URL is built from currentToken() (the live/rotated token) would leave
	// the live token un-redacted in any *url.Error on the failure path.
	tok := mq.currentToken()
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", tok)

	payload := map[string]interface{}{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "HTML",
	}
	if keyboard != nil {
		payload["reply_markup"] = keyboard
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	resp, err := mq.client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		// R28 · *url.Error from http.Client embeds the URL (with token)
		// in its Error() output; redact the SAME token the URL was built
		// from so retries/logs downstream never see the credential.
		return fmt.Errorf("http post: %w", redactTokenErr(err, tok))
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return nil
	}

	// Read the error body and try to parse as APIErrorResponse.
	data, _ := io.ReadAll(resp.Body)
	var apiResp APIErrorResponse
	if json.Unmarshal(data, &apiResp) == nil && !apiResp.OK {
		return &telegramAPIError{
			StatusCode:  resp.StatusCode,
			ErrorCode:   apiResp.ErrorCode,
			Description: apiResp.Description,
		}
	}

	return &telegramAPIError{
		StatusCode:  resp.StatusCode,
		ErrorCode:   resp.StatusCode,
		Description: string(data),
	}
}
