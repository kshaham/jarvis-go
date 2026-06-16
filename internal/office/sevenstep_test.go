package office

import (
	"strings"
	"testing"

	"github.com/kshaham/jarvis/internal/office/spawnpayload"
)

func boolPtr(b bool) *bool { return &b }

func TestResolveSevenStepMode(t *testing.T) {
	// Default (unset) is OFF -- opt-in, unlike perpetual.
	if ResolveSevenStepMode(spawnpayload.SpawnTeamRequest{}) {
		t.Error("expected 7-step mode OFF when unset")
	}
	if !ResolveSevenStepMode(spawnpayload.SpawnTeamRequest{SevenStepMode: boolPtr(true)}) {
		t.Error("expected 7-step mode ON when explicitly true")
	}
	if ResolveSevenStepMode(spawnpayload.SpawnTeamRequest{SevenStepMode: boolPtr(false)}) {
		t.Error("expected 7-step mode OFF when explicitly false")
	}
}

func TestKickstart_SevenStepSectionGatedOnFlag(t *testing.T) {
	cfg := &TeamMemberConfig{ID: "a", Name: "Ada", Role: "Backend", Tier: "worker"}

	on := BuildKickstartPrompt(cfg, "/repo", nil, "ship it", "", false, nil, true)
	if !strings.Contains(on, "7-Step Engineering") {
		t.Error("expected the 7-step section when sevenStepMode is true")
	}
	for _, marker := range []string{"/spec", "/plan", "/build", "/test", "/review", "/code-simplify", "/ship", "cm task phase"} {
		if !strings.Contains(on, marker) {
			t.Errorf("expected 7-step section to mention %q", marker)
		}
	}

	off := BuildKickstartPrompt(cfg, "/repo", nil, "ship it", "", false, nil, false)
	if strings.Contains(off, "7-Step Engineering") {
		t.Error("did not expect the 7-step section when sevenStepMode is false")
	}
}

func TestSetIsSevenStepMode(t *testing.T) {
	m, _, _ := newCtrlManager(t, DefaultConfig())
	if m.IsSevenStepMode() {
		t.Fatal("expected 7-step mode off by default")
	}
	m.SetSevenStepMode(true)
	if !m.IsSevenStepMode() {
		t.Error("expected 7-step mode on after SetSevenStepMode(true)")
	}
	// Surfaced in the status payload.
	if v, ok := m.GetOfficeState()["sevenStepMode"].(bool); !ok || !v {
		t.Error("expected sevenStepMode=true in GetOfficeState payload")
	}
}
