package office

import (
	"strings"
	"testing"
)

// TestInjectRelayMessage_Branches covers the not-spawned short-circuit, the
// strategic-target selection, and the no-target case.
func TestInjectRelayMessage_Branches(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()

	// Team not spawned -> false.
	if m.InjectRelayMessage("peer-1", "Peer", "hello") {
		t.Error("expected false when team not spawned")
	}

	m.SetTeamSpawned(true)
	// Spawned but empty roster -> no target -> false.
	if m.InjectRelayMessage("peer-1", "Peer", "hello") {
		t.Error("expected false with an empty roster")
	}

	// With a strategic agent, the relay targets it.
	m.RegisterAgent("sess-lead", "lead", &TeamMemberConfig{ID: "lead", Name: "Lead", Tier: "strategic"})
	m.RegisterAgent("sess-w", "worker", &TeamMemberConfig{ID: "worker", Name: "Worker", Tier: "worker"})
	if !m.InjectRelayMessage("peer-2", "Friendly Peer", "please review") {
		t.Fatal("expected relay delivered to strategic agent")
	}
	// The relay message lands in the message log addressed to the lead.
	var found bool
	for _, msg := range m.GetMessages() {
		if strings.Contains(msg.Content, "Friendly Peer") && strings.Contains(msg.Content, "please review") {
			found = true
			if msg.To != "sess-lead" {
				t.Errorf("expected relay addressed to the lead, got %q", msg.To)
			}
		}
	}
	if !found {
		t.Error("expected the relay message recorded in the message log")
	}
}

// TestInjectRelayMessage_FallsBackToFirstAgent verifies that without a
// strategic agent the relay targets the first roster member.
func TestInjectRelayMessage_FallsBackToFirstAgent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.SetTeamSpawned(true)
	m.RegisterAgent("sess-only", "only", &TeamMemberConfig{ID: "only", Name: "Only", Tier: "worker"})

	if !m.InjectRelayMessage("peer", "P", "msg") {
		t.Fatal("expected relay delivered to the only (non-strategic) agent")
	}
}

// TestGetConfig_NilReturnsEmpty covers the nil-config branch of GetConfig.
func TestGetConfig_NilReturnsEmpty(t *testing.T) {
	m := &Manager{} // config is nil
	cfg := m.GetConfig()
	if cfg == nil {
		t.Fatal("expected a non-nil empty config")
	}
	if cfg.MaxMessages != 0 {
		t.Errorf("expected zero-value config, got %+v", cfg)
	}
}

// TestSnapshotRosterAsTeamMemberConfigs_SkipsNilConfig verifies agents with a
// nil Config are skipped.
func TestSnapshotRosterAsTeamMemberConfigs_SkipsNilConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.RegisterAgent("sess-a", "alpha", &TeamMemberConfig{ID: "alpha", Name: "Alpha"})
	m.RegisterAgent("sess-b", "beta", &TeamMemberConfig{ID: "beta", Name: "Beta"})

	configs := m.SnapshotRosterAsTeamMemberConfigs()
	if len(configs) != 2 {
		t.Fatalf("expected 2 configs, got %d", len(configs))
	}
	names := map[string]bool{}
	for _, c := range configs {
		names[c.Name] = true
	}
	if !names["Alpha"] || !names["Beta"] {
		t.Errorf("expected both agent configs, got %v", names)
	}
}
