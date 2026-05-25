package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// webhookTimeout caps how long a single outbound webhook call can take.
// Bounded so a stuck remote can't starve the dispatch worker pool.
const webhookTimeout = 10 * time.Second

// TelegramSender is the minimal interface for sending a message to a chat.
// Kept string-based for chat IDs because Telegram supports both numeric IDs
// and channel usernames ("@mychannel"), and the existing *telegram.Handler
// queue uses strings. Zero coupling to the telegram package from here.
type TelegramSender interface {
	SendFormattedMessage(chatID, text string, keyboard interface{})
}

// Dispatch is the data payload a channel receives.
type Dispatch struct {
	Event     string      `json:"event"`
	Data      interface{} `json:"data"`
	RouteID   string      `json:"routeId"`
	RouteName string      `json:"routeName"`
	Timestamp time.Time   `json:"timestamp"`
}

// sendTelegram posts to the configured chat via the injected sender. The
// existing telegram queue is fire-and-forget, so we don't get a delivery
// confirmation here; we log and increment metrics based on queue acceptance.
func sendTelegram(sender TelegramSender, spec ChannelSpec, d Dispatch) error {
	if sender == nil {
		return fmt.Errorf("telegram sender not configured")
	}
	if spec.Target == "" {
		return fmt.Errorf("telegram target (chat ID) is empty")
	}
	text := fmt.Sprintf("[%s] %s", d.Event, d.RouteName)
	if summary := payloadSummary(d.Data); summary != "" {
		text += "\n" + summary
	}
	sender.SendFormattedMessage(spec.Target, text, nil)
	return nil
}

// sendWebhook POSTs a JSON Dispatch to the target URL with any configured
// headers. 10-second timeout, no retries (users wanting durability layer a
// real queue on top).
func sendWebhook(ctx context.Context, client *http.Client, spec ChannelSpec, d Dispatch) error {
	body, err := json.Marshal(d)
	if err != nil {
		return fmt.Errorf("marshal webhook payload: %w", err)
	}
	reqCtx, cancel := context.WithTimeout(ctx, webhookTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, "POST", spec.Target, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range spec.Headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// expoPushEndpoint is the public Expo push API. No auth required: the
// ExponentPushToken in spec.Target itself authenticates the recipient.
// Documented at https://docs.expo.dev/push-notifications/sending-notifications/.
const expoPushEndpoint = "https://exp.host/--/api/v2/push/send"

// sendExpoPush dispatches a single notification through Expo's push
// service. Routing logic (which events go to which device tokens) is
// the same as for every other channel -- this function only knows how
// to translate one Dispatch into one Expo push payload.
//
// Failures are not retried; transient errors are logged and the
// dispatcher's metric increments accordingly. Devices with stale or
// revoked tokens get a "DeviceNotRegistered" response which the caller
// can wire into auto-cleanup later (Phase 2 leaves the row in place).
func sendExpoPush(ctx context.Context, client *http.Client, spec ChannelSpec, d Dispatch) error {
	if spec.Target == "" {
		return fmt.Errorf("expo push target (token) is empty")
	}
	title := fmt.Sprintf("[%s] %s", d.Event, d.RouteName)
	body := payloadSummary(d.Data)
	if body == "" {
		body = "Tap to open jarvis-mobile."
	}
	payload := []map[string]interface{}{
		{
			"to":       spec.Target,
			"title":    title,
			"body":     body,
			"sound":    "default",
			"priority": "high",
			"data": map[string]interface{}{
				"event":     d.Event,
				"route":     d.RouteName,
				"routeId":   d.RouteID,
				"timestamp": d.Timestamp,
				"payload":   d.Data,
			},
		},
	}
	buf, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal expo push payload: %w", err)
	}
	reqCtx, cancel := context.WithTimeout(ctx, webhookTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, "POST", expoPushEndpoint, bytes.NewReader(buf))
	if err != nil {
		return fmt.Errorf("build expo push request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "gzip, deflate")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("expo push request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("expo push returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// sendLog emits a structured slog entry. Useful for piping to external log
// aggregators (Loki, CloudWatch) without a separate webhook.
func sendLog(spec ChannelSpec, d Dispatch) error {
	slog.Info("notification",
		"label", spec.Target,
		"event", d.Event,
		"route", d.RouteName,
		"data", d.Data,
	)
	return nil
}

// payloadSummary renders the payload into a short display string without
// overwhelming the message. JSON-encodes with a 280-char cap.
func payloadSummary(data interface{}) string {
	if data == nil {
		return ""
	}
	b, err := json.Marshal(data)
	if err != nil {
		return ""
	}
	s := string(b)
	if len(s) > 280 {
		return s[:280] + "..."
	}
	return s
}

// parseChatID parses Telegram chat IDs which are int64 but may arrive as
// either quoted strings or raw numbers in JSON.
func parseChatID(s string) (int64, error) {
	var id int64
	_, err := fmt.Sscanf(s, "%d", &id)
	return id, err
}
