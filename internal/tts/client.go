package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kshaham/jarvis/internal/logscrub"
)

const (
	// maxResponseBytes caps a successful response body so a misbehaving or
	// hostile endpoint (baseURL is operator-configurable) can't OOM the daemon.
	maxResponseBytes = 32 << 20 // 32 MiB
	// maxErrorBodyBytes caps the echoed error body.
	maxErrorBodyBytes = 8 << 10 // 8 KiB
)

// ErrNoAPIKey is returned when no OpenAI key is configured. The handler maps
// this to an actionable "configure OpenAI key" response rather than a 500.
var ErrNoAPIKey = errors.New("tts: no OpenAI API key configured")

const (
	defaultBaseURL = "https://api.openai.com/v1"
	SpeechModel    = "gpt-4o-mini-tts"
	ScriptModel    = "gpt-4.1-mini"
)

// Client is a minimal OpenAI Audio + Chat client.
type Client struct {
	apiKey  string
	baseURL string
	http    *http.Client
}

// NewClient builds a client. baseURL may be empty to use the OpenAI default;
// tests pass an httptest URL.
func NewClient(apiKey, baseURL string) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 90 * time.Second},
	}
}

// SpeechRequest configures one /audio/speech call.
type SpeechRequest struct {
	Voice        string
	Input        string
	Instructions string // optional tone guidance (gpt-4o-mini-tts)
}

// CreateSpeech returns raw MP3 bytes for the given voice + text.
func (c *Client) CreateSpeech(ctx context.Context, req SpeechRequest) ([]byte, error) {
	if c.apiKey == "" {
		return nil, ErrNoAPIKey
	}
	payload := map[string]any{
		"model":           SpeechModel,
		"voice":           req.Voice,
		"input":           req.Input,
		"response_format": "mp3",
	}
	if req.Instructions != "" {
		payload["instructions"] = req.Instructions
	}
	body, err := c.post(ctx, "/audio/speech", payload)
	if err != nil {
		return nil, err
	}
	return body, nil
}

// Script is the condensed, ordered narration.
type Script struct {
	Intro    string          `json:"intro"`
	Segments []ScriptSegment `json:"segments"`
}

// ScriptSegment is one speaker's condensed line.
type ScriptSegment struct {
	SessionID string `json:"sessionId"`
	Text      string `json:"text"`
}

// CondenseScript asks a small chat model to turn the raw transcript into a
// short intro + ordered per-speaker segments under wordBudget words.
func (c *Client) CondenseScript(ctx context.Context, transcript string, wordBudget int) (Script, error) {
	if c.apiKey == "" {
		return Script{}, ErrNoAPIKey
	}
	sys := fmt.Sprintf(`You write concise spoken standup recaps. Return ONLY JSON of the form {"intro": string, "segments": [{"sessionId": string, "text": string}]}. The intro is one short sentence spoken by a narrator and MUST state that the following voices are AI-generated. Preserve the order speakers appear in the transcript. Keep the TOTAL words across intro + all segments under %d. Use each speaker's sessionId exactly as given. Do not invent speakers.`, wordBudget)
	payload := map[string]any{
		"model": ScriptModel,
		"messages": []map[string]string{
			{"role": "system", "content": sys},
			{"role": "user", "content": transcript},
		},
		"response_format": map[string]string{"type": "json_object"},
	}
	raw, err := c.post(ctx, "/chat/completions", payload)
	if err != nil {
		return Script{}, err
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Script{}, fmt.Errorf("tts: decode chat response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return Script{}, errors.New("tts: chat response had no choices")
	}
	var script Script
	if err := json.Unmarshal([]byte(parsed.Choices[0].Message.Content), &script); err != nil {
		return Script{}, fmt.Errorf("tts: decode script JSON: %w", err)
	}
	return script, nil
}

// post sends a JSON body and returns the raw response bytes (audio or JSON).
func (c *Client) post(ctx context.Context, path string, payload any) ([]byte, error) {
	buf, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Read only a bounded slice of the error body and SCRUB it: OpenAI's 401
		// body echoes the submitted key ("Incorrect API key provided: sk-..."),
		// and this error string is surfaced to SSE clients / Telegram / the
		// replay buffer. logscrub.All redacts the key; the cap bounds a hostile
		// baseURL returning a huge body.
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return nil, fmt.Errorf("tts: openai %s -> %d: %s", path, resp.StatusCode,
			logscrub.All(strings.TrimSpace(string(errBody))))
	}
	// Bound the success body so an arbitrarily large (or attacker-streamed)
	// response can't exhaust memory within the request timeout window.
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, err
	}
	return data, nil
}
