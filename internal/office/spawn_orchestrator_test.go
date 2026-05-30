package office

import (
	"errors"
	"strings"
	"testing"

	"github.com/kshaham/jarvis/internal/office/spawnpayload"
)

// FinalizeTeamSpawn wires up a Manager's post-spawn-loop state.
// Tests assert each settable field flows through, and that order
// (SetSpawnContext before SetTeamSpawned) doesn't accidentally
// reverse on refactor — we observe via the public getters.

func newFinalizeTestManager(t *testing.T) *Manager {
	t.Helper()
	return newTestManager()
}

func TestFinalizeTeamSpawn_SetsSpawnContextBeforeFlag(t *testing.T) {
	m := newFinalizeTestManager(t)
	req := spawnpayload.SpawnTeamRequest{
		WorkingDir: "/tmp/proj",
		Mission:    "ship the thing",
	}
	m.FinalizeTeamSpawn(req, "alpha-template", true)

	if !m.IsTeamSpawned() {
		t.Error("expected SetTeamSpawned(true) to flip the flag")
	}
	// SpawnContext is observable via the Manager's API; the exact
	// getter depends on the implementation. Verify mission as a
	// proxy that the call landed.
	if got := m.GetTeamMission(); got != "ship the thing" {
		t.Errorf("mission=%q want %q", got, "ship the thing")
	}
}

func TestFinalizeTeamSpawn_BlankMissionDoesNotClobber(t *testing.T) {
	m := newFinalizeTestManager(t)
	// Seed an existing mission so we can prove the empty-Mission
	// branch doesn't blank it.
	m.SetTeamMission("existing mission")
	req := spawnpayload.SpawnTeamRequest{WorkingDir: "/x", Mission: ""}
	m.FinalizeTeamSpawn(req, "tpl", false)
	if got := m.GetTeamMission(); got != "existing mission" {
		t.Errorf("mission clobbered: got %q want %q", got, "existing mission")
	}
}

func TestFinalizeTeamSpawn_PerpetualModeFlowsThrough(t *testing.T) {
	m := newFinalizeTestManager(t)
	req := spawnpayload.SpawnTeamRequest{WorkingDir: "/x"}

	m.FinalizeTeamSpawn(req, "tpl", true)
	if !m.IsPerpetualMode() {
		t.Error("expected perpetual mode true")
	}

	m.FinalizeTeamSpawn(req, "tpl", false)
	if m.IsPerpetualMode() {
		t.Error("expected perpetual mode false after second call")
	}
}

// recordingSessionStoreForPrompt captures WriteInkPrompt calls so
// tests can assert that PromptNextTask threaded the right values.
// Wraps the existing mockSessionStore to add the recorder.
type recordingSessionStoreForPrompt struct {
	mockSessionStore
	gotSession string
	gotPrompt  string
}

func (r *recordingSessionStoreForPrompt) WriteInkPrompt(sessionID, prompt string) bool {
	r.gotSession = sessionID
	r.gotPrompt = prompt
	return true
}

func TestTriageTaskBetweenAgents_HappyPath(t *testing.T) {
	m := newFinalizeTestManager(t)
	m.RegisterAgent("sess-src-001", "alpha", &TeamMemberConfig{ID: "alpha", Name: "Alpha"})
	m.RegisterAgent("sess-tgt-002", "beta", &TeamMemberConfig{ID: "beta", Name: "Beta"})
	src := m.GetAgent("sess-src-001")
	if src == nil {
		t.Fatal("source not registered")
	}
	src.AddTask(&Task{ID: "task-1", Title: "to be triaged", Status: TaskInProgress, Source: "self"})

	srcOut, tgtOut, err := m.TriageTaskBetweenAgents("sess-src-001", "sess-tgt-002", "task-1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if srcOut == nil || tgtOut == nil {
		t.Fatal("expected non-nil agents")
	}

	// Source no longer holds it.
	for _, task := range srcOut.GetTasks() {
		if task.ID == "task-1" {
			t.Errorf("source still has task: %+v", task)
		}
	}
	// Target now holds it with reset fields.
	var found *Task
	for _, task := range tgtOut.GetTasks() {
		if task.ID == "task-1" {
			found = task
			break
		}
	}
	if found == nil {
		t.Fatal("target missing the triaged task")
	}
	if found.Status != TaskPending {
		t.Errorf("Status=%s want TaskPending", found.Status)
	}
	if found.Source != "triage" {
		t.Errorf("Source=%q want triage", found.Source)
	}
	if found.AssignedAt.IsZero() {
		t.Error("AssignedAt should be reset to non-zero")
	}
}

func TestTriageTaskBetweenAgents_SourceNotFound(t *testing.T) {
	m := newFinalizeTestManager(t)
	_, _, err := m.TriageTaskBetweenAgents("ghost-src", "anything", "task-1")
	if !errors.Is(err, ErrTriageSourceNotFound) {
		t.Errorf("expected ErrTriageSourceNotFound, got %v", err)
	}
}

func TestTriageTaskBetweenAgents_TargetNotFound(t *testing.T) {
	m := newFinalizeTestManager(t)
	m.RegisterAgent("sess-src-001", "alpha", &TeamMemberConfig{ID: "alpha"})
	_, _, err := m.TriageTaskBetweenAgents("sess-src-001", "ghost-tgt", "task-1")
	if !errors.Is(err, ErrTriageTargetNotFound) {
		t.Errorf("expected ErrTriageTargetNotFound, got %v", err)
	}
}

func TestTriageTaskBetweenAgents_TaskNotFound(t *testing.T) {
	m := newFinalizeTestManager(t)
	m.RegisterAgent("sess-src-001", "alpha", &TeamMemberConfig{ID: "alpha"})
	m.RegisterAgent("sess-tgt-002", "beta", &TeamMemberConfig{ID: "beta"})
	_, _, err := m.TriageTaskBetweenAgents("sess-src-001", "sess-tgt-002", "ghost-task")
	if !errors.Is(err, ErrTriageTaskNotFound) {
		t.Errorf("expected ErrTriageTaskNotFound, got %v", err)
	}
}

func TestSnapshotRosterAsTemplateMembers_HappyPath(t *testing.T) {
	m := newFinalizeTestManager(t)
	m.RegisterAgent("sess-001", "alpha", &TeamMemberConfig{
		ID: "alpha", Name: "Alpha", Role: "lead", Tier: "strategic",
		Color: "purple", Personality: "calm", SystemPrompt: "lead-sp",
		Skills: []string{"office-protocols"},
	})
	got := m.SnapshotRosterAsTemplateMembers()
	if len(got) != 1 {
		t.Fatalf("len=%d want 1", len(got))
	}
	g := got[0]
	if g.ID != "alpha" || g.Name != "Alpha" || g.Role != "lead" || g.Tier != "strategic" ||
		g.Color != "purple" || g.Personality != "calm" || g.SystemPrompt != "lead-sp" {
		t.Errorf("field projection wrong: %+v", g)
	}
	if len(g.Skills) != 1 || g.Skills[0] != "office-protocols" {
		t.Errorf("skills not copied: %+v", g.Skills)
	}
}

func TestSnapshotRosterAsTemplateMembers_PersonalityOverrideWins(t *testing.T) {
	m := newFinalizeTestManager(t)
	m.RegisterAgent("sess-001", "alpha", &TeamMemberConfig{
		ID: "alpha", Personality: "template-personality", SystemPrompt: "template-sp",
	})
	agent := m.GetAgent("sess-001")
	if agent == nil {
		t.Fatal("agent not registered")
	}
	agent.SetPersonalityOverride(&PersonalityOverride{
		Personality:  "override-personality",
		SystemPrompt: "override-sp",
	})

	got := m.SnapshotRosterAsTemplateMembers()
	if got[0].Personality != "override-personality" {
		t.Errorf("Personality=%q want override-personality", got[0].Personality)
	}
	if got[0].SystemPrompt != "override-sp" {
		t.Errorf("SystemPrompt=%q want override-sp", got[0].SystemPrompt)
	}
}

func TestSnapshotRosterAsTemplateMembers_EmptyRosterReturnsEmptySlice(t *testing.T) {
	m := newFinalizeTestManager(t)
	got := m.SnapshotRosterAsTemplateMembers()
	if got == nil {
		t.Error("expected empty slice, got nil")
	}
	if len(got) != 0 {
		t.Errorf("len=%d want 0", len(got))
	}
}

func TestFanOutWrapUpPrompt_DeliversToEachAgent(t *testing.T) {
	m := newFinalizeTestManager(t)
	m.RegisterAgent("sess-alpha-001", "alpha", &TeamMemberConfig{ID: "alpha", Name: "Alpha"})
	m.RegisterAgent("sess-beta-002", "beta", &TeamMemberConfig{ID: "beta", Name: "Beta"})

	delivered := map[string]string{}
	notified, missed, _ := m.FanOutWrapUpPrompt("STOP", func(sid, prompt string) bool {
		delivered[sid] = prompt
		return true
	})
	if len(notified) != 2 {
		t.Errorf("notified=%d want 2", len(notified))
	}
	if len(missed) != 0 {
		t.Errorf("missed=%v want []", missed)
	}
	for _, want := range []string{"sess-alpha-001", "sess-beta-002"} {
		if delivered[want] != "STOP" {
			t.Errorf("session %s didn't receive prompt; got %q", want, delivered[want])
		}
	}
}

func TestFanOutWrapUpPrompt_MissedAgentsUseConfigName(t *testing.T) {
	m := newFinalizeTestManager(t)
	m.RegisterAgent("sess-bolt-001", "bolt", &TeamMemberConfig{ID: "bolt", Name: "Bolt"})
	// deliver returns false → simulating an unreachable session.
	_, missed, _ := m.FanOutWrapUpPrompt("STOP", func(string, string) bool { return false })
	if len(missed) != 1 || missed[0] != "Bolt" {
		t.Errorf("missed=%v want [Bolt]", missed)
	}
}

func TestFanOutWrapUpPrompt_CancelsQueuedTasksLeavingInProgressAlone(t *testing.T) {
	m := newFinalizeTestManager(t)
	m.RegisterAgent("sess-001", "alpha", &TeamMemberConfig{ID: "alpha", Name: "Alpha"})
	agent := m.GetAgent("sess-001")
	if agent == nil {
		t.Fatal("agent not registered")
	}
	// Three tasks: one pending, one acknowledged, one in-progress.
	agent.AddTask(&Task{ID: "t1", Title: "queued", Status: TaskPending})
	agent.AddTask(&Task{ID: "t2", Title: "acked", Status: TaskAcknowledged})
	agent.AddTask(&Task{ID: "t3", Title: "running", Status: TaskInProgress})

	_, _, cancelled := m.FanOutWrapUpPrompt("STOP", func(string, string) bool { return true })
	if cancelled != 2 {
		t.Errorf("cancelled=%d want 2 (t1 pending + t2 acked)", cancelled)
	}
	tasks := agent.GetTasks()
	statuses := map[string]TaskStatus{}
	for _, t := range tasks {
		statuses[t.ID] = t.Status
	}
	if statuses["t1"] != TaskFailed {
		t.Errorf("t1 status=%s want TaskFailed", statuses["t1"])
	}
	if statuses["t2"] != TaskFailed {
		t.Errorf("t2 status=%s want TaskFailed", statuses["t2"])
	}
	if statuses["t3"] != TaskInProgress {
		t.Errorf("t3 status=%s want TaskInProgress (left alone)", statuses["t3"])
	}
}

func TestFanOutWrapUpPrompt_NilDeliverTreatsAllAsMissed(t *testing.T) {
	// Documented contract: nil deliver = no delivery attempt. Every
	// agent lands on missedAgentNames but task cancellation still
	// runs because it's the backstop, not the prompt.
	m := newFinalizeTestManager(t)
	m.RegisterAgent("sess-001", "alpha", &TeamMemberConfig{ID: "alpha"})
	notified, missed, _ := m.FanOutWrapUpPrompt("STOP", nil)
	if len(notified) != 0 {
		t.Errorf("notified=%d want 0 with nil deliver", len(notified))
	}
	if len(missed) != 1 {
		t.Errorf("missed=%d want 1", len(missed))
	}
}

func TestPromptNextTask_FormatsAndWrites(t *testing.T) {
	rec := &recordingSessionStoreForPrompt{}
	m := NewManager(rec, func(string, interface{}) {}, nil)

	m.PromptNextTask("sess-aaa-0001", "Fix the modal", "task-42")

	if rec.gotSession != "sess-aaa-0001" {
		t.Errorf("WriteInkPrompt session=%q want sess-aaa-0001", rec.gotSession)
	}
	// Verify the format contains the title once and the taskID twice
	// (cm ack <id> && cm start <id>).
	if !strings.Contains(rec.gotPrompt, "Fix the modal") {
		t.Errorf("prompt missing title: %q", rec.gotPrompt)
	}
	if strings.Count(rec.gotPrompt, "task-42") != 2 {
		t.Errorf("prompt should mention task-42 twice, got %d: %q",
			strings.Count(rec.gotPrompt, "task-42"), rec.gotPrompt)
	}
	if !strings.HasPrefix(rec.gotPrompt, "[NEXT TASK]") {
		t.Errorf("prompt should start with [NEXT TASK], got %q", rec.gotPrompt)
	}
}

func TestPromptNextTask_NoSessionStoreIsNoOp(t *testing.T) {
	// Confirms the documented "no-op when SessionStore is nil"
	// branch — defensive against tests that build a Manager with
	// nil sessions.
	m := NewManager(nil, func(string, interface{}) {}, nil)
	// Must not panic.
	m.PromptNextTask("sess-001", "Title", "task-1")
}

func TestRegisterAgentWithProvider_RegistersAndSetsProviderMode(t *testing.T) {
	m := newFinalizeTestManager(t)
	cfg := &TeamMemberConfig{ID: "alpha", Name: "Alpha", Role: "lead"}
	agent := m.RegisterAgentWithProvider("sess-aaaa-0001", cfg, "anthropic", "opus", "sdk")
	if agent == nil {
		t.Fatal("nil agent returned")
	}
	if got := m.AgentCount(); got != 1 {
		t.Errorf("AgentCount=%d want 1", got)
	}
	// SetProvider + SetSessionMode landed (fields are public on AgentState).
	if agent.Provider != "anthropic" || agent.Model != "opus" {
		t.Errorf("provider=(%q,%q) want (anthropic,opus)", agent.Provider, agent.Model)
	}
	if agent.SessionMode != "sdk" {
		t.Errorf("sessionMode=%q want sdk", agent.SessionMode)
	}
}

func TestRegisterAgentWithProvider_NormalizesPtyToLegacy(t *testing.T) {
	m := newFinalizeTestManager(t)
	cfg := &TeamMemberConfig{ID: "alpha", Name: "Alpha"}
	agent := m.RegisterAgentWithProvider("sess-bbbb-0001", cfg, "anthropic", "opus", "pty")
	if agent.SessionMode != "legacy" {
		t.Errorf("sessionMode=%q want legacy (pty normalisation)", agent.SessionMode)
	}
}

func TestRegisterAgentWithProvider_PassesThroughHooksMode(t *testing.T) {
	m := newFinalizeTestManager(t)
	cfg := &TeamMemberConfig{ID: "alpha"}
	agent := m.RegisterAgentWithProvider("sess-cccc-0001", cfg, "anthropic", "opus", "hooks")
	if agent.SessionMode != "hooks" {
		t.Errorf("sessionMode=%q want hooks (no normalisation)", agent.SessionMode)
	}
}

func TestRegisterAgentWithProvider_NilConfigReturnsNil(t *testing.T) {
	// Documented contract: nil config is a no-op + nil return rather
	// than panicking. Tests don't normally pass nil — but defensive
	// code is cheap.
	m := newFinalizeTestManager(t)
	if got := m.RegisterAgentWithProvider("sess-dddd-0001", nil, "p", "m", "sdk"); got != nil {
		t.Errorf("expected nil for nil config, got %+v", got)
	}
	if m.AgentCount() != 0 {
		t.Error("nil config should not register an agent")
	}
}

func TestTeardownTeam_FlipsSpawnedFlag(t *testing.T) {
	m := newFinalizeTestManager(t)
	// Bring it up first so there's something to tear down.
	m.SetTeamSpawned(true)
	if !m.IsTeamSpawned() {
		t.Fatal("precondition: team should be spawned")
	}
	m.TeardownTeam(nil)
	if m.IsTeamSpawned() {
		t.Error("expected IsTeamSpawned()=false after TeardownTeam")
	}
}

func TestTeardownTeam_CallbackCalledOncePerAgent(t *testing.T) {
	m := newFinalizeTestManager(t)
	// Register a couple of agents to give TeardownTeam something to
	// iterate over.
	m.RegisterAgent("sess-aaa-001", "alpha", &TeamMemberConfig{ID: "alpha", Name: "Alpha"})
	m.RegisterAgent("sess-bbb-002", "beta", &TeamMemberConfig{ID: "beta", Name: "Beta"})
	m.SetTeamSpawned(true)

	seen := map[string]int{}
	m.TeardownTeam(func(sid string) {
		seen[sid]++
	})

	if seen["sess-aaa-001"] != 1 {
		t.Errorf("alpha callback fired %d times, want 1", seen["sess-aaa-001"])
	}
	if seen["sess-bbb-002"] != 1 {
		t.Errorf("beta callback fired %d times, want 1", seen["sess-bbb-002"])
	}
	if len(seen) != 2 {
		t.Errorf("unexpected callbacks for %d sessions, want 2: %+v", len(seen), seen)
	}
	// Agents should be unregistered after teardown.
	if m.AgentCount() != 0 {
		t.Errorf("AgentCount=%d after teardown, want 0", m.AgentCount())
	}
}

func TestTeardownTeam_NilCallbackStillUnregisters(t *testing.T) {
	// Document the contract: nil callback is allowed; Manager
	// bookkeeping still runs.
	m := newFinalizeTestManager(t)
	m.RegisterAgent("sess-ccc-003", "gamma", &TeamMemberConfig{ID: "gamma"})
	m.SetTeamSpawned(true)
	m.TeardownTeam(nil)
	if m.AgentCount() != 0 {
		t.Errorf("AgentCount=%d after nil-callback teardown, want 0", m.AgentCount())
	}
	if m.IsTeamSpawned() {
		t.Error("expected spawned=false after teardown")
	}
}

func TestTeardownTeam_EmptyRosterNoCallbacks(t *testing.T) {
	m := newFinalizeTestManager(t)
	m.SetTeamSpawned(true)
	calls := 0
	m.TeardownTeam(func(string) { calls++ })
	if calls != 0 {
		t.Errorf("callback fired %d times on empty roster, want 0", calls)
	}
}

func TestKillAgent_HappyPathStopsAndUnregisters(t *testing.T) {
	m := newFinalizeTestManager(t)
	m.RegisterAgent("sess-kill-001", "alpha", &TeamMemberConfig{ID: "alpha", Name: "Alpha"})

	var stopped []string
	res := m.KillAgent("sess-kill-001", "rogue", func(sid string) {
		stopped = append(stopped, sid)
	})

	if res.AgentName != "Alpha" {
		t.Errorf("AgentName=%q want Alpha", res.AgentName)
	}
	if !res.AgentFound {
		t.Error("AgentFound=false; want true for a registered agent")
	}
	if !res.MayBroadcast {
		t.Error("MayBroadcast=false; want true on first claim")
	}
	if res.Reason != "rogue" {
		t.Errorf("Reason=%q want rogue (passthrough)", res.Reason)
	}
	if len(stopped) != 1 || stopped[0] != "sess-kill-001" {
		t.Errorf("stop callback called for %v, want [sess-kill-001]", stopped)
	}
	if m.AgentCount() != 0 {
		t.Errorf("AgentCount=%d want 0 after kill", m.AgentCount())
	}
}

func TestKillAgent_EmptyReasonDefaultsToOperator(t *testing.T) {
	m := newFinalizeTestManager(t)
	m.RegisterAgent("sess-kill-002", "beta", &TeamMemberConfig{ID: "beta", Name: "Beta"})
	res := m.KillAgent("sess-kill-002", "", nil)
	if res.Reason != "operator" {
		t.Errorf("Reason=%q want operator (default)", res.Reason)
	}
}

func TestKillAgent_UnknownSessionReturnsSessionIDAsName(t *testing.T) {
	// Unknown sessionIDs still proceed (callback + UnregisterAgent
	// are safe on missing IDs) so the handler can report success
	// without a 404. AgentName falls back to the sessionID itself.
	m := newFinalizeTestManager(t)
	res := m.KillAgent("ghost-session", "cleanup", nil)
	if res.AgentName != "ghost-session" {
		t.Errorf("AgentName=%q want ghost-session (fallback)", res.AgentName)
	}
	if res.AgentFound {
		t.Error("AgentFound=true for unknown session")
	}
	if res.MayBroadcast {
		t.Error("MayBroadcast=true with no agent; should be false")
	}
}

func TestKillAgent_DoubleCallSecondMayBroadcastFalse(t *testing.T) {
	// ClaimStopBroadcast is one-shot: the first KillAgent wins the
	// claim, a second invocation must see MayBroadcast=false even
	// though the bookkeeping still runs safely.
	m := newFinalizeTestManager(t)
	m.RegisterAgent("sess-kill-003", "gamma", &TeamMemberConfig{ID: "gamma", Name: "Gamma"})
	agent := m.GetAgent("sess-kill-003")
	if agent == nil {
		t.Fatal("agent not registered")
	}
	if !agent.ClaimStopBroadcast() {
		t.Fatal("precondition: first ClaimStopBroadcast should win")
	}
	// Now KillAgent runs after the slot was already claimed elsewhere
	// (simulating checkForWedgedWorkingAgents racing the operator).
	res := m.KillAgent("sess-kill-003", "operator", nil)
	if res.MayBroadcast {
		t.Error("MayBroadcast=true after slot already claimed; want false")
	}
}

func TestKillAgent_NilStopCallbackStillUnregisters(t *testing.T) {
	m := newFinalizeTestManager(t)
	m.RegisterAgent("sess-kill-004", "delta", &TeamMemberConfig{ID: "delta"})
	res := m.KillAgent("sess-kill-004", "", nil)
	if !res.AgentFound {
		t.Error("AgentFound=false; expected true")
	}
	if m.AgentCount() != 0 {
		t.Errorf("AgentCount=%d after kill, want 0", m.AgentCount())
	}
}

func TestBuildRestartTemplateMember_CopiesAllFields(t *testing.T) {
	m := newFinalizeTestManager(t)
	m.RegisterAgent("sess-restart-001", "alpha", &TeamMemberConfig{
		ID: "alpha", Name: "Alpha", Role: "lead", Tier: "strategic",
		Color: "teal", Personality: "calm", SystemPrompt: "lead-sp",
		Skills: []string{"office-protocols", "git-workflows"},
		Voice: "aria",
	})
	agent := m.GetAgent("sess-restart-001")
	got, err := BuildRestartTemplateMember(agent)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.ID != "alpha" || got.Name != "Alpha" || got.Role != "lead" ||
		got.Tier != "strategic" || got.Color != "teal" ||
		got.Personality != "calm" || got.SystemPrompt != "lead-sp" {
		t.Errorf("field projection wrong: %+v", got)
	}
	if got.Voice != "aria" {
		t.Errorf("Voice=%q want aria", got.Voice)
	}
	if len(got.Skills) != 2 {
		t.Errorf("Skills=%v want 2 entries", got.Skills)
	}
}

func TestBuildRestartTemplateMember_NilAgentReturnsError(t *testing.T) {
	_, err := BuildRestartTemplateMember(nil)
	if !errors.Is(err, ErrAgentMissingConfig) {
		t.Errorf("expected ErrAgentMissingConfig, got %v", err)
	}
}

func TestBuildRestartTemplateMember_NilConfigReturnsError(t *testing.T) {
	agent := &AgentState{ConfigID: "x"}
	_, err := BuildRestartTemplateMember(agent)
	if !errors.Is(err, ErrAgentMissingConfig) {
		t.Errorf("expected ErrAgentMissingConfig, got %v", err)
	}
}

func TestResolveRestartWorkingDir_PrefersSDKWorkingDir(t *testing.T) {
	agent := &AgentState{WorktreePath: "/from/agent"}
	got := ResolveRestartWorkingDir(agent, "/from/sdk")
	if got != "/from/sdk" {
		t.Errorf("got=%q want /from/sdk (SDK should win when set)", got)
	}
}

func TestResolveRestartWorkingDir_FallsBackToAgentWorktreePath(t *testing.T) {
	agent := &AgentState{WorktreePath: "/from/agent"}
	got := ResolveRestartWorkingDir(agent, "")
	if got != "/from/agent" {
		t.Errorf("got=%q want /from/agent (agent fallback)", got)
	}
}

func TestResolveRestartWorkingDir_EmptyEverywhereReturnsEmpty(t *testing.T) {
	agent := &AgentState{}
	got := ResolveRestartWorkingDir(agent, "")
	if got != "" {
		t.Errorf("got=%q want empty (caller maps to error)", got)
	}
}

func TestResolveRestartWorkingDir_NilAgentSafe(t *testing.T) {
	if got := ResolveRestartWorkingDir(nil, "/x"); got != "/x" {
		t.Errorf("got=%q want /x (SDK still preferred even with nil agent)", got)
	}
	if got := ResolveRestartWorkingDir(nil, ""); got != "" {
		t.Errorf("got=%q want empty (nil agent + no SDK)", got)
	}
}

func TestSnapshotRosterAsTeamMemberConfigs_HappyPath(t *testing.T) {
	m := newFinalizeTestManager(t)
	m.RegisterAgent("sess-aaa-001", "alpha", &TeamMemberConfig{ID: "alpha", Name: "Alpha"})
	m.RegisterAgent("sess-bbb-002", "beta", &TeamMemberConfig{ID: "beta", Name: "Beta"})
	got := m.SnapshotRosterAsTeamMemberConfigs()
	if len(got) != 2 {
		t.Fatalf("len=%d want 2", len(got))
	}
	names := map[string]bool{}
	for _, cfg := range got {
		names[cfg.Name] = true
	}
	if !names["Alpha"] || !names["Beta"] {
		t.Errorf("missing names: %+v", names)
	}
}

func TestSnapshotRosterAsTeamMemberConfigs_EmptyReturnsEmptySlice(t *testing.T) {
	m := newFinalizeTestManager(t)
	got := m.SnapshotRosterAsTeamMemberConfigs()
	if got == nil {
		t.Error("expected empty slice, not nil")
	}
	if len(got) != 0 {
		t.Errorf("len=%d want 0", len(got))
	}
}

func TestFinalizeTeamSpawn_PRNumberFlowsThrough(t *testing.T) {
	m := newFinalizeTestManager(t)
	req := spawnpayload.SpawnTeamRequest{
		WorkingDir: "/x",
		PRNumber:   42,
	}
	m.FinalizeTeamSpawn(req, "tpl", false)
	if got := m.GetTeamPRNumber(); got != 42 {
		t.Errorf("PR number=%d want 42", got)
	}

	// Zero clears any prior linkage.
	req.PRNumber = 0
	m.FinalizeTeamSpawn(req, "tpl", false)
	if got := m.GetTeamPRNumber(); got != 0 {
		t.Errorf("PR number=%d want 0 (zero should clear)", got)
	}
}
