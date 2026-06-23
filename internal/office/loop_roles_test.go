package office

// Tests for the additive loop-engineering overlay (Planner / Maker / Verifier
// standing teammates). The whole feature is opt-in and default OFF, so these
// tests assert both that the toggles resolve correctly and that the routing /
// verification behavior fails open (a busy/dead Verifier never wedges the loop).
//
// Convention: plain `testing`, table loops, no testify. Reuses the shared
// helpers newCtrlManager, idleTestConfig, waitFor, boolPtr and the
// m.criteriaEvaluator stub seam.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/office/spawnpayload"
	"github.com/kshaham/jarvis/internal/teamtemplates"
)

// ---------------------------------------------------------------------------
// 1. Resolver toggle (ResolveLoopRolesMode) -- mirrors TestResolveSevenStepMode.
// ---------------------------------------------------------------------------

func TestResolveLoopRolesMode(t *testing.T) {
	// Default (unset) is OFF -- opt-in, like 7-step mode.
	if ResolveLoopRolesMode(spawnpayload.SpawnTeamRequest{}) {
		t.Error("expected loop-roles mode OFF when unset")
	}
	if !ResolveLoopRolesMode(spawnpayload.SpawnTeamRequest{LoopRoles: boolPtr(true)}) {
		t.Error("expected loop-roles mode ON when explicitly true")
	}
	if ResolveLoopRolesMode(spawnpayload.SpawnTeamRequest{LoopRoles: boolPtr(false)}) {
		t.Error("expected loop-roles mode OFF when explicitly false")
	}
}

// ---------------------------------------------------------------------------
// 2. Manager toggle -- mirrors TestSetIsSevenStepMode.
// ---------------------------------------------------------------------------

func TestSetIsLoopRolesMode(t *testing.T) {
	m, _, _ := newCtrlManager(t, DefaultConfig())
	if m.IsLoopRolesMode() {
		t.Fatal("expected loop-roles mode off by default")
	}
	m.SetLoopRolesMode(true)
	if !m.IsLoopRolesMode() {
		t.Error("expected loop-roles mode on after SetLoopRolesMode(true)")
	}
	// Surfaced in the status payload for the SPA.
	if v, ok := m.GetOfficeState()["loopRolesMode"].(bool); !ok || !v {
		t.Error("expected loopRolesMode=true in GetOfficeState payload")
	}
	m.SetLoopRolesMode(false)
	if m.IsLoopRolesMode() {
		t.Error("expected loop-roles mode off after SetLoopRolesMode(false)")
	}
}

// ---------------------------------------------------------------------------
// 3. Per-role MODEL resolution over ResolveMemberProviderModel.
// ---------------------------------------------------------------------------

func TestResolveMemberProviderModel_PerRole(t *testing.T) {
	cases := []struct {
		name      string
		member    teamtemplates.TeamTemplateMember
		provider  string // team-wide provider
		zaiSonnet string
		wantModel string
	}{
		{
			name:      "verifier on claude defaults to opus",
			member:    teamtemplates.TeamTemplateMember{LoopRole: LoopRoleVerifier},
			provider:  "claude-subscription",
			wantModel: "claude-opus-4-8",
		},
		{
			name:      "verifier on codex defaults to gpt-5.5",
			member:    teamtemplates.TeamTemplateMember{LoopRole: LoopRoleVerifier},
			provider:  "openai-codex",
			wantModel: "gpt-5.5",
		},
		{
			name:      "maker on claude defaults to sonnet",
			member:    teamtemplates.TeamTemplateMember{LoopRole: LoopRoleMaker},
			provider:  "claude-subscription",
			wantModel: "claude-sonnet-4-5",
		},
		{
			name:      "maker on codex defaults to gpt-5.4",
			member:    teamtemplates.TeamTemplateMember{LoopRole: LoopRoleMaker},
			provider:  "openai-codex",
			wantModel: "gpt-5.4",
		},
		{
			name:      "planner falls through to strategic opus default",
			member:    teamtemplates.TeamTemplateMember{LoopRole: LoopRolePlanner, Tier: "strategic"},
			provider:  "claude-subscription",
			wantModel: "claude-opus-4-8",
		},
		{
			name:      "explicit template model wins over verifier role default",
			member:    teamtemplates.TeamTemplateMember{LoopRole: LoopRoleVerifier, Model: "claude-haiku-4-5"},
			provider:  "claude-subscription",
			wantModel: "claude-haiku-4-5",
		},
		{
			name:      "zai guard intact: maker on zai-glm keeps the GLM model, not sonnet",
			member:    teamtemplates.TeamTemplateMember{LoopRole: LoopRoleMaker},
			provider:  "zai-glm",
			zaiSonnet: "glm-5.1",
			wantModel: "glm-5.1",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, gotModel := ResolveMemberProviderModel(tc.member, nil, tc.provider, tc.zaiSonnet)
			if gotModel != tc.wantModel {
				t.Errorf("model = %q, want %q", gotModel, tc.wantModel)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 4. Per-role EFFORT: DefaultEffortForRole table + ResolveSessionTuning floor.
// ---------------------------------------------------------------------------

func TestDefaultEffortForRole(t *testing.T) {
	cases := []struct {
		loopRole string
		tier     string
		want     string
	}{
		{LoopRoleVerifier, "strategic", "max"},
		{LoopRoleVerifier, "worker", "max"},
		{LoopRoleMaker, "worker", spawnpayload.DefaultEffortWorker},
		{LoopRolePlanner, "strategic", spawnpayload.DefaultEffortStrategic},
		// No loop role: fall through to the tier-based default.
		{"", "strategic", spawnpayload.DefaultEffortStrategic},
		{"", "worker", spawnpayload.DefaultEffortWorker},
	}
	for _, tc := range cases {
		if got := spawnpayload.DefaultEffortForRole(tc.loopRole, tc.tier); got != tc.want {
			t.Errorf("DefaultEffortForRole(%q, %q) = %q, want %q", tc.loopRole, tc.tier, got, tc.want)
		}
	}
}

// TestResolveSessionTuning_RoleEffortFloorIsOverridable verifies the role
// default acts only as a FLOOR: team-wide and per-member overrides still win
// over a Verifier's "max" floor.
func TestResolveSessionTuning_RoleEffortFloorIsOverridable(t *testing.T) {
	// Verifier floor with no overrides -> max.
	if got := spawnpayload.ResolveSessionTuning(spawnpayload.SpawnTeamRequest{}, LoopRoleVerifier, "strategic", nil); got.Effort != "max" {
		t.Errorf("verifier floor: Effort=%q want max", got.Effort)
	}
	// Team-wide DefaultEffort overrides the role floor.
	teamWide := spawnpayload.ResolveSessionTuning(
		spawnpayload.SpawnTeamRequest{DefaultEffort: "low"}, LoopRoleVerifier, "strategic", nil)
	if teamWide.Effort != "low" {
		t.Errorf("team-wide override: Effort=%q want low", teamWide.Effort)
	}
	// Per-member override wins over both the role floor and the team-wide value.
	perMember := spawnpayload.ResolveSessionTuning(
		spawnpayload.SpawnTeamRequest{DefaultEffort: "low"}, LoopRoleVerifier, "strategic",
		&spawnpayload.MemberOverride{Effort: "high"})
	if perMember.Effort != "high" {
		t.Errorf("per-member override: Effort=%q want high", perMember.Effort)
	}
}

// ---------------------------------------------------------------------------
// 5. Kickstart gating -- mirrors TestKickstart_SevenStepSectionGatedOnFlag.
// ---------------------------------------------------------------------------

func TestBuildKickstartPrompt_LoopRolePersonas(t *testing.T) {
	cases := []struct {
		loopRole string
		markers  []string
	}{
		{LoopRolePlanner, []string{"PLANNER", "DISCOVER", "cm task self-add", "--criterion"}},
		{LoopRoleMaker, []string{"MAKER", "EXECUTE", "one queued task"}},
		{LoopRoleVerifier, []string{"VERIFIER", "independent checker", "reject"}},
	}
	for _, tc := range cases {
		t.Run(tc.loopRole, func(t *testing.T) {
			cfg := &TeamMemberConfig{
				ID: "a", Name: "Ada", Role: "Backend", Tier: "worker",
				LoopRole:     tc.loopRole,
				SystemPrompt: "GENERIC-FALLBACK-PROMPT",
			}
			out := BuildKickstartPrompt(cfg, "/repo", nil, "ship it", "", true, nil, false)
			for _, marker := range tc.markers {
				if !strings.Contains(out, marker) {
					t.Errorf("loopRole %q: expected persona to mention %q", tc.loopRole, marker)
				}
			}
			// The per-role persona REPLACES the generic SystemPrompt.
			if strings.Contains(out, "GENERIC-FALLBACK-PROMPT") {
				t.Errorf("loopRole %q: generic SystemPrompt should be replaced by the persona", tc.loopRole)
			}
		})
	}

	// No loop role: the generic SystemPrompt is used unchanged, and no persona
	// markers leak in. This preserves existing behavior for non-loop teams.
	plain := &TeamMemberConfig{ID: "b", Name: "Bob", Role: "Backend", Tier: "worker", SystemPrompt: "GENERIC-FALLBACK-PROMPT"}
	out := BuildKickstartPrompt(plain, "/repo", nil, "ship it", "", true, nil, false)
	if !strings.Contains(out, "GENERIC-FALLBACK-PROMPT") {
		t.Error("expected generic SystemPrompt for a member with no loop role")
	}
	if strings.Contains(out, "You are the PLANNER") || strings.Contains(out, "You are the VERIFIER") {
		t.Error("did not expect any loop-role persona for a member with no loop role")
	}
}

// ---------------------------------------------------------------------------
// 6. Verifier-routing behavior (the heart): the criteriaEvaluator seam routes
// to the live Verifier, the verdict feeds the reject/escalation counters, and
// an unavailable Verifier FAILS OPEN (completion allowed).
// ---------------------------------------------------------------------------

// TestVerifierRouting_RejectFeedsCriteriaRejectCounter verifies that when the
// independent evaluator (the Verifier seam) rejects, gatedAutoComplete withholds
// completion and bumps the per-task CriteriaRejectCount that drives escalation.
func TestVerifierRouting_RejectFeedsCriteriaRejectCounter(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m, store, _ := newCtrlManager(t, idleTestConfig())
	m.SetSpawnContext("tmpl", t.TempDir())
	m.SetLoopRolesMode(true)
	m.gateFactory = passGate // deterministic gate passes...

	var calls int
	var mu sync.Mutex
	// ...but the live-Verifier seam rejects the acceptance criteria.
	m.criteriaEvaluator = func(_ context.Context, _, _ string, criteria []string) (CriteriaVerdict, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		return CriteriaVerdict{
			AllPass: false,
			Results: []CriterionResult{{Criterion: criteria[0], Pass: false, Reason: "not met"}},
		}, nil
	}

	m.RegisterAgent("sess-mk", "mk", &TeamMemberConfig{ID: "mk", Name: "Mack", Role: "Backend", Tier: "worker", LoopRole: LoopRoleMaker})
	maker := m.GetAgent("sess-mk")
	maker.AddTask(&Task{ID: "t-r", Title: "Add feature", Status: TaskInProgress,
		StartedAt: time.Now(), AcceptanceCriteria: []string{"tests added"}})

	m.gatedAutoComplete(maker, "sess-mk", "t-r", "Add feature", "Mack", 6*time.Minute)

	// The criteria-not-met nudge confirms the evaluator was consulted and rejected.
	if !waitFor(2*time.Second, func() bool {
		for _, w := range store.writesFor("sess-mk") {
			if strings.Contains(w.Input, "[CRITERIA NOT MET]") {
				return true
			}
		}
		return false
	}) {
		t.Fatal("expected a [CRITERIA NOT MET] nudge after the verifier rejected")
	}
	mu.Lock()
	gotCalls := calls
	mu.Unlock()
	if gotCalls == 0 {
		t.Error("expected the live-verifier criteria evaluator to be invoked")
	}
	// Task must remain in progress (not auto-completed) on a reject.
	if got := maker.ActiveTask(); got == nil || got.ID != "t-r" {
		t.Error("task must remain in progress when the verifier rejects it")
	}
	// The reject must feed the per-task counter that escalates at >=2.
	_, _, rejectCount, found := maker.TaskGateInfo("t-r")
	if !found || rejectCount < 1 {
		t.Errorf("expected CriteriaRejectCount >= 1 after a reject, got %d (found=%v)", rejectCount, found)
	}
}

// TestVerifierRouting_FailsOpenWhenNoVerifierLive verifies the load-bearing
// fail-open: with loop-roles mode ON but NO live Verifier, verifierEvaluate
// resolves to an EvaluatorError verdict (allow completion) rather than a hard
// reject or a wedged loop.
func TestVerifierRouting_FailsOpenWhenNoVerifierLive(t *testing.T) {
	m, _, _ := newCtrlManager(t, idleTestConfig())
	m.SetLoopRolesMode(true)
	// A Maker is live, but no Verifier.
	m.RegisterAgent("sess-mo", "mo", &TeamMemberConfig{ID: "mo", Name: "Mona", Role: "Backend", Tier: "worker", LoopRole: LoopRoleMaker})

	verdict, err := m.verifierEvaluate(context.Background(), "/some/worktree", "did the work", []string{"tests pass"})
	if err != nil {
		t.Fatalf("verifierEvaluate must never return a hard error, got %v", err)
	}
	if verdict.EvaluatorError == "" {
		t.Error("expected an EvaluatorError verdict (fail-open) when no Verifier is live")
	}
	// Fail-open means completion is allowed: AllPass must NOT be a hard false
	// that blocks. The gated-complete path treats EvaluatorError as fail-open.
	if verdict.AllPass {
		t.Error("a fail-open verdict should not report AllPass=true")
	}
}

// TestVerifierRouting_GatedAutoCompleteFailsOpenOnEvaluatorError verifies the
// end-to-end fail-open through gatedAutoComplete: an evaluator error (busy/dead
// Verifier) still completes the task because the deterministic gate passed.
func TestVerifierRouting_GatedAutoCompleteFailsOpenOnEvaluatorError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m, _, _ := newCtrlManager(t, idleTestConfig())
	m.SetSpawnContext("tmpl", t.TempDir())
	m.SetLoopRolesMode(true)
	m.gateFactory = passGate
	// Simulate a busy/dead Verifier: the seam returns EvaluatorError.
	m.criteriaEvaluator = func(_ context.Context, _, _ string, _ []string) (CriteriaVerdict, error) {
		return CriteriaVerdict{EvaluatorError: "verifier busy"}, nil
	}
	m.RegisterAgent("sess-fb", "fb", &TeamMemberConfig{ID: "fb", Name: "Fabi", Role: "Backend", Tier: "worker", LoopRole: LoopRoleMaker})
	maker := m.GetAgent("sess-fb")
	maker.AddTask(&Task{ID: "t-fo", Title: "Add feature", Status: TaskInProgress,
		StartedAt: time.Now(), AcceptanceCriteria: []string{"tests added"}})

	m.gatedAutoComplete(maker, "sess-fb", "t-fo", "Add feature", "Fabi", 6*time.Minute)

	completed := waitFor(2*time.Second, func() bool {
		for _, tk := range maker.GetTasks() {
			if tk.ID == "t-fo" && tk.Status == TaskCompleted {
				return true
			}
		}
		return false
	})
	if !completed {
		t.Error("evaluator error must fail-open: task should still complete when the gate passed")
	}
}

// TestEvaluateCriteria_PrefersInjectedEvaluator verifies the single routing
// point prefers m.criteriaEvaluator (the Verifier seam / test stub) and falls
// back to the package func only when the seam is nil.
func TestEvaluateCriteria_PrefersInjectedEvaluator(t *testing.T) {
	m, _, _ := newCtrlManager(t, idleTestConfig())
	called := false
	m.criteriaEvaluator = func(_ context.Context, _, _ string, _ []string) (CriteriaVerdict, error) {
		called = true
		return CriteriaVerdict{AllPass: true}, nil
	}
	verdict, err := m.EvaluateCriteria(context.Background(), "/wt", "summary", []string{"c1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected EvaluateCriteria to prefer the injected evaluator")
	}
	if !verdict.AllPass {
		t.Error("expected the injected evaluator's verdict to be returned")
	}
}

// ---------------------------------------------------------------------------
// 7. Restart-carry: LoopRole survives a restart/respawn plan.
// ---------------------------------------------------------------------------

func TestLoopRole_SurvivesRestartTemplate(t *testing.T) {
	m, _, _ := newCtrlManager(t, idleTestConfig())
	m.RegisterAgent("sess-v", "v", &TeamMemberConfig{
		ID: "v", Name: "Vera", Role: "Reviewer", Tier: "strategic", LoopRole: LoopRoleVerifier,
	})
	agent := m.GetAgent("sess-v")

	member, err := BuildRestartTemplateMember(agent)
	if err != nil {
		t.Fatalf("BuildRestartTemplateMember: %v", err)
	}
	if member.LoopRole != LoopRoleVerifier {
		t.Errorf("restart template dropped LoopRole: got %q want %q", member.LoopRole, LoopRoleVerifier)
	}
}

func TestLoopRole_SurvivesRespawnPlan(t *testing.T) {
	m, _, _ := newCtrlManager(t, idleTestConfig())
	rec := &spawnedMemberRecord{
		Config: &TeamMemberConfig{
			ID: "p", Name: "Pat", Role: "Planner", Tier: "strategic", LoopRole: LoopRolePlanner,
		},
		WorkingDir: "/tmp/work",
		Provider:   "claude-subscription",
	}
	plan := m.buildRespawnPlan(rec)
	if plan.Member.LoopRole != LoopRolePlanner {
		t.Errorf("respawn plan dropped LoopRole: got %q want %q", plan.Member.LoopRole, LoopRolePlanner)
	}
}

// ---------------------------------------------------------------------------
// 8. Snapshot round-trip for LoopRolesMode.
// ---------------------------------------------------------------------------

func TestLoopRolesMode_SnapshotRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Cleanup(func() { time.Sleep(60 * time.Millisecond) })

	m, _, _ := newCtrlManager(t, DefaultConfig())
	m.SetTeamSpawned(true)
	m.SetTeamMission("loop the loop")
	m.SetLoopRolesMode(true)
	m.RegisterAgent("sess-1", "cfg-1", &TeamMemberConfig{
		ID: "cfg-1", Name: "Pat", Role: "Planner", Tier: "strategic", LoopRole: LoopRolePlanner,
	})

	snap, err := m.CreateSnapshot("loop-snap", "/tmp/work", nil)
	if err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}
	if !snap.LoopRolesMode {
		t.Error("expected LoopRolesMode=true in the created snapshot")
	}

	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".jarvis", "office-memory", "snapshots", "loop-snap.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected snapshot file at %s, got err: %v", path, err)
	}

	loaded, err := LoadSnapshot("loop-snap")
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	if !loaded.LoopRolesMode {
		t.Error("expected LoopRolesMode=true preserved across the snapshot round-trip")
	}
	// The member's LoopRole must survive too.
	if len(loaded.Members) != 1 || loaded.Members[0].Config.LoopRole != LoopRolePlanner {
		t.Errorf("expected the member's LoopRole preserved, got %+v", loaded.Members)
	}
}

// TestLoopRolesMode_SnapshotDefaultsOff verifies the opt-in default: a team that
// never enabled loop-roles mode round-trips with the flag OFF.
func TestLoopRolesMode_SnapshotDefaultsOff(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Cleanup(func() { time.Sleep(60 * time.Millisecond) })

	m, _, _ := newCtrlManager(t, DefaultConfig())
	m.SetTeamSpawned(true)
	m.RegisterAgent("sess-x", "cfg-x", &TeamMemberConfig{ID: "cfg-x", Name: "X", Tier: "worker"})

	snap, err := m.CreateSnapshot("off-snap", "/tmp/work", nil)
	if err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}
	if snap.LoopRolesMode {
		t.Error("expected LoopRolesMode=false by default (opt-in)")
	}
	loaded, err := LoadSnapshot("off-snap")
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	if loaded.LoopRolesMode {
		t.Error("expected LoopRolesMode=false preserved by default")
	}
}

// TestFindStrategicLeaderForTriage_ExcludesLoopRoles verifies that triage /
// ask-for-help routing skips standing Planner/Verifier teammates in loop-roles
// mode (so they stay on their discipline), still returns a non-loop strategic
// lead when one exists, and is byte-for-byte findStrategicAgent when OFF.
func TestFindStrategicLeaderForTriage_ExcludesLoopRoles(t *testing.T) {
	// Loop-roles ON, the only strategic agents are a Planner + Verifier: no
	// eligible leader -> nil (callers skip).
	t.Run("only loop-role strategic agents -> nil", func(t *testing.T) {
		m, _, _ := newCtrlManager(t, idleTestConfig())
		m.SetLoopRolesMode(true)
		m.RegisterAgent("sess-p", "p", &TeamMemberConfig{ID: "p", Name: "Pat", Tier: "strategic", LoopRole: LoopRolePlanner})
		m.RegisterAgent("sess-v", "v", &TeamMemberConfig{ID: "v", Name: "Vera", Tier: "strategic", LoopRole: LoopRoleVerifier})
		if got := m.findStrategicLeaderForTriage("sess-other"); got != nil {
			t.Errorf("expected nil (no eligible leader), got %q", got.Config.Name)
		}
	})

	// Loop-roles ON with a non-loop strategic lead present: that lead is eligible.
	t.Run("non-loop strategic lead is eligible", func(t *testing.T) {
		m, _, _ := newCtrlManager(t, idleTestConfig())
		m.SetLoopRolesMode(true)
		m.RegisterAgent("sess-p", "p", &TeamMemberConfig{ID: "p", Name: "Pat", Tier: "strategic", LoopRole: LoopRolePlanner})
		m.RegisterAgent("sess-l", "l", &TeamMemberConfig{ID: "l", Name: "Lee", Tier: "strategic"})
		got := m.findStrategicLeaderForTriage("sess-other")
		if got == nil || got.Config.Name != "Lee" {
			t.Errorf("expected the non-loop strategic lead Lee, got %v", got)
		}
	})

	// Loop-roles OFF: behaves like findStrategicAgent (a Planner is just a
	// strategic agent and remains selectable).
	t.Run("off mode ignores loop role", func(t *testing.T) {
		m, _, _ := newCtrlManager(t, idleTestConfig())
		m.RegisterAgent("sess-p", "p", &TeamMemberConfig{ID: "p", Name: "Pat", Tier: "strategic", LoopRole: LoopRolePlanner})
		if got := m.findStrategicLeaderForTriage("sess-other"); got == nil {
			t.Error("expected the strategic agent to be selectable when loop-roles mode is OFF")
		}
	})
}
