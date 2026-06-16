package office

import (
	"context"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/ralph"
)

// stubGate is a fixed-result ObjectiveGate for tests.
type stubGate struct{ result ralph.GateResult }

func (g stubGate) Check(string) ralph.GateResult { return g.result }

func passGate() ralph.ObjectiveGate { return stubGate{ralph.GateResult{Pass: true, ExitCode: 0}} }
func failGate() ralph.ObjectiveGate {
	return stubGate{ralph.GateResult{Pass: false, ExitCode: 1, Command: "go test ./..."}}
}

// waitFor polls cond up to d, returning whether it became true. Used to await
// the async gated-auto-complete goroutine.
func waitFor(d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}

// TestLoopHardStop_PausesAfterIterationCap verifies replenishment stops once the
// iteration cap is reached, emitting office:loopMaxReached instead.
func TestLoopHardStop_PausesAfterIterationCap(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // escalation log writes under HOME
	cfg := idleTestConfig()
	cfg.RalphMaxLoopIterations = 2
	m, store, rec := newCtrlManager(t, cfg)
	m.SetPerpetualMode(true)
	m.RegisterAgent("sess-hs", "hs", &TeamMemberConfig{ID: "hs", Name: "Solo", Role: "Backend", Tier: "worker"})

	m.CheckForIdleAgents() // iteration 1 -> replenish
	m.CheckForIdleAgents() // iteration 2 -> replenish
	writesBefore := len(store.writesFor("sess-hs"))
	if writesBefore == 0 {
		t.Fatal("expected at least one replenishment write before the cap")
	}

	m.CheckForIdleAgents() // cap reached -> pause, no replenish
	if got := len(store.writesFor("sess-hs")); got != writesBefore {
		t.Errorf("expected no replenishment after iteration cap, writes went %d -> %d", writesBefore, got)
	}
	if !rec.has("office:loopMaxReached") {
		t.Error("expected office:loopMaxReached broadcast when the hard stop fires")
	}
}

// TestLoopHardStop_Unlimited verifies the default (cap 0) preserves perpetual
// behavior -- replenishment keeps firing.
func TestLoopHardStop_Unlimited(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // isolate loop-state writes from the real home
	cfg := idleTestConfig()
	cfg.RalphMaxLoopIterations = 0
	m, store, rec := newCtrlManager(t, cfg)
	m.SetPerpetualMode(true)
	m.RegisterAgent("sess-un", "un", &TeamMemberConfig{ID: "un", Name: "Endless", Role: "Backend", Tier: "worker"})

	for i := 0; i < 5; i++ {
		m.CheckForIdleAgents()
	}
	if len(store.writesFor("sess-un")) == 0 {
		t.Error("expected continuous replenishment when cap is 0")
	}
	if rec.has("office:loopMaxReached") {
		t.Error("did not expect a hard stop when cap is 0")
	}
}

// TestLoopState_PersistAndRestore verifies an agent's perpetual-loop iteration
// count survives across a (simulated) daemon restart.
func TestLoopState_PersistAndRestore(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := idleTestConfig()
	m, _, _ := newCtrlManager(t, cfg)
	m.SetPerpetualMode(true)
	m.RegisterAgent("sess-ps", "ps", &TeamMemberConfig{ID: "ps", Name: "Persist", Role: "Backend", Tier: "worker"})

	// Two iterations -> persisted loopIteration == 2. Keyed by ConfigID ("ps").
	m.CheckForIdleAgents()
	m.CheckForIdleAgents()
	if !waitFor(2*time.Second, func() bool {
		s, ok := loadLoopState("ps")
		return ok && s.LoopIteration == 2
	}) {
		s, _ := loadLoopState("ps")
		t.Fatalf("expected persisted loopIteration=2, got %+v", s)
	}

	// Simulate restart: a fresh agent reloads its prior loop budget.
	agent2 := m.GetAgent("sess-ps")
	agent2.LoopIterationCount = 0 // pretend cold start
	m.RestoreLoopState(agent2)
	agent2.mu.Lock()
	got := agent2.LoopIterationCount
	agent2.mu.Unlock()
	if got != 2 {
		t.Errorf("expected restored LoopIterationCount=2, got %d", got)
	}
}

// TestRegisterAgent_RestoresRecentLoopState verifies the production wiring: a
// recent persisted loop budget is resumed when the agent (re)registers, so the
// hard-stop survives a daemon restart instead of silently resetting.
func TestRegisterAgent_RestoresRecentLoopState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := saveLoopState("backend", RalphLoopState{
		ConfigID: "backend", LoopIteration: 7, StartedAt: time.Now(), LastRun: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	m, _, _ := newCtrlManager(t, DefaultConfig())
	m.RegisterAgent("sess-new", "backend", &TeamMemberConfig{ID: "backend", Name: "B", Role: "Backend", Tier: "worker"})
	agent := m.GetAgent("sess-new")
	agent.mu.Lock()
	got := agent.LoopIterationCount
	agent.mu.Unlock()
	if got != 7 {
		t.Errorf("expected restored LoopIterationCount=7 on register, got %d", got)
	}
}

// TestRegisterAgent_IgnoresStaleLoopState verifies the staleness guard: an old
// loop-state file (a different team that reused the ConfigID) is NOT resumed, so
// a brand-new agent doesn't inherit a stale/capped budget.
func TestRegisterAgent_IgnoresStaleLoopState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := saveLoopState("backend", RalphLoopState{
		ConfigID: "backend", LoopIteration: 9,
		StartedAt: time.Now().Add(-48 * time.Hour), LastRun: time.Now().Add(-48 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	m, _, _ := newCtrlManager(t, DefaultConfig())
	m.RegisterAgent("sess-fresh", "backend", &TeamMemberConfig{ID: "backend", Name: "B", Role: "Backend", Tier: "worker"})
	agent := m.GetAgent("sess-fresh")
	agent.mu.Lock()
	got := agent.LoopIterationCount
	agent.mu.Unlock()
	if got != 0 {
		t.Errorf("expected stale loop state ignored (count 0), got %d", got)
	}
}

// TestLoopStatePath_NoCollisionForSlugTwins verifies the collision-resistant
// hash suffix: two distinct ConfigIDs that slugify to the SAME string must NOT
// share a loop-state file (else one team inherits another's iteration budget).
func TestLoopStatePath_NoCollisionForSlugTwins(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// "backend:team-a" and "backend@team-a" both slugify to "backend_team-a".
	a, b := "backend:team-a", "backend@team-a"
	if slugifyKey(a) != slugifyKey(b) {
		t.Fatalf("test premise broken: slugs differ (%q vs %q)", slugifyKey(a), slugifyKey(b))
	}
	if loopStatePath(a) == loopStatePath(b) {
		t.Fatalf("distinct ConfigIDs collided on one loop-state file: %s", loopStatePath(a))
	}

	// Round-trip: each key reads back only its own state.
	if err := saveLoopState(a, RalphLoopState{ConfigID: a, LoopIteration: 3, LastRun: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := saveLoopState(b, RalphLoopState{ConfigID: b, LoopIteration: 8, LastRun: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if sa, ok := loadLoopState(a); !ok || sa.LoopIteration != 3 {
		t.Errorf("key a: expected iteration 3, got %+v (ok=%v)", sa, ok)
	}
	if sb, ok := loadLoopState(b); !ok || sb.LoopIteration != 8 {
		t.Errorf("key b: expected iteration 8, got %+v (ok=%v)", sb, ok)
	}
}

// TestLoopState_EmptyKeyGuard verifies an empty ConfigID never reads or writes a
// shared on-disk file (mirrors saveLoopState's existing empty-key no-op).
func TestLoopState_EmptyKeyGuard(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := saveLoopState("", RalphLoopState{LoopIteration: 5, LastRun: time.Now()}); err != nil {
		t.Fatalf("saveLoopState(\"\") should no-op, got %v", err)
	}
	if s, ok := loadLoopState(""); ok {
		t.Errorf("loadLoopState(\"\") must return ok=false, got %+v", s)
	}
}

func TestTaskCategoryBlocked(t *testing.T) {
	m, _, _ := newCtrlManager(t, DefaultConfig())
	blocked := []string{
		"Refactor authentication module",
		"Add payment retry logic",
		"Deploy to production",
		"Run schema migration for users",
	}
	for _, title := range blocked {
		if ok, _ := m.taskCategoryBlocked(title); !ok {
			t.Errorf("expected %q to be category-blocked", title)
		}
	}
	allowed := []string{
		"Fix typo in README",
		"Write tests for parser",
		"Extract helper function in utils.go",
	}
	for _, title := range allowed {
		if ok, cat := m.taskCategoryBlocked(title); ok {
			t.Errorf("expected %q to be allowed, but blocked by %q", title, cat)
		}
	}
}

func TestRecordAndLoadEscalations(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := RecordEscalation(EscalationRecord{
		SessionID: "s1", AgentName: "Ann", Reason: "recovery_exhausted", RecoveryAttempts: 3,
	}); err != nil {
		t.Fatalf("record 1: %v", err)
	}
	if err := RecordEscalation(EscalationRecord{
		SessionID: "s2", AgentName: "Bob", Reason: "blocked_max_triage", TaskIDs: []string{"t-9"},
	}); err != nil {
		t.Fatalf("record 2: %v", err)
	}
	got, err := LoadEscalations()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 escalations, got %d", len(got))
	}
	if got[0].Reason != "recovery_exhausted" || got[1].Reason != "blocked_max_triage" {
		t.Errorf("unexpected reasons: %q, %q", got[0].Reason, got[1].Reason)
	}
}

func TestLoadEscalations_MissingFileEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	got, err := LoadEscalations()
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %d", len(got))
	}
}

// TestAcceptedChangeRate_FromTaskTransitions verifies the loop-quality metric:
// one completed + one failed task yields a 0.5 accepted-change rate.
func TestAcceptedChangeRate_FromTaskTransitions(t *testing.T) {
	m, _, _ := newCtrlManager(t, DefaultConfig())
	m.RegisterAgent("sess-ar", "ar", &TeamMemberConfig{ID: "ar", Name: "Rate", Role: "Backend", Tier: "worker"})
	agent := m.GetAgent("sess-ar")
	agent.AddTask(&Task{ID: "ok", Title: "good", Status: TaskInProgress})
	agent.AddTask(&Task{ID: "bad", Title: "bad", Status: TaskInProgress})

	agent.UpdateTaskStatus("ok", TaskCompleted)
	agent.UpdateTaskStatus("bad", TaskFailed)

	snap := m.GetMetricsSnapshot()
	if snap.TotalTasksCompleted != 1 || snap.TotalTasksRejected != 1 {
		t.Fatalf("expected 1 completed / 1 rejected, got %d / %d", snap.TotalTasksCompleted, snap.TotalTasksRejected)
	}
	if snap.AcceptedChangeRate != 0.5 {
		t.Errorf("expected AcceptedChangeRate 0.5, got %v", snap.AcceptedChangeRate)
	}
}

// TestGatedAutoComplete_GateFailWithholdsCompletion verifies a failing verify
// gate prevents auto-completing the task and nudges the agent instead.
func TestGatedAutoComplete_GateFailWithholdsCompletion(t *testing.T) {
	m, store, _ := newCtrlManager(t, DefaultConfig())
	m.SetSpawnContext("tmpl", t.TempDir())
	m.gateFactory = failGate
	m.RegisterAgent("sess-gf", "gf", &TeamMemberConfig{ID: "gf", Name: "Gail", Role: "Backend", Tier: "worker"})
	agent := m.GetAgent("sess-gf")
	agent.AddTask(&Task{ID: "t-1", Title: "Wire handler", Status: TaskInProgress, StartedAt: time.Now()})

	m.gatedAutoComplete(agent, "sess-gf", "t-1", "Wire handler", "Gail", 6*time.Minute)

	// Wait for the async gate to resolve and the nudge to land.
	if !waitFor(2*time.Second, func() bool { return len(store.writesFor("sess-gf")) > 0 }) {
		t.Fatal("expected a verification-failed nudge write")
	}
	if got := agent.ActiveTask(); got == nil || got.ID != "t-1" {
		t.Error("task must remain active/in-progress after a gate failure (not auto-completed)")
	}
}

// TestGatedAutoComplete_IndependentEvaluatorRejects verifies the maker/checker
// split: even when the objective gate passes, an independent evaluator that
// rejects the acceptance criteria withholds completion.
func TestGatedAutoComplete_IndependentEvaluatorRejects(t *testing.T) {
	m, store, _ := newCtrlManager(t, DefaultConfig())
	m.SetSpawnContext("tmpl", t.TempDir())
	m.gateFactory = passGate // objective gate passes...
	// ...but the independent evaluator rejects.
	m.criteriaEvaluator = func(_ context.Context, _, _ string, _ []string) (CriteriaVerdict, error) {
		return CriteriaVerdict{AllPass: false, Results: []CriterionResult{{Criterion: "tests added", Pass: false, Reason: "no tests"}}}, nil
	}
	m.RegisterAgent("sess-ev", "ev", &TeamMemberConfig{ID: "ev", Name: "Eve", Role: "Backend", Tier: "worker"})
	agent := m.GetAgent("sess-ev")
	agent.AddTask(&Task{ID: "t-3", Title: "Add feature", Status: TaskInProgress,
		StartedAt: time.Now(), AcceptanceCriteria: []string{"tests added"}})

	m.gatedAutoComplete(agent, "sess-ev", "t-3", "Add feature", "Eve", 6*time.Minute)

	if !waitFor(2*time.Second, func() bool { return len(store.writesFor("sess-ev")) > 0 }) {
		t.Fatal("expected a criteria-not-met nudge")
	}
	if got := agent.ActiveTask(); got == nil || got.ID != "t-3" {
		t.Error("task must remain in progress when the independent evaluator rejects it")
	}
}

// TestGatedAutoComplete_EvaluatorFailOpen verifies an evaluator error does not
// wedge the loop -- the task still completes (gate passed).
func TestGatedAutoComplete_EvaluatorFailOpen(t *testing.T) {
	m, _, _ := newCtrlManager(t, DefaultConfig())
	m.SetSpawnContext("tmpl", t.TempDir())
	m.gateFactory = passGate
	m.criteriaEvaluator = func(_ context.Context, _, _ string, _ []string) (CriteriaVerdict, error) {
		return CriteriaVerdict{EvaluatorError: "evaluator down"}, nil // fail-open
	}
	m.RegisterAgent("sess-fo", "fo", &TeamMemberConfig{ID: "fo", Name: "Flo", Role: "Backend", Tier: "worker"})
	agent := m.GetAgent("sess-fo")
	agent.AddTask(&Task{ID: "t-4", Title: "Add feature", Status: TaskInProgress,
		StartedAt: time.Now(), AcceptanceCriteria: []string{"tests added"}})

	m.gatedAutoComplete(agent, "sess-fo", "t-4", "Add feature", "Flo", 6*time.Minute)

	completed := waitFor(2*time.Second, func() bool {
		for _, tk := range agent.GetTasks() {
			if tk.ID == "t-4" && tk.Status == TaskCompleted {
				return true
			}
		}
		return false
	})
	if !completed {
		t.Error("evaluator error must fail-open: task should still complete when the gate passed")
	}
}

// TestGatedAutoComplete_GatePassCompletes verifies a passing gate auto-completes
// the task.
func TestGatedAutoComplete_GatePassCompletes(t *testing.T) {
	m, _, _ := newCtrlManager(t, DefaultConfig())
	m.SetSpawnContext("tmpl", t.TempDir())
	m.gateFactory = passGate
	m.RegisterAgent("sess-gp", "gp", &TeamMemberConfig{ID: "gp", Name: "Gus", Role: "Backend", Tier: "worker"})
	agent := m.GetAgent("sess-gp")
	agent.AddTask(&Task{ID: "t-2", Title: "Add test", Status: TaskInProgress, StartedAt: time.Now()})

	m.gatedAutoComplete(agent, "sess-gp", "t-2", "Add test", "Gus", 6*time.Minute)

	completed := waitFor(2*time.Second, func() bool {
		for _, tk := range agent.GetTasks() {
			if tk.ID == "t-2" && tk.Status == TaskCompleted {
				return true
			}
		}
		return false
	})
	if !completed {
		t.Error("expected task to be auto-completed when the gate passes")
	}
}
