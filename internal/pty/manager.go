package pty

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
)

// SessionManager coordinates multiple Session instances.
type SessionManager struct {
	mu          sync.RWMutex
	sessions    map[string]*Session
	createMu    sync.Mutex
	maxSessions int
	events      chan SessionEvent
}

// NewSessionManager creates a new manager with the given session limit.
func NewSessionManager(maxSessions int) *SessionManager {
	return &SessionManager{
		sessions:    make(map[string]*Session),
		maxSessions: maxSessions,
		events:      make(chan SessionEvent, eventChannelSize),
	}
}

// CreateSession creates and starts a new session.
func (m *SessionManager) CreateSession(ctx context.Context, config SessionConfig) (*Session, error) {
	m.createMu.Lock()
	defer m.createMu.Unlock()

	m.mu.RLock()
	count := len(m.sessions)
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

// StopAllSessions stops all sessions.
func (m *SessionManager) StopAllSessions(ctx context.Context) error {
	m.mu.RLock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	m.mu.RUnlock()

	for _, id := range ids {
		_ = m.StopSession(ctx, id)
	}
	return nil
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
	return len(m.sessions)
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
