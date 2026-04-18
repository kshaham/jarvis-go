package office

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newDevInstanceTestManager returns a Manager wired for fast feedback and
// points HOME at t.TempDir() so persistent dev-instance JSON files don't
// pollute the real office-memory directory. Borrows the shape of
// newOrcTestManager but trims the unrelated timers.
func newDevInstanceTestManager(t *testing.T) *Manager {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	cfg := DefaultConfig()
	cfg.IdleCheckInterval = 50 * time.Millisecond
	m := NewManager(&recordingSessionStore{}, func(string, interface{}) {}, cfg)
	return m
}

func TestSetDevInstance_RejectsPortConflict(t *testing.T) {
	m := newDevInstanceTestManager(t)

	if err := m.SetDevInstance("frontend", "http://jarvis:3001", 0, false); err != nil {
		t.Fatalf("first announce: %v", err)
	}
	err := m.SetDevInstance("api", "http://jarvis:3001", 0, false)
	if err == nil {
		t.Fatal("expected port conflict, got nil")
	}
	if !strings.Contains(err.Error(), "already held by slot") {
		t.Errorf("expected 'already held by slot' in error, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "frontend") {
		t.Errorf("error should name the conflicting slot; got %q", err.Error())
	}

	// The second call must NOT have mutated state. Port 3001 should still
	// belong to "frontend" only; "api" should not appear in the listing.
	all := m.ListDevInstances()
	if _, ok := all["api"]; ok {
		t.Error("conflicting announce should not have created the api slot")
	}
	if inst, ok := all["frontend"]; !ok || inst.URL != "http://jarvis:3001" {
		t.Errorf("frontend slot missing or mutated; got %+v", inst)
	}
}

func TestSetDevInstance_ForceBypassesConflict(t *testing.T) {
	m := newDevInstanceTestManager(t)

	if err := m.SetDevInstance("frontend", "http://jarvis:3001", 0, false); err != nil {
		t.Fatalf("first announce: %v", err)
	}
	if err := m.SetDevInstance("api", "http://jarvis:3001", 0, true); err != nil {
		t.Fatalf("force announce should bypass conflict, got: %v", err)
	}

	all := m.ListDevInstances()
	if _, ok := all["api"]; !ok {
		t.Fatal("api slot should exist after --force announce")
	}
	if !all["api"].ForceAnnounced {
		t.Errorf("ForceAnnounced should be true on api slot")
	}
}

func TestSetDevInstance_SameSlotReannounceAllowed(t *testing.T) {
	m := newDevInstanceTestManager(t)

	if err := m.SetDevInstance("app", "http://jarvis:3001", 100, false); err != nil {
		t.Fatalf("first announce: %v", err)
	}
	// Re-announcing the same slot on the same port must NOT collide with
	// itself — the conflict check explicitly skips the slot being written.
	if err := m.SetDevInstance("app", "http://jarvis:3001", 200, false); err != nil {
		t.Fatalf("re-announce same slot same port: %v", err)
	}
	inst, _ := m.GetDevInstance("app")
	if inst.PID != 200 {
		t.Errorf("PID should update on re-announce; got %d", inst.PID)
	}
}

func TestSetDevInstance_InvalidSlotNameRejected(t *testing.T) {
	m := newDevInstanceTestManager(t)
	cases := []string{
		"has space",
		"has/slash",
		"has.dot",
		"has:colon",
		"",
		strings.Repeat("x", 33), // 33 chars, just over the 32 cap
	}
	for _, name := range cases {
		if err := m.SetDevInstance(name, "http://jarvis:3001", 0, false); err == nil {
			// Empty string is normalized to "app" by the setter, so that's allowed.
			if name == "" {
				continue
			}
			t.Errorf("slot name %q should have been rejected", name)
		}
	}
}

func TestCheckDevInstanceHealth_AutoClearsAfterThreeFailures(t *testing.T) {
	m := newDevInstanceTestManager(t)

	// Test server that always 500s so every health check counts as a failure.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	if err := m.SetDevInstance("app", srv.URL, 0, false); err != nil {
		t.Fatalf("announce: %v", err)
	}

	// Force each check through even though NextCheckAt might still be
	// pending — zero out the gate between calls so the test doesn't have
	// to sleep for the full adaptive interval.
	for i := 1; i <= 3; i++ {
		m.checkOneDevInstance("app", time.Now())
		inst, ok := m.GetDevInstance("app")
		if i < 3 {
			if !ok {
				t.Fatalf("after %d failures the slot should still exist", i)
			}
			if inst.HealthFailures != i {
				t.Errorf("after check #%d, HealthFailures=%d, want %d", i, inst.HealthFailures, i)
			}
			// Reset the NextCheckAt gate so the next checkOneDevInstance
			// runs immediately instead of honoring the backoff window.
			m.mu.Lock()
			if inner, ok := m.teamDevInstances["app"]; ok {
				inner.NextCheckAt = time.Time{}
			}
			m.mu.Unlock()
		}
	}

	// Third failure should have triggered auto-clear.
	_, stillThere := m.GetDevInstance("app")
	if stillThere {
		t.Error("slot should be auto-cleared after 3 consecutive failures")
	}
}

func TestCheckDevInstanceHealth_ResetsOnSuccess(t *testing.T) {
	m := newDevInstanceTestManager(t)

	// Flip-flop server: fail once, then succeed.
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := m.SetDevInstance("app", srv.URL, 0, false); err != nil {
		t.Fatalf("announce: %v", err)
	}

	m.checkOneDevInstance("app", time.Now())
	if inst, _ := m.GetDevInstance("app"); inst.HealthFailures != 1 {
		t.Fatalf("first check should have failed; HealthFailures=%d", inst.HealthFailures)
	}

	// Bypass backoff gate for the next call.
	m.mu.Lock()
	if inner, ok := m.teamDevInstances["app"]; ok {
		inner.NextCheckAt = time.Time{}
	}
	m.mu.Unlock()

	m.checkOneDevInstance("app", time.Now())
	inst, ok := m.GetDevInstance("app")
	if !ok {
		t.Fatal("slot should still exist")
	}
	if inst.HealthFailures != 0 {
		t.Errorf("HealthFailures should reset to 0 on recovery; got %d", inst.HealthFailures)
	}
	if !inst.NextCheckAt.IsZero() {
		t.Errorf("NextCheckAt should reset on recovery; got %v", inst.NextCheckAt)
	}
}

func TestCheckDevInstanceHealth_FourXXCountsAsHealthy(t *testing.T) {
	m := newDevInstanceTestManager(t)

	// Auth-gated dev instance legitimately returns 401 on the root path.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	if err := m.SetDevInstance("app", srv.URL, 0, false); err != nil {
		t.Fatalf("announce: %v", err)
	}
	m.checkOneDevInstance("app", time.Now())
	inst, _ := m.GetDevInstance("app")
	if inst.HealthFailures != 0 {
		t.Errorf("4xx should count as healthy; HealthFailures=%d", inst.HealthFailures)
	}
}

func TestDevInstancePortConflict(t *testing.T) {
	m := newDevInstanceTestManager(t)
	_ = m.SetDevInstance("a", "http://jarvis:3001", 0, false)
	_ = m.SetDevInstance("b", "http://jarvis:4000", 0, false)

	// Same slot, same port → no conflict (callable as self).
	if conflict := m.DevInstancePortConflict("a", "http://jarvis:3001"); conflict != "" {
		t.Errorf("self-port-reuse should not conflict; got %q", conflict)
	}
	// Different slot, same port → conflict.
	if conflict := m.DevInstancePortConflict("c", "http://jarvis:3001"); conflict != "a" {
		t.Errorf("conflict should name slot 'a'; got %q", conflict)
	}
	// Different slot, different port → no conflict.
	if conflict := m.DevInstancePortConflict("c", "http://jarvis:5000"); conflict != "" {
		t.Errorf("unrelated port should not conflict; got %q", conflict)
	}
	// Empty URL → no conflict.
	if conflict := m.DevInstancePortConflict("c", ""); conflict != "" {
		t.Errorf("empty URL should not conflict; got %q", conflict)
	}
}
