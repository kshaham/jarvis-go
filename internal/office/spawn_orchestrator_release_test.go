package office

import (
	"testing"

	"github.com/kshaham/jarvis/internal/office/spawnpayload"
)

// TestReleaseTeamSpawnSlot_RollsBackSpawnedFlag verifies the
// failure-rollback path clears the spawned flag claimed by a prior
// FinalizeTeamSpawn so the operator can retry without restarting jarvis.
func TestReleaseTeamSpawnSlot_RollsBackSpawnedFlag(t *testing.T) {
	m := newTestManager()

	// Simulate a spawn that claimed the slot.
	m.FinalizeTeamSpawn(spawnpayload.SpawnTeamRequest{WorkingDir: "/tmp/p"}, "tpl", false)
	if !m.IsTeamSpawned() {
		t.Fatal("precondition: expected team spawned after FinalizeTeamSpawn")
	}

	m.ReleaseTeamSpawnSlot()

	if m.IsTeamSpawned() {
		t.Error("expected ReleaseTeamSpawnSlot to clear the spawned flag")
	}
}

// TestReleaseTeamSpawnSlot_IdempotentWhenNotSpawned verifies releasing an
// unclaimed slot is a harmless no-op (still reports not-spawned).
func TestReleaseTeamSpawnSlot_IdempotentWhenNotSpawned(t *testing.T) {
	m := newTestManager()
	if m.IsTeamSpawned() {
		t.Fatal("precondition: fresh manager should not be spawned")
	}
	m.ReleaseTeamSpawnSlot()
	if m.IsTeamSpawned() {
		t.Error("expected still-not-spawned after releasing an unclaimed slot")
	}
}
