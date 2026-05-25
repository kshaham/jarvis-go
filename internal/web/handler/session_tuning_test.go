package handler

import "testing"

// Tests for the R1.3 sessionTuning resolver. Asserts that the
// jarvis defaults < team-wide < per-member precedence holds for
// every field. Budget-cap fields were removed because jarvis runs
// against subscription-billed providers where dollar limits are
// meaningless.

func TestResolveSessionTuning_Defaults(t *testing.T) {
	got := resolveSessionTuning(spawnTeamRequest{}, "worker", nil)
	if got.Effort != defaultEffortWorker {
		t.Errorf("worker default Effort=%q, want %q", got.Effort, defaultEffortWorker)
	}
	if !got.ExcludeDynamicSystemPromptSections {
		t.Error("default should exclude dynamic system prompt sections (cache-friendly)")
	}
	if !got.StrictMcpConfig {
		t.Error("default should strict-mcp-config")
	}
	if got.FallbackModel != "" {
		t.Errorf("default FallbackModel=%q, want empty", got.FallbackModel)
	}
}

func TestResolveSessionTuning_StrategicTierHighEffort(t *testing.T) {
	got := resolveSessionTuning(spawnTeamRequest{}, "strategic", nil)
	if got.Effort != defaultEffortStrategic {
		t.Errorf("strategic default Effort=%q, want %q", got.Effort, defaultEffortStrategic)
	}
}

func TestResolveSessionTuning_TeamWideOverridesDefaults(t *testing.T) {
	excl := false
	strict := false
	req := spawnTeamRequest{
		DefaultEffort:                      "low",
		DefaultFallbackModel:               "sonnet",
		ExcludeDynamicSystemPromptSections: &excl,
		StrictMcpConfig:                    &strict,
	}
	got := resolveSessionTuning(req, "worker", nil)
	if got.Effort != "low" || got.FallbackModel != "sonnet" {
		t.Errorf("team-wide override not applied: %+v", got)
	}
	if got.ExcludeDynamicSystemPromptSections || got.StrictMcpConfig {
		t.Error("team-wide bool overrides should flip defaults to false")
	}
}

func TestResolveSessionTuning_MemberOverrideWinsOverTeam(t *testing.T) {
	req := spawnTeamRequest{
		DefaultEffort:        "low",
		DefaultFallbackModel: "sonnet",
	}
	ov := &memberOverride{
		Effort:        "high",
		FallbackModel: "opus",
	}
	got := resolveSessionTuning(req, "worker", ov)
	if got.Effort != "high" {
		t.Errorf("member Effort should win: got %q", got.Effort)
	}
	if got.FallbackModel != "opus" {
		t.Errorf("member FallbackModel should win: got %q", got.FallbackModel)
	}
}

func TestResolveSessionTuning_MemberPartialOverrideKeepsRest(t *testing.T) {
	req := spawnTeamRequest{
		DefaultEffort:        "low",
		DefaultFallbackModel: "sonnet",
	}
	ov := &memberOverride{Effort: "high"} // only effort overridden
	got := resolveSessionTuning(req, "worker", ov)
	if got.Effort != "high" {
		t.Errorf("Effort=%q, want high", got.Effort)
	}
	if got.FallbackModel != "sonnet" {
		t.Errorf("FallbackModel should keep team value: got %q", got.FallbackModel)
	}
}

// R3.5 · Resume + fork are strictly per-member. The team-wide request
// has no analogue (no DefaultResumeClaudeSessionID), so the resolver
// just passes the override through verbatim.
func TestResolveSessionTuning_ResumeForkPlumbing(t *testing.T) {
	ov := &memberOverride{
		ResumeClaudeSessionID: "claude-sess-42",
		ForkResumedSession:    true,
	}
	got := resolveSessionTuning(spawnTeamRequest{}, "worker", ov)
	if got.ResumeClaudeSessionID != "claude-sess-42" {
		t.Errorf("ResumeClaudeSessionID=%q, want claude-sess-42", got.ResumeClaudeSessionID)
	}
	if !got.ForkResumedSession {
		t.Error("ForkResumedSession should be true")
	}
}

func TestResolveSessionTuning_NoOverrideMeansNoResume(t *testing.T) {
	// Routine spawn (no fork) should leave both resume fields at zero.
	got := resolveSessionTuning(spawnTeamRequest{}, "worker", nil)
	if got.ResumeClaudeSessionID != "" {
		t.Errorf("ResumeClaudeSessionID=%q, want empty for non-fork spawn", got.ResumeClaudeSessionID)
	}
	if got.ForkResumedSession {
		t.Error("ForkResumedSession should default to false")
	}
}

func TestResolveSessionTuning_EmptyOverrideDoesNotZeroResume(t *testing.T) {
	// A member override with NO resume fields shouldn't accidentally
	// scrub a future team-wide default. Today there is no team-wide
	// default for resume, so this is mostly a pin against regression
	// if someone later adds DefaultResumeClaudeSessionID at the team
	// level: the current behaviour is "blank member override leaves
	// the zero value", which the test below asserts.
	ov := &memberOverride{Effort: "high"} // no resume fields
	got := resolveSessionTuning(spawnTeamRequest{}, "worker", ov)
	if got.ResumeClaudeSessionID != "" {
		t.Errorf("got non-empty ResumeClaudeSessionID=%q from blank member", got.ResumeClaudeSessionID)
	}
	if got.ForkResumedSession {
		t.Error("got ForkResumedSession=true from blank member")
	}
}
