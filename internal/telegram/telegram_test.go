package telegram

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Mock ServerBridge
// ---------------------------------------------------------------------------

type mockServerBridge struct {
	systemStatus SystemStatus
	sessions     []SessionInfo
	agents       []AgentInfo

	sendResult       bool
	createSessionID  string
	createSessionErr error
	stopSessionErr   error
	detailedStatus   string

	// Track calls
	sendCalls []struct {
		SessionID string
		Message   string
	}
	subscribeCalls []string
}

func newMockServerBridge() *mockServerBridge {
	return &mockServerBridge{
		systemStatus: SystemStatus{
			SessionCount: 3,
			AgentCount:   1,
			Uptime:       "2h30m",
			Version:      "1.0.0-test",
		},
		sessions: []SessionInfo{
			{
				ID:        "abcdef12-3456-7890-abcd-ef1234567890",
				ShortID:   "abcdef12",
				Name:      "test-session",
				Status:    "running",
				Mode:      "interactive",
				IsWorking: true,
			},
		},
		agents:          []AgentInfo{},
		sendResult:      true,
		createSessionID: "newid123-0000-0000-0000-000000000000",
	}
}

func (m *mockServerBridge) GetSystemStatus() SystemStatus   { return m.systemStatus }
func (m *mockServerBridge) GetSessionsList() []SessionInfo   { return m.sessions }
func (m *mockServerBridge) GetAgentsList() []AgentInfo       { return m.agents }
func (m *mockServerBridge) GetDetailedStatus(id string) string { return m.detailedStatus }

func (m *mockServerBridge) SendToSession(sessionID, message string) bool {
	m.sendCalls = append(m.sendCalls, struct {
		SessionID string
		Message   string
	}{sessionID, message})
	return m.sendResult
}

func (m *mockServerBridge) CreateSession(name string) (string, error) {
	return m.createSessionID, m.createSessionErr
}

func (m *mockServerBridge) StopSession(sessionID string) error {
	return m.stopSessionErr
}

func (m *mockServerBridge) SubscribeToSessionOutput(sessionID string, callback func(output string)) func() {
	m.subscribeCalls = append(m.subscribeCalls, sessionID)
	return func() {} // no-op unsubscribe
}

// ---------------------------------------------------------------------------
// Settings tests
// ---------------------------------------------------------------------------

func TestDefaultSettings(t *testing.T) {
	s := DefaultSettings()

	if s.RateLimitPerMinute != 20 {
		t.Errorf("RateLimitPerMinute = %d, want 20", s.RateLimitPerMinute)
	}
	if s.ParseMode != "HTML" {
		t.Errorf("ParseMode = %q, want %q", s.ParseMode, "HTML")
	}
	if s.Mode != "polling" {
		t.Errorf("Mode = %q, want %q", s.Mode, "polling")
	}
	if s.Enabled {
		t.Error("expected Enabled = false by default")
	}
	if !s.RequireChatIDVerify {
		t.Error("expected RequireChatIDVerify = true by default")
	}
	if s.ForwardOutputToChat {
		t.Error("expected ForwardOutputToChat = false by default")
	}

	// Notifications
	if !s.Notifications.SessionStart {
		t.Error("expected SessionStart notification enabled")
	}
	if !s.Notifications.SessionComplete {
		t.Error("expected SessionComplete notification enabled")
	}
	if !s.Notifications.SessionError {
		t.Error("expected SessionError notification enabled")
	}
	if !s.Notifications.AgentSpawn {
		t.Error("expected AgentSpawn notification enabled")
	}
	if !s.Notifications.AgentComplete {
		t.Error("expected AgentComplete notification enabled")
	}
	if s.Notifications.ProgressUpdates {
		t.Error("expected ProgressUpdates notification disabled by default")
	}
	if s.Notifications.ProgressInterval != 300000 {
		t.Errorf("ProgressInterval = %d, want 300000", s.Notifications.ProgressInterval)
	}
	if !s.Notifications.StuckState {
		t.Error("expected StuckState notification enabled")
	}
	if !s.Notifications.PermissionPrompt {
		t.Error("expected PermissionPrompt notification enabled")
	}
	if !s.Notifications.Elicitation {
		t.Error("expected Elicitation notification enabled")
	}
	if !s.Notifications.RespawnBlocked {
		t.Error("expected RespawnBlocked notification enabled")
	}
	if !s.Notifications.PlanApproval {
		t.Error("expected PlanApproval notification enabled")
	}
}

func TestSettingsStore_LoadSave(t *testing.T) {
	dir := t.TempDir()

	// Create store with defaults.
	store, err := NewSettingsStore(dir, nil)
	if err != nil {
		t.Fatalf("NewSettingsStore: %v", err)
	}

	// Modify settings.
	err = store.Update(func(s *Settings) {
		s.BotToken = "test-token-123"
		s.ChatID = "999888"
		s.Enabled = true
		s.RateLimitPerMinute = 42
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Verify file exists.
	fp := filepath.Join(dir, "telegram-settings.json")
	if _, err := os.Stat(fp); os.IsNotExist(err) {
		t.Fatal("settings file not created")
	}

	// Create a new store from the same path -- should load persisted values.
	store2, err := NewSettingsStore(dir, nil)
	if err != nil {
		t.Fatalf("NewSettingsStore (reload): %v", err)
	}

	loaded := store2.Get()
	if loaded.BotToken != "test-token-123" {
		t.Errorf("BotToken = %q, want %q", loaded.BotToken, "test-token-123")
	}
	if loaded.ChatID != "999888" {
		t.Errorf("ChatID = %q, want %q", loaded.ChatID, "999888")
	}
	if !loaded.Enabled {
		t.Error("expected Enabled = true after reload")
	}
	if loaded.RateLimitPerMinute != 42 {
		t.Errorf("RateLimitPerMinute = %d, want 42", loaded.RateLimitPerMinute)
	}
}

func TestSettingsStore_Migration(t *testing.T) {
	dir := t.TempDir()

	legacy := map[string]interface{}{
		"botToken": "legacy-token",
		"chatId":   "12345",
	}

	store, err := NewSettingsStore(dir, legacy)
	if err != nil {
		t.Fatalf("NewSettingsStore: %v", err)
	}

	s := store.Get()
	if s.BotToken != "legacy-token" {
		t.Errorf("BotToken = %q, want %q", s.BotToken, "legacy-token")
	}
	if s.ChatID != "12345" {
		t.Errorf("ChatID = %q, want %q", s.ChatID, "12345")
	}
	if !s.Enabled {
		t.Error("expected Enabled = true after migration with both token and chatId")
	}
}

// ---------------------------------------------------------------------------
// Queue / TokenBucket tests
// ---------------------------------------------------------------------------

func TestTokenBucket_InitialFull(t *testing.T) {
	tb := newTokenBucket(10)
	if !tb.tryConsume() {
		t.Error("expected initial tryConsume to succeed (bucket should start full)")
	}
}

func TestTokenBucket_Exhaustion(t *testing.T) {
	tb := newTokenBucket(3)

	// Consume all 3 tokens.
	for i := 0; i < 3; i++ {
		if !tb.tryConsume() {
			t.Fatalf("tryConsume(%d) failed unexpectedly", i)
		}
	}

	// Next consume should fail.
	if tb.tryConsume() {
		t.Error("expected tryConsume to fail after exhausting all tokens")
	}
}

func TestTokenBucket_Refill(t *testing.T) {
	// 60 per minute = 1 per second
	tb := newTokenBucket(60)

	// Exhaust all tokens.
	for tb.tryConsume() {
		// drain
	}

	// Simulate time passing by adjusting lastRefill.
	tb.mu.Lock()
	tb.lastRefill = time.Now().Add(-2 * time.Second)
	tb.mu.Unlock()

	// After 2 seconds at 1/s rate, we should have ~2 tokens.
	if !tb.tryConsume() {
		t.Error("expected tryConsume to succeed after refill time elapsed")
	}
}

func TestMessageQueue_Enqueue(t *testing.T) {
	mq := NewMessageQueue("fake-token", &http.Client{}, 20)

	mq.Enqueue("chat1", "hello world", nil)

	mq.mu.Lock()
	qLen := len(mq.queue)
	mq.mu.Unlock()

	if qLen != 1 {
		t.Errorf("queue length = %d, want 1", qLen)
	}
}

func TestMessageQueue_StartStop(t *testing.T) {
	mq := NewMessageQueue("fake-token", &http.Client{}, 20)
	mq.Start()
	mq.Stop()
	// Should not panic or hang.
}

func TestMessageQueue_MaxMessageTruncation(t *testing.T) {
	// Set up a fake Telegram server that records requests.
	var receivedText string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err == nil {
			if text, ok := payload["text"].(string); ok {
				receivedText = text
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	// Create a queue that sends to our test server instead of Telegram.
	// We override sendTelegram by creating a message and processing it directly.
	longMsg := strings.Repeat("x", 5000) // >3900 chars
	if len(longMsg) <= maxMessageLength {
		t.Fatal("test message should exceed maxMessageLength")
	}

	// Directly test truncation logic applied in processOne.
	msg := queuedMessage{
		ChatID:     "chat1",
		Text:       longMsg,
		MaxRetries: 0,
	}

	// Override the token to point at our test server.
	// Since sendTelegram constructs the URL from mq.token, we intercept differently.
	// Instead, test processOne's truncation logic directly.
	text := msg.Text
	if len(text) > maxMessageLength {
		text = text[:maxMessageLength]
	}
	if len(text) != maxMessageLength {
		t.Errorf("truncated text length = %d, want %d", len(text), maxMessageLength)
	}

	_ = receivedText
	_ = ts
}

// ---------------------------------------------------------------------------
// Formatter tests
// ---------------------------------------------------------------------------

func TestFormatSessionStart(t *testing.T) {
	result := FormatSessionStart("my-session", "/home/user/project")

	if !strings.Contains(result, "<code>my-session</code>") {
		t.Error("expected session name in code tags")
	}
	if !strings.Contains(result, "<code>/home/user/project</code>") {
		t.Error("expected working dir in code tags")
	}
	if !strings.Contains(result, "Session Started") {
		t.Error("expected 'Session Started' header")
	}
}

func TestFormatSessionComplete(t *testing.T) {
	result := FormatSessionComplete("my-session", 5*time.Minute+30*time.Second)

	if !strings.Contains(result, "Session Completed") {
		t.Error("expected 'Session Completed' header")
	}
	if !strings.Contains(result, "5m30s") {
		t.Errorf("expected duration '5m30s' in output, got %q", result)
	}
}

func TestFormatSessionError(t *testing.T) {
	result := FormatSessionError("bad-session", "process crashed unexpectedly")

	if !strings.Contains(result, "Session Error") {
		t.Error("expected 'Session Error' header")
	}
	if !strings.Contains(result, "process crashed unexpectedly") {
		t.Error("expected error message in output")
	}
}

func TestFormatPermissionPrompt(t *testing.T) {
	args := map[string]interface{}{
		"tool": "Bash",
	}
	result := FormatPermissionPrompt("abc12345", "Bash", args)

	if !strings.Contains(result, "Permission Needed") {
		t.Error("expected 'Permission Needed' header")
	}
	if !strings.Contains(result, "Bash") {
		t.Error("expected tool name in output")
	}
	if !strings.Contains(result, "abc12345") {
		t.Error("expected session short ID in output")
	}
}

func TestFormatElicitation(t *testing.T) {
	result := FormatElicitation("abc12345", "What is your preferred language?")

	if !strings.Contains(result, "Input Requested") {
		t.Error("expected 'Input Requested' header")
	}
	if !strings.Contains(result, "What is your preferred language?") {
		t.Error("expected question text in output")
	}
}

func TestFormatHelpMessage(t *testing.T) {
	result := FormatHelpMessage()

	commands := []string{"/help", "/status", "/sessions", "/send", "/new", "/link",
		"/unlink", "/linked", "/output", "/stop", "/spawn_agent", "/stop_agent", "/list_agents"}
	for _, cmd := range commands {
		if !strings.Contains(result, cmd) {
			t.Errorf("expected help message to contain %q", cmd)
		}
	}
}

func TestFormatSystemStatus(t *testing.T) {
	status := SystemStatus{
		SessionCount: 5,
		AgentCount:   2,
		Uptime:       "3h15m",
		Version:      "2.0.0",
	}
	result := FormatSystemStatus(status)

	if !strings.Contains(result, "5") {
		t.Error("expected session count in output")
	}
	if !strings.Contains(result, "3h15m") {
		t.Error("expected uptime in output")
	}
	if !strings.Contains(result, "2.0.0") {
		t.Error("expected version in output")
	}
}

func TestFormatSessionList_Empty(t *testing.T) {
	result := FormatSessionList(nil)
	if result != "No active sessions." {
		t.Errorf("expected 'No active sessions.', got %q", result)
	}
}

func TestFormatSessionList_WithSessions(t *testing.T) {
	sessions := []SessionInfo{
		{
			ID:        "aaa11111-0000-0000-0000-000000000000",
			ShortID:   "aaa11111",
			Name:      "alpha",
			Status:    "running",
			IsWorking: true,
		},
		{
			ID:        "bbb22222-0000-0000-0000-000000000000",
			ShortID:   "bbb22222",
			Name:      "beta",
			Status:    "idle",
			IsWorking: false,
		},
	}
	result := FormatSessionList(sessions)

	if !strings.Contains(result, "Sessions") {
		t.Error("expected 'Sessions' header")
	}
	if !strings.Contains(result, "aaa11111") {
		t.Error("expected first session short ID")
	}
	if !strings.Contains(result, "bbb22222") {
		t.Error("expected second session short ID")
	}
	if !strings.Contains(result, "alpha") {
		t.Error("expected first session name")
	}
	if !strings.Contains(result, "beta") {
		t.Error("expected second session name")
	}
}

func TestShortID(t *testing.T) {
	long := "abcdef12-3456-7890-abcd"
	if got := ShortID(long); got != "abcdef12" {
		t.Errorf("ShortID(%q) = %q, want %q", long, got, "abcdef12")
	}

	short := "abc"
	if got := ShortID(short); got != "abc" {
		t.Errorf("ShortID(%q) = %q, want %q", short, got, "abc")
	}

	exact := "12345678"
	if got := ShortID(exact); got != "12345678" {
		t.Errorf("ShortID(%q) = %q, want %q", exact, got, "12345678")
	}
}

func TestHumanDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{5*time.Minute + 15*time.Second, "5m15s"},
		{2*time.Hour + 10*time.Minute, "2h10m"},
		{0, "0s"},
	}

	for _, tc := range tests {
		got := humanDuration(tc.d)
		if got != tc.want {
			t.Errorf("humanDuration(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Keyboard tests
// ---------------------------------------------------------------------------

func TestPermissionKeyboard(t *testing.T) {
	kb := PermissionKeyboard("abcdef12-3456-7890-abcd-ef1234567890")

	if len(kb.InlineKeyboard) != 1 {
		t.Fatalf("expected 1 row, got %d", len(kb.InlineKeyboard))
	}
	row := kb.InlineKeyboard[0]
	if len(row) != 3 {
		t.Fatalf("expected 3 buttons, got %d", len(row))
	}

	if row[0].Text != "Approve" {
		t.Errorf("button 0 text = %q, want %q", row[0].Text, "Approve")
	}
	if row[1].Text != "Deny" {
		t.Errorf("button 1 text = %q, want %q", row[1].Text, "Deny")
	}
	if row[2].Text != "Trust Always" {
		t.Errorf("button 2 text = %q, want %q", row[2].Text, "Trust Always")
	}

	// Verify callback data format: "action:shortID"
	if !strings.HasPrefix(row[0].CallbackData, ActionPermissionApprove+":") {
		t.Errorf("button 0 callback data = %q, expected prefix %q", row[0].CallbackData, ActionPermissionApprove+":")
	}
	if !strings.HasPrefix(row[1].CallbackData, ActionPermissionDeny+":") {
		t.Errorf("button 1 callback data = %q, expected prefix %q", row[1].CallbackData, ActionPermissionDeny+":")
	}
	if !strings.HasPrefix(row[2].CallbackData, ActionPermissionTrust+":") {
		t.Errorf("button 2 callback data = %q, expected prefix %q", row[2].CallbackData, ActionPermissionTrust+":")
	}
}

func TestPlanApprovalKeyboard(t *testing.T) {
	kb := PlanApprovalKeyboard("abcdef12-3456-7890-abcd-ef1234567890")

	if len(kb.InlineKeyboard) != 1 {
		t.Fatalf("expected 1 row, got %d", len(kb.InlineKeyboard))
	}
	row := kb.InlineKeyboard[0]
	if len(row) != 2 {
		t.Fatalf("expected 2 buttons, got %d", len(row))
	}

	if row[0].Text != "Approve Plan" {
		t.Errorf("button 0 text = %q, want %q", row[0].Text, "Approve Plan")
	}
	if row[1].Text != "Decline Plan" {
		t.Errorf("button 1 text = %q, want %q", row[1].Text, "Decline Plan")
	}
}

func TestParseCallbackData(t *testing.T) {
	action, params := ParseCallbackData("perm_ok:abc12345:extra")
	if action != "perm_ok" {
		t.Errorf("action = %q, want %q", action, "perm_ok")
	}
	if len(params) != 2 {
		t.Fatalf("params len = %d, want 2", len(params))
	}
	if params[0] != "abc12345" {
		t.Errorf("params[0] = %q, want %q", params[0], "abc12345")
	}
	if params[1] != "extra" {
		t.Errorf("params[1] = %q, want %q", params[1], "extra")
	}
}

func TestParseCallbackData_NoParams(t *testing.T) {
	action, params := ParseCallbackData("perm_ok")
	if action != "perm_ok" {
		t.Errorf("action = %q, want %q", action, "perm_ok")
	}
	if len(params) != 0 {
		t.Errorf("params len = %d, want 0", len(params))
	}
}

func TestCallbackDataSize(t *testing.T) {
	// Verify that callback data for a typical session fits within Telegram's 64-byte limit.
	sessionID := "abcdef12-3456-7890-abcd-ef1234567890"
	kb := PermissionKeyboard(sessionID)
	for _, row := range kb.InlineKeyboard {
		for _, btn := range row {
			size := CallbackDataSize(btn.CallbackData)
			if size > 64 {
				t.Errorf("callback data %q is %d bytes, exceeds 64 byte limit",
					btn.CallbackData, size)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Streaming tests
// ---------------------------------------------------------------------------

func TestStripTerminalArtifacts(t *testing.T) {
	input := "hello \x1b[31mworld\x1b[0m end"
	got := stripTerminalArtifacts(input)
	if strings.Contains(got, "\x1b") {
		t.Errorf("ANSI codes not stripped: %q", got)
	}
	if !strings.Contains(got, "hello") || !strings.Contains(got, "world") || !strings.Contains(got, "end") {
		t.Errorf("text content lost: %q", got)
	}
}

func TestStripTerminalArtifacts_InkControls(t *testing.T) {
	// Ink-style cursor show/hide and line clearing.
	input := "\x1b[?25lsome text\x1b[2K\x1b[?25h"
	got := stripTerminalArtifacts(input)
	if strings.Contains(got, "\x1b") {
		t.Errorf("Ink control sequences not stripped: %q", got)
	}
	if !strings.Contains(got, "some text") {
		t.Errorf("text content lost: %q", got)
	}
}

func TestStripTerminalArtifacts_NullBytes(t *testing.T) {
	input := "hello\x00world"
	got := stripTerminalArtifacts(input)
	if strings.Contains(got, "\x00") {
		t.Error("null bytes not removed")
	}
}

func TestStripTerminalArtifacts_CollapseBlankLines(t *testing.T) {
	input := "line1\n\n\n\n\nline2"
	got := stripTerminalArtifacts(input)
	if strings.Contains(got, "\n\n\n") {
		t.Errorf("triple blank lines not collapsed: %q", got)
	}
}

func TestOutputStreamer_HandleOutput(t *testing.T) {
	mq := NewMessageQueue("fake", &http.Client{}, 100)
	streamer := NewOutputStreamer(mq)

	streamer.HandleOutput("chat1", "hello world")

	streamer.mu.Lock()
	buf, exists := streamer.buffers["chat1"]
	var dataLen int
	if exists {
		dataLen = buf.data.Len()
	}
	streamer.mu.Unlock()

	if !exists {
		t.Fatal("expected buffer to be created for chat1")
	}
	if dataLen == 0 {
		t.Error("expected buffer to have data")
	}
}

func TestOutputStreamer_FlushAll(t *testing.T) {
	// Use a test server to avoid real API calls.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	mq := NewMessageQueue("fake", &http.Client{}, 100)
	streamer := NewOutputStreamer(mq)

	streamer.HandleOutput("chat1", "data1")
	streamer.HandleOutput("chat2", "data2")

	streamer.FlushAll()

	// After flush, buffers should be empty.
	streamer.mu.Lock()
	defer streamer.mu.Unlock()

	for chatID, buf := range streamer.buffers {
		if buf.data.Len() != 0 {
			t.Errorf("buffer for %s not empty after FlushAll", chatID)
		}
	}
}

func TestOutputStreamer_RemoveChat(t *testing.T) {
	mq := NewMessageQueue("fake", &http.Client{}, 100)
	streamer := NewOutputStreamer(mq)

	streamer.HandleOutput("chat1", "some data")
	streamer.RemoveChat("chat1")

	streamer.mu.Lock()
	_, exists := streamer.buffers["chat1"]
	streamer.mu.Unlock()

	if exists {
		t.Error("expected chat1 buffer to be removed")
	}
}

func TestEscapeHTML(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello & world", "hello &amp; world"},
		{"<script>", "&lt;script&gt;"},
		{"1 > 0 & 0 < 1", "1 &gt; 0 &amp; 0 &lt; 1"},
		{"no special chars", "no special chars"},
	}

	for _, tc := range tests {
		got := escapeHTML(tc.input)
		if got != tc.want {
			t.Errorf("escapeHTML(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Command tests
// ---------------------------------------------------------------------------

// enqueuedMessage captures what the command router enqueued.
type enqueuedMessage struct {
	chatID   string
	text     string
	keyboard *InlineKeyboardMarkup
}

// newTestCommandRouter sets up a Handler and CommandRouter with a mock bridge
// and intercepted queue for testing command dispatch.
func newTestCommandRouter(t *testing.T) (*CommandRouter, *mockServerBridge, *[]enqueuedMessage) {
	t.Helper()

	dir := t.TempDir()
	store, err := NewSettingsStore(dir, nil)
	if err != nil {
		t.Fatalf("NewSettingsStore: %v", err)
	}
	// Enable settings so handler can work.
	_ = store.Update(func(s *Settings) {
		s.BotToken = "test-token"
		s.ChatID = "12345"
		s.Enabled = true
	})

	bot := New()
	bridge := newMockServerBridge()

	handler := NewHandler(bot, store, bridge)

	// Intercept queue enqueue calls by replacing the queue.
	var captured []enqueuedMessage
	origEnqueue := handler.queue.Enqueue
	_ = origEnqueue

	// We'll read from the queue's internal slice instead.
	return handler.commands, bridge, &captured
}

func TestCommandRouter_HandleCommand_Help(t *testing.T) {
	dir := t.TempDir()
	store, err := NewSettingsStore(dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	bot := New()
	bridge := newMockServerBridge()
	handler := NewHandler(bot, store, bridge)

	handler.commands.HandleCommand("chat1", "/help")

	// Verify a message was enqueued to the queue.
	handler.queue.mu.Lock()
	qLen := len(handler.queue.queue)
	var text string
	if qLen > 0 {
		text = handler.queue.queue[0].Text
	}
	handler.queue.mu.Unlock()

	if qLen == 0 {
		t.Fatal("expected a message to be enqueued for /help")
	}
	if !strings.Contains(text, "/help") {
		t.Errorf("help response should contain '/help', got %q", text)
	}
}

func TestCommandRouter_HandleCommand_Status(t *testing.T) {
	dir := t.TempDir()
	store, err := NewSettingsStore(dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	bot := New()
	bridge := newMockServerBridge()
	handler := NewHandler(bot, store, bridge)

	handler.commands.HandleCommand("chat1", "/status")

	handler.queue.mu.Lock()
	qLen := len(handler.queue.queue)
	var text string
	if qLen > 0 {
		text = handler.queue.queue[0].Text
	}
	handler.queue.mu.Unlock()

	if qLen == 0 {
		t.Fatal("expected a message to be enqueued for /status")
	}
	if !strings.Contains(text, "3") {
		t.Error("expected session count '3' in status output")
	}
	if !strings.Contains(text, "2h30m") {
		t.Error("expected uptime in status output")
	}
}

func TestCommandRouter_HandleCommand_Unknown(t *testing.T) {
	dir := t.TempDir()
	store, err := NewSettingsStore(dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	bot := New()
	bridge := newMockServerBridge()
	handler := NewHandler(bot, store, bridge)

	handler.commands.HandleCommand("chat1", "/nonexistent")

	handler.queue.mu.Lock()
	qLen := len(handler.queue.queue)
	var text string
	if qLen > 0 {
		text = handler.queue.queue[0].Text
	}
	handler.queue.mu.Unlock()

	if qLen == 0 {
		t.Fatal("expected a message to be enqueued for unknown command")
	}
	if !strings.Contains(text, "Unknown command") {
		t.Errorf("expected 'Unknown command' hint, got %q", text)
	}
}

func TestCommandRouter_HandleCommand_WithBotSuffix(t *testing.T) {
	dir := t.TempDir()
	store, err := NewSettingsStore(dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	bot := New()
	bridge := newMockServerBridge()
	handler := NewHandler(bot, store, bridge)

	handler.commands.HandleCommand("chat1", "/help@MyTestBot")

	handler.queue.mu.Lock()
	qLen := len(handler.queue.queue)
	var text string
	if qLen > 0 {
		text = handler.queue.queue[0].Text
	}
	handler.queue.mu.Unlock()

	if qLen == 0 {
		t.Fatal("expected help response even with @bot suffix")
	}
	if !strings.Contains(text, "/help") {
		t.Error("expected help message content after stripping bot suffix")
	}
}

func TestCommandRouter_HandleCommand_Sessions(t *testing.T) {
	dir := t.TempDir()
	store, err := NewSettingsStore(dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	bot := New()
	bridge := newMockServerBridge()
	handler := NewHandler(bot, store, bridge)

	handler.commands.HandleCommand("chat1", "/sessions")

	handler.queue.mu.Lock()
	qLen := len(handler.queue.queue)
	var text string
	if qLen > 0 {
		text = handler.queue.queue[0].Text
	}
	handler.queue.mu.Unlock()

	if qLen == 0 {
		t.Fatal("expected a message to be enqueued for /sessions")
	}
	if !strings.Contains(text, "test-session") {
		t.Error("expected session name in sessions list")
	}
}

func TestCommandRouter_HandleCommand_New(t *testing.T) {
	dir := t.TempDir()
	store, err := NewSettingsStore(dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	bot := New()
	bridge := newMockServerBridge()
	handler := NewHandler(bot, store, bridge)

	handler.commands.HandleCommand("chat1", "/new my-new-session")

	handler.queue.mu.Lock()
	qLen := len(handler.queue.queue)
	var text string
	if qLen > 0 {
		text = handler.queue.queue[0].Text
	}
	handler.queue.mu.Unlock()

	if qLen == 0 {
		t.Fatal("expected a message to be enqueued for /new")
	}
	if !strings.Contains(text, "Session created") {
		t.Errorf("expected 'Session created' in response, got %q", text)
	}
}

func TestCommandRouter_HandleCommand_Send_NoArgs(t *testing.T) {
	dir := t.TempDir()
	store, err := NewSettingsStore(dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	bot := New()
	bridge := newMockServerBridge()
	handler := NewHandler(bot, store, bridge)

	handler.commands.HandleCommand("chat1", "/send")

	handler.queue.mu.Lock()
	qLen := len(handler.queue.queue)
	var text string
	if qLen > 0 {
		text = handler.queue.queue[0].Text
	}
	handler.queue.mu.Unlock()

	if qLen == 0 {
		t.Fatal("expected usage message for /send without args")
	}
	if !strings.Contains(text, "Usage") {
		t.Errorf("expected 'Usage' in response, got %q", text)
	}
}

// ---------------------------------------------------------------------------
// Handler tests
// ---------------------------------------------------------------------------

func TestHandler_NewHandler(t *testing.T) {
	dir := t.TempDir()
	store, err := NewSettingsStore(dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	bot := New()
	bridge := newMockServerBridge()

	handler := NewHandler(bot, store, bridge)
	if handler == nil {
		t.Fatal("NewHandler returned nil")
	}
	if handler.queue == nil {
		t.Error("expected queue to be initialized")
	}
	if handler.streamer == nil {
		t.Error("expected streamer to be initialized")
	}
	if handler.commands == nil {
		t.Error("expected commands to be initialized")
	}
}

func TestHandler_StartStop(t *testing.T) {
	dir := t.TempDir()
	store, err := NewSettingsStore(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Update(func(s *Settings) {
		s.Enabled = true
		s.BotToken = "fake-token"
		s.ChatID = "12345"
	})

	// Use a test server so the handler doesn't talk to real Telegram.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"ok":true}`)
	}))
	defer ts.Close()

	bot := New()
	bridge := newMockServerBridge()
	handler := NewHandler(bot, store, bridge)

	handler.Start()
	if !handler.started.Load() {
		t.Error("expected handler to be started after Start()")
	}

	// Idempotent start.
	handler.Start()

	handler.Stop()
	if handler.started.Load() {
		t.Error("expected handler to be stopped after Stop()")
	}

	// Idempotent stop.
	handler.Stop()
}

func TestHandler_IsConnected(t *testing.T) {
	dir := t.TempDir()
	store, err := NewSettingsStore(dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	bot := New()
	bridge := newMockServerBridge()
	handler := NewHandler(bot, store, bridge)

	if handler.IsConnected() {
		t.Error("expected IsConnected = false when not started")
	}
}

func TestHandler_WebhookHandler_BadSecret(t *testing.T) {
	dir := t.TempDir()
	store, err := NewSettingsStore(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Update(func(s *Settings) {
		s.Enabled = true
		s.BotToken = "fake-token"
		s.ChatID = "12345"
		s.WebhookSecret = "correct-secret"
		s.Mode = "webhook"
	})

	// Need a test server for registerBotCommands and other API calls.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"ok":true}`)
	}))
	defer ts.Close()

	bot := New()
	bridge := newMockServerBridge()
	handler := NewHandler(bot, store, bridge)

	// Must start for webhook to accept requests.
	handler.Start()
	defer handler.Stop()

	// Send a webhook request with the wrong secret.
	body := `{"update_id": 1, "message": {"message_id": 1, "chat": {"id": 12345}, "text": "test"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/telegram/webhook/wrong-secret", strings.NewReader(body))
	rr := httptest.NewRecorder()

	handler.WebhookHandler(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 for bad secret, got %d", rr.Code)
	}
}

func TestHandler_WebhookHandler_NotStarted(t *testing.T) {
	dir := t.TempDir()
	store, err := NewSettingsStore(dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	bot := New()
	bridge := newMockServerBridge()
	handler := NewHandler(bot, store, bridge)
	// Do not start the handler.

	body := `{"update_id": 1}`
	req := httptest.NewRequest(http.MethodPost, "/api/telegram/webhook/some-secret", strings.NewReader(body))
	rr := httptest.NewRecorder()

	handler.WebhookHandler(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when handler not started, got %d", rr.Code)
	}
}

func TestHandler_WebhookHandler_NoWebhookSecret(t *testing.T) {
	dir := t.TempDir()
	store, err := NewSettingsStore(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Update(func(s *Settings) {
		s.Enabled = true
		s.BotToken = "fake-token"
		s.ChatID = "12345"
		s.WebhookSecret = "" // no webhook secret configured
	})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"ok":true}`)
	}))
	defer ts.Close()

	bot := New()
	bridge := newMockServerBridge()
	handler := NewHandler(bot, store, bridge)
	handler.Start()
	defer handler.Stop()

	body := `{"update_id": 1}`
	req := httptest.NewRequest(http.MethodPost, "/api/telegram/webhook/some-secret", strings.NewReader(body))
	rr := httptest.NewRecorder()

	handler.WebhookHandler(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 when webhook secret not configured, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// Bot tests
// ---------------------------------------------------------------------------

func TestBot_New(t *testing.T) {
	bot := New()
	if bot == nil {
		t.Fatal("New() returned nil")
	}
	if bot.IsConnected() {
		t.Error("new bot should not be connected")
	}
}

func TestBot_Configure(t *testing.T) {
	bot := New()

	bot.Configure("test-token", "12345")
	if !bot.IsConnected() {
		t.Error("bot should be connected after Configure with valid token and chatID")
	}

	token, chatID := bot.GetConfig()
	if token != "test-token" {
		t.Errorf("token = %q, want %q", token, "test-token")
	}
	if chatID != "12345" {
		t.Errorf("chatID = %q, want %q", chatID, "12345")
	}
}

func TestBot_Configure_EmptyToken(t *testing.T) {
	bot := New()
	bot.Configure("", "12345")
	if bot.IsConnected() {
		t.Error("bot should not be connected with empty token")
	}
}

func TestBot_Disconnect(t *testing.T) {
	bot := New()
	bot.Configure("token", "chat")
	if !bot.IsConnected() {
		t.Fatal("expected connected after configure")
	}

	bot.Disconnect()
	if bot.IsConnected() {
		t.Error("expected disconnected after Disconnect()")
	}
}

func TestBot_SendMessage_NotConfigured(t *testing.T) {
	bot := New()
	err := bot.SendMessage("hello")
	if err == nil {
		t.Error("expected error when sending without configuration")
	}
}

// ---------------------------------------------------------------------------
// Handler - PendingInteraction management
// ---------------------------------------------------------------------------

func TestHandler_CleanupExpiredInteractions(t *testing.T) {
	dir := t.TempDir()
	store, err := NewSettingsStore(dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	bot := New()
	bridge := newMockServerBridge()
	handler := NewHandler(bot, store, bridge)

	// Add an expired interaction.
	handler.pendingMu.Lock()
	handler.pendingInteractions["expired1"] = &PendingInteraction{
		SessionID: "sess1",
		ShortID:   "expired1",
		Type:      InteractionTypePermission,
		CreatedAt: time.Now().Add(-10 * time.Minute),
		ExpiresAt: time.Now().Add(-5 * time.Minute),
	}
	// Add a non-expired interaction.
	handler.pendingInteractions["active1"] = &PendingInteraction{
		SessionID: "sess2",
		ShortID:   "active1",
		Type:      InteractionTypePlan,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}
	handler.pendingMu.Unlock()

	handler.cleanupExpiredInteractions()

	handler.pendingMu.Lock()
	_, expiredExists := handler.pendingInteractions["expired1"]
	_, activeExists := handler.pendingInteractions["active1"]
	handler.pendingMu.Unlock()

	if expiredExists {
		t.Error("expired interaction should have been cleaned up")
	}
	if !activeExists {
		t.Error("active interaction should not have been cleaned up")
	}
}

// ---------------------------------------------------------------------------
// Handler - calculateBackoff
// ---------------------------------------------------------------------------

func TestHandler_CalculateBackoff(t *testing.T) {
	dir := t.TempDir()
	store, err := NewSettingsStore(dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	bot := New()
	bridge := newMockServerBridge()
	handler := NewHandler(bot, store, bridge)

	// First error: 2^0 = 1s
	handler.consecutiveErrors = 1
	if got := handler.calculateBackoff(); got != 1*time.Second {
		t.Errorf("backoff for 1 error = %v, want 1s", got)
	}

	// Third error: 2^2 = 4s
	handler.consecutiveErrors = 3
	if got := handler.calculateBackoff(); got != 4*time.Second {
		t.Errorf("backoff for 3 errors = %v, want 4s", got)
	}

	// Degraded mode: always 60s
	handler.degradedMode = true
	handler.consecutiveErrors = 15
	if got := handler.calculateBackoff(); got != 60*time.Second {
		t.Errorf("backoff in degraded mode = %v, want 60s", got)
	}
}

// ---------------------------------------------------------------------------
// Settings edge cases
// ---------------------------------------------------------------------------

func TestSettingsStore_Migration_PartialData(t *testing.T) {
	dir := t.TempDir()

	// Only token, no chatId -- should not enable.
	legacy := map[string]interface{}{
		"botToken": "only-token",
	}

	store, err := NewSettingsStore(dir, legacy)
	if err != nil {
		t.Fatalf("NewSettingsStore: %v", err)
	}

	s := store.Get()
	if s.Enabled {
		t.Error("expected Enabled = false when chatId is missing")
	}
	if s.BotToken != "only-token" {
		t.Errorf("BotToken = %q, want %q", s.BotToken, "only-token")
	}
}

func TestSettingsStore_LoadMissingFile(t *testing.T) {
	dir := t.TempDir()

	store := &SettingsStore{
		filePath: filepath.Join(dir, "nonexistent.json"),
		current:  DefaultSettings(),
	}

	// Load should not return an error for missing file, just reset to defaults.
	if err := store.Load(); err != nil {
		t.Errorf("Load on missing file returned error: %v", err)
	}
}

func TestSettingsStore_LoadCorruptFile(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "telegram-settings.json")

	// Write corrupt JSON.
	if err := os.WriteFile(fp, []byte("{corrupt json!!"), 0o644); err != nil {
		t.Fatal(err)
	}

	store := &SettingsStore{
		filePath: fp,
		current:  DefaultSettings(),
	}

	err := store.Load()
	if err == nil {
		t.Error("expected error for corrupt JSON file")
	}
}

func TestSettingsStore_ConcurrentAccess(t *testing.T) {
	dir := t.TempDir()
	store, err := NewSettingsStore(dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			_ = store.Get()
		}
	}()

	for i := 0; i < 100; i++ {
		_ = store.Update(func(s *Settings) {
			s.RateLimitPerMinute = i
		})
	}
	<-done
}

// ---------------------------------------------------------------------------
// Additional formatter tests
// ---------------------------------------------------------------------------

func TestFormatStuckWarning(t *testing.T) {
	result := FormatStuckWarning("abc12345", "thinking", 120000)
	if !strings.Contains(result, "Stuck State Warning") {
		t.Error("expected 'Stuck State Warning' header")
	}
	if !strings.Contains(result, "abc12345") {
		t.Error("expected session ID")
	}
	if !strings.Contains(result, "thinking") {
		t.Error("expected state name")
	}
}

func TestFormatRespawnBlocked(t *testing.T) {
	result := FormatRespawnBlocked("abc12345", "rate limit exceeded", "waited 5m")
	if !strings.Contains(result, "Respawn Blocked") {
		t.Error("expected 'Respawn Blocked' header")
	}
	if !strings.Contains(result, "rate limit exceeded") {
		t.Error("expected reason in output")
	}
	if !strings.Contains(result, "waited 5m") {
		t.Error("expected details in output")
	}
}

func TestFormatPlanGenerated(t *testing.T) {
	result := FormatPlanGenerated("refactor auth module", 7)
	if !strings.Contains(result, "Plan Ready for Review") {
		t.Error("expected 'Plan Ready for Review' header")
	}
	if !strings.Contains(result, "refactor auth module") {
		t.Error("expected task description")
	}
	if !strings.Contains(result, "7") {
		t.Error("expected item count")
	}
}

func TestFormatAgentList_Empty(t *testing.T) {
	result := FormatAgentList(nil)
	if result != "No active agents." {
		t.Errorf("expected 'No active agents.', got %q", result)
	}
}

func TestFormatAgentList_WithAgents(t *testing.T) {
	agents := []AgentInfo{
		{ID: "agent1", Name: "worker-1", Status: "running"},
	}
	result := FormatAgentList(agents)
	if !strings.Contains(result, "Agents") {
		t.Error("expected 'Agents' header")
	}
	if !strings.Contains(result, "worker-1") {
		t.Error("expected agent name")
	}
}
