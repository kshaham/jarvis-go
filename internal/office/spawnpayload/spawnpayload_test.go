package spawnpayload

import "testing"

func boolPtr(b bool) *bool { return &b }

func TestCodegraphOptOut(t *testing.T) {
	cases := []struct {
		name string
		req  SpawnTeamRequest
		want bool
	}{
		{"nil = default off", SpawnTeamRequest{}, false},
		{"explicit false", SpawnTeamRequest{DisableCodegraph: boolPtr(false)}, false},
		{"explicit true", SpawnTeamRequest{DisableCodegraph: boolPtr(true)}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CodegraphOptOut(tc.req); got != tc.want {
				t.Errorf("CodegraphOptOut = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEccOptOut(t *testing.T) {
	cases := []struct {
		name string
		req  SpawnTeamRequest
		want bool
	}{
		{"nil = default off", SpawnTeamRequest{}, false},
		{"explicit false", SpawnTeamRequest{DisableEcc: boolPtr(false)}, false},
		{"explicit true", SpawnTeamRequest{DisableEcc: boolPtr(true)}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EccOptOut(tc.req); got != tc.want {
				t.Errorf("EccOptOut = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDefaultEffortFor(t *testing.T) {
	if got := DefaultEffortFor("strategic"); got != DefaultEffortStrategic {
		t.Errorf("strategic tier: got %q want %q", got, DefaultEffortStrategic)
	}
	for _, tier := range []string{"worker", "", "anything-else"} {
		if got := DefaultEffortFor(tier); got != DefaultEffortWorker {
			t.Errorf("tier %q: got %q want %q", tier, got, DefaultEffortWorker)
		}
	}
}

func TestResolveSessionTuning_DefaultsOnly(t *testing.T) {
	got := ResolveSessionTuning(SpawnTeamRequest{}, "", "worker", nil)
	if got.Effort != DefaultEffortWorker {
		t.Errorf("Effort=%q want %q", got.Effort, DefaultEffortWorker)
	}
	if got.FallbackModel != "" {
		t.Errorf("FallbackModel=%q want empty", got.FallbackModel)
	}
	if !got.ExcludeDynamicSystemPromptSections {
		t.Error("ExcludeDynamicSystemPromptSections default should be true")
	}
	if !got.StrictMcpConfig {
		t.Error("StrictMcpConfig default should be true")
	}
}

func TestResolveSessionTuning_StrategicTierBumpsEffort(t *testing.T) {
	got := ResolveSessionTuning(SpawnTeamRequest{}, "", "strategic", nil)
	if got.Effort != DefaultEffortStrategic {
		t.Errorf("strategic Effort=%q want %q", got.Effort, DefaultEffortStrategic)
	}
}

func TestResolveSessionTuning_TeamWideOverrides(t *testing.T) {
	req := SpawnTeamRequest{
		DefaultEffort:                      "low",
		DefaultFallbackModel:               "sonnet",
		ExcludeDynamicSystemPromptSections: boolPtr(false),
		StrictMcpConfig:                    boolPtr(false),
	}
	got := ResolveSessionTuning(req, "", "worker", nil)
	if got.Effort != "low" {
		t.Errorf("Effort=%q want low", got.Effort)
	}
	if got.FallbackModel != "sonnet" {
		t.Errorf("FallbackModel=%q want sonnet", got.FallbackModel)
	}
	if got.ExcludeDynamicSystemPromptSections {
		t.Error("explicit false override not applied")
	}
	if got.StrictMcpConfig {
		t.Error("StrictMcpConfig false override not applied")
	}
}

func TestResolveSessionTuning_PerMemberOverridesWin(t *testing.T) {
	req := SpawnTeamRequest{DefaultEffort: "low", DefaultFallbackModel: "sonnet"}
	ov := &MemberOverride{
		Effort:                "max",
		FallbackModel:         "opus",
		ResumeClaudeSessionID: "abc-123-xyz",
		ForkResumedSession:    true,
	}
	got := ResolveSessionTuning(req, "", "worker", ov)
	if got.Effort != "max" {
		t.Errorf("Effort=%q want max", got.Effort)
	}
	if got.FallbackModel != "opus" {
		t.Errorf("FallbackModel=%q want opus", got.FallbackModel)
	}
	if got.ResumeClaudeSessionID != "abc-123-xyz" {
		t.Errorf("ResumeClaudeSessionID=%q", got.ResumeClaudeSessionID)
	}
	if !got.ForkResumedSession {
		t.Error("ForkResumedSession=false")
	}
}

func TestResolveSessionTuning_PerMemberBlankFallsBack(t *testing.T) {
	// Per-member overrides with blank Effort/FallbackModel should not
	// override the team-wide values — only non-blank values win.
	req := SpawnTeamRequest{DefaultEffort: "low", DefaultFallbackModel: "sonnet"}
	ov := &MemberOverride{Effort: "", FallbackModel: ""}
	got := ResolveSessionTuning(req, "", "worker", ov)
	if got.Effort != "low" {
		t.Errorf("Effort=%q want low (team-wide should hold)", got.Effort)
	}
	if got.FallbackModel != "sonnet" {
		t.Errorf("FallbackModel=%q want sonnet (team-wide should hold)", got.FallbackModel)
	}
}
