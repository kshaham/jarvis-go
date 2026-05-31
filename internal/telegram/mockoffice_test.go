package telegram

import "sync"

// ---------------------------------------------------------------------------
// Mock OfficeBridge -- shared across handler / command tests
// ---------------------------------------------------------------------------

// mockOfficeBridge implements OfficeBridge for testing lead-agent command
// dispatch and pending-leader-query routing without importing the office
// package. All fields are configurable per-test; method calls are recorded
// so tests can assert side effects.
type mockOfficeBridge struct {
	mu sync.Mutex

	teamActive       bool
	hasPendingQuery  bool
	pendingSessionID string

	leadSessionID string
	leadName      string
	leadStatus    string
	leadStatusOK  bool

	// Recorded calls
	clearPendingCalls int
	leadReplyCallback func(sessionID, text string)
}

func newMockOfficeBridge() *mockOfficeBridge {
	return &mockOfficeBridge{
		teamActive:    true,
		leadSessionID: "lead-sess-1",
		leadName:      "Captain",
		leadStatus:    "Lead is working on auth.",
		leadStatusOK:  true,
	}
}

func (m *mockOfficeBridge) HasPendingLeaderQuery() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.hasPendingQuery
}

func (m *mockOfficeBridge) GetPendingLeaderSessionID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pendingSessionID
}

func (m *mockOfficeBridge) ClearPendingLeaderQuery() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clearPendingCalls++
	m.hasPendingQuery = false
}

func (m *mockOfficeBridge) IsTeamActive() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.teamActive
}

func (m *mockOfficeBridge) GetLeadAgentInfo(pinnedSessionID string) (string, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.leadSessionID, m.leadName
}

func (m *mockOfficeBridge) GetLeadStatusText(pinnedSessionID string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.leadStatus, m.leadStatusOK
}

func (m *mockOfficeBridge) SetLeadReplyCallback(fn func(sessionID, text string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.leadReplyCallback = fn
}

func (m *mockOfficeBridge) getCallback() func(sessionID, text string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.leadReplyCallback
}

// ---------------------------------------------------------------------------
// Test helpers for inspecting the handler's message queue
// ---------------------------------------------------------------------------

// drainQueue snapshots and clears the handler's enqueued messages. The queue is
// never Start()ed in these tests, so messages accumulate in the internal slice
// and we read them directly under the queue lock.
func drainQueue(h *Handler) []queuedMessage {
	h.queue.mu.Lock()
	defer h.queue.mu.Unlock()
	out := make([]queuedMessage, len(h.queue.queue))
	copy(out, h.queue.queue)
	h.queue.queue = h.queue.queue[:0]
	return out
}

// lastEnqueued returns the most recently enqueued message text, or "" if the
// queue is empty.
func lastEnqueued(h *Handler) string {
	h.queue.mu.Lock()
	defer h.queue.mu.Unlock()
	if len(h.queue.queue) == 0 {
		return ""
	}
	return h.queue.queue[len(h.queue.queue)-1].Text
}

// queueLen returns the current number of enqueued messages.
func queueLen(h *Handler) int {
	h.queue.mu.Lock()
	defer h.queue.mu.Unlock()
	return len(h.queue.queue)
}

// newTestHandler builds a Handler with a mock bridge and a settings store
// enabled with a chat ID. Returns the handler and the mock bridge. The queue is
// intentionally NOT started so enqueued messages stay inspectable.
func newTestHandler(t testingTB) (*Handler, *mockServerBridge) {
	t.Helper()
	dir := t.TempDir()
	store, err := NewSettingsStore(dir, nil)
	if err != nil {
		t.Fatalf("NewSettingsStore: %v", err)
	}
	if err := store.Update(func(s *Settings) {
		s.BotToken = "test-token"
		s.ChatID = "12345"
		s.Enabled = true
	}); err != nil {
		t.Fatalf("store.Update: %v", err)
	}
	bot := New()
	bridge := newMockServerBridge()
	h := NewHandler(bot, store, bridge)
	return h, bridge
}

// testingTB is the minimal testing surface we depend on, so helpers compile
// against *testing.T without importing it at file scope conflicts.
type testingTB interface {
	Helper()
	TempDir() string
	Fatalf(format string, args ...any)
}
