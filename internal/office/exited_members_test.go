package office

import (
	"sync"
	"testing"
	"time"
)

// stubRespawn records the ConfigIDs checkForExitedMembers asks it to respawn so
// each table case can assert exactly which members (if any) the watchdog tried
// to heal. Thread-safe even though the watchdog runs single-threaded in tests,
// so the assertions never race a stray goroutine.
type stubRespawn struct {
	mu     sync.Mutex
	called []string
	err    error
}

func (s *stubRespawn) fn(configID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.called = append(s.called, configID)
	return s.err
}

func (s *stubRespawn) calls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.called))
	copy(out, s.called)
	return out
}

// seedExitedMember builds a manager with a single spawned member ("alpha") whose
// session has DIED (recorded in spawnedMembers but never registered as a live
// agent), past the spawn grace period, in perpetual mode. Callers tweak the
// returned manager per case.
func seedExitedMember(t *testing.T) (*Manager, *stubRespawn) {
	t.Helper()
	m := NewManager(nil, func(string, interface{}) {}, DefaultConfig())
	stub := &stubRespawn{}
	m.SetRespawnMemberFunc(stub.fn)
	m.SetPerpetualMode(true)

	m.mu.Lock()
	// Team spawned well past the grace period so the watchdog is armed.
	m.teamSpawnedAt = time.Now().Add(-1 * time.Hour)
	m.spawnedMembers["alpha"] = &spawnedMemberRecord{
		Config:     &TeamMemberConfig{ID: "alpha", Name: "Alpha"},
		WorkingDir: "/tmp/work",
		Provider:   "anthropic",
		Model:      "opus",
		Perpetual:  true,
	}
	m.mu.Unlock()
	return m, stub
}

func TestCheckForExitedMembers(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(m *Manager)
		wantCalls []string
	}{
		{
			name:      "respawns dead perpetual member past grace",
			mutate:    func(m *Manager) {},
			wantCalls: []string{"alpha"},
		},
		{
			name:      "skips when wrapping up",
			mutate:    func(m *Manager) { m.SetWrappingUp(true) },
			wantCalls: nil,
		},
		{
			name:      "skips when not perpetual",
			mutate:    func(m *Manager) { m.SetPerpetualMode(false) },
			wantCalls: nil,
		},
		{
			name: "skips within grace period",
			mutate: func(m *Manager) {
				m.mu.Lock()
				m.teamSpawnedAt = time.Now() // brand new spawn -> inside grace
				m.mu.Unlock()
			},
			wantCalls: nil,
		},
		{
			name: "skips when member is live",
			mutate: func(m *Manager) {
				m.RegisterAgent("sess-alpha-001", "alpha",
					&TeamMemberConfig{ID: "alpha", Name: "Alpha"})
			},
			wantCalls: nil,
		},
		{
			name: "skips within 90s respawn debounce",
			mutate: func(m *Manager) {
				m.mu.Lock()
				m.respawnHistory["alpha"] = []time.Time{time.Now().Add(-30 * time.Second)}
				m.mu.Unlock()
			},
			wantCalls: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, stub := seedExitedMember(t)
			tt.mutate(m)

			m.checkForExitedMembers()

			got := stub.calls()
			if len(got) != len(tt.wantCalls) {
				t.Fatalf("respawn calls = %v, want %v", got, tt.wantCalls)
			}
			for i := range tt.wantCalls {
				if got[i] != tt.wantCalls[i] {
					t.Fatalf("respawn call[%d] = %q, want %q", i, got[i], tt.wantCalls[i])
				}
			}
		})
	}
}

// TestCheckForExitedMembers_NilCallbackIsInert verifies the watchdog no-ops when
// the respawn callback was never wired (a manager constructed without a handler).
func TestCheckForExitedMembers_NilCallbackIsInert(t *testing.T) {
	m, _ := seedExitedMember(t)
	m.SetRespawnMemberFunc(nil)
	// Must not panic and must do nothing.
	m.checkForExitedMembers()
}

// TestCheckForExitedMembers_PastDebounceRespawns confirms a member whose last
// respawn attempt is older than 90s IS retried (complements the debounce-skip
// case above).
func TestCheckForExitedMembers_PastDebounceRespawns(t *testing.T) {
	m, stub := seedExitedMember(t)
	m.mu.Lock()
	m.respawnHistory["alpha"] = []time.Time{time.Now().Add(-2 * time.Minute)}
	m.mu.Unlock()

	m.checkForExitedMembers()

	if got := stub.calls(); len(got) != 1 || got[0] != "alpha" {
		t.Fatalf("respawn calls = %v, want [alpha]", got)
	}
}
