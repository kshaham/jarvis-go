package office

import (
	"testing"

	"github.com/kshaham/jarvis/internal/office/spawnpayload"
	"github.com/kshaham/jarvis/internal/teamtemplates"
)

func TestVoiceSurvivesSnapshotRestore(t *testing.T) {
	snap := &TeamSnapshot{
		Members: []SnapshotMemberEntry{
			{ConfigID: "m1", Config: TeamMemberConfig{ID: "m1", Name: "Alice", Voice: "marin"}},
		},
	}
	overrides, _ := BuildRestoreOverrides(snap, false)
	if len(overrides) != 1 || overrides[0].Voice != "marin" {
		t.Fatalf("voice not carried into restore override: %+v", overrides)
	}
}

// TestVoiceThreadsThroughRestoreApply asserts that Voice set in a
// MemberOverride survives the full Apply->BuildConfigs chain.
func TestVoiceThreadsThroughRestoreApply(t *testing.T) {
	base := []teamtemplates.TeamTemplateMember{
		{ID: "m1", Name: "Alice", Role: "lead", Tier: "strategic"},
	}
	overrides := []spawnpayload.MemberOverride{
		{ID: "m1", Voice: "marin"},
	}
	ApplyMemberOverrides(base, overrides)
	configs := BuildTeamMemberConfigs(base)
	if len(configs) != 1 {
		t.Fatalf("expected 1 config, got %d", len(configs))
	}
	if configs[0].Voice != "marin" {
		t.Errorf("Voice=%q want marin after Apply->BuildConfigs", configs[0].Voice)
	}
}
