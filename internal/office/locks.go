package office

import (
	"fmt"
	"strings"
	"time"
)

// AcquireLock attempts to acquire a file lock.
func (m *Manager) AcquireLock(path, sessionID, ownerName string) (*FileLock, error) {
	normalized := normalizePath(path)

	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, ok := m.fileLocks[normalized]; ok {
		if time.Now().Before(existing.ExpiresAt) && existing.Owner != sessionID {
			return nil, fmt.Errorf("file locked by %s until %s", existing.OwnerName, existing.ExpiresAt.Format(time.RFC3339))
		}
	}

	lock := &FileLock{
		Path:       normalized,
		Owner:      sessionID,
		OwnerName:  ownerName,
		AcquiredAt: time.Now(),
		ExpiresAt:  time.Now().Add(m.config.FileLockTTL),
	}
	m.fileLocks[normalized] = lock
	return lock, nil
}

// ReleaseLock releases a file lock.
func (m *Manager) ReleaseLock(path, sessionID string) bool {
	normalized := normalizePath(path)

	m.mu.Lock()
	defer m.mu.Unlock()

	lock, ok := m.fileLocks[normalized]
	if !ok {
		return false
	}
	if lock.Owner != sessionID {
		return false
	}
	delete(m.fileLocks, normalized)
	return true
}

// GetLocks returns all active file locks, lazily cleaning up expired ones.
func (m *Manager) GetLocks() []*FileLock {
	m.mu.Lock() // Write lock needed for cleanup
	defer m.mu.Unlock()

	now := time.Now()
	for path, lock := range m.fileLocks {
		// Tolerate a nil entry the same way the copy loop below does;
		// without this the cleanup loop would nil-deref on lock.ExpiresAt
		// while the very next loop already guards against it. Drop the
		// nil so the map doesn't keep an unusable entry.
		if lock == nil {
			delete(m.fileLocks, path)
			continue
		}
		if now.After(lock.ExpiresAt) {
			delete(m.fileLocks, path)
		}
	}
	// R26 concurrency review: deep copies so callers don't race
	// with writers mutating lock.Owner (ReassignSessionID) or
	// lock.ExpiresAt. Matches the R21 pattern for GetMeetings /
	// the R26 fix for GetBugTasks.
	result := make([]*FileLock, 0, len(m.fileLocks))
	for _, lock := range m.fileLocks {
		if lock == nil {
			continue
		}
		copied := *lock
		result = append(result, &copied)
	}
	return result
}

// CleanupExpiredLocks removes expired locks.
func (m *Manager) CleanupExpiredLocks() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	count := 0
	for path, lock := range m.fileLocks {
		if now.After(lock.ExpiresAt) {
			delete(m.fileLocks, path)
			count++
		}
	}
	return count
}

func normalizePath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.TrimRight(path, "/")
	return path
}
