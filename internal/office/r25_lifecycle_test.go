package office

// R25 · Pause/resume + ReassignSessionID + workdir bookkeeping tests.

import (
	"testing"
	"time"
)

func TestSetPaused_TogglesAndRecordsMetadata(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Cleanup(func() { time.Sleep(60 * time.Millisecond) })
	m := NewManager(&recordingSessionStore{}, func(string, interface{}) {}, nil)

	if m.IsPaused() {
		t.Fatal("manager should start unpaused")
	}
	was := m.SetPaused(true, "deploy window")
	if was {
		t.Errorf("expected wasPaused=false on first pause")
	}
	if !m.IsPaused() {
		t.Fatal("IsPaused should be true after SetPaused(true, _)")
	}
	paused, since, reason := m.PauseSnapshot()
	if !paused || reason != "deploy window" || since.IsZero() {
		t.Errorf("unexpected snapshot: paused=%v reason=%q since=%v", paused, reason, since)
	}

	was = m.SetPaused(true, "another")
	if !was {
		t.Errorf("expected wasPaused=true on idempotent second pause")
	}

	was = m.SetPaused(false, "")
	if !was {
		t.Errorf("expected wasPaused=true when transitioning back to unpaused")
	}
	if m.IsPaused() {
		t.Fatal("expected unpaused after SetPaused(false, _)")
	}
}

func TestReassignSessionID_MovesAgentAndLocks(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Cleanup(func() { time.Sleep(60 * time.Millisecond) })
	m := NewManager(&recordingSessionStore{}, func(string, interface{}) {}, nil)

	m.RegisterAgent("sess-old", "cfg-a", &TeamMemberConfig{ID: "cfg-a", Name: "Ada", Tier: "strategic"})
	// Claim a file lock on the old session so we can verify it
	// migrates to the new session id.
	if _, err := m.AcquireLock("/code/foo.go", "sess-old", "Ada"); err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}

	ok := m.ReassignSessionID("sess-old", "sess-new")
	if !ok {
		t.Fatal("expected reassignment to succeed")
	}

	if m.GetAgent("sess-old") != nil {
		t.Fatal("old session id should no longer resolve to an agent")
	}
	a := m.GetAgent("sess-new")
	if a == nil {
		t.Fatal("new session id should resolve to the same AgentState")
	}
	if a.SessionID != "sess-new" {
		t.Errorf("agent.SessionID not updated: got %q", a.SessionID)
	}
	if byCfg := m.GetAgentByConfig("cfg-a"); byCfg == nil || byCfg.SessionID != "sess-new" {
		t.Error("configIndex should point to the new session id")
	}
	locks := m.GetLocks()
	if len(locks) != 1 || locks[0].Owner != "sess-new" {
		t.Errorf("lock should have migrated to new owner, got %+v", locks)
	}
}

func TestReassignSessionID_RefusesCollision(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Cleanup(func() { time.Sleep(60 * time.Millisecond) })
	m := NewManager(&recordingSessionStore{}, func(string, interface{}) {}, nil)
	m.RegisterAgent("sess-a", "cfg-a", &TeamMemberConfig{ID: "cfg-a", Name: "A", Tier: "worker"})
	m.RegisterAgent("sess-b", "cfg-b", &TeamMemberConfig{ID: "cfg-b", Name: "B", Tier: "worker"})

	if ok := m.ReassignSessionID("sess-a", "sess-b"); ok {
		t.Error("expected collision to fail")
	}
	// Both agents still resolvable — nothing should have changed.
	if m.GetAgent("sess-a") == nil || m.GetAgent("sess-b") == nil {
		t.Error("expected both agents to survive the failed reassignment")
	}
}

func TestReassignSessionID_RejectsEmptyOrIdentical(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Cleanup(func() { time.Sleep(60 * time.Millisecond) })
	m := NewManager(&recordingSessionStore{}, func(string, interface{}) {}, nil)
	if m.ReassignSessionID("", "x") {
		t.Error("empty old id should fail")
	}
	if m.ReassignSessionID("x", "") {
		t.Error("empty new id should fail")
	}
	if m.ReassignSessionID("same", "same") {
		t.Error("identical ids should fail")
	}
}
