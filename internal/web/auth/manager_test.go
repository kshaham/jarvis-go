package auth

import (
	"sync"
	"testing"
	"time"
)

// TestValidateSessionConcurrentRLock validates the same session from N goroutines
// under -race to confirm no data race on LastActivity and that all validations succeed.
func TestValidateSessionConcurrentRLock(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir, false)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	token := mgr.CreateSession()
	if token == "" {
		t.Fatal("CreateSession returned empty token")
	}

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			if !mgr.ValidateSession(token) {
				t.Errorf("ValidateSession returned false for valid session")
			}
		}()
	}
	wg.Wait()

	// LastActivity must have been bumped (it should be >= the session creation time).
	mgr.mu.RLock()
	entry, ok := mgr.sessions[token]
	mgr.mu.RUnlock()
	if !ok {
		t.Fatal("session was unexpectedly removed")
	}
	if entry.LastActivity().IsZero() {
		t.Error("LastActivity is zero after concurrent validates")
	}
	if time.Since(entry.LastActivity()) > 5*time.Second {
		t.Errorf("LastActivity looks stale: %v", entry.LastActivity())
	}
}

// TestValidateSessionExpiry verifies that an expired session is rejected.
func TestValidateSessionExpiry(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir, false)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	token := mgr.CreateSession()

	// Manually backdate LastActivity so the session appears expired.
	mgr.mu.RLock()
	entry := mgr.sessions[token]
	mgr.mu.RUnlock()
	entry.SetLastActivity(time.Now().Add(-(sessionTimeoutDur + time.Second)))

	if mgr.ValidateSession(token) {
		t.Error("ValidateSession accepted an expired session")
	}
	// The expired session should have been deleted.
	mgr.mu.RLock()
	_, still := mgr.sessions[token]
	mgr.mu.RUnlock()
	if still {
		t.Error("expired session was not removed from the map")
	}
}

// TestValidateSessionUnknown verifies that an unknown token is rejected.
func TestValidateSessionUnknown(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir, false)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if mgr.ValidateSession("not-a-real-token") {
		t.Error("ValidateSession accepted an unknown token")
	}
}
