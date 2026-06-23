package office

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kshaham/jarvis/internal/safego"
)

// This file is the heart of the loop-engineering overlay (Planner / Maker /
// Verifier standing teammates). Every routine here is GATED on
// m.IsLoopRolesMode(): when the flag is off (the default) none of it runs and
// teams behave exactly as before. The three responsibilities split as:
//
//   - Planner: DISCOVER+PLAN only. Self-replenishes with SMALL machine-checkable
//     tasks, each carrying explicit --criterion acceptance criteria. Never
//     executes them itself.
//   - Maker: EXECUTE. Pulls from its queue. Planner self-adds are auto-routed to
//     the least-loaded Maker.
//   - Verifier: VERIFY. The deterministic command gate stays the cheap FIRST
//     pass; the SEMANTIC acceptance-criteria check is routed to the live
//     Verifier (independent, fresh process, pointed at the Maker's worktree).
//
// FAIL-OPEN is load-bearing throughout: a busy / dead / rate-limited / slow
// Verifier MUST resolve to CriteriaVerdict{EvaluatorError} (allow completion),
// never a hard reject or a wedged loop.

// findAgentByRole returns the live agent whose LoopRole matches the given role,
// or nil when none is live. Mirrors findAvailableAgent's locking discipline
// (read m.agents under m.mu). Unlike findStrategicAgent, selection is by
// explicit LoopRole, never by Tier -- the spec requires the Planner/Maker/
// Verifier be picked by role so a Tier-based selector (used by blocked-task
// triage + brainstorm) is never repurposed and accidentally regressed.
func (m *Manager) findAgentByRole(loopRole string) *AgentState {
	if loopRole == "" {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, agent := range m.agents {
		if agent != nil && agent.Config != nil && agent.Config.LoopRole == loopRole {
			return agent
		}
	}
	return nil
}

// findLeastLoadedMaker returns the Maker with the fewest queued tasks (active +
// pending), or nil when no Maker is live. Used to balance Planner self-adds
// across Makers. Read under m.mu so the roster snapshot is consistent; the
// per-agent task counts are taken via the agent's own locked accessors.
func (m *Manager) findLeastLoadedMaker() *AgentState {
	m.mu.RLock()
	makers := make([]*AgentState, 0, len(m.agents))
	for _, agent := range m.agents {
		if agent != nil && agent.Config != nil && agent.Config.LoopRole == LoopRoleMaker {
			makers = append(makers, agent)
		}
	}
	m.mu.RUnlock()

	var best *AgentState
	bestLoad := int(^uint(0) >> 1) // max int
	for _, maker := range makers {
		load := maker.PendingTaskCount()
		if maker.ActiveTask() != nil {
			load++
		}
		if load < bestLoad {
			bestLoad = load
			best = maker
		}
	}
	return best
}

// routePlannerTaskToMaker auto-routes a Planner self-added task to the
// least-loaded Maker. Gated on loop-roles mode AND the source agent being the
// Planner. REUSES TriageTaskBetweenAgents (RemoveTask + stamp Source + AddTask)
// so the existing taskQueueUpdated broadcast + ownership semantics apply
// unchanged; the move is then re-stamped Source="planner-route".
//
// Atomicity: TriageTaskBetweenAgents is not transactional across both queues,
// so this copies AcceptHandoff's panic-safe re-home discipline -- if anything
// panics after the task leaves the Planner and before it lands on the Maker,
// the deferred guard re-homes it on the Planner so a task can never vanish from
// both queues. A TOCTOU re-check confirms the task is still on the Planner
// (status pending, not already moved/started) before the move.
//
// Returns true when the task was routed. A nil Maker (none live yet) leaves the
// task on the Planner's queue -- harmless, it just isn't executed until a Maker
// appears and the next planning cycle re-routes.
func (m *Manager) routePlannerTaskToMaker(planner *AgentState, taskID string) bool {
	if !m.IsLoopRolesMode() || planner == nil || planner.Config == nil {
		return false
	}
	if planner.Config.LoopRole != LoopRolePlanner {
		return false
	}
	maker := m.findLeastLoadedMaker()
	if maker == nil {
		return false
	}
	plannerSid := m.sessionIDOf(planner)
	makerSid := m.sessionIDOf(maker)
	if plannerSid == "" || makerSid == "" || plannerSid == makerSid {
		return false
	}

	// TOCTOU re-check: confirm the task is still owned by the Planner and is in
	// a movable (pending/acknowledged) state under the Planner's lock before
	// triaging. Capture a full copy of the task while we confirm ownership so a
	// panic mid-move can re-home it WITHOUT losing title/criteria. A task the
	// Maker already pulled, or one the operator deleted, must not be moved twice.
	snapshot, owned := plannerMovableTaskSnapshot(planner, taskID)
	if !owned {
		return false
	}

	routed := false
	// Panic-safe re-home: if TriageTaskBetweenAgents panics mid-move (after
	// RemoveTask, before AddTask) the task is gone from the Planner and not yet
	// on the Maker. Re-home the captured copy on the Planner so a task can never
	// vanish from BOTH queues. Mirrors AcceptHandoff's defer guard.
	defer func() {
		if r := recover(); r != nil {
			slog.Error("planner-route panic; re-homing task on planner",
				"taskId", taskID, "planner", plannerSid, "rec", r)
			// Re-home only when the task is on neither queue (success path
			// already homed it on the Maker; the Planner may still hold it if
			// RemoveTask never ran).
			if !routed && !plannerOwnsMovableTask(planner, taskID) {
				rehome := snapshot
				rehome.Status = TaskPending
				rehome.AssignedAt = time.Now()
				rehome.Source = "planner-route-rehome"
				planner.AddTask(&rehome)
			}
		}
	}()

	_, target, err := m.TriageTaskBetweenAgents(plannerSid, makerSid, taskID)
	if err != nil {
		slog.Debug("planner-route triage failed; task stays on planner",
			"taskId", taskID, "err", err)
		return false
	}
	routed = true

	// Re-stamp the Source so the dashboard distinguishes a planner-route from a
	// manual operator triage. TriageTaskBetweenAgents stamps "triage".
	target.SetTaskSource(taskID, "planner-route")

	m.broadcast("office:taskQueueUpdated", map[string]interface{}{
		"sessionId": makerSid,
	})
	if payload := m.BuildMemberPayload(target); payload != nil {
		m.broadcast("office:memberUpdated", payload)
	}
	plannerName := plannerSid
	if planner.Config != nil {
		plannerName = planner.Config.Name
	}
	makerName := makerSid
	if maker.Config != nil {
		makerName = maker.Config.Name
	}
	slog.Info("Routed planner self-add to least-loaded maker",
		"taskId", taskID, "planner", plannerName, "maker", makerName)
	return routed
}

// plannerOwnsMovableTask reports whether the Planner still owns the task in a
// movable (pending/acknowledged) state. Read via the agent's locked accessor so
// the status check doesn't race a concurrent move/start.
func plannerOwnsMovableTask(planner *AgentState, taskID string) bool {
	for _, t := range planner.GetTasks() {
		if t.ID == taskID {
			return t.Status == TaskPending || t.Status == TaskAcknowledged
		}
	}
	return false
}

// plannerMovableTaskSnapshot returns a value copy of the Planner's movable task
// (pending/acknowledged) plus owned=true, or owned=false when the task is gone
// or not movable. GetTasks returns deep copies, so dereferencing the returned
// pointer yields a copy safe to re-home after the original leaves the queue.
func plannerMovableTaskSnapshot(planner *AgentState, taskID string) (Task, bool) {
	for _, t := range planner.GetTasks() {
		if t.ID == taskID {
			if t.Status == TaskPending || t.Status == TaskAcknowledged {
				return *t, true
			}
			return Task{}, false
		}
	}
	return Task{}, false
}

// EvaluateCriteria is the single routing point for the SEMANTIC acceptance-
// criteria check. It prefers m.criteriaEvaluator (set to verifierEvaluate when
// a live Verifier is present in loop-roles mode, or a test stub) and falls back
// to the package-level EvaluateAcceptanceCriteria (a fresh `claude` process).
// Both completion paths (idle/async and the HTTP sync path) funnel through here
// so the routing decision lives in exactly one place.
//
// It NEVER returns a hard error for evaluator/parse failures: a usable verdict
// always accompanies a nil error, with EvaluatorError set on infra failure so
// callers fail open.
func (m *Manager) EvaluateCriteria(ctx context.Context, worktreeDir, summary string, criteria []string) (CriteriaVerdict, error) {
	eval := m.criteriaEvaluator
	if eval == nil {
		eval = EvaluateAcceptanceCriteria
	}
	return eval(ctx, worktreeDir, summary, criteria)
}

// verifierAgent returns the live Verifier (loop role == verifier) or nil. The
// lock is held only for the lookup; the caller RELEASES it before any round-
// trip so a slow Verifier check never holds m.mu.
func (m *Manager) verifierAgent() *AgentState {
	return m.findAgentByRole(LoopRoleVerifier)
}

// verifierEvaluate is the live-Verifier acceptance-criteria check. It matches
// the criteriaEvaluator signature exactly so it can be assigned to
// m.criteriaEvaluator at spawn when a Verifier is present in loop-roles mode.
//
// Behavior:
//   - Locate the Verifier (verifierAgent). No live Verifier -> fail open
//     (EvaluatorError) so completion is never blocked by a missing checker.
//   - Per-Verifier in-flight guard (mirrors autoGateInFlight): a check already
//     running for this Verifier -> fail open rather than stacking a second
//     expensive round-trip.
//   - Notify the live Verifier session so the dashboard shows WHO is checking,
//     then run the INDEPENDENT check as a fresh process (reusing
//     EvaluateAcceptanceCriteria's exec + envelope/fence parsing + usableVerdict
//     fail-open from criteria_review.go) pointed at the MAKER's worktree, not
//     the Verifier's own checkout.
//
// FAIL-OPEN is load-bearing: every failure mode resolves to a usable verdict
// with a nil error, never a hard error or a wedged loop.
func (m *Manager) verifierEvaluate(ctx context.Context, worktreeDir, summary string, criteria []string) (CriteriaVerdict, error) {
	verifier := m.verifierAgent()
	if verifier == nil {
		// No live Verifier: fail open (the deterministic gate already ran as
		// the cheap first pass; a missing semantic checker must not block).
		return CriteriaVerdict{EvaluatorError: "no live verifier"}, nil
	}
	vsid := m.sessionIDOf(verifier)
	if vsid == "" {
		return CriteriaVerdict{EvaluatorError: "verifier has no session"}, nil
	}

	// Per-Verifier in-flight guard. The map is always initialized in
	// NewManager, so no nil check is needed.
	m.mu.Lock()
	if m.verifierInFlight[vsid] {
		m.mu.Unlock()
		return CriteriaVerdict{EvaluatorError: "verifier busy"}, nil
	}
	m.verifierInFlight[vsid] = true
	sessions := m.sessions
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.verifierInFlight, vsid)
		m.mu.Unlock()
	}()

	// Point the check at the MAKER's worktree (the directory under review), not
	// the Verifier's own checkout. Prefer the explicit worktreeDir the caller
	// passed (the Maker's worktree / team working dir); fall back to the team
	// working dir when empty.
	target := worktreeDir
	if target == "" {
		m.mu.RLock()
		target = m.lastWorkingDir
		m.mu.RUnlock()
	}
	if target == "" {
		return CriteriaVerdict{EvaluatorError: "no maker worktree to verify"}, nil
	}

	// Notify the live Verifier so the dashboard reflects who is checking. This
	// is informational (the independent verdict comes from the fresh process
	// below); never block on it.
	if sessions != nil {
		sessions.WriteInkPrompt(vsid, fmt.Sprintf(
			"[VERIFY] Independently checking a completed change against its acceptance criteria:\n%s\n"+
				"This runs as a fresh, independent review against the maker's worktree. You did NOT write the code -- do not be generous.",
			summary))
	}

	// Independent semantic check: a fresh process (no exposure to the maker's
	// reasoning), pointed at the maker's worktree, with the SAME envelope/fence
	// parsing + usableVerdict fail-open as the package path -- but PINNED to the
	// stronger evaluator model so the operator's "Verifier = stronger model"
	// choice is honored at the point the verdict is authored, not just on the
	// standing Verifier agent.
	return evaluateCriteriaWithModel(ctx, target, summary, criteria, verifierEvaluatorModel)
}

// MaybeWireVerifierEvaluator installs verifierEvaluate as the criteria
// evaluator when loop-roles mode is on AND a live Verifier is present. Called at
// spawn (and safe to call again after a respawn). When the conditions do not
// hold it leaves m.criteriaEvaluator untouched (nil in production -> the package
// fallback), preserving existing behavior for teams not opting into loop-roles.
//
// A test-injected criteriaEvaluator is never overwritten: tests set the field
// directly before driving completion, so only install the production seam when
// the field is still nil.
func (m *Manager) MaybeWireVerifierEvaluator() {
	if !m.IsLoopRolesMode() {
		return
	}
	if m.verifierAgent() == nil {
		return
	}
	m.mu.Lock()
	if m.criteriaEvaluator == nil {
		m.criteriaEvaluator = m.verifierEvaluate
	}
	m.mu.Unlock()
}

// sendDiscoverPlanPrompt is the Planner-only DISCOVER+PLAN replenishment. It
// replaces the generic self-replenishment prompt for the Planner in loop-roles
// mode: the Planner DISCOVERs work (scans repo, TODOs, failing tests, coverage
// gaps) and PLANs it as SMALL machine-checkable tasks, each carrying explicit
// --criterion acceptance criteria. The Planner never executes them itself --
// routePlannerTaskToMaker hands each self-add to the least-loaded Maker.
func (m *Manager) sendDiscoverPlanPrompt(agent *AgentState) {
	sid := m.sessionIDOf(agent)
	mission := m.GetTeamMission()
	missionLine := ""
	if mission != "" {
		missionLine = fmt.Sprintf("\nTeam Mission: %s\n", mission)
	}

	constraintsLine := ""
	if inv := m.GetProjectInvariants(); len(inv) > 0 {
		constraintsLine = "\n[CURRENT CONSTRAINTS] (re-read every loop -- honor these now)\n"
		for _, it := range inv {
			constraintsLine += "- " + it + "\n"
		}
	}

	prompt := fmt.Sprintf(`[PLANNER: DISCOVER + PLAN NOW] You are the Planner. Your job is to DISCOVER work and PLAN it -- never to execute it yourself.
%s%s
DISCOVER (scan the repo RIGHT NOW):
1. Run 'git log --oneline -20' to see recent changes -- look for follow-ups, regressions, half-done work.
2. Find TODOs/FIXMEs: grep -rn "TODO\|FIXME" . --include="*.go" --include="*.js" --include="*.ts"
3. Run the test suite and note any failures or coverage gaps.
4. Check 'cm roster' so you do not plan work a teammate already owns.

PLAN (queue SMALL, machine-checkable tasks, each WITH explicit acceptance criteria):
Every task MUST carry at least one --criterion describing a machine-checkable pass condition. Repeat --criterion for multiple criteria:
  cm task self-add "Fix nil deref in parseConfig" --priority high --criterion "go test ./internal/config/... passes" --criterion "no new vet warnings"
  cm task self-add "Add table test for normaliseEffort" --priority medium --criterion "go test ./... passes" --criterion "normaliseEffort covered by at least 3 cases"

Keep each task SMALL and verifiable (tests, lint-fix, docs, refactors).
Do NOT self-add auth/payments/deploy/migration/dependency-upgrade/architecture work -- those need a human; escalate to your lead instead.
You DISCOVER and PLAN only -- the Maker executes and the Verifier checks. Do NOT start or implement these tasks yourself.

Queue at least 2 tasks now, each with a --criterion.`, missionLine, constraintsLine)

	m.mu.RLock()
	sessions := m.sessions
	m.mu.RUnlock()

	if sessions != nil {
		sessions.WriteInkPrompt(sid, prompt)
		agent.mu.Lock()
		agent.LastPromptSentAt = time.Now()
		agent.mu.Unlock()
	}

	agentName := sid
	if agent.Config != nil {
		agentName = agent.Config.Name
	}
	m.broadcast("office:agentReplenishment", map[string]interface{}{
		"sessionId": sid,
		"name":      agentName,
		"type":      "discover-plan",
	})
	m.emitEvent(AgentReplenishmentEvent{
		SessionID: sid,
		AgentName: agentName,
		Type:      "self-replenishment",
	})
	slog.Info("Sent planner discover+plan prompt", "name", agentName)
}

// loopRoleOf returns the agent's loop role, or "" when unset / no config.
func loopRoleOf(agent *AgentState) string {
	if agent == nil || agent.Config == nil {
		return ""
	}
	return agent.Config.LoopRole
}

// AsyncRoutePlannerTask routes a Planner self-add to the least-loaded Maker off
// the request goroutine. Gated on loop-roles mode + Planner role; a no-op
// otherwise. Wrapped in safego so a routing panic can't take down the process.
func (m *Manager) AsyncRoutePlannerTask(planner *AgentState, taskID string) {
	if !m.IsLoopRolesMode() || loopRoleOf(planner) != LoopRolePlanner {
		return
	}
	safego.Go("office.routePlannerTaskToMaker", func() {
		m.routePlannerTaskToMaker(planner, taskID)
	})
}
