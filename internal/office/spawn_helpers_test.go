package office

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/office/spawnpayload"
	"github.com/kshaham/jarvis/internal/teamtemplates"
)

// stubLookup returns a TemplateLookup backed by a fixed map.
func stubLookup(m map[string]*teamtemplates.TeamTemplate) TemplateLookup {
	return func(id string) *teamtemplates.TeamTemplate { return m[id] }
}

// alphaTeam is a 3-member template used across happy-path tests.
var alphaTeam = &teamtemplates.TeamTemplate{
	ID:   "alpha",
	Name: "Alpha",
	Members: []teamtemplates.TeamTemplateMember{
		{ID: "m1", Name: "Mia", Role: "lead", Tier: "strategic"},
		{ID: "m2", Name: "Max", Role: "frontend", Tier: "worker"},
		{ID: "m3", Name: "Vic", Role: "backend", Tier: "worker"},
	},
}

func TestResolveMemberList_NoOverridesReturnsFullTemplate(t *testing.T) {
	req := spawnpayload.SpawnTeamRequest{TemplateID: "alpha"}
	lookup := stubLookup(map[string]*teamtemplates.TeamTemplate{"alpha": alphaTeam})
	got, used := ResolveMemberList(req, lookup)
	if used != "alpha" {
		t.Errorf("templateUsed=%q want alpha", used)
	}
	if len(got) != 3 {
		t.Fatalf("len=%d want 3", len(got))
	}
	for i, want := range []string{"m1", "m2", "m3"} {
		if got[i].ID != want {
			t.Errorf("[%d] id=%q want %q", i, got[i].ID, want)
		}
	}
}

func TestResolveMemberList_BlankTemplateFallsBackToOffice7(t *testing.T) {
	office7 := &teamtemplates.TeamTemplate{
		ID:      "office-7",
		Members: []teamtemplates.TeamTemplateMember{{ID: "m1", Name: "Default"}},
	}
	req := spawnpayload.SpawnTeamRequest{} // no TemplateID
	lookup := stubLookup(map[string]*teamtemplates.TeamTemplate{"office-7": office7})
	got, used := ResolveMemberList(req, lookup)
	if used != "office-7" {
		t.Errorf("templateUsed=%q want office-7", used)
	}
	if len(got) != 1 || got[0].ID != "m1" {
		t.Errorf("unexpected members: %+v", got)
	}
}

func TestResolveMemberList_UnknownTemplateFallsBackToOffice7(t *testing.T) {
	office7 := &teamtemplates.TeamTemplate{
		ID:      "office-7",
		Members: []teamtemplates.TeamTemplateMember{{ID: "fallback"}},
	}
	req := spawnpayload.SpawnTeamRequest{TemplateID: "ghost-template"}
	lookup := stubLookup(map[string]*teamtemplates.TeamTemplate{"office-7": office7})
	got, used := ResolveMemberList(req, lookup)
	if used != "office-7" {
		t.Errorf("templateUsed=%q want office-7", used)
	}
	if len(got) != 1 || got[0].ID != "fallback" {
		t.Errorf("unexpected members: %+v", got)
	}
}

func TestResolveMemberList_NoLookupFallsBackToFirstBuiltin(t *testing.T) {
	// Real builtins so we don't have to stub them; just assert the
	// loop reaches the BuiltInTemplates() path.
	req := spawnpayload.SpawnTeamRequest{}
	got, used := ResolveMemberList(req, nil)
	if len(got) == 0 {
		t.Fatal("expected fallback to first builtin to produce members")
	}
	if used == "" {
		t.Error("templateUsed should not be empty in builtin-fallback path")
	}
}

func TestResolveMemberList_OverridesDropRemovedMembers(t *testing.T) {
	// Operator removed m2 in the modal — overrides list has only m1 + m3.
	req := spawnpayload.SpawnTeamRequest{
		TemplateID: "alpha",
		MemberOverrides: []spawnpayload.MemberOverride{
			{ID: "m1"},
			{ID: "m3"},
		},
	}
	lookup := stubLookup(map[string]*teamtemplates.TeamTemplate{"alpha": alphaTeam})
	got, _ := ResolveMemberList(req, lookup)
	if len(got) != 2 {
		t.Fatalf("len=%d want 2", len(got))
	}
	if got[0].ID != "m1" || got[1].ID != "m3" {
		t.Errorf("dropped wrong member: ids=%v", []string{got[0].ID, got[1].ID})
	}
}

func TestResolveMemberList_OverridesPreserveTemplateOrder(t *testing.T) {
	// Overrides intentionally listed out-of-template-order; the result
	// should still follow template order (m1, m2, m3).
	req := spawnpayload.SpawnTeamRequest{
		TemplateID: "alpha",
		MemberOverrides: []spawnpayload.MemberOverride{
			{ID: "m3"}, {ID: "m1"}, {ID: "m2"},
		},
	}
	lookup := stubLookup(map[string]*teamtemplates.TeamTemplate{"alpha": alphaTeam})
	got, _ := ResolveMemberList(req, lookup)
	if len(got) != 3 {
		t.Fatalf("len=%d", len(got))
	}
	for i, want := range []string{"m1", "m2", "m3"} {
		if got[i].ID != want {
			t.Errorf("[%d] %q want %q", i, got[i].ID, want)
		}
	}
}

func TestResolveMemberList_SyntheticMembersAppendedAfterTemplateOrder(t *testing.T) {
	req := spawnpayload.SpawnTeamRequest{
		TemplateID: "alpha",
		MemberOverrides: []spawnpayload.MemberOverride{
			{ID: "m1"},
			{ID: "m2"},
			{ID: "m3"},
			{ID: "synth-1", Name: "Synthi", Role: "qa", Tier: "worker", Color: "pink"},
			{ID: "synth-2"}, // intentionally minimal to test defaults
		},
	}
	lookup := stubLookup(map[string]*teamtemplates.TeamTemplate{"alpha": alphaTeam})
	got, _ := ResolveMemberList(req, lookup)
	if len(got) != 5 {
		t.Fatalf("len=%d want 5", len(got))
	}
	for i, want := range []string{"m1", "m2", "m3", "synth-1", "synth-2"} {
		if got[i].ID != want {
			t.Errorf("[%d] %q want %q", i, got[i].ID, want)
		}
	}

	synth1 := got[3]
	if synth1.Name != "Synthi" || synth1.Role != "qa" || synth1.Tier != "worker" || synth1.Color != "pink" {
		t.Errorf("synth-1 fields not propagated: %+v", synth1)
	}
	// Minimal synth-2 should fall back to defaults derived from the
	// override fields (ID as name, "Engineer" role, "worker" tier,
	// "blue" colour).
	synth2 := got[4]
	if synth2.Name != "synth-2" {
		t.Errorf("synth-2 Name fallback=%q want id", synth2.Name)
	}
	if synth2.Role != "Engineer" {
		t.Errorf("synth-2 Role default=%q want Engineer", synth2.Role)
	}
	if synth2.Tier != "worker" {
		t.Errorf("synth-2 Tier default=%q want worker", synth2.Tier)
	}
	if synth2.Color != "blue" {
		t.Errorf("synth-2 Color default=%q want blue", synth2.Color)
	}
}

func TestSpawnDelaySleep_NoopForZeroDelay(t *testing.T) {
	start := time.Now()
	SpawnDelaySleep("alpha", 0, false)
	if elapsed := time.Since(start); elapsed > 5*time.Millisecond {
		t.Errorf("expected near-zero elapsed, got %v", elapsed)
	}
}

func TestSpawnDelaySleep_NoopWhenLast(t *testing.T) {
	// Last member should NOT sleep — even with a non-zero delay
	// configured — because nothing follows.
	start := time.Now()
	SpawnDelaySleep("alpha", 1000, true)
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Errorf("last-member sleep fired: elapsed=%v", elapsed)
	}
}

func TestSpawnDelaySleep_SleepsForDelayMs(t *testing.T) {
	start := time.Now()
	SpawnDelaySleep("alpha", 50, false)
	elapsed := time.Since(start)
	if elapsed < 50*time.Millisecond {
		t.Errorf("expected at least 50ms, got %v", elapsed)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("slept too long: %v", elapsed)
	}
}

func TestSpawnDelaySleep_CapsAtMax(t *testing.T) {
	// Tweak MaxSpawnDelay down to a short value via the exported
	// variable so the test isn't a real 60-second wait.
	orig := MaxSpawnDelay
	MaxSpawnDelay = 100 * time.Millisecond
	t.Cleanup(func() { MaxSpawnDelay = orig })

	start := time.Now()
	SpawnDelaySleep("alpha", 5_000, false) // requested 5s
	elapsed := time.Since(start)
	if elapsed >= time.Second {
		t.Errorf("cap not applied: slept %v, expected ~100ms", elapsed)
	}
}

func TestResolveMemberWorkingDir_NilOverrideReturnsTeamWide(t *testing.T) {
	got, err := ResolveMemberWorkingDir("/team-wide", "m1", nil, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if got != "/team-wide" {
		t.Errorf("got %q want /team-wide", got)
	}
}

func TestResolveMemberWorkingDir_BlankOverrideReturnsTeamWide(t *testing.T) {
	got, err := ResolveMemberWorkingDir(
		"/team-wide", "m1",
		&spawnpayload.MemberOverride{WorkingDir: ""},
		nil,
	)
	if err != nil || got != "/team-wide" {
		t.Errorf("got (%q, %v) want (/team-wide, nil)", got, err)
	}
}

func TestResolveMemberWorkingDir_OverrideCallsResolver(t *testing.T) {
	resolver := func(raw string) (string, error) {
		return raw + "-resolved", nil
	}
	got, err := ResolveMemberWorkingDir(
		"/team-wide", "m1",
		&spawnpayload.MemberOverride{WorkingDir: "/per-member"},
		resolver,
	)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if got != "/per-member-resolved" {
		t.Errorf("got %q want /per-member-resolved", got)
	}
}

func TestResolveMemberWorkingDir_ResolverErrorWrappedWithMemberID(t *testing.T) {
	resolver := func(string) (string, error) {
		return "", errors.New("denied by allowlist")
	}
	_, err := ResolveMemberWorkingDir(
		"/team-wide", "vex",
		&spawnpayload.MemberOverride{WorkingDir: "/etc"},
		resolver,
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "vex") {
		t.Errorf("error should mention member ID, got: %v", err)
	}
	if !strings.Contains(err.Error(), "denied by allowlist") {
		t.Errorf("error should wrap resolver error, got: %v", err)
	}
}

func TestResolveMemberWorkingDir_NilResolverWithOverrideReturnsOverrideUnchanged(t *testing.T) {
	// Documents the contract: a nil resolver means "no allowlist
	// check available", which the caller has accepted by passing
	// nil. Production never does this, but tests do.
	got, err := ResolveMemberWorkingDir(
		"/team-wide", "m1",
		&spawnpayload.MemberOverride{WorkingDir: "/unsafe-but-allowed-in-test"},
		nil,
	)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if got != "/unsafe-but-allowed-in-test" {
		t.Errorf("got %q", got)
	}
}

func TestResolveMemberProviderModel_TeamWideOnly(t *testing.T) {
	// No template pin, no override, no Z.AI sonnet → team-wide
	// provider wins; model stays empty.
	provider, model := ResolveMemberProviderModel(
		teamtemplates.TeamTemplateMember{ID: "m1"},
		nil,
		"anthropic",
		"",
	)
	if provider != "anthropic" {
		t.Errorf("provider=%q want anthropic", provider)
	}
	if model != "" {
		t.Errorf("model=%q want empty", model)
	}
}

func TestResolveMemberProviderModel_TemplatePinsBeatTeamWide(t *testing.T) {
	provider, model := ResolveMemberProviderModel(
		teamtemplates.TeamTemplateMember{ID: "m1", Provider: "zai-glm", Model: "glm-5"},
		nil,
		"anthropic",
		"",
	)
	if provider != "zai-glm" {
		t.Errorf("provider=%q want zai-glm (template pin)", provider)
	}
	if model != "glm-5" {
		t.Errorf("model=%q want glm-5", model)
	}
}

func TestResolveMemberProviderModel_OverrideBeatsTemplate(t *testing.T) {
	provider, model := ResolveMemberProviderModel(
		teamtemplates.TeamTemplateMember{ID: "m1", Provider: "anthropic", Model: "opus"},
		&spawnpayload.MemberOverride{Provider: "zai-glm", Model: "glm-5.1"},
		"anthropic",
		"",
	)
	if provider != "zai-glm" {
		t.Errorf("provider=%q want zai-glm (override)", provider)
	}
	if model != "glm-5.1" {
		t.Errorf("model=%q want glm-5.1", model)
	}
}

func TestResolveMemberProviderModel_BlankOverrideFieldsDontClobber(t *testing.T) {
	provider, model := ResolveMemberProviderModel(
		teamtemplates.TeamTemplateMember{Provider: "anthropic", Model: "opus"},
		&spawnpayload.MemberOverride{Provider: "", Model: ""},
		"zai-glm",
		"",
	)
	if provider != "anthropic" {
		t.Errorf("provider=%q want anthropic (template pin should hold)", provider)
	}
	if model != "opus" {
		t.Errorf("model=%q want opus (template pin should hold)", model)
	}
}

func TestResolveMemberProviderModel_StrategicClaudeDefaultsToOpus48(t *testing.T) {
	// Strategic-tier lead on a first-party Claude provider with no pinned
	// model → defaults to Opus 4.8 (the model that authors Dynamic Workflows).
	for _, prov := range []string{"claude-subscription", "", "claude-api"} {
		provider, model := ResolveMemberProviderModel(
			teamtemplates.TeamTemplateMember{ID: "lead", Tier: "strategic"},
			nil,
			prov,
			"",
		)
		if model != "claude-opus-4-8" {
			t.Errorf("provider=%q strategic model=%q want claude-opus-4-8", provider, model)
		}
	}
}

func TestResolveMemberProviderModel_WorkerClaudeStaysDefault(t *testing.T) {
	// Worker tier must NOT be upgraded to Opus — keeps the cheaper default.
	_, model := ResolveMemberProviderModel(
		teamtemplates.TeamTemplateMember{ID: "w1", Tier: "worker"},
		nil,
		"claude-subscription",
		"",
	)
	if model != "" {
		t.Errorf("worker model=%q want empty (no Opus upgrade)", model)
	}
}

func TestResolveMemberProviderModel_StrategicZAINotOpus(t *testing.T) {
	// Strategic on a non-Claude provider (zai-glm) must not get Opus 4.8;
	// the Z.AI sonnet fallback fills the blank model instead.
	_, model := ResolveMemberProviderModel(
		teamtemplates.TeamTemplateMember{ID: "lead", Tier: "strategic"},
		nil,
		"zai-glm",
		"glm-5",
	)
	if model != "glm-5" {
		t.Errorf("strategic zai model=%q want glm-5 (no Opus upgrade for non-Claude)", model)
	}
}

func TestResolveMemberProviderModel_ExplicitModelBeatsOpusDefault(t *testing.T) {
	// An explicit template/override model on a strategic claude member wins
	// over the Opus-4.8 default.
	_, model := ResolveMemberProviderModel(
		teamtemplates.TeamTemplateMember{ID: "lead", Tier: "strategic", Provider: "claude-api", Model: "claude-sonnet-4-6"},
		nil,
		"claude-api",
		"",
	)
	if model != "claude-sonnet-4-6" {
		t.Errorf("model=%q want claude-sonnet-4-6 (explicit pin beats Opus default)", model)
	}
}

func TestResolveMemberProviderModel_ZAISonnetFillsBlankModel(t *testing.T) {
	// Z.AI allocator assigned glm-5 to this member; nothing else
	// pinned a model.
	provider, model := ResolveMemberProviderModel(
		teamtemplates.TeamTemplateMember{ID: "m1"},
		nil,
		"zai-glm",
		"glm-5",
	)
	if provider != "zai-glm" {
		t.Errorf("provider=%q", provider)
	}
	if model != "glm-5" {
		t.Errorf("model=%q want glm-5 (Z.AI sonnet fallback)", model)
	}
}

func TestResolveMemberProviderModel_ZAISonnetDoesNotOverrideExplicitModel(t *testing.T) {
	// Override pinned opus; Z.AI sonnet should NOT replace it.
	_, model := ResolveMemberProviderModel(
		teamtemplates.TeamTemplateMember{},
		&spawnpayload.MemberOverride{Model: "opus-explicit"},
		"zai-glm",
		"glm-5-from-allocator",
	)
	if model != "opus-explicit" {
		t.Errorf("Z.AI sonnet clobbered explicit model: %q", model)
	}
}

func TestResolveMemberProviderModel_StrategicOpenAIDefaultsToFlagship(t *testing.T) {
	// Strategic-tier lead on an OpenAI provider with no pinned model →
	// defaults to the flagship gpt-5.5 (Codex harness).
	for _, prov := range []string{"openai-codex", "openai-custom"} {
		_, model := ResolveMemberProviderModel(
			teamtemplates.TeamTemplateMember{ID: "lead", Tier: "strategic"},
			nil,
			prov,
			"",
		)
		if model != "gpt-5.5" {
			t.Errorf("provider=%q strategic model=%q want gpt-5.5", prov, model)
		}
	}
}

func TestResolveMemberProviderModel_WorkerOpenAIDefaultsToGPT54(t *testing.T) {
	// Worker-tier OpenAI member with no pinned model → gpt-5.4 (one step down
	// from the gpt-5.5 leads).
	for _, prov := range []string{"openai-codex", "openai-custom"} {
		_, model := ResolveMemberProviderModel(
			teamtemplates.TeamTemplateMember{ID: "w1", Tier: "worker"},
			nil,
			prov,
			"",
		)
		if model != "gpt-5.4" {
			t.Errorf("provider=%q worker model=%q want gpt-5.4", prov, model)
		}
	}
}

func TestResolveMemberProviderModel_ExplicitOpenAIModelWins(t *testing.T) {
	// An explicit pin beats both the strategic-flagship and worker-mini
	// defaults for OpenAI providers.
	_, model := ResolveMemberProviderModel(
		teamtemplates.TeamTemplateMember{ID: "lead", Tier: "strategic", Model: "gpt-5.3-codex"},
		nil,
		"openai-codex",
		"",
	)
	if model != "gpt-5.3-codex" {
		t.Errorf("model=%q want gpt-5.3-codex (explicit pin beats default)", model)
	}
}

func TestHarnessFor(t *testing.T) {
	cases := map[string]string{
		"openai-codex":        "codex",
		"openai-custom":       "codex",
		"claude-subscription": "claude",
		"claude-api":          "claude",
		"zai-glm":             "claude",
		"":                    "claude",
		"something-unknown":   "claude",
	}
	for provider, want := range cases {
		if got := HarnessFor(provider); got != want {
			t.Errorf("HarnessFor(%q)=%q want %q", provider, got, want)
		}
	}
}

// recordingEnvBuilder is a ProviderEnvBuilder stub that records each
// (provider, model, sonnet) triple it sees. Used to verify the loop
// passes per-member values through correctly.
func recordingEnvBuilder(calls *[]string) ProviderEnvBuilder {
	return func(provider, model, sonnet string) map[string]string {
		*calls = append(*calls, provider+"|"+model+"|"+sonnet)
		return map[string]string{
			"PROVIDER": provider, "MODEL": model, "SONNET": sonnet,
		}
	}
}

func TestBuildPerMemberEnvOverrides_HappyPath(t *testing.T) {
	overrides := []spawnpayload.MemberOverride{
		{ID: "alpha", Provider: "anthropic", Model: "opus"},
		{ID: "beta", Provider: "zai-glm", Model: "glm-5"},
	}
	sonnetAlloc := map[string]string{"alpha": "sonnet-pinned-A", "beta": "sonnet-pinned-B"}
	var calls []string
	got := BuildPerMemberEnvOverrides(overrides, sonnetAlloc, recordingEnvBuilder(&calls))

	if len(got) != 2 {
		t.Fatalf("len=%d want 2", len(got))
	}
	if got["alpha"]["PROVIDER"] != "anthropic" || got["alpha"]["MODEL"] != "opus" {
		t.Errorf("alpha env wrong: %+v", got["alpha"])
	}
	if got["alpha"]["SONNET"] != "sonnet-pinned-A" {
		t.Errorf("alpha sonnet not threaded: %+v", got["alpha"])
	}
	if got["beta"]["SONNET"] != "sonnet-pinned-B" {
		t.Errorf("beta sonnet not threaded: %+v", got["beta"])
	}
	if len(calls) != 2 {
		t.Errorf("builder called %d times, want 2", len(calls))
	}
}

func TestBuildPerMemberEnvOverrides_NilSonnetAllocMeansBlankSonnet(t *testing.T) {
	// When Z.AI isn't in play, sonnetAlloc is nil and the loop must
	// pass "" to the env builder (not panic on map lookup).
	overrides := []spawnpayload.MemberOverride{
		{ID: "alpha", Provider: "anthropic", Model: "opus"},
	}
	var calls []string
	got := BuildPerMemberEnvOverrides(overrides, nil, recordingEnvBuilder(&calls))
	if got["alpha"]["SONNET"] != "" {
		t.Errorf("nil sonnetAlloc should yield empty SONNET, got %q", got["alpha"]["SONNET"])
	}
}

func TestBuildPerMemberEnvOverrides_MemberWithoutSonnetAllocGetsBlank(t *testing.T) {
	// Z.AI allocation is non-nil but doesn't include this member's ID.
	overrides := []spawnpayload.MemberOverride{
		{ID: "alpha"}, {ID: "beta"},
	}
	sonnetAlloc := map[string]string{"alpha": "pinned"}
	var calls []string
	got := BuildPerMemberEnvOverrides(overrides, sonnetAlloc, recordingEnvBuilder(&calls))
	if got["beta"]["SONNET"] != "" {
		t.Errorf("missing-from-alloc member should get empty SONNET, got %q", got["beta"]["SONNET"])
	}
}

func TestBuildPerMemberEnvOverrides_EmptyOverridesReturnsEmptyMap(t *testing.T) {
	got := BuildPerMemberEnvOverrides(nil, nil, func(string, string, string) map[string]string {
		t.Fatal("builder should not be called for empty overrides")
		return nil
	})
	if got == nil || len(got) != 0 {
		t.Errorf("expected empty map, got %+v", got)
	}
}

func TestPreassignHookSessionIDs_HappyPath(t *testing.T) {
	members := []teamtemplates.TeamTemplateMember{
		{ID: "alpha"}, {ID: "beta"}, {ID: "gamma"},
	}
	preassigned, hookInfo := PreassignHookSessionIDs(members)
	if len(preassigned) != 3 {
		t.Fatalf("preassigned len=%d want 3", len(preassigned))
	}
	if len(hookInfo) != 3 {
		t.Fatalf("hookInfo len=%d want 3", len(hookInfo))
	}
	// Each member has a UUID; values are non-empty + unique.
	seen := map[string]struct{}{}
	for _, id := range preassigned {
		if id == "" {
			t.Error("empty session ID in preassigned map")
		}
		if _, dup := seen[id]; dup {
			t.Errorf("duplicate session ID across members: %s", id)
		}
		seen[id] = struct{}{}
	}
}

func TestPreassignHookSessionIDs_ParallelViewsAgree(t *testing.T) {
	// preassigned map and hookInfo slice must describe the SAME
	// configID→sessionID mapping. The dispatcher uses one; the
	// worktree writer uses the other; divergence would mean Claude
	// reads a hook URL that points at a sessionID jarvis doesn't
	// know about.
	members := []teamtemplates.TeamTemplateMember{
		{ID: "alpha"}, {ID: "beta"},
	}
	preassigned, hookInfo := PreassignHookSessionIDs(members)
	for _, info := range hookInfo {
		if preassigned[info.ConfigID] != info.SessionID {
			t.Errorf("divergence on %s: preassigned=%s hookInfo=%s",
				info.ConfigID, preassigned[info.ConfigID], info.SessionID)
		}
	}
}

func TestPreassignHookSessionIDs_EmptyInputEmptyOutputs(t *testing.T) {
	preassigned, hookInfo := PreassignHookSessionIDs(nil)
	if preassigned == nil {
		t.Error("preassigned should be empty map, not nil")
	}
	if len(preassigned) != 0 {
		t.Errorf("preassigned len=%d want 0", len(preassigned))
	}
	if len(hookInfo) != 0 {
		t.Errorf("hookInfo len=%d want 0", len(hookInfo))
	}
}

func TestPreassignHookSessionIDs_HookInfoPreservesMemberOrder(t *testing.T) {
	// hookSpawnInfo is consumed in order by SetupWorktreesWithHooks
	// — the order it lays out worktree settings files. Must match
	// the input member order for predictable per-agent CWDs.
	members := []teamtemplates.TeamTemplateMember{
		{ID: "first"}, {ID: "second"}, {ID: "third"},
	}
	_, hookInfo := PreassignHookSessionIDs(members)
	for i, want := range []string{"first", "second", "third"} {
		if hookInfo[i].ConfigID != want {
			t.Errorf("hookInfo[%d].ConfigID=%q want %q", i, hookInfo[i].ConfigID, want)
		}
	}
}

func TestIndexMemberOverrides_HappyPath(t *testing.T) {
	overrides := []spawnpayload.MemberOverride{
		{ID: "alpha", Model: "glm-5.1"},
		{ID: "beta", Effort: "high"},
		{ID: "gamma"},
	}
	got := IndexMemberOverrides(overrides)
	if len(got) != 3 {
		t.Fatalf("len=%d want 3", len(got))
	}
	if got["alpha"].Model != "glm-5.1" {
		t.Errorf("alpha lookup mismatch: %+v", got["alpha"])
	}
	if got["beta"].Effort != "high" {
		t.Errorf("beta lookup mismatch: %+v", got["beta"])
	}
}

func TestIndexMemberOverrides_EmptyInputReturnsEmptyMap(t *testing.T) {
	// Documented invariant: returns empty map, not nil, so callers
	// can index unconditionally.
	got := IndexMemberOverrides(nil)
	if got == nil {
		t.Error("got nil, want empty map")
	}
	if len(got) != 0 {
		t.Errorf("len=%d want 0", len(got))
	}
}

func TestIndexMemberOverrides_DuplicateIDsKeepLastWriter(t *testing.T) {
	// Documented Go map behaviour: last writer wins. The spawn modal
	// shouldn't produce duplicates, but be explicit about the
	// resolution so a future bug in the spawn flow surfaces here.
	overrides := []spawnpayload.MemberOverride{
		{ID: "a", Model: "first"},
		{ID: "a", Model: "second"},
	}
	got := IndexMemberOverrides(overrides)
	if got["a"].Model != "second" {
		t.Errorf("duplicate ID resolution: got %q, expected last-writer", got["a"].Model)
	}
}

func TestResolvePerpetualMode_DefaultsToTrue(t *testing.T) {
	if !ResolvePerpetualMode(spawnpayload.SpawnTeamRequest{}) {
		t.Error("nil PerpetualMode should default to true")
	}
}

func TestResolvePerpetualMode_ExplicitOverridesHonored(t *testing.T) {
	on := true
	off := false
	if !ResolvePerpetualMode(spawnpayload.SpawnTeamRequest{PerpetualMode: &on}) {
		t.Error("explicit true should return true")
	}
	if ResolvePerpetualMode(spawnpayload.SpawnTeamRequest{PerpetualMode: &off}) {
		t.Error("explicit false should return false (one-shot team)")
	}
}

func TestCollectModelRequests_OrderedMemberIDsPlusModelMap(t *testing.T) {
	members := []teamtemplates.TeamTemplateMember{
		{ID: "alpha"}, {ID: "beta"}, {ID: "gamma"},
	}
	overrides := []spawnpayload.MemberOverride{
		{ID: "alpha", Model: "glm-5.1"},
		{ID: "gamma", Model: "glm-5"},
		// beta omitted intentionally.
	}
	ids, models := CollectModelRequests(members, overrides)
	if len(ids) != 3 {
		t.Fatalf("len(ids)=%d want 3", len(ids))
	}
	for i, want := range []string{"alpha", "beta", "gamma"} {
		if ids[i] != want {
			t.Errorf("ids[%d]=%q want %q", i, ids[i], want)
		}
	}
	if models["alpha"] != "glm-5.1" || models["gamma"] != "glm-5" {
		t.Errorf("model map mismatch: %+v", models)
	}
	if _, has := models["beta"]; has {
		t.Errorf("beta should be absent (no override Model), got %q", models["beta"])
	}
}

func TestCollectModelRequests_EmptyOverridesEmptyModelMap(t *testing.T) {
	members := []teamtemplates.TeamTemplateMember{{ID: "x"}}
	ids, models := CollectModelRequests(members, nil)
	if len(ids) != 1 || ids[0] != "x" {
		t.Errorf("ids=%v want [x]", ids)
	}
	if len(models) != 0 {
		t.Errorf("models should be empty, got %+v", models)
	}
}

func TestCollectModelRequests_BlankModelOverridesSkipped(t *testing.T) {
	// An override with Model="" should NOT land in the requested-models
	// map — the allocator interprets absent entries as "use default".
	members := []teamtemplates.TeamTemplateMember{{ID: "a"}}
	overrides := []spawnpayload.MemberOverride{{ID: "a", Model: ""}}
	_, models := CollectModelRequests(members, overrides)
	if _, has := models["a"]; has {
		t.Errorf("blank-Model override leaked into map: %+v", models)
	}
}

func TestBuildTeamMemberConfigs_PreservesFields(t *testing.T) {
	in := []teamtemplates.TeamTemplateMember{
		{
			ID: "m1", Name: "Mia", Role: "lead", Tier: "strategic",
			Color: "purple", Emoji: "🦉",
			Personality:  "calm",
			SystemPrompt: "you are the lead",
			Skills:       []string{"office-protocols", "implementation-plan"},
		},
		{
			ID: "m2", Name: "Bolt", Role: "backend", Tier: "worker",
			Color: "orange", Skills: []string{"go-concurrency"},
		},
	}
	got := BuildTeamMemberConfigs(in)
	if len(got) != 2 {
		t.Fatalf("len=%d want 2", len(got))
	}
	// First member fields.
	g := got[0]
	if g.ID != "m1" || g.Name != "Mia" || g.Role != "lead" || g.Tier != "strategic" ||
		g.Color != "purple" || g.Personality != "calm" ||
		g.SystemPrompt != "you are the lead" {
		t.Errorf("m1 field mismatch: %+v", g)
	}
	if len(g.Skills) != 2 || g.Skills[0] != "office-protocols" {
		t.Errorf("m1 Skills not preserved: %+v", g.Skills)
	}
}

func TestBuildTeamMemberConfigs_EmptyInputReturnsEmpty(t *testing.T) {
	got := BuildTeamMemberConfigs(nil)
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %+v", got)
	}
	got = BuildTeamMemberConfigs([]teamtemplates.TeamTemplateMember{})
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %+v", got)
	}
}

func TestBuildTeamMemberConfigs_ReturnsDistinctPointers(t *testing.T) {
	// Caller may mutate returned configs (e.g. ResumeClaudeSessionID
	// propagation). Each must be its own pointer.
	in := []teamtemplates.TeamTemplateMember{
		{ID: "a"}, {ID: "b"},
	}
	got := BuildTeamMemberConfigs(in)
	if got[0] == got[1] {
		t.Error("BuildTeamMemberConfigs returned aliased pointers")
	}
	got[0].Name = "mutated"
	if got[1].Name == "mutated" {
		t.Error("mutation aliased between members")
	}
}

func TestApplyMemberOverrides_NoOverridesNoOp(t *testing.T) {
	members := []teamtemplates.TeamTemplateMember{
		{ID: "m1", Name: "Mia", Role: "lead"},
	}
	ApplyMemberOverrides(members, nil)
	if members[0].Name != "Mia" || members[0].Role != "lead" {
		t.Errorf("nil overrides mutated: %+v", members[0])
	}
	ApplyMemberOverrides(members, []spawnpayload.MemberOverride{})
	if members[0].Name != "Mia" {
		t.Errorf("empty overrides mutated: %+v", members[0])
	}
}

func TestApplyMemberOverrides_NoMembersNoOp(t *testing.T) {
	// Should not panic / error on empty member list.
	ApplyMemberOverrides(nil, []spawnpayload.MemberOverride{{ID: "x", Name: "X"}})
}

func TestApplyMemberOverrides_OnlyNonBlankFieldsWin(t *testing.T) {
	members := []teamtemplates.TeamTemplateMember{
		{ID: "m1", Name: "Mia", Role: "lead", Tier: "strategic", Color: "purple", Emoji: "🦉"},
	}
	overrides := []spawnpayload.MemberOverride{
		// Blank fields should NOT clobber template values.
		{ID: "m1", Name: "", Role: "", Tier: "", Color: "", Emoji: ""},
	}
	ApplyMemberOverrides(members, overrides)
	if members[0].Name != "Mia" {
		t.Errorf("blank Name override clobbered template: %q", members[0].Name)
	}
	if members[0].Role != "lead" {
		t.Errorf("blank Role override clobbered template: %q", members[0].Role)
	}
}

func TestApplyMemberOverrides_AllFieldsOverlay(t *testing.T) {
	members := []teamtemplates.TeamTemplateMember{
		{ID: "m1", Name: "Mia", Role: "lead", Tier: "strategic", Color: "purple",
			Emoji: "🦉", Personality: "old", SystemPrompt: "old-sp",
			Skills: []string{"old1", "old2"}},
	}
	overrides := []spawnpayload.MemberOverride{
		{ID: "m1", Name: "Newname", Role: "newrole", Tier: "worker", Color: "blue",
			Emoji: "🌟", Personality: "new", SystemPrompt: "new-sp",
			Skills: []string{"newonly"}},
	}
	ApplyMemberOverrides(members, overrides)
	got := members[0]
	if got.Name != "Newname" || got.Role != "newrole" || got.Tier != "worker" ||
		got.Color != "blue" || got.Emoji != "🌟" || got.Personality != "new" ||
		got.SystemPrompt != "new-sp" {
		t.Errorf("overlay missed a field: %+v", got)
	}
	if len(got.Skills) != 1 || got.Skills[0] != "newonly" {
		t.Errorf("Skills not replaced: %+v", got.Skills)
	}
}

func TestApplyMemberOverrides_MembersWithoutOverridesUntouched(t *testing.T) {
	members := []teamtemplates.TeamTemplateMember{
		{ID: "m1", Name: "Mia"},
		{ID: "m2", Name: "Max"}, // no override
	}
	overrides := []spawnpayload.MemberOverride{
		{ID: "m1", Name: "MiaNew"},
	}
	ApplyMemberOverrides(members, overrides)
	if members[0].Name != "MiaNew" {
		t.Errorf("m1 not overridden: %q", members[0].Name)
	}
	if members[1].Name != "Max" {
		t.Errorf("m2 was modified despite having no override: %q", members[1].Name)
	}
}

func TestApplyMemberOverrides_EmptySkillsKeepsTemplate(t *testing.T) {
	// len(ov.Skills) == 0 should leave template Skills intact.
	members := []teamtemplates.TeamTemplateMember{
		{ID: "m1", Skills: []string{"template-skill"}},
	}
	overrides := []spawnpayload.MemberOverride{
		{ID: "m1", Skills: nil},
	}
	ApplyMemberOverrides(members, overrides)
	if len(members[0].Skills) != 1 || members[0].Skills[0] != "template-skill" {
		t.Errorf("nil Skills clobbered template: %+v", members[0].Skills)
	}
}

func TestResolveMemberList_OverrideRetentionDoesNotMutateTemplate(t *testing.T) {
	// The function shouldn't mutate the input template's Members
	// slice; tests would catch regressions where the helper reused
	// the slice header.
	template := &teamtemplates.TeamTemplate{
		ID: "alpha",
		Members: []teamtemplates.TeamTemplateMember{
			{ID: "m1"}, {ID: "m2"},
		},
	}
	req := spawnpayload.SpawnTeamRequest{
		TemplateID: "alpha",
		MemberOverrides: []spawnpayload.MemberOverride{
			{ID: "m1"}, // drops m2
		},
	}
	_, _ = ResolveMemberList(req, stubLookup(map[string]*teamtemplates.TeamTemplate{"alpha": template}))
	if len(template.Members) != 2 {
		t.Errorf("template.Members mutated; len=%d want 2", len(template.Members))
	}
	if template.Members[0].ID != "m1" || template.Members[1].ID != "m2" {
		t.Errorf("template member order mutated: %+v", template.Members)
	}
}
