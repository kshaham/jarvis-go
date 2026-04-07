package pty

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
)

// SessionManager coordinates multiple Session instances (both PTY and SDK).
type SessionManager struct {
	mu          sync.RWMutex
	sessions    map[string]*Session
	sdkSessions map[string]*SDKSession
	createMu    sync.Mutex
	maxSessions int
	events      chan SessionEvent
}

// NewSessionManager creates a new manager with the given session limit.
func NewSessionManager(maxSessions int) *SessionManager {
	return &SessionManager{
		sessions:    make(map[string]*Session),
		sdkSessions: make(map[string]*SDKSession),
		maxSessions: maxSessions,
		events:      make(chan SessionEvent, eventChannelSize),
	}
}

// CreateSession creates and starts a new session.
func (m *SessionManager) CreateSession(ctx context.Context, config SessionConfig) (*Session, error) {
	m.createMu.Lock()
	defer m.createMu.Unlock()

	m.mu.RLock()
	count := len(m.sessions) + len(m.sdkSessions)
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

	if err := session.Stop(); err != nil {
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
	m.mu.RUnlock()

	for _, id := range ptyIDs {
		_ = m.StopSession(ctx, id)
	}
	for _, id := range sdkIDs {
		_ = m.StopSDKSession(id)
	}
	return nil
}

// CreateSDKSession creates and starts a new SDK-based session (no PTY).
func (m *SessionManager) CreateSDKSession(ctx context.Context, config SessionConfig) (*SDKSession, error) {
	m.createMu.Lock()
	defer m.createMu.Unlock()

	m.mu.RLock()
	count := len(m.sessions) + len(m.sdkSessions)
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
	return len(m.sessions) + len(m.sdkSessions)
}

// Events returns the aggregated event channel.
func (m *SessionManager) Events() <-chan SessionEvent {
	return m.events
}

func (m *SessionManager) forwardEvents(session *Session) {
	for {
		select {
		case event, ok := <-session.Events():
			if !ok {
				return // Channel closed, session done
			}
			select {
			case m.events <- event:
			default:
			}
		case <-session.Done():
			return // Session stopped
		}
	}
}
