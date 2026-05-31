package notifications

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// spyTelegram records every SendFormattedMessage call so tests can assert
// the chat ID, text body, and keyboard passed to the telegram queue.
type spyTelegram struct {
	mu    sync.Mutex
	calls []spyTelegramCall
}

type spyTelegramCall struct {
	chatID   string
	text     string
	keyboard interface{}
}

func (s *spyTelegram) SendFormattedMessage(chatID, text string, keyboard interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, spyTelegramCall{chatID: chatID, text: text, keyboard: keyboard})
}

func (s *spyTelegram) snapshot() []spyTelegramCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]spyTelegramCall, len(s.calls))
	copy(out, s.calls)
	return out
}

// ---- sendTelegram ----

func TestSendTelegram_NilSenderErrors(t *testing.T) {
	err := sendTelegram(nil, ChannelSpec{Target: "123"}, Dispatch{Event: "x"})
	if err == nil {
		t.Fatal("expected error when sender is nil")
	}
	if !strings.Contains(err.Error(), "not configured") {
		t.Errorf("error should mention not configured, got %v", err)
	}
}

func TestSendTelegram_EmptyTargetErrors(t *testing.T) {
	spy := &spyTelegram{}
	err := sendTelegram(spy, ChannelSpec{Target: ""}, Dispatch{Event: "x"})
	if err == nil {
		t.Fatal("expected error when target is empty")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error should mention empty target, got %v", err)
	}
	if len(spy.snapshot()) != 0 {
		t.Error("sender should not be called when target is empty")
	}
}

func TestSendTelegram_SendsFormattedMessageWithSummary(t *testing.T) {
	spy := &spyTelegram{}
	spec := ChannelSpec{Type: ChannelTelegram, Target: "@mychannel"}
	d := Dispatch{
		Event:     "session:idle",
		RouteName: "Idle Alert",
		Data:      map[string]string{"id": "s-42"},
	}
	if err := sendTelegram(spy, spec, d); err != nil {
		t.Fatalf("sendTelegram: %v", err)
	}
	calls := spy.snapshot()
	if len(calls) != 1 {
		t.Fatalf("expected exactly 1 send, got %d", len(calls))
	}
	got := calls[0]
	if got.chatID != "@mychannel" {
		t.Errorf("chatID = %q, want @mychannel", got.chatID)
	}
	// Header line is "[event] routeName".
	if !strings.HasPrefix(got.text, "[session:idle] Idle Alert") {
		t.Errorf("text header wrong: %q", got.text)
	}
	// Summary appended on its own line.
	if !strings.Contains(got.text, `"id":"s-42"`) {
		t.Errorf("text should contain payload summary, got %q", got.text)
	}
	if !strings.Contains(got.text, "\n") {
		t.Errorf("summary should be on a new line, got %q", got.text)
	}
	if got.keyboard != nil {
		t.Errorf("keyboard should be nil, got %v", got.keyboard)
	}
}

func TestSendTelegram_NoSummaryWhenDataNil(t *testing.T) {
	spy := &spyTelegram{}
	d := Dispatch{Event: "session:idle", RouteName: "R", Data: nil}
	if err := sendTelegram(spy, ChannelSpec{Target: "1"}, d); err != nil {
		t.Fatalf("sendTelegram: %v", err)
	}
	calls := spy.snapshot()
	if len(calls) != 1 {
		t.Fatalf("expected 1 send, got %d", len(calls))
	}
	if strings.Contains(calls[0].text, "\n") {
		t.Errorf("no summary expected when data is nil, got %q", calls[0].text)
	}
	if calls[0].text != "[session:idle] R" {
		t.Errorf("text = %q, want exactly the header", calls[0].text)
	}
}

// ---- sendWebhook ----

func TestSendWebhook_PostsJSONWithHeaders(t *testing.T) {
	var (
		gotMethod      string
		gotContentType string
		gotCustom      string
		gotBody        Dispatch
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		gotCustom = r.Header.Get("X-Auth")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	spec := ChannelSpec{
		Type:    ChannelWebhook,
		Target:  srv.URL,
		Headers: map[string]string{"X-Auth": "secret-token"},
	}
	d := Dispatch{Event: "office:agentStuck", RouteID: "r-9", RouteName: "Stuck"}
	if err := sendWebhook(context.Background(), srv.Client(), spec, d); err != nil {
		t.Fatalf("sendWebhook: %v", err)
	}
	if gotMethod != "POST" {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotContentType != "application/json" {
		t.Errorf("content-type = %q, want application/json", gotContentType)
	}
	if gotCustom != "secret-token" {
		t.Errorf("custom header = %q, want secret-token", gotCustom)
	}
	if gotBody.Event != "office:agentStuck" || gotBody.RouteID != "r-9" {
		t.Errorf("decoded body = %+v, want event/routeId preserved", gotBody)
	}
}

func TestSendWebhook_4xxReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	err := sendWebhook(context.Background(), srv.Client(), ChannelSpec{Target: srv.URL}, Dispatch{Event: "x"})
	if err == nil {
		t.Fatal("expected error for 403 response")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error should mention HTTP 403, got %v", err)
	}
}

func TestSendWebhook_5xxReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	err := sendWebhook(context.Background(), srv.Client(), ChannelSpec{Target: srv.URL}, Dispatch{Event: "x"})
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention HTTP 500, got %v", err)
	}
}

func TestSendWebhook_2xxAnd3xxSucceed(t *testing.T) {
	// 399 is below the 400 error threshold and must be treated as success.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(399)
	}))
	defer srv.Close()
	if err := sendWebhook(context.Background(), srv.Client(), ChannelSpec{Target: srv.URL}, Dispatch{Event: "x"}); err != nil {
		t.Errorf("status 399 should not error, got %v", err)
	}
}

func TestSendWebhook_BadURLReturnsRequestError(t *testing.T) {
	// A malformed URL fails at request build time.
	err := sendWebhook(context.Background(), http.DefaultClient, ChannelSpec{Target: "http://[::1]:namedport"}, Dispatch{Event: "x"})
	if err == nil {
		t.Fatal("expected error for malformed URL")
	}
}

func TestSendWebhook_TransportErrorPropagates(t *testing.T) {
	// Point at a server we immediately close so the request fails at the
	// transport layer (connection refused).
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	err := sendWebhook(context.Background(), &http.Client{Timeout: time.Second}, ChannelSpec{Target: url}, Dispatch{Event: "x"})
	if err == nil {
		t.Fatal("expected transport error after server closed")
	}
	if !strings.Contains(err.Error(), "webhook request") {
		t.Errorf("error should wrap webhook request, got %v", err)
	}
}

func TestSendExpoPush_SuccessThroughRedirect(t *testing.T) {
	var gotToken, gotTitle, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var arr []map[string]interface{}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &arr)
		if len(arr) == 1 {
			gotToken, _ = arr[0]["to"].(string)
			gotTitle, _ = arr[0]["title"].(string)
			gotBody, _ = arr[0]["body"].(string)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data":[{"status":"ok"}]}`))
	}))
	defer srv.Close()

	// redirectTransport (defined in expo_test.go) rewrites the hardcoded
	// expo endpoint to our test server, exercising the real success path.
	client := &http.Client{Transport: redirectTransport{to: srv.URL, base: http.DefaultTransport}}
	spec := ChannelSpec{Type: ChannelExpoPush, Target: "ExponentPushToken[xyz]"}
	d := Dispatch{Event: "session:idle", RouteName: "Push", Data: map[string]string{"id": "s-7"}}
	if err := sendExpoPush(context.Background(), client, spec, d); err != nil {
		t.Fatalf("sendExpoPush success path: %v", err)
	}
	if gotToken != "ExponentPushToken[xyz]" {
		t.Errorf("to = %q, want ExponentPushToken[xyz]", gotToken)
	}
	if !strings.Contains(gotTitle, "session:idle") {
		t.Errorf("title missing event: %q", gotTitle)
	}
	if !strings.Contains(gotBody, "s-7") {
		t.Errorf("body should carry payload summary, got %q", gotBody)
	}
}

func TestSendExpoPush_EmptyBodyFallsBackToDefault(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var arr []map[string]interface{}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &arr)
		if len(arr) == 1 {
			gotBody, _ = arr[0]["body"].(string)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &http.Client{Transport: redirectTransport{to: srv.URL, base: http.DefaultTransport}}
	// nil Data -> payloadSummary "" -> default body text.
	d := Dispatch{Event: "x", RouteName: "R", Data: nil}
	if err := sendExpoPush(context.Background(), client, ChannelSpec{Target: "ExponentPushToken[z]"}, d); err != nil {
		t.Fatalf("sendExpoPush: %v", err)
	}
	if !strings.Contains(gotBody, "jarvis-mobile") {
		t.Errorf("empty payload should fall back to default body, got %q", gotBody)
	}
}

func TestSendExpoPush_TransportErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	client := &http.Client{Transport: redirectTransport{to: url, base: http.DefaultTransport}, Timeout: time.Second}
	err := sendExpoPush(context.Background(), client, ChannelSpec{Target: "ExponentPushToken[z]"}, Dispatch{Event: "x"})
	if err == nil {
		t.Fatal("expected transport error after server closed")
	}
	if !strings.Contains(err.Error(), "expo push request") {
		t.Errorf("error should wrap expo push request, got %v", err)
	}
}

// ---- sendLog ----

func TestSendLog_AlwaysSucceeds(t *testing.T) {
	// sendLog only emits a slog line and returns nil. Drive it with both
	// populated and empty data to exercise the call without panicking.
	if err := sendLog(ChannelSpec{Target: "standup"}, Dispatch{Event: "x", RouteName: "R", Data: map[string]int{"n": 1}}); err != nil {
		t.Errorf("sendLog returned error: %v", err)
	}
	if err := sendLog(ChannelSpec{Target: ""}, Dispatch{Event: "y", Data: nil}); err != nil {
		t.Errorf("sendLog (empty) returned error: %v", err)
	}
}

// ---- payloadSummary ----

func TestPayloadSummary_NilReturnsEmpty(t *testing.T) {
	if got := payloadSummary(nil); got != "" {
		t.Errorf("payloadSummary(nil) = %q, want empty", got)
	}
}

func TestPayloadSummary_EncodesJSON(t *testing.T) {
	got := payloadSummary(map[string]string{"k": "v"})
	if got != `{"k":"v"}` {
		t.Errorf("payloadSummary = %q, want {\"k\":\"v\"}", got)
	}
}

func TestPayloadSummary_TruncatesLongOutput(t *testing.T) {
	// Build a value whose JSON exceeds 280 chars.
	long := strings.Repeat("a", 400)
	got := payloadSummary(map[string]string{"big": long})
	if !strings.HasSuffix(got, "...") {
		t.Errorf("expected truncation ellipsis, got suffix %q", got[len(got)-5:])
	}
	// 280 chars of content + the 3-char ellipsis.
	if len(got) != 283 {
		t.Errorf("truncated length = %d, want 283", len(got))
	}
}

func TestPayloadSummary_UnmarshalableReturnsEmpty(t *testing.T) {
	// A channel cannot be JSON-marshalled, forcing the error branch.
	got := payloadSummary(make(chan int))
	if got != "" {
		t.Errorf("payloadSummary(chan) = %q, want empty on marshal error", got)
	}
}

func TestPayloadSummary_ExactlyAtBoundaryNotTruncated(t *testing.T) {
	// Construct a string whose JSON form is exactly 280 chars: a quoted
	// string of 278 'a' characters -> 278 + 2 quotes = 280.
	val := strings.Repeat("a", 278)
	got := payloadSummary(val)
	if len(got) != 280 {
		t.Fatalf("setup wrong: len=%d, want 280", len(got))
	}
	if strings.HasSuffix(got, "...") {
		t.Errorf("280-char output should NOT be truncated, got ...suffix")
	}
}

// ---- parseChatID ----

func TestParseChatID(t *testing.T) {
	cases := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"123", 123, false},
		{"-1001234567890", -1001234567890, false},
		{"0", 0, false},
		{"notanumber", 0, true},
	}
	for _, c := range cases {
		got, err := parseChatID(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseChatID(%q): expected error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseChatID(%q): unexpected error %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("parseChatID(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}
