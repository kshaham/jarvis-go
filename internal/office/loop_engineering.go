package office

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kshaham/jarvis/internal/ralph"
	"github.com/kshaham/jarvis/internal/safego"
)

// taskCriteria returns the acceptance criteria for a task in the agent's queue,
// or nil when the task is gone or has none.
func taskCriteria(agent *AgentState, taskID string) []string {
	for _, t := range agent.GetTasks() {
		if t.ID == taskID {
			return t.AcceptanceCriteria
		}
	}
	return nil
}

// This file makes the perpetual ("RALPH") loop adhere to loop-engineering
// discipline: an objective gate (the verify command), hard stops (iteration +
// wall-clock caps), a durable escalation record, and a self-added-task
// category blocklist. See docs: the perpetual loop is recovery.go's
// CheckForIdleAgents -> sendSelfReplenishmentPrompt; these helpers are wired
// into that path.

// objectiveGate builds the configured objective gate for the current team. It
// uses Config.VerifyCommand when set, otherwise auto-detects from the working
// directory. Exposed as a field-backed seam so tests can inject a fast stub
// instead of running a real test suite.
func (m *Manager) objectiveGate() ralph.ObjectiveGate {
	if m.gateFactory != nil {
		return m.gateFactory()
	}
	m.mu.RLock()
	cmd := m.config.VerifyCommand
	m.mu.RUnlock()
	return ralph.NewBasicCommandGate(cmd)
}

// runObjectiveGate runs the verify command in the team working directory and
// returns the result. A skipped gate (no command, missing binary) is reported
// as Skipped so callers decide whether an un-gateable project may proceed.
func (m *Manager) runObjectiveGate() ralph.GateResult {
	m.mu.RLock()
	workingDir := m.lastWorkingDir
	m.mu.RUnlock()
	if workingDir == "" {
		return ralph.GateResult{Skipped: true, Summary: "no working directory", CheckedAt: time.Now()}
	}
	return m.objectiveGate().Check(workingDir)
}

// loopHardStopReached reports whether the agent's perpetual loop has exceeded
// its iteration or wall-clock budget. The returned reason is empty when no cap
// is hit. Caps of 0 mean "unlimited" (legacy behavior preserved).
func (m *Manager) loopHardStopReached(agent *AgentState) (bool, string) {
	m.mu.RLock()
	maxIter := m.config.RalphMaxLoopIterations
	maxDur := m.config.PerpetualLoopMaxDuration
	m.mu.RUnlock()

	agent.mu.Lock()
	iter := agent.LoopIterationCount
	startedAt := agent.LoopStartedAt
	agent.mu.Unlock()

	if maxIter > 0 && iter >= maxIter {
		return true, fmt.Sprintf("iteration cap reached (%d/%d)", iter, maxIter)
	}
	if maxDur > 0 && !startedAt.IsZero() && time.Since(startedAt) >= maxDur {
		return true, fmt.Sprintf("time cap reached (%s elapsed, limit %s)",
			time.Since(startedAt).Round(time.Second), maxDur)
	}
	return false, ""
}

// noteLoopIteration records one perpetual-loop iteration for an agent, starting
// the clock on the first one. Called each time the loop replenishes the agent
// with new self-found work. The state is persisted so a daemon restart resumes
// the iteration count (and thus the hard stop) instead of cold-starting.
func (m *Manager) noteLoopIteration(agent *AgentState) {
	// Key by the STABLE ConfigID, not the mutable sessionID: an in-place
	// restart (ReassignSessionID) changes the sessionID, so a sessionID-keyed
	// file would never be found by RestoreLoopState on the next spawn and the
	// hard-stop budget would silently reset. ConfigID survives reassigns.
	key := agent.ConfigID
	agent.mu.Lock()
	if agent.LoopStartedAt.IsZero() {
		agent.LoopStartedAt = time.Now()
	}
	agent.LoopIterationCount++
	state := RalphLoopState{
		ConfigID:      key,
		SessionID:     m.sessionIDOf(agent), // informational only
		LoopIteration: agent.LoopIterationCount,
		StartedAt:     agent.LoopStartedAt,
		LastRun:       time.Now(),
	}
	agent.mu.Unlock()
	// Synchronous (not async): the write is tiny and replenishments are minutes
	// apart in production, so this never hot-loops -- and a synchronous write
	// guarantees iteration N+1 can't be clobbered by a late iteration-N write.
	if err := saveLoopState(key, state); err != nil {
		slog.Debug("save ralph loop state failed", "err", err)
	}
}

// loopStateResumeWindow bounds how recent a persisted loop state must be to be
// resumed. A daemon restart resumes within minutes; this window keeps a much
// older file (e.g. a DIFFERENT team that reused the same role/ConfigID weeks
// later) from making a brand-new agent inherit a stale -- possibly already
// capped -- iteration budget.
const loopStateResumeWindow = 12 * time.Hour

// RestoreLoopState reloads an agent's perpetual-loop iteration count + start
// time from disk if a RECENT prior run persisted them. Called after
// RegisterAgent so a daemon restart resumes the hard-stop budget instead of
// starting fresh. Keyed by ConfigID so it survives sessionID reassignment; a
// staleness window guards against cross-team ConfigID collisions.
func (m *Manager) RestoreLoopState(agent *AgentState) {
	if agent == nil {
		return
	}
	key := agent.ConfigID
	state, ok := loadLoopState(key)
	if !ok {
		return
	}
	// Ignore stale state: only a recent run is a genuine restart-resume.
	if !state.LastRun.IsZero() && time.Since(state.LastRun) > loopStateResumeWindow {
		return
	}
	agent.mu.Lock()
	agent.LoopIterationCount = state.LoopIteration
	agent.LoopStartedAt = state.StartedAt
	agent.mu.Unlock()
	slog.Info("resumed perpetual loop state", "configId", key, "iteration", state.LoopIteration)
}

// RalphLoopState is the durable record of one agent's perpetual-loop progress
// (loop-engineering requirement C: the agent forgets, the repo does not).
type RalphLoopState struct {
	ConfigID      string    `json:"configId"`
	SessionID     string    `json:"sessionId,omitempty"`
	LoopIteration int       `json:"loopIteration"`
	StartedAt     time.Time `json:"startedAt"`
	LastRun       time.Time `json:"lastRun"`
}

// loopStatePath is the per-agent loop-state file. The filename pairs a readable
// whitelist slug (for humans browsing the directory) with a collision-resistant
// hash suffix derived from the RAW key. The slug alone is lossy -- everything
// outside [A-Za-z0-9._-] maps to '_', so distinct ConfigIDs like "backend:team-a"
// and "backend@team-a" would share one file and cross-pollute each other's
// iteration budget. The SHA-256 suffix makes the path injective so no two
// distinct ConfigIDs (operators can set arbitrary member IDs) can ever collide.
func loopStatePath(key string) string {
	home, _ := os.UserHomeDir()
	slug := slugifyKey(key)
	if len(slug) > 48 {
		slug = slug[:48]
	}
	sum := sha256.Sum256([]byte(key))
	suffix := hex.EncodeToString(sum[:])[:16]
	return filepath.Join(home, ".jarvis", "office-memory",
		"ralph-loop-state-"+slug+"-"+suffix+".json")
}

// slugifyKey replaces every character outside [A-Za-z0-9._-] with '_'. Used only
// for the human-readable prefix of loopStatePath; collision-resistance comes from
// the hash suffix, not this lossy slug.
func slugifyKey(key string) string {
	var b strings.Builder
	b.Grow(len(key))
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// saveLoopState writes one agent's loop state atomically (tmp+rename), cleaning
// up the tmp file on a rename failure so a full/locked disk can't accumulate
// orphans -- matching the convention in learning.go / snapshot.go / statusline.go.
func saveLoopState(key string, state RalphLoopState) error {
	if key == "" {
		return nil
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	path := loopStatePath(key)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// loadLoopState reads a persisted loop state. ok=false when no file exists.
// Mirrors saveLoopState's empty-key guard: an empty ConfigID must never resolve
// to a shared on-disk file that a misconfigured agent could read stale state from.
func loadLoopState(key string) (RalphLoopState, bool) {
	if key == "" {
		return RalphLoopState{}, false
	}
	data, err := os.ReadFile(loopStatePath(key))
	if err != nil {
		return RalphLoopState{}, false
	}
	var s RalphLoopState
	if err := json.Unmarshal(data, &s); err != nil {
		return RalphLoopState{}, false
	}
	return s, true
}

// pauseLoopForLimit pauses an agent's perpetual loop because a hard stop was
// reached. It latches (LoopPausedForLimit) so the escalation only fires once,
// records a durable escalation, and broadcasts a loop_max event for the
// dashboard. Returns true the first time it fires for this agent.
func (m *Manager) pauseLoopForLimit(agent *AgentState, reason string) bool {
	agent.mu.Lock()
	if agent.LoopPausedForLimit {
		agent.mu.Unlock()
		return false
	}
	agent.LoopPausedForLimit = true
	agent.mu.Unlock()

	sid := m.sessionIDOf(agent)
	agentName := sid
	if agent.Config != nil {
		agentName = agent.Config.Name
	}

	slog.Warn("perpetual loop hard stop reached; pausing replenishment for human review",
		"agent", agentName, "reason", reason)

	if err := RecordEscalation(EscalationRecord{
		Timestamp: time.Now(),
		SessionID: sid,
		AgentName: agentName,
		Reason:    "loop_hard_stop",
		Detail:    reason,
	}); err != nil {
		slog.Debug("record loop hard-stop escalation failed", "err", err)
	}

	m.broadcast("office:loopMaxReached", map[string]interface{}{
		"sessionId": sid,
		"agentName": agentName,
		"reason":    reason,
	})
	m.NotifyOfficeEvent("Perpetual loop paused",
		fmt.Sprintf("%s hit a loop hard stop (%s). Replenishment paused for human review.", agentName, reason))
	return true
}

// TaskCategoryBlocked is the exported wrapper around taskCategoryBlocked for
// the HTTP self-add handler. Returns the matched blocked category when blocked.
func (m *Manager) TaskCategoryBlocked(text string) (bool, string) {
	return m.taskCategoryBlocked(text)
}

// taskCategoryBlocked reports whether a self-added task title/description falls
// into a blocked category (auth, payments, deploy, architecture, ...). Loops
// belong on small machine-checkable work, not irreversible judgment calls.
// Returns the matched category when blocked.
func (m *Manager) taskCategoryBlocked(text string) (bool, string) {
	m.mu.RLock()
	categories := m.config.BlockedTaskCategories
	m.mu.RUnlock()
	lower := strings.ToLower(text)
	for _, c := range categories {
		if c == "" {
			continue
		}
		if strings.Contains(lower, strings.ToLower(c)) {
			return true, c
		}
	}
	return false, ""
}

// gatedAutoComplete decides whether a stale, silently-finished in-progress task
// may be auto-completed. Instead of stamping "completed" on unverified output
// (the worst anti-gate), it runs the objective verify gate asynchronously and
// only completes on a pass (or a skip -- an un-gateable project keeps the legacy
// liveness behavior). On a real gate failure it nudges the agent to finish the
// work and leaves the task in progress.
func (m *Manager) gatedAutoComplete(agent *AgentState, sid, taskID, taskTitle, agentName string, taskAge time.Duration) {
	// Guard against overlapping verify runs for the same session. The map is
	// always initialized in NewManager, so no nil check is needed here.
	m.mu.Lock()
	if m.autoGateInFlight[sid] {
		m.mu.Unlock()
		return
	}
	m.autoGateInFlight[sid] = true
	m.mu.Unlock()

	safego.Go("office.gatedAutoComplete", func() {
		result := m.runObjectiveGate()

		m.mu.Lock()
		delete(m.autoGateInFlight, sid)
		sessions := m.sessions
		m.mu.Unlock()

		// The agent may have moved on while the gate ran -- only act if the
		// same task is still the active, in-progress one.
		active := agent.ActiveTask()
		if active == nil || active.ID != taskID {
			return
		}

		if !result.Pass && !result.Skipped {
			// Objective gate rejected the work. Do not auto-complete; nudge.
			slog.Info("Gated auto-complete withheld: verify gate failed",
				"agent", agentName, "taskId", taskID, "command", result.Command, "exitCode", result.ExitCode)
			if sessions != nil {
				nudge := fmt.Sprintf(
					"[VERIFICATION FAILED] Your task %q is not done -- the verify gate failed (`%s`, exit %d). "+
						"Fix the failure, re-run it, and only then `cm task complete %s`.",
					taskTitle, result.Command, result.ExitCode, taskID)
				sessions.WriteInkPrompt(sid, nudge)
				agent.mu.Lock()
				agent.LastPromptSentAt = time.Now()
				agent.mu.Unlock()
			}
			m.broadcast("office:gateCheckFailed", map[string]interface{}{
				"sessionId": sid,
				"agentName": agentName,
				"taskId":    taskID,
				"command":   result.Command,
				"exitCode":  result.ExitCode,
			})
			return
		}

		// Maker/checker split (loop-engineering requirement B): the objective
		// gate is the cheap deterministic check; an INDEPENDENT evaluator (a
		// fresh process, no exposure to the maker's reasoning) is the semantic
		// check. Run it only when the task carries acceptance criteria, and
		// only after the objective gate passed. Fail-open: an evaluator error
		// must not wedge the loop.
		if criteria := taskCriteria(agent, taskID); len(criteria) > 0 {
			eval := m.criteriaEvaluator
			if eval == nil {
				eval = EvaluateAcceptanceCriteria
			}
			m.mu.RLock()
			worktree := m.lastWorkingDir
			m.mu.RUnlock()
			verdict, _ := eval(context.Background(), worktree,
				"Autonomous idle auto-complete of task "+taskID+" ("+taskTitle+")", criteria)
			if verdict.EvaluatorError == "" && !verdict.AllPass {
				// Independent checker rejected the work. Do not complete; nudge
				// and count it as a rejected change.
				slog.Info("Gated auto-complete withheld: independent evaluator rejected criteria",
					"agent", agentName, "taskId", taskID)
				if m.metrics != nil {
					m.metrics.TotalTasksRejected.Add(1)
				}
				agent.IncrementCriteriaReject(taskID)
				if sessions != nil {
					nudge := fmt.Sprintf(
						"[CRITERIA NOT MET] An independent reviewer found task %q does not yet satisfy its "+
							"acceptance criteria. Address the gaps, then `cm task complete %s`.", taskTitle, taskID)
					sessions.WriteInkPrompt(sid, nudge)
					agent.mu.Lock()
					agent.LastPromptSentAt = time.Now()
					agent.mu.Unlock()
				}
				m.broadcast("office:criteriaRejected", map[string]interface{}{
					"sessionId": sid,
					"agentName": agentName,
					"taskId":    taskID,
				})
				return
			}
		}

		// Gate passed (or skipped on an un-gateable project): complete it.
		reason := "auto-completed (idle agent, verify passed)"
		if result.Skipped {
			reason = "auto-completed (idle agent, no verify gate)"
		}
		agent.UpdateTaskStatus(taskID, TaskCompleted)
		slog.Info("Auto-completed idle agent task",
			"agent", agentName, "taskId", taskID, "title", RedactSecrets(taskTitle),
			"taskAge", taskAge, "gateSkipped", result.Skipped)

		m.broadcast("office:taskUpdated", map[string]interface{}{
			"sessionId": sid,
			"taskId":    taskID,
			"status":    string(TaskCompleted),
			"reason":    reason,
		})

		agent.mu.Lock()
		delete(agent.ProgressChecks, taskID)
		agent.mu.Unlock()

		if sessions != nil {
			for _, t := range agent.GetTasks() {
				if t.Status == TaskPending || t.Status == TaskAcknowledged {
					startPrompt := fmt.Sprintf("[AUTO-START] Your previous task was completed. Starting next task:\ncm task start %s", t.ID)
					sessions.WriteInkPrompt(sid, startPrompt)
					slog.Info("Auto-started next task", "agent", agentName, "taskId", t.ID, "title", t.Title)
					break
				}
			}
		}
	})
}

// lowAcceptanceThreshold is the accepted-change rate below which the loop is
// judged to be creating review work rather than saving it (loop engineering's
// "< 50%" rule).
const lowAcceptanceThreshold = 0.5

// lowAcceptanceWarnCooldown throttles the low-acceptance broadcast.
const lowAcceptanceWarnCooldown = 30 * time.Minute

// maybeWarnLowAcceptance broadcasts office:lowAcceptanceRate when the perpetual
// loop's accepted-change rate falls below the threshold, throttled so it fires
// at most once per cooldown. Requires a minimum sample so a single early
// rejection doesn't trip it.
func (m *Manager) maybeWarnLowAcceptance() {
	if !m.IsPerpetualMode() {
		return
	}
	snap := m.GetMetricsSnapshot()
	sample := snap.TotalTasksCompleted + snap.TotalTasksRejected
	if sample < 4 {
		return // too little data to judge
	}
	if snap.AcceptedChangeRate >= lowAcceptanceThreshold {
		return
	}
	m.mu.Lock()
	if !m.lastLowAcceptWarnAt.IsZero() && time.Since(m.lastLowAcceptWarnAt) < lowAcceptanceWarnCooldown {
		m.mu.Unlock()
		return
	}
	m.lastLowAcceptWarnAt = time.Now()
	m.mu.Unlock()

	slog.Warn("perpetual loop accepted-change rate below threshold",
		"rate", snap.AcceptedChangeRate, "completed", snap.TotalTasksCompleted, "rejected", snap.TotalTasksRejected)
	m.broadcast("office:lowAcceptanceRate", map[string]interface{}{
		"rate":      snap.AcceptedChangeRate,
		"completed": snap.TotalTasksCompleted,
		"rejected":  snap.TotalTasksRejected,
	})
}

// ---------------------------------------------------------------------------
// Durable escalation-to-humans record (loop-engineering requirement C).
// ---------------------------------------------------------------------------

// EscalationRecord is one durable "the loop needed a human" entry, appended to
// escalations.jsonl. Gives operators a single queryable place to see what the
// perpetual loop struggled with (stuck agents, blocked tasks, hard stops).
type EscalationRecord struct {
	Timestamp        time.Time `json:"timestamp"`
	SessionID        string    `json:"sessionId"`
	AgentName        string    `json:"agentName"`
	Reason           string    `json:"reason"` // recovery_exhausted | blocked_max_triage | loop_hard_stop | gate_failed
	Detail           string    `json:"detail,omitempty"`
	TaskIDs          []string  `json:"taskIds,omitempty"`
	RecoveryAttempts int       `json:"recoveryAttempts,omitempty"`
}

// escalationsPath is the durable escalation log location. Mirrors
// experimentsPath so all office-memory persistence shares one convention.
func escalationsPath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".jarvis", "office-memory", "escalations.jsonl")
}

// escalationsOpenFile is a seam so tests can force open errors (mirrors
// experimentsOpenFile).
var escalationsOpenFile = func(path string) (io.WriteCloser, error) {
	return os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
}

// RecordEscalation appends one escalation to the durable JSONL log. Like
// RecordExperiment it surfaces the close error so a disk-full append isn't
// silently dropped.
func RecordEscalation(record EscalationRecord) error {
	if record.Timestamp.IsZero() {
		record.Timestamp = time.Now()
	}
	path := escalationsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := escalationsOpenFile(path)
	if err != nil {
		return err
	}
	data, err := json.Marshal(record)
	if err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close escalation log: %w", err)
	}
	return nil
}

// LoadEscalations reads back all durable escalation records (newest last).
// Returns an empty slice when the log does not exist yet.
func LoadEscalations() ([]EscalationRecord, error) {
	data, err := os.ReadFile(escalationsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []EscalationRecord
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec EscalationRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			slog.Debug("skip malformed escalation line", "err", err)
			continue
		}
		out = append(out, rec)
	}
	return out, nil
}
