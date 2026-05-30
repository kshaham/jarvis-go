package office

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// TeamMemberConfig defines a team member's role and personality.
type TeamMemberConfig struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Role         string   `json:"role"`
	Tier         string   `json:"tier"` // "strategic" or "worker"
	Color        string   `json:"color"`
	Personality  string   `json:"personality"`
	Voice        string   `json:"voice,omitempty"`
	Skills       []string `json:"skills"`
	SystemPrompt string   `json:"systemPrompt"`
}

// TaskStatus represents the status of a task.
type TaskStatus string

const (
	TaskPending        TaskStatus = "pending"
	TaskAcknowledged   TaskStatus = "acknowledged"
	TaskInProgress     TaskStatus = "in_progress"
	TaskCompleted      TaskStatus = "completed"
	TaskFailed         TaskStatus = "failed"
	TaskBlocked        TaskStatus = "blocked"
	TaskHandoffPending TaskStatus = "handoff_pending" // R29 · waiting on receiver accept/reject
)

// TaskProgressEntry is a single timestamped progress update on a task.
// The ProgressLog ring-buffer on Task preserves a short history so the
// office UI can show "how we got here" instead of only the latest note.
type TaskProgressEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Note      string    `json:"note"`
}

// taskProgressLogCap bounds how many progress entries we keep per task.
// Older entries are dropped from the front when exceeded -- the latest
// is always preserved and mirrored onto Task.Description for back-compat.
const taskProgressLogCap = 20

// Task is a work item assigned to an agent.
type Task struct {
	ID                 string              `json:"id"`
	Title              string              `json:"title"`
	Description        string              `json:"description"`
	Status             TaskStatus          `json:"status"`
	Priority           string              `json:"priority"` // "high", "medium", "low"
	AssignedAt         time.Time           `json:"assignedAt"`
	StartedAt          time.Time           `json:"startedAt,omitempty"`
	CompletedAt        time.Time           `json:"completedAt,omitempty"`
	Source             string              `json:"source"` // "leader", "self", "bug", "reassigned", "handoff"
	BlockedBy          []string            `json:"blockedBy,omitempty"`
	BlockerDescription string              `json:"blockerDescription,omitempty"`
	ProgressLog        []TaskProgressEntry `json:"progressLog,omitempty"`
	// R29 · Set when the current owner has initiated a handoff. Cleared
	// on accept/reject. Task.Status is TaskHandoffPending while this
	// pointer is non-nil and the task physically stays in the sender's
	// queue until accepted.
	Handoff *HandoffRequest `json:"handoff,omitempty"`

	// R36 · Free-form labels for cost-by-feature rollups. Validated
	// via ValidateTaskLabel (lowercase alphanumeric + '-', ≤40 chars,
	// ≤10 per task).
	Labels []string `json:"labels,omitempty"`
	// AcceptanceCriteria holds the operator/goal-contract conditions
	// that must be satisfied before the task is considered complete.
	// Validated and mutated via SetTaskCriteria; capped at
	// maxCriteriaPerTask (10). Never changes task Status.
	AcceptanceCriteria []string `json:"acceptanceCriteria,omitempty"`
	// CriteriaRejectCount is bumped by IncrementCriteriaReject each
	// time a reviewer determines the current criteria have not been
	// met. Advisory only -- never transitions task Status.
	CriteriaRejectCount int `json:"criteriaRejectCount,omitempty"`
	// R36 · Cost / turn snapshots captured at the transitions into
	// TaskInProgress (start) and TaskCompleted/TaskFailed (final
	// delta). FinalCostUSD = agent.TotalCostUSD at completion minus
	// StartCostUSD; same for turns. Attribution is approximate when
	// an agent runs concurrent tasks -- SDK sessions serialize turns
	// so this rarely matters in practice.
	StartCostUSD   float64 `json:"startCostUSD,omitempty"`
	StartTurnCount int64   `json:"startTurnCount,omitempty"`
	FinalCostUSD   float64 `json:"finalCostUSD,omitempty"`
	FinalTurnCount int64   `json:"finalTurnCount,omitempty"`
}

// TaskQueue holds a list of tasks for an agent.
type TaskQueue struct {
	Tasks []*Task `json:"tasks"`
}

// AckNudgeState tracks nudge attempts for task acknowledgment.
type AckNudgeState struct {
	LastNudgeAt time.Time
	Count       int
}

// PersonalityOverride holds custom personality/prompt for an agent.
type PersonalityOverride struct {
	Personality  string `json:"personality,omitempty"`
	SystemPrompt string `json:"systemPrompt,omitempty"`
}

// WhiteboardNote is a freeform note from an agent.
type WhiteboardNote struct {
	Content   string    `json:"content"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// FileLock represents a distributed file lock.
type FileLock struct {
	Path       string    `json:"path"`
	Owner      string    `json:"owner"` // sessionID
	OwnerName  string    `json:"ownerName"`
	AcquiredAt time.Time `json:"acquiredAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

// AgentState consolidates all per-agent state.
type AgentState struct {
	mu sync.Mutex

	// sessionID is the agent's live session ID. It is mutated by
	// ReassignSessionID (the R25 in-place restart) concurrently with reads from
	// the idle monitor, telegram poll, payload marshalers, etc. Stored as an
	// atomic.Pointer so EVERY read is lock-free and race-free regardless of the
	// caller's lock context (an a.mu-guarded accessor would deadlock at the many
	// call sites that already hold a.mu). Read via SessionID(); write via
	// setSessionID(). The field is unexported so a direct racy read is a compile
	// error -- this is the structural guarantee that the whole class stays fixed.
	sessionID    atomic.Pointer[string]
	ConfigID     string
	Config       *TeamMemberConfig
	WorktreePath string

	// Provider + model the agent was spawned with. Set once by the spawn
	// handler and read back by BuildMemberPayload so the UI can show which
	// LLM each agent is running on (Active Now cards + current-team banner).
	Provider string
	Model    string

	// SessionMode records which spawn path produced this agent: "sdk" |
	// "hooks" | "legacy" | "" (unknown / pre-R37 install). Set by the
	// office spawn handler after a successful CreateSession /
	// CreateSDKSession and surfaced in BuildMemberPayload so the
	// dashboard can render the "via {mode}" badge.
	SessionMode string

	// ActiveTool is the name of the Claude tool the agent is currently
	// invoking (e.g. "Bash", "Read", "Edit"), or "" when idle / thinking.
	// Written by the SDK event forwarder on every tool_use event.
	ActiveTool string

	// Response channels (replaces pending*Responses Maps). R19 wraps
	// standup / brainstorm / leader-task channels in waitSlot so a
	// late reply from round N cannot pollute round N+1 — the reader
	// marks the slot done via defer before returning, after which
	// Submit* calls hit the closed flag and drop with a counter.
	StandupSlot    *waitSlot
	BrainstormSlot *waitSlot
	LeaderTaskSlot *waitSlot
	// QuestionSlot is installed by Manager.AskQuestion when this agent is
	// the current recipient of a `cm ask` from a teammate. The asker's
	// HTTP handler blocks on the slot's channel until either the
	// recipient calls AnswerQuestion (slot.deliver), the per-recipient
	// timeout fires and AskQuestion rotates to a fallback (slot.finish +
	// new slot installed), or the total wait elapses (slot.finish + 200
	// returned to asker with status="timeout"). Only one question can be
	// outstanding per recipient at a time -- AskQuestion overwrites this
	// field when re-targeting and finishes the previous slot to drop any
	// late reply that lands during the rotation.
	QuestionSlot *waitSlot
	// QuestionSlotQID is the ID of the question the currently-installed
	// QuestionSlot serves. AnswerQuestion checks it so that when two questions
	// target the same recipient (e.g. both pick the same team lead), an answer
	// for the OLDER question isn't misdelivered to the newer question's slot.
	QuestionSlotQID string
	// Reflection / curiosity are still raw channels — they're not
	// part of the round-pollution pattern (single reply, slot is torn
	// down on the reader side). Leaving them untouched keeps the
	// refactor surgical.
	ReflectionCh chan string
	CuriosityCh  chan string

	// Task queue (private -- use AddTask/GetTasks/UpdateTaskStatus)
	taskQueue *TaskQueue

	// SDK session working state (set by event forwarder, not task lifecycle)
	SDKWorking bool

	// Timing state
	LastPromptSentAt    time.Time
	LastCuriosityAt     time.Time
	LastReplenishmentAt time.Time
	LastLateStandupAt   time.Time
	LastReflectionAt    time.Time
	// LastAgentAPIAt is "we heard FROM this agent most recently" -- any
	// incoming API call from the agent (task-ack, task-start,
	// task-complete, task-progress, self-add, memory-write, etc.)
	// touches it. Used by checkForStuckAgents to avoid falsely flagging
	// an agent as stuck just because their SDK turn is still running
	// (multi-step tool-heavy turns can take 5+ minutes without emitting
	// an EventTokens that would update LastTurnAt).
	LastAgentAPIAt time.Time

	// Recovery tracking
	RecoveryAttempts    int
	RecoveryWindowStart time.Time

	// Ack nudge tracking
	AckNudges map[string]*AckNudgeState

	// Progress tracking
	ProgressChecks map[string]time.Time

	// Whiteboard
	WhiteboardNote *WhiteboardNote

	// Personality override
	PersonalityOverride *PersonalityOverride

	// R35 · Per-turn assistant-text buffer. Accumulated from
	// EventOutput, flushed on EventIdle, forwarded to Telegram when
	// a /lead or /ask prompt is waiting on a reply from this session.
	turnTextBuf strings.Builder

	// Cost + token aggregation, fed by the SSE event forwarder on every
	// pty.EventTokens. Enables per-agent spend and token displays in the
	// office UI without a new endpoint -- exposed via BuildMemberPayload.
	TotalCostUSD      float64
	TotalInputTokens  int64
	TotalOutputTokens int64
	TurnCount         int64
	LastTurnAt        time.Time
	// Last ~20 per-turn cost values (newest last). Powers the per-card
	// sparkline and agent-detail cost chart. Bounded at recentTurnCostsCap.
	RecentTurnCosts []float64
	// R19 · Snapshot of the SDK session's dropped-event counter, synced
	// on every heartbeat (Idle/Working/Tokens). Non-zero means the
	// outbound events channel overflowed at some point and the
	// dashboard may have stale data. Surfaced on the member card.
	EventsDropped uint64

	// R20 · Output-repeat loop advisory. loopDetect is the rolling
	// hash ring; InLoop is a snapshot flag flipped on when the ring
	// detects 3+ matches within 5 chunks. Advisory only — no
	// auto-action. Exposed on the member payload so the card can
	// show a "⟳ loop?" badge.
	loopDetect *loopDetector
	InLoop     bool

	// R21 · Tool-level loop advisory. Higher-fidelity signal than
	// the text-level detector — keyed on (toolName, resultHash)
	// tuples so only genuine re-runs of the same tool producing the
	// same output count. Dashboard shows a red "⟲ <tool>" badge
	// instead of the orange text-level one.
	toolLoopDetect *toolLoopDetector
	ToolInLoop     bool
	LoopingTool    string

	// R21 code review (third pass) · Idempotency guard for stop-
	// broadcast paths. Both OfficeAgentKill and
	// checkForWedgedWorkingAgents emit distinct SSE events
	// (office:agentKilled vs office:agentIdleStopped) and then call
	// UnregisterAgent. If they race (operator clicks kill at the
	// exact moment the wedge detector fires), both toasts hit the
	// dashboard for one stop. ClaimStopBroadcast returns true
	// exactly once per agent so the loser of the race skips its
	// broadcast without affecting the UnregisterAgent cleanup
	// (which is already idempotent via the !ok guard).
	stopBroadcastOnce sync.Once

	// deathOnce makes Manager.HandleAgentDeath's side effects (task
	// reassignment, dev-instance kill, agentDied broadcast) run AT MOST ONCE
	// per agent. HandleAgentDeath is reachable concurrently for the same
	// sessionID from the event-forwarder EventExit, the idle-monitor health
	// check, and the claude-hooks SessionEnd dispatcher; its GetAgent!=nil
	// guard is a non-atomic check-then-act, so without this two callers both
	// reassign every pending task (doubling the strategic queue) and twice
	// SIGTERM the dev-instance process tree.
	deathOnce sync.Once
}

// ClaimDeath returns true on the first call per agent and false thereafter.
// HandleAgentDeath checks this before its reassignment/dev-kill/broadcast work
// so concurrent death notifications for the same sessionID don't double-apply
// those side effects.
func (a *AgentState) ClaimDeath() bool {
	claimed := false
	a.deathOnce.Do(func() { claimed = true })
	return claimed
}

// ClaimStopBroadcast returns true on the first call per agent and
// false thereafter. Callers about to emit a stop-reason SSE event
// (office:agentKilled, office:agentIdleStopped) should check this
// first and skip the broadcast if it returns false — the other
// path already fired one.
func (a *AgentState) ClaimStopBroadcast() bool {
	claimed := false
	a.stopBroadcastOnce.Do(func() { claimed = true })
	return claimed
}

// recentTurnCostsCap bounds the per-agent recent-turn cost history.
const recentTurnCostsCap = 20

// NewAgentState creates a new agent state.
func NewAgentState(sessionID, configID string, config *TeamMemberConfig) *AgentState {
	a := &AgentState{
		ConfigID:       configID,
		Config:         config,
		taskQueue:      &TaskQueue{Tasks: make([]*Task, 0)},
		AckNudges:      make(map[string]*AckNudgeState),
		ProgressChecks: make(map[string]time.Time),
	}
	a.setSessionID(sessionID)
	return a
}

// SessionID returns the agent's current session ID (lock-free atomic read; safe
// to call from any goroutine in any lock context).
func (a *AgentState) SessionID() string {
	if p := a.sessionID.Load(); p != nil {
		return *p
	}
	return ""
}

// setSessionID atomically updates the agent's session ID. Called at construction
// and by ReassignSessionID during an in-place restart.
func (a *AgentState) setSessionID(s string) {
	a.sessionID.Store(&s)
}

// TouchAgentAPI records that we just received a call from this agent.
// Called by every /api/office/agent/* handler so the stuck-detector
// can tell a truly-frozen agent from one that's in a long tool-heavy
// turn and thus quiet on EventTokens.
func (a *AgentState) TouchAgentAPI() {
	a.mu.Lock()
	a.LastAgentAPIAt = time.Now()
	a.mu.Unlock()
}

// ActiveTask returns a DEEP COPY of the first in_progress task, or nil. A copy
// (not the live pointer) so callers that read its fields outside a.mu don't race
// the in-place task mutators. No caller mutates via the return (mutators act on
// a.taskQueue.Tasks directly), so copying is behavior-preserving.
func (a *AgentState) ActiveTask() *Task {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, t := range a.taskQueue.Tasks {
		if t.Status == TaskInProgress {
			return deepCopyTask(t)
		}
	}
	return nil
}

// deepCopyTask returns a fully independent copy of t (all reference fields
// cloned) so a marshaled snapshot can't race in-place mutations of the live
// task under a.mu.
func deepCopyTask(t *Task) *Task {
	if t == nil {
		return nil
	}
	cp := *t
	if t.BlockedBy != nil {
		cp.BlockedBy = append([]string(nil), t.BlockedBy...)
	}
	if t.Labels != nil {
		cp.Labels = append([]string(nil), t.Labels...)
	}
	if t.AcceptanceCriteria != nil {
		cp.AcceptanceCriteria = append([]string(nil), t.AcceptanceCriteria...)
	}
	if t.ProgressLog != nil {
		cp.ProgressLog = append([]TaskProgressEntry(nil), t.ProgressLog...)
	}
	if t.Handoff != nil {
		h := *t.Handoff
		cp.Handoff = &h
	}
	return &cp
}

// ActiveTaskCopy returns a DEEP COPY of the first in_progress task for safe
// marshaling into payloads (the live pointer is mutated under a.mu).
func (a *AgentState) ActiveTaskCopy() *Task {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, t := range a.taskQueue.Tasks {
		if t.Status == TaskInProgress {
			return deepCopyTask(t)
		}
	}
	return nil
}

// GetTasksCopy returns DEEP COPIES of all tasks, safe to marshal outside a.mu.
func (a *AgentState) GetTasksCopy() []*Task {
	a.mu.Lock()
	defer a.mu.Unlock()
	result := make([]*Task, len(a.taskQueue.Tasks))
	for i, t := range a.taskQueue.Tasks {
		result[i] = deepCopyTask(t)
	}
	return result
}

// GetTaskLabels returns a copy of the Labels for the task with the given ID,
// read under a.mu. Use this instead of reading t.Labels off a GetTasks() live
// pointer from another goroutine -- SetTaskLabels mutates it under a.mu.
func (a *AgentState) GetTaskLabels(taskID string) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, t := range a.taskQueue.Tasks {
		if t != nil && t.ID == taskID {
			return append([]string(nil), t.Labels...)
		}
	}
	return nil
}

// PendingTaskCount returns the number of pending/acknowledged tasks.
func (a *AgentState) PendingTaskCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	count := 0
	for _, t := range a.taskQueue.Tasks {
		if t.Status == TaskPending || t.Status == TaskAcknowledged {
			count++
		}
	}
	return count
}

// NextPendingTask returns the first pending/acknowledged task, or nil.
func (a *AgentState) NextPendingTask() *Task {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, t := range a.taskQueue.Tasks {
		if t.Status == TaskPending || t.Status == TaskAcknowledged {
			// Deep copy: callers read fields outside a.mu and dispatch by ID; no
			// caller mutates via this pointer.
			return deepCopyTask(t)
		}
	}
	return nil
}

// AddTask appends a task to the agent's queue under the mutex.
func (a *AgentState) AddTask(task *Task) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.taskQueue.Tasks = append(a.taskQueue.Tasks, task)
}

// TaskGateInfo captures the completion-gate inputs for a task under the
// mutex, returning a copied criteria slice so callers never read live
// task fields while the lock is released.
func (a *AgentState) TaskGateInfo(taskID string) (criteria []string, gated bool, rejectCount int, found bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, t := range a.taskQueue.Tasks {
		if t != nil && t.ID == taskID {
			criteria = append([]string(nil), t.AcceptanceCriteria...)
			gated = TaskHasGatedLabel(t)
			rejectCount = t.CriteriaRejectCount
			return criteria, gated, rejectCount, true
		}
	}
	return nil, false, 0, false
}

// GetTasks returns a shallow copy of the task list under the mutex.
// GetTasks returns DEEP COPIES of all tasks under the mutex. It previously
// returned a shallow copy (live *Task pointers), which made every caller that
// read a task field outside a.mu (the recovery ticker, telegram poll, payload
// marshalers, prompt builders, ...) a data race against the in-place task
// mutators. Deep-copying here closes that whole class structurally -- the ONLY
// caller that needed the live pointer (InitiateHandoff) was reworked to operate
// on a.taskQueue.Tasks directly under the lock. Mutators (AddTask,
// UpdateTaskStatus, SetTaskProgress, ...) all act on a.taskQueue.Tasks directly,
// so none depend on this returning live pointers.
func (a *AgentState) GetTasks() []*Task {
	a.mu.Lock()
	defer a.mu.Unlock()
	result := make([]*Task, len(a.taskQueue.Tasks))
	for i, t := range a.taskQueue.Tasks {
		result[i] = deepCopyTask(t)
	}
	return result
}

// UpdateTaskStatus updates the status of a task by ID. Returns true if found.
func (a *AgentState) UpdateTaskStatus(taskID string, status TaskStatus) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, t := range a.taskQueue.Tasks {
		if t.ID == taskID {
			a.applyTaskStatusLocked(t, status)
			return true
		}
	}
	return false
}

// applyTaskStatusLocked writes the status + timestamps + R36 cost
// snapshot onto the given task pointer. Caller MUST hold a.mu —
// the *Locked suffix is the contract.
//
// Extracted so CompleteTask (which needs lookup+mutation under one
// lock acquisition) can reuse the bookkeeping without re-acquiring
// a.mu through the public UpdateTaskStatus.
func (a *AgentState) applyTaskStatusLocked(t *Task, status TaskStatus) {
	t.Status = status
	now := time.Now()
	if status == TaskInProgress && t.StartedAt.IsZero() {
		t.StartedAt = now
		// R36 · Snapshot agent cost + turn count for per-task
		// rollups. The delta against these at completion feeds the
		// cost-by-label view in Insights.
		t.StartCostUSD = a.TotalCostUSD
		t.StartTurnCount = a.TurnCount
	}
	if status == TaskCompleted || status == TaskFailed {
		t.CompletedAt = now
		// R36 · Final delta. If the task never entered
		// TaskInProgress (direct pending->failed), the Start*
		// zero values mean FinalCostUSD == current cumulative —
		// close enough to "nothing tracked" that a downstream
		// filter on StartedAt.IsZero() can drop it from aggregates.
		t.FinalCostUSD = a.TotalCostUSD - t.StartCostUSD
		t.FinalTurnCount = a.TurnCount - t.StartTurnCount
	}
	// Reclaim per-task side-channel state whenever the task leaves
	// TaskInProgress. Previously only the auto-complete path in
	// recovery.go cleared ProgressChecks; normal completions, fails,
	// and blocks left entries stranded — a slow per-agent leak.
	if status != TaskInProgress {
		delete(a.ProgressChecks, t.ID)
		delete(a.AckNudges, t.ID)
	}
}

// CompleteTaskResult bundles the metadata OfficeAgentTaskComplete
// needs after applying the status mutation. Lets the handler decide
// whether to fire SSE / ultrareview / re-tasking without a second
// scan of the task queue.
type CompleteTaskResult struct {
	// Found is true when a task with the requested ID exists. When
	// false the caller should write a no-op response.
	Found bool

	// AlreadyComplete is true when the task was already in
	// TaskCompleted on entry; mutation was skipped. Caller should
	// short-circuit so repeated `cm task complete` calls don't
	// re-fire broadcasts (observed 6× re-fire in prod before the
	// idempotency guard).
	AlreadyComplete bool

	// Task is the pointer captured during the lookup. Safe to read
	// post-mutation because the lock is dropped only after the
	// status update completes.
	Task *Task

	// Title is the task's title at lookup time. Captured because the
	// screenshot-gate check needs it even after the task moves out
	// of the active set.
	Title string
}

// CompleteTask is the single-lock equivalent of:
//  1. iterating GetTasks() to capture title + prior status,
//  2. short-circuiting when already TaskCompleted,
//  3. calling UpdateTaskStatus(taskID, TaskCompleted).
//
// Captures snapshot + applies update under one mutex acquisition so
// no concurrent writer can land between the lookup and the
// mutation. Caller (handler) uses the result to drive SSE,
// ultrareview, and re-tasking branches.
func (a *AgentState) CompleteTask(taskID string) CompleteTaskResult {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, t := range a.taskQueue.Tasks {
		if t == nil || t.ID != taskID {
			continue
		}
		if t.Status == TaskCompleted {
			return CompleteTaskResult{Found: true, AlreadyComplete: true, Task: t, Title: t.Title}
		}
		title := t.Title
		a.applyTaskStatusLocked(t, TaskCompleted)
		return CompleteTaskResult{Found: true, Task: t, Title: title}
	}
	return CompleteTaskResult{}
}

// SetTaskProgress appends a progress note to the task's ProgressLog and
// mirrors it onto Description for backward compat with readers that only
// look at the current state. Log is ring-buffered at taskProgressLogCap.
// Returns true if the task was found.
func (a *AgentState) SetTaskProgress(taskID string, progress string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, t := range a.taskQueue.Tasks {
		if t.ID == taskID {
			t.Description = progress
			t.ProgressLog = append(t.ProgressLog, TaskProgressEntry{
				Timestamp: time.Now(),
				Note:      progress,
			})
			if len(t.ProgressLog) > taskProgressLogCap {
				t.ProgressLog = t.ProgressLog[len(t.ProgressLog)-taskProgressLogCap:]
			}
			return true
		}
	}
	return false
}

// SetTaskBlockerDescription sets Task.BlockerDescription under the agent lock.
// Returns true if the task was found.
func (a *AgentState) SetTaskBlockerDescription(taskID string, desc string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, t := range a.taskQueue.Tasks {
		if t.ID == taskID {
			t.BlockerDescription = desc
			return true
		}
	}
	return false
}

// turnTextBufCap bounds the per-agent turn-text buffer so a runaway
// assistant message can't balloon memory. 128 KB is far beyond any
// reasonable single turn's final text output but well under the cost
// of letting it grow unchecked.
const turnTextBufCap = 128 * 1024

// AppendTurnText buffers a chunk of assistant text for the current
// turn. Called by the SDK event forwarder on every pty.EventOutput
// so the full turn's text is available at EventIdle for forwarding
// to Telegram (R35 lead-reply flow). Hard-caps at turnTextBufCap to
// bound memory; overflow is silently dropped (rare; the truncation
// pill in renderLeadReply covers this case for users).
func (a *AgentState) AppendTurnText(s string) {
	if s == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.turnTextBuf.Len()+len(s) > turnTextBufCap {
		remaining := turnTextBufCap - a.turnTextBuf.Len()
		if remaining > 0 {
			a.turnTextBuf.WriteString(s[:remaining])
		}
		return
	}
	a.turnTextBuf.WriteString(s)
}

// FlushTurnText returns the accumulated turn text and clears the
// buffer. Called by the SDK forwarder on every pty.EventIdle.
func (a *AgentState) FlushTurnText() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.turnTextBuf.String()
	a.turnTextBuf.Reset()
	return s
}

// RecordTurnUsage accumulates token + cost from a single turn. Called from
// the SSE event forwarder on every pty.EventTokens. Thread-safe; appends to
// the bounded RecentTurnCosts ring so sparklines can render without a
// separate tracker.
func (a *AgentState) RecordTurnUsage(input, output int64, cost float64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.TotalInputTokens += input
	a.TotalOutputTokens += output
	a.TotalCostUSD += cost
	a.TurnCount++
	a.LastTurnAt = time.Now()
	a.RecentTurnCosts = append(a.RecentTurnCosts, cost)
	if len(a.RecentTurnCosts) > recentTurnCostsCap {
		a.RecentTurnCosts = a.RecentTurnCosts[len(a.RecentTurnCosts)-recentTurnCostsCap:]
	}
}

// UsageSnapshot returns a copy-safe view of aggregated usage for use in
// member payloads + dashboards.
type UsageSnapshot struct {
	TotalCostUSD      float64   `json:"totalCostUSD"`
	TotalInputTokens  int64     `json:"totalInputTokens"`
	TotalOutputTokens int64     `json:"totalOutputTokens"`
	TurnCount         int64     `json:"turnCount"`
	LastTurnAt        time.Time `json:"lastTurnAt,omitempty"`
	RecentTurnCosts   []float64 `json:"recentTurnCosts,omitempty"`
	EventsDropped     uint64    `json:"eventsDropped,omitempty"`
	InLoop            bool      `json:"inLoop,omitempty"`
	ToolInLoop        bool      `json:"toolInLoop,omitempty"`
	LoopingTool       string    `json:"loopingTool,omitempty"`
}

// Usage returns a snapshot of the agent's cumulative cost + token counts.
func (a *AgentState) Usage() UsageSnapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	recent := make([]float64, len(a.RecentTurnCosts))
	copy(recent, a.RecentTurnCosts)
	return UsageSnapshot{
		TotalCostUSD:      a.TotalCostUSD,
		TotalInputTokens:  a.TotalInputTokens,
		TotalOutputTokens: a.TotalOutputTokens,
		TurnCount:         a.TurnCount,
		LastTurnAt:        a.LastTurnAt,
		RecentTurnCosts:   recent,
		EventsDropped:     a.EventsDropped,
		InLoop:            a.InLoop,
		ToolInLoop:        a.ToolInLoop,
		LoopingTool:       a.LoopingTool,
	}
}

// SetEventsDropped syncs the SDK-session-level drop counter into the
// agent so BuildMemberPayload can surface it without poking the pty
// package. Called from the SSE forwarder on each heartbeat event.
func (a *AgentState) SetEventsDropped(n uint64) {
	a.mu.Lock()
	a.EventsDropped = n
	a.mu.Unlock()
}

// SetSDKWorking sets the SDK working flag under the mutex.
func (a *AgentState) SetSDKWorking(working bool) {
	a.mu.Lock()
	a.SDKWorking = working
	a.mu.Unlock()
}

// SetWorktreePath updates the agent's working-directory bookkeeping
// under the agent mutex and returns the previous value. R26
// concurrency review: WorktreePath was being written raw by the
// workdir-bump handler, racing with any goroutine reading it.
func (a *AgentState) SetWorktreePath(path string) (previous string) {
	a.mu.Lock()
	previous = a.WorktreePath
	a.WorktreePath = path
	a.mu.Unlock()
	return
}

// GetWorktreePath returns the agent's current working-directory
// bookkeeping under the agent mutex. Paired with SetWorktreePath to
// keep the field data-race-free across goroutines.
func (a *AgentState) GetWorktreePath() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.WorktreePath
}

// SetProvider records the LLM provider + model string the agent was spawned
// with. Safe to call from the spawn handler; read back by BuildMemberPayload.
func (a *AgentState) SetProvider(provider, model string) {
	a.mu.Lock()
	a.Provider = provider
	a.Model = model
	a.mu.Unlock()
}

// SetSessionMode records which spawn mode produced this agent ("sdk",
// "hooks", "legacy"). Used by BuildMemberPayload to surface the "via
// {mode}" badge on each agent's dashboard card so operators can see at
// a glance which billing path is active.
func (a *AgentState) SetSessionMode(mode string) {
	a.mu.Lock()
	a.SessionMode = mode
	a.mu.Unlock()
}

// SetActiveTool records the name of the tool the agent is currently invoking.
// Pass "" to clear. Used by the SDK event forwarder.
func (a *AgentState) SetActiveTool(name string) {
	a.mu.Lock()
	a.ActiveTool = name
	a.mu.Unlock()
}

// GetSDKWorking reads the SDK working flag under the mutex.
func (a *AgentState) GetSDKWorking() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.SDKWorking
}

// GetPersonalityOverride returns a snapshot of the override under the mutex.
func (a *AgentState) GetPersonalityOverride() *PersonalityOverride {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.PersonalityOverride == nil {
		return nil
	}
	copied := *a.PersonalityOverride
	return &copied
}

// SetPersonalityOverride sets the override under the mutex.
func (a *AgentState) SetPersonalityOverride(override *PersonalityOverride) {
	a.mu.Lock()
	a.PersonalityOverride = override
	a.mu.Unlock()
}

// SetVoice sets the member's TTS voice under the mutex.
func (a *AgentState) SetVoice(voice string) {
	a.mu.Lock()
	if a.Config != nil {
		a.Config.Voice = voice
	}
	a.mu.Unlock()
}

// GetVoice returns the member's TTS voice under the mutex. Use this rather
// than reading a.Config.Voice directly from another goroutine -- SetVoice
// writes it under the lock, so an unguarded cross-goroutine read is a data
// race.
func (a *AgentState) GetVoice() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.Config == nil {
		return ""
	}
	return a.Config.Voice
}

// SnapshotConfig returns a value copy of the member's Config taken under the
// mutex. Use this instead of dereferencing *a.Config directly from another
// goroutine: SetVoice mutates a.Config.Voice under a.mu, so an unguarded
// struct copy races that write. Returns a zero Config when none is set.
func (a *AgentState) SnapshotConfig() TeamMemberConfig {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.Config == nil {
		return TeamMemberConfig{}
	}
	return *a.Config
}

// RemoveTask removes a task by ID and returns it, or nil if not found.
func (a *AgentState) RemoveTask(taskID string) *Task {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i, t := range a.taskQueue.Tasks {
		if t.ID == taskID {
			a.taskQueue.Tasks = append(a.taskQueue.Tasks[:i], a.taskQueue.Tasks[i+1:]...)
			return t
		}
	}
	return nil
}
