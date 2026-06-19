package office

import (
	"errors"
	"testing"
)

// recordSpawnedMember registers an agent + snapshots it so PrepareMemberRespawn
// can find a spawn record to rebuild. Mirrors the FinalizeTeamSpawn path
// (RegisterAgent -> RecordSpawnedMembers) without the full spawn flow.
func recordSpawnedMember(m *Manager, configID, sessionID string) {
	m.RegisterAgent(sessionID, configID, &TeamMemberConfig{ID: configID, Name: configID, Role: "Backend"})
	m.RecordSpawnedMembers("/tmp/work", "claude-subscription", "sonnet", true)
}

// TestPrepareMemberRespawn_TurnCapExemptFromCap verifies that a member whose
// death was a clean turn-cap rollover (flagged via MarkTurnCapDeath) can be
// respawned an unbounded number of times without tripping the respawn cap.
// This is the load-bearing half of the team-shrinkage fix: legitimate
// steady-state work hitting the turn ceiling must never permanently retire a
// perpetual member.
func TestPrepareMemberRespawn_TurnCapExemptFromCap(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	recordSpawnedMember(m, "atlas", "sess-atlas")

	// Far more rollovers than respawnCapCount (3): each marked as a turn-cap
	// death, so none of them should consume the respawn-cap budget.
	for i := 0; i < respawnCapCount*4; i++ {
		m.MarkTurnCapDeath("sess-atlas")
		if _, err := m.PrepareMemberRespawn("atlas"); err != nil {
			t.Fatalf("turn-cap rollover %d should be exempt from respawn cap, got %v", i, err)
		}
	}

	// The exempt path must not have recorded any respawn-history entries.
	m.mu.RLock()
	hist := len(m.respawnHistory["atlas"])
	m.mu.RUnlock()
	if hist != 0 {
		t.Errorf("turn-cap rollovers should not record respawn history, got %d entries", hist)
	}
}

// TestPrepareMemberRespawn_GenuineCrashStillTripsCap verifies the cap still
// fires for a member that keeps crashing WITHOUT a turn-cap mark. The
// exemption must be scoped strictly to turn-cap-tagged exits.
func TestPrepareMemberRespawn_GenuineCrashStillTripsCap(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	recordSpawnedMember(m, "byte", "sess-byte")

	// No MarkTurnCapDeath -> genuine crash flaps. First respawnCapCount
	// succeed, then the cap trips.
	for i := 0; i < respawnCapCount; i++ {
		if _, err := m.PrepareMemberRespawn("byte"); err != nil {
			t.Fatalf("crash respawn %d should succeed before cap, got %v", i, err)
		}
	}
	if _, err := m.PrepareMemberRespawn("byte"); !errors.Is(err, ErrRespawnCapReached) {
		t.Fatalf("expected ErrRespawnCapReached after %d crashes, got %v", respawnCapCount, err)
	}
}

// TestMarkTurnCapDeath_ConsumedOnce verifies the flag is consumed exactly once
// per mark: a single turn-cap death exempts a single respawn, after which a
// subsequent crash (no fresh mark) falls back to cap accounting.
func TestMarkTurnCapDeath_ConsumedOnce(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	recordSpawnedMember(m, "pixel", "sess-pixel")

	// One mark exempts one respawn.
	m.MarkTurnCapDeath("sess-pixel")
	if !m.consumeTurnCapDeath("pixel") {
		t.Fatal("first consume after mark should report true")
	}
	// Second consume without a fresh mark must report false (flag cleared).
	if m.consumeTurnCapDeath("pixel") {
		t.Error("turn-cap flag should be consumed exactly once per mark")
	}
}

// TestMarkTurnCapDeath_UnknownSessionNoOp covers the early return for a
// sessionID with no live agent (e.g. already unregistered). Must not panic and
// must not flag anything.
func TestMarkTurnCapDeath_UnknownSessionNoOp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.MarkTurnCapDeath("ghost-session")
	if m.consumeTurnCapDeath("ghost-session") {
		t.Error("MarkTurnCapDeath on an unknown session should not flag any member")
	}
}
