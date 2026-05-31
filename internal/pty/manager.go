package pty

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/google/uuid"
)

// newCodexSession is a seam over NewCodexSession so a test can inject a session
// whose Start() fails (the only failure mode is "already started", which a
// freshly-constructed session can never hit). Production points at the real
// constructor, so behavior is identical.
var newCodexSession = NewCodexSession

// stopAllAfterCollect is a test-only hook invoked by StopAllSessions after it
// snapshots the live session ids but before it stops them. Production leaves it
// nil. Tests use it to delete sessions out from under the stop loops, forcing
// the "not found" warn branches that model the real concurrent-shutdown race.
var stopAllAfterCollect func(*SessionManager)

// stopSession is a seam over (*Session).Stop so a test can force the otherwise
// unreachable error-propagation branch in StopSession. The production
// (*Session).Stop only ever returns nil (it logs/swallows teardown failures
// internally), so the `err != nil` arm can't be exercised through the real
// method. Production points at the real method, so behavior is identical.
var stopSession = func(s *Session) error { return s.Stop() }

// SessionManager coordinates multiple Session instances (both PTY and SDK).
type SessionManager struct {
	mu            sync.RWMutex
	sessions      map[string]*Session
	sdkSessions   map[string]*SDKSession
	codexSessions map[string]*CodexSession
	createMu      sync.Mutex
	maxSessions   int
	events        chan SessionEvent
}

// NewSessionManager creates a new manager with the given session limit.
func NewSessionManager(maxSessions int) *SessionManager {
	return &SessionManager{
		sessions:      make(map[string]*Session),
		sdkSessions:   make(map[string]*SDKSession),
		codexSessions: make(map[string]*CodexSession),
		maxSessions:   maxSessions,
		events:        make(chan SessionEvent, eventChannelSize),
	}
}

// sessionCount returns the total live session count across all backends.
// Caller must hold at least an RLock.
func (m *SessionManager) sessionCount() int {
	return len(m.sessions) + len(m.sdkSessions) + len(m.codexSessions)
}

// CreateSession creates and starts a new session.
func (m *SessionManager) CreateSession(ctx context.Context, config SessionConfig) (*Session, error) {
	m.createMu.Lock()
	defer m.createMu.Unlock()

	m.mu.RLock()
	count := m.sessionCount()
	m.mu.RUnlock()

	if count >= m.maxSessions {
		return nil, fmt.Errorf("max sessions reached (%d)", m.maxSessions)
	}

	if config.ID == "" {
		config.ID = uuid.New().String()
	}

	session := NewSession(config)
	if err := session.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start session: %w", err)
	}

	m.mu.Lock()
	m.sessions[session.ID] = session
	m.mu.Unlock()

	// NOTE: Don't forward events here -- the HTTP handler's forwardSessionEvents()
	// reads from session.Events() and broadcasts via SSE. Go channels have a single
	// consumer, so only one goroutine should read the events channel.

	return session, nil
}

// StopSession stops and removes a session.
func (m *SessionManager) StopSession(_ context.Context, id string) error {
	m.mu.RLock()
	session, ok := m.sessions[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("session not found: %s", id)
	}

	if err := stopSession(session); err != nil {
		return err
	}

	m.mu.Lock()
	delete(m.sessions, id)
	m.mu.Unlock()
	return nil
}

// StopAllSessions stops all sessions (both PTY and SDK).
func (m *SessionManager) StopAllSessions(ctx context.Context) error {
	m.mu.RLock()
	ptyIDs := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ptyIDs = append(ptyIDs, id)
	}
	sdkIDs := make([]string, 0, len(m.sdkSessions))
	for id := range m.sdkSessions {
		sdkIDs = append(sdkIDs, id)
	}
	codexIDs := make([]string, 0, len(m.codexSessions))
	for id := range m.codexSessions {
		codexIDs = append(codexIDs, id)
	}
	m.mu.RUnlock()

	// Test seam: lets a test simulate the real shutdown race where a session
	// is removed from its map (e.g. by a concurrent Stop*Session) between the
	// snapshot above and the stop loops below, so the per-id "not found" warn
	// branches are exercised deterministically. nil in production.
	if hook := stopAllAfterCollect; hook != nil {
		hook(m)
	}

	for _, id := range ptyIDs {
		if err := m.StopSession(ctx, id); err != nil {
			slog.Warn("Failed to stop PTY session during shutdown", "sessionId", id, "error", err)
		}
	}
	for _, id := range sdkIDs {
		if err := m.StopSDKSession(id); err != nil {
			slog.Warn("Failed to stop SDK session during shutdown", "sessionId", id, "error", err)
		}
	}
	for _, id := range codexIDs {
		if err := m.StopCodexSession(id); err != nil {
			slog.Warn("Failed to stop Codex session during shutdown", "sessionId", id, "error", err)
		}
	}
	return nil
}

// CreateSDKSession creates and starts a new SDK-based session (no PTY).
func (m *SessionManager) CreateSDKSession(ctx context.Context, config SessionConfig) (*SDKSession, error) {
	m.createMu.Lock()
	defer m.createMu.Unlock()

	m.mu.RLock()
	count := m.sessionCount()
	m.mu.RUnlock()

	if count >= m.maxSessions {
		return nil, fmt.Errorf("max sessions reached (%d)", m.maxSessions)
	}

	if config.ID == "" {
		config.ID = uuid.New().String()
	}

	session := NewSDKSession(config)
	if err := session.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start SDK session: %w", err)
	}

	m.mu.Lock()
	m.sdkSessions[session.ID] = session
	m.mu.Unlock()

	return session, nil
}

// GetSDKSession returns an SDK session by ID.
func (m *SessionManager) GetSDKSession(id string) (*SDKSession, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sdkSessions[id]
	return s, ok
}

// RegisterSDKSessionForTest inserts a pre-built SDKSession into the
// manager's internal map so tests can exercise handler code paths
// that look up sessions by ID without spinning up a real Claude
// subprocess. Production callers go through CreateSDKSession; this
// shortcut bypasses subprocess lifecycle entirely and is unsafe for
// anything other than test fixtures.
func (m *SessionManager) RegisterSDKSessionForTest(s *SDKSession) {
	if s == nil {
		return
	}
	m.mu.Lock()
	m.sdkSessions[s.ID] = s
	m.mu.Unlock()
}

// GetAllSDKSessions returns every live SDK session. Snapshot copy so
// callers can iterate without holding the manager lock (useful for
// bulk operations like daily-cap enforcement that call StopSDKSession
// on each returned pointer, which would deadlock under an RLock).
func (m *SessionManager) GetAllSDKSessions() []*SDKSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*SDKSession, 0, len(m.sdkSessions))
	for _, s := range m.sdkSessions {
		if s != nil {
			out = append(out, s)
		}
	}
	return out
}

// StopSDKSession stops and removes an SDK session.
func (m *SessionManager) StopSDKSession(id string) error {
	m.mu.RLock()
	session, ok := m.sdkSessions[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("SDK session not found: %s", id)
	}
	session.Stop()
	m.mu.Lock()
	delete(m.sdkSessions, id)
	m.mu.Unlock()
	return nil
}

// GetAllSessionIDs returns all session IDs (both PTY and SDK) under a single lock.
func (m *SessionManager) GetAllSessionIDs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := make([]string, 0, m.sessionCount())
	for id := range m.sessions {
		ids = append(ids, id)
	}
	for id := range m.sdkSessions {
		ids = append(ids, id)
	}
	for id := range m.codexSessions {
		ids = append(ids, id)
	}
	return ids
}

// ForEachSDKSession iterates over all SDK sessions under a read lock.
func (m *SessionManager) ForEachSDKSession(fn func(id string, s *SDKSession)) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for id, s := range m.sdkSessions {
		fn(id, s)
	}
}

// GetSession returns a session by ID.
func (m *SessionManager) GetSession(id string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	return s, ok
}

// GetAllSessions returns all sessions.
func (m *SessionManager) GetAllSessions() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		result = append(result, s)
	}
	return result
}

// GetSessionCount returns the number of active sessions.
func (m *SessionManager) GetSessionCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessionCount()
}

// CreateCodexSession creates and starts a Codex CLI session (ModeCodexExec).
func (m *SessionManager) CreateCodexSession(ctx context.Context, config SessionConfig) (*CodexSession, error) {
	m.createMu.Lock()
	defer m.createMu.Unlock()

	m.mu.RLock()
	count := m.sessionCount()
	m.mu.RUnlock()
	if count >= m.maxSessions {
		return nil, fmt.Errorf("max sessions reached (%d)", m.maxSessions)
	}
	if config.ID == "" {
		config.ID = uuid.New().String()
	}
	session := newCodexSession(config)
	if err := session.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start Codex session: %w", err)
	}
	m.mu.Lock()
	m.codexSessions[session.ID] = session
	m.mu.Unlock()
	return session, nil
}

// GetCodexSession returns a Codex session by ID.
func (m *SessionManager) GetCodexSession(id string) (*CodexSession, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.codexSessions[id]
	return s, ok
}

// GetAllCodexSessions returns a snapshot of every live Codex session.
func (m *SessionManager) GetAllCodexSessions() []*CodexSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*CodexSession, 0, len(m.codexSessions))
	for _, s := range m.codexSessions {
		if s != nil {
			out = append(out, s)
		}
	}
	return out
}

// StopCodexSession stops and removes a Codex session.
func (m *SessionManager) StopCodexSession(id string) error {
	m.mu.RLock()
	session, ok := m.codexSessions[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("Codex session not found: %s", id)
	}
	session.Stop()
	m.mu.Lock()
	delete(m.codexSessions, id)
	m.mu.Unlock()
	return nil
}

// Events returns the aggregated event channel.
func (m *SessionManager) Events() <-chan SessionEvent {
	return m.events
}
