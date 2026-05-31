package office

// R29 · Structured agent-to-agent task handoffs.
//
// When agent A wants to pass task T to agent B, A calls InitiateHandoff.
// The task stays physically in A's queue but transitions to status
// `handoff_pending` and the Task.Handoff pointer is set. B sees the
// pending handoff via its queue payload (extended by the agent/tasks
// handler in web/handler/office.go) and calls AcceptHandoff or
// RejectHandoff.
//
// Accept: the task is removed from A's queue and appended to B's
// queue with Source=HandoffSource. Status reverts to in_progress; the
// handoff record is appended to the audit log and persisted.
//
// Reject: the task stays with A, status reverts to in_progress, A
// receives a chat message so the rejection isn't buried.
//
// Stale sweep: recovery.go periodically calls SweepStaleHandoffs which
// auto-rejects handoffs whose receiver has left the roster. A 15-min
// threshold is default; configurable via
// ~/.jarvis/office-memory/handoff-stale-seconds.txt.

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// handoffRecheckHook is a test-only seam fired by AcceptHandoff,
// CancelHandoff, and finishReject AFTER resolveHandoffPair returns but
// BEFORE the sender-lock re-acquire. Production leaves it nil (a no-op).
// Tests use it to deterministically simulate a concurrent resolver that
// flips the live handoff status between the lock-free resolve and the
// locked re-check, exercising the "handoff no longer pending" guards
// without an inherently racy goroutine schedule. Behaviour-preserving:
// when nil the call is skipped entirely.
var handoffRecheckHook func(taskID string)

func fireHandoffRecheckHook(taskID string) {
	if handoffRecheckHook != nil {
		handoffRecheckHook(taskID)
	}
}

// HandoffStatus is the lifecycle state of a HandoffRequest.
type HandoffStatus string

const (
	HandoffPending   HandoffStatus = "pending"
	HandoffAccepted  HandoffStatus = "accepted"
	HandoffRejected  HandoffStatus = "rejected"
	HandoffExpired   HandoffStatus = "expired"
	HandoffCancelled HandoffStatus = "cancelled" // R32 · sender retracted before resolution
)

// HandoffSource tags a task's Source field once the task has been
// accepted via handoff. "handoff" is distinct from "reassigned"
// (HandleAgentDeath uses the latter) so Insights / Timeline can show
// the two flows separately.
const HandoffSource = "handoff"

// HandoffRequest records a single handoff attempt.
type HandoffRequest struct {
	ID            string        `json:"id"`
	TaskID        string        `json:"taskId"`
	TaskTitle     string        `json:"taskTitle"`
	FromSessionID string        `json:"fromSessionId"`
	FromConfigID  string        `json:"fromConfigId"`
	FromName      string        `json:"fromName,omitempty"`
	ToSessionID   string        `json:"toSessionId"`
	ToConfigID    string        `json:"toConfigId"`
	ToName        string        `json:"toName,omitempty"`
	Context       string        `json:"context"`
	Status        HandoffStatus `json:"status"`
	CreatedAt     time.Time     `json:"createdAt"`
	ResolvedAt    time.Time     `json:"resolvedAt,omitempty"`
	RejectReason  string        `json:"rejectReason,omitempty"`
}

const (
	// HandoffContextLimit caps the handoff context payload. Context is
	// a hint to the receiver, not a memory dump — deliberately tight so
	// agents don't try to paste entire files.
	HandoffContextLimit = 2 * 1024
	// HandoffMaxPendingPerSender stops an agent from mass-dumping work.
	HandoffMaxPendingPerSender = 5
	// HandoffAuditCap bounds the persisted audit file (front-drop).
	HandoffAuditCap = 1000
	// DefaultHandoffStale is the default stale-sweep threshold. An
	// override can live at ~/.jarvis/office-memory/handoff-stale-seconds.txt.
	DefaultHandoffStale = 15 * time.Minute
)

// Broadcast event names — consumers in office.js subscribe to these.
const (
	EventHandoffInitiated = "office:handoffInitiated"
	EventHandoffAccepted  = "office:handoffAccepted"
	EventHandoffRejected  = "office:handoffRejected"
	EventHandoffExpired   = "office:handoffExpired"
	EventHandoffCancelled = "office:handoffCancelled" // R32 · sender retracted
)

// handoffStore is the Manager's persistent audit log for handoffs.
// Appended to on every resolve (accept/reject/expire). Not guarded by
// Manager.mu — it has its own mutex because the audit write path
// doesn't need the full manager lock.
//
// One store per Manager (lazily initialised on first resolve) so
// tests that shadow HOME with t.Setenv + t.TempDir see a clean state
// without a global reset dance.
type handoffStore struct {
	mu      sync.Mutex
	loaded  bool
	entries []HandoffRequest
}

func (m *Manager) ensureHandoffStore() *handoffStore {
	m.handoffOnce.Do(func() {
		m.handoff = &handoffStore{
			entries: loadPersistedHandoffs(),
			loaded:  true,
		}
	})
	return m.handoff
}

func handoffsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".jarvis", "office-memory", "handoffs.json")
}

// loadHandoffStaleThreshold reads an override from
// ~/.jarvis/office-memory/handoff-stale-seconds.txt, falling back to
// DefaultHandoffStale. Parsing or read failures fall back silently —
// this is a power-user escape hatch, not a critical setting.
func loadHandoffStaleThreshold() time.Duration {
	home, err := os.UserHomeDir()
	if err != nil {
		return DefaultHandoffStale
	}
	p := filepath.Join(home, ".jarvis", "office-memory", "handoff-stale-seconds.txt")
	data, err := os.ReadFile(p)
	if err != nil {
		return DefaultHandoffStale
	}
	var seconds int
	if _, err := fmt.Sscanf(string(data), "%d", &seconds); err != nil || seconds <= 0 {
		return DefaultHandoffStale
	}
	return time.Duration(seconds) * time.Second
}

// loadPersistedHandoffs reads the audit log. Missing file = empty
// slice. Parse failures quarantine the file the same way other
// persisted stores do (via quarantineFile helper in persistence.go).
func loadPersistedHandoffs() []HandoffRequest {
	p := handoffsPath()
	if p == "" {
		return nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var out []HandoffRequest
	if err := json.Unmarshal(data, &out); err != nil {
		ts := time.Now().Unix()
		corrupt := fmt.Sprintf("%s.corrupt-%d", p, ts)
		if renameErr := os.Rename(p, corrupt); renameErr == nil {
			slog.Error("quarantined corrupt handoffs.json", "path", p, "corrupt", corrupt, "parseErr", err)
		} else {
			slog.Warn("failed to quarantine corrupt handoffs.json", "path", p, "err", err, "renameErr", renameErr)
		}
		return nil
	}
	return out
}

// savePersistedHandoffs writes the audit log atomically.
func savePersistedHandoffs(entries []HandoffRequest) error {
	p := handoffsPath()
	if p == "" {
		return fmt.Errorf("no home dir")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// appendHandoffAudit adds a resolved handoff to the audit log.
func (s *handoffStore) append(h HandoffRequest) {
	s.mu.Lock()
	s.entries = append(s.entries, h)
	// Front-drop once we exceed the cap. Same pattern as taskProgressLogCap.
	if len(s.entries) > HandoffAuditCap {
		s.entries = s.entries[len(s.entries)-HandoffAuditCap:]
	}
	snapshot := make([]HandoffRequest, len(s.entries))
	copy(snapshot, s.entries)
	s.mu.Unlock()
	if err := savePersistedHandoffs(snapshot); err != nil {
		slog.Warn("failed to persist handoff audit", "err", err)
	}
}

// snapshot returns a copy of the current audit log for UI listing.
func (s *handoffStore) snapshot() []HandoffRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]HandoffRequest, len(s.entries))
	copy(out, s.entries)
	return out
}

// ---------------------------------------------------------------------------
// Manager methods
// ---------------------------------------------------------------------------

// InitiateHandoff begins a handoff from the task's current owner to the
// agent identified by toConfigID. Validates ownership, roster
// membership, pending-limit, context size. Returns the created
// HandoffRequest or an error suitable for surfacing to an agent-facing
// HTTP response.
func (m *Manager) InitiateHandoff(fromSessionID, taskID, toConfigID, context string) (*HandoffRequest, error) {
	if len(context) > HandoffContextLimit {
		return nil, fmt.Errorf("context too large: %d bytes (limit %d)", len(context), HandoffContextLimit)
	}
	sender := m.GetAgent(fromSessionID)
	if sender == nil {
		return nil, fmt.Errorf("sender agent not found")
	}
	receiver := m.GetAgentByConfig(toConfigID)
	if receiver == nil {
		return nil, fmt.Errorf("receiver not in roster")
	}
	if receiver.SessionID() == fromSessionID {
		return nil, fmt.Errorf("cannot hand off to self")
	}

	fromName := ""
	if sender.Config != nil {
		fromName = sender.Config.Name
	}
	toName := ""
	if receiver.Config != nil {
		toName = receiver.Config.Name
	}

	// Find the target, count pending handoffs, validate, build the request, and
	// commit -- ALL under sender.mu in one critical section. This makes the
	// pending-count + existence + status checks atomic with the mutation (two
	// concurrent initiates can't both pass and clobber each other's Handoff),
	// AND removes the only place that needed an escaped live *Task pointer from
	// GetTasks() -- so GetTasks() now returns deep copies for every (read-only)
	// caller, eliminating that whole live-pointer race class.
	sender.mu.Lock()
	pending := 0
	var targetTask *Task
	for _, t := range sender.taskQueue.Tasks {
		if t.Status == TaskHandoffPending && t.Handoff != nil {
			pending++
		}
		if t.ID == taskID {
			targetTask = t
		}
	}
	if targetTask == nil {
		sender.mu.Unlock()
		return nil, fmt.Errorf("task not found")
	}
	if pending >= HandoffMaxPendingPerSender {
		sender.mu.Unlock()
		return nil, fmt.Errorf("too many pending handoffs (limit %d)", HandoffMaxPendingPerSender)
	}
	if targetTask.Handoff != nil {
		sender.mu.Unlock()
		return nil, fmt.Errorf("task already has a pending handoff")
	}
	if targetTask.Status != TaskInProgress {
		status := targetTask.Status
		sender.mu.Unlock()
		return nil, fmt.Errorf("task must be in_progress to hand off (current: %s)", status)
	}
	h := &HandoffRequest{
		ID:            fmt.Sprintf("ho-%d", time.Now().UnixNano()),
		TaskID:        taskID,
		TaskTitle:     targetTask.Title,
		FromSessionID: fromSessionID,
		FromConfigID:  sender.ConfigID,
		FromName:      fromName,
		ToSessionID:   receiver.SessionID(),
		ToConfigID:    receiver.ConfigID,
		ToName:        toName,
		Context:       context,
		Status:        HandoffPending,
		CreatedAt:     time.Now(),
	}
	targetTask.Status = TaskHandoffPending
	targetTask.Handoff = h
	targetTask.ProgressLog = append(targetTask.ProgressLog, TaskProgressEntry{
		Timestamp: h.CreatedAt,
		Note:      fmt.Sprintf("[handoff → %s] %s", toName, context),
	})
	if len(targetTask.ProgressLog) > taskProgressLogCap {
		targetTask.ProgressLog = targetTask.ProgressLog[len(targetTask.ProgressLog)-taskProgressLogCap:]
	}
	sender.mu.Unlock()

	m.auditLog("office:handoffInitiated", fromSessionID, fromName, map[string]interface{}{
		"taskId": taskID, "to": receiver.SessionID(), "toName": toName,
	})
	m.broadcast(EventHandoffInitiated, map[string]interface{}{
		"handoff": h,
	})
	return h, nil
}

// AcceptHandoff finalises a pending handoff: task moves from sender's
// queue to receiver's queue with Source=HandoffSource. Called by the
// receiver (via CLI or force-accept). Returns the accepted request.
func (m *Manager) AcceptHandoff(receiverSessionID, taskID string) (*HandoffRequest, error) {
	h, _, sender, receiver, err := m.resolveHandoffPair(taskID, "")
	if err != nil {
		return nil, err
	}
	if h.ToSessionID != receiverSessionID {
		return nil, fmt.Errorf("not your handoff")
	}

	// Atomically re-verify, CLAIM, and remove the task under the sender's mutex.
	// resolveHandoffPair's pending-check ran lock-free, so two concurrent accepts
	// (receiver CLI + operator force-accept) could both pass it and both
	// copy+AddTask the task into the receiver's queue -> duplicate. Re-checking
	// the LIVE task's handoff status under sender.mu and flipping it to
	// HandoffAccepted in the same critical section makes the loser observe the
	// claimed status (or a missing task) and bail.
	fireHandoffRecheckHook(taskID)
	sender.mu.Lock()
	idx := -1
	for i, t := range sender.taskQueue.Tasks {
		if t.ID == taskID {
			idx = i
			break
		}
	}
	if idx == -1 {
		sender.mu.Unlock()
		return nil, fmt.Errorf("handoff task no longer in sender queue")
	}
	live := sender.taskQueue.Tasks[idx]
	if live.Handoff == nil || live.Handoff.Status != HandoffPending {
		sender.mu.Unlock()
		return nil, fmt.Errorf("handoff no longer pending")
	}
	resolvedAt := time.Now()
	live.Handoff.Status = HandoffAccepted // claim — blocks a concurrent accept's re-check
	live.Handoff.ResolvedAt = resolvedAt
	// Snapshot the resolved handoff record UNDER the lock. The previous code
	// re-wrote h.Status/h.ResolvedAt after Unlock, which raced a concurrent
	// finishReject mutating the same live *HandoffRequest. live.Handoff is the
	// same pointer as h, so stamping + copying it here is the single
	// synchronised mutation; the append/broadcast/return all use the copy.
	resolved := *live.Handoff
	copied := *live
	// Remove from sender queue while still holding the lock.
	sender.taskQueue.Tasks = append(sender.taskQueue.Tasks[:idx], sender.taskQueue.Tasks[idx+1:]...)
	sender.mu.Unlock()

	// Prepare the moved copy for the receiver. Deep-copy ProgressLog so the
	// receiver's append can't mutate the (shared) backing array of the snapshot.
	copied.Handoff = nil
	copied.Status = TaskInProgress
	copied.Source = HandoffSource
	copied.AssignedAt = resolvedAt // "new owner got this now"
	copied.ProgressLog = append(append([]TaskProgressEntry(nil), copied.ProgressLog...), TaskProgressEntry{
		Timestamp: copied.AssignedAt,
		Note:      fmt.Sprintf("[handoff accepted by %s]", receiver.Config.Name),
	})
	if len(copied.ProgressLog) > taskProgressLogCap {
		copied.ProgressLog = copied.ProgressLog[len(copied.ProgressLog)-taskProgressLogCap:]
	}

	receiver.AddTask(&copied)

	m.ensureHandoffStore().append(resolved)

	senderName := ""
	if sender.Config != nil {
		senderName = sender.Config.Name
	}
	m.auditLog("office:handoffAccepted", receiver.SessionID(), receiver.Config.Name, map[string]interface{}{
		"taskId": taskID, "from": sender.SessionID(), "fromName": senderName,
	})
	m.broadcast(EventHandoffAccepted, map[string]interface{}{
		"handoff": &resolved,
	})
	return &resolved, nil
}

// RejectHandoff cancels a pending handoff. The task returns to the
// sender in in_progress status; the sender receives a chat message so
// the rejection isn't silently lost in the activity feed.
func (m *Manager) RejectHandoff(receiverSessionID, taskID, reason string) (*HandoffRequest, error) {
	h, task, sender, receiver, err := m.resolveHandoffPair(taskID, "")
	if err != nil {
		return nil, err
	}
	if h.ToSessionID != receiverSessionID {
		return nil, fmt.Errorf("not your handoff")
	}
	return m.finishReject(h, task, sender, receiver, reason)
}

// CancelHandoff retracts a pending handoff initiated by the caller.
// Ownership is enforced by resolveHandoffPair's requireFromSession
// argument: only the sender of the pending handoff can cancel it.
// On success the task returns to in_progress, the handoff pointer
// clears, and an audit entry with status=cancelled is persisted.
// No chat message -- the sender doesn't need to notify themself.
// R32 · closes the R29 "cancel-pending-handoff by sender" carry-over.
func (m *Manager) CancelHandoff(senderSessionID, taskID string) (*HandoffRequest, error) {
	h, task, sender, _, err := m.resolveHandoffPair(taskID, senderSessionID)
	if err != nil {
		return nil, err
	}

	// Claim + stamp the handoff record under sender.mu (same rationale as
	// finishReject/AcceptHandoff): the live *HandoffRequest must only be
	// mutated under the lock, and the loser of a concurrent resolve bails.
	fireHandoffRecheckHook(taskID)
	sender.mu.Lock()
	if task.Handoff == nil || task.Handoff.Status != HandoffPending {
		sender.mu.Unlock()
		return nil, fmt.Errorf("handoff no longer pending")
	}
	senderName := ""
	if sender.Config != nil {
		senderName = sender.Config.Name
	}
	resolvedAt := time.Now()
	h.Status = HandoffCancelled
	h.ResolvedAt = resolvedAt
	h.RejectReason = "cancelled by sender"
	snapshot := *h
	task.Handoff = nil
	task.Status = TaskInProgress
	task.ProgressLog = append(task.ProgressLog, TaskProgressEntry{
		Timestamp: resolvedAt,
		Note:      fmt.Sprintf("[handoff cancelled by %s]", senderName),
	})
	if len(task.ProgressLog) > taskProgressLogCap {
		task.ProgressLog = task.ProgressLog[len(task.ProgressLog)-taskProgressLogCap:]
	}
	sender.mu.Unlock()

	m.ensureHandoffStore().append(snapshot)

	m.auditLog("office:handoffCancelled", sender.SessionID(), senderName, map[string]interface{}{
		"taskId": snapshot.TaskID, "to": snapshot.ToSessionID, "toName": snapshot.ToName,
	})
	m.broadcast(EventHandoffCancelled, map[string]interface{}{"handoff": &snapshot})
	return &snapshot, nil
}

// ForceAcceptHandoff is the operator-auth variant. Bypasses the
// receiver identity check — used when the receiver is unresponsive
// but the operator has decided the task should transfer.
func (m *Manager) ForceAcceptHandoff(taskID string) (*HandoffRequest, error) {
	h, _, _, receiver, err := m.resolveHandoffPair(taskID, "")
	if err != nil {
		return nil, err
	}
	return m.AcceptHandoff(receiver.SessionID(), h.TaskID)
}

// ForceRejectHandoff is the operator-auth counterpart. Same behaviour
// as RejectHandoff but callable without the receiver's session token.
func (m *Manager) ForceRejectHandoff(taskID, reason string) (*HandoffRequest, error) {
	h, task, sender, receiver, err := m.resolveHandoffPair(taskID, "")
	if err != nil {
		return nil, err
	}
	return m.finishReject(h, task, sender, receiver, reason)
}

// finishReject shared tail for receiver-reject and operator-force-reject.
func (m *Manager) finishReject(h *HandoffRequest, task *Task, sender, receiver *AgentState, reason string) (*HandoffRequest, error) {
	// Claim, mutate the task, and stamp the handoff record ALL under
	// sender.mu. resolveHandoffPair's pending-check ran lock-free, so a
	// concurrent accept/reject could share the same live *HandoffRequest.
	// Re-checking the LIVE handoff's status under the lock (and writing
	// h.Status/ResolvedAt/RejectReason here, not after Unlock) makes the
	// loser of an accept/reject race bail and removes the unsynchronised
	// writes to *h that previously raced with AcceptHandoff. We snapshot
	// *h under the lock so the audit append / broadcast / return value
	// never touch the live pointer after release.
	fireHandoffRecheckHook(task.ID)
	sender.mu.Lock()
	if task.Handoff == nil || task.Handoff.Status != HandoffPending {
		sender.mu.Unlock()
		return nil, fmt.Errorf("handoff no longer pending")
	}
	resolvedAt := time.Now()
	h.Status = HandoffRejected
	h.ResolvedAt = resolvedAt
	h.RejectReason = reason
	task.Handoff = nil
	task.Status = TaskInProgress
	task.ProgressLog = append(task.ProgressLog, TaskProgressEntry{
		Timestamp: resolvedAt,
		Note:      fmt.Sprintf("[handoff rejected by %s: %s]", receiver.Config.Name, reason),
	})
	if len(task.ProgressLog) > taskProgressLogCap {
		task.ProgressLog = task.ProgressLog[len(task.ProgressLog)-taskProgressLogCap:]
	}
	snapshot := *h
	sender.mu.Unlock()

	m.ensureHandoffStore().append(snapshot)

	// Targeted chat message so the sender sees the rejection.
	senderName := ""
	if sender.Config != nil {
		senderName = sender.Config.Name
	}
	receiverName := ""
	if receiver.Config != nil {
		receiverName = receiver.Config.Name
	}
	msg := fmt.Sprintf("%s declined your handoff of task %q: %s", receiverName, snapshot.TaskTitle, reason)
	m.SendMessage(receiver.SessionID(), sender.SessionID(), msg)

	m.auditLog("office:handoffRejected", receiver.SessionID(), receiverName, map[string]interface{}{
		"taskId": snapshot.TaskID, "reason": reason, "to": sender.SessionID(), "toName": senderName,
	})
	m.broadcast(EventHandoffRejected, map[string]interface{}{
		"handoff": &snapshot,
	})
	return &snapshot, nil
}

// resolveHandoffPair looks up the current pending handoff for a task
// and returns (handoff, task, sender, receiver, err).
// If requireFromSession is non-empty, enforces ownership.
func (m *Manager) resolveHandoffPair(taskID, requireFromSession string) (*HandoffRequest, *Task, *AgentState, *AgentState, error) {
	roster := m.GetRoster()
	for _, a := range roster {
		// Scan this agent's tasks UNDER its lock so reading t.Handoff /
		// t.Handoff.Status doesn't race a concurrent handoff mutation (which all
		// happen under a.mu). Capture the LIVE task + handoff pointers to return
		// -- callers (finishReject/AcceptHandoff) re-acquire a.mu to mutate.
		a.mu.Lock()
		var foundTask *Task
		var foundHandoff *HandoffRequest
		for _, t := range a.taskQueue.Tasks {
			if t.ID == taskID && t.Handoff != nil && t.Handoff.Status == HandoffPending {
				foundTask = t
				foundHandoff = t.Handoff
				break
			}
		}
		a.mu.Unlock()
		if foundTask == nil {
			continue
		}
		// FromSessionID/ToSessionID are immutable after the handoff is created,
		// so reading them after releasing a.mu is safe.
		if requireFromSession != "" && foundHandoff.FromSessionID != requireFromSession {
			return nil, nil, nil, nil, fmt.Errorf("not your handoff")
		}
		sender := m.GetAgent(foundHandoff.FromSessionID)
		receiver := m.GetAgent(foundHandoff.ToSessionID)
		if sender == nil || receiver == nil {
			return nil, nil, nil, nil, fmt.Errorf("handoff references missing agent")
		}
		return foundHandoff, foundTask, sender, receiver, nil
	}
	return nil, nil, nil, nil, fmt.Errorf("no pending handoff for task")
}

// ListPendingHandoffs returns all currently-pending handoff requests
// across the roster. Used by the operator-facing endpoint and by the
// stale-sweep tick.
func (m *Manager) ListPendingHandoffs() []*HandoffRequest {
	var out []*HandoffRequest
	for _, a := range m.GetRoster() {
		// GetTasksCopy returns DEEP copies (incl. a copied Handoff), so reading
		// t.Handoff here doesn't race a concurrent mutation of the live task.
		for _, t := range a.GetTasksCopy() {
			if t.Handoff != nil && t.Handoff.Status == HandoffPending {
				// Return a copy so callers can't mutate through the pointer.
				copy := *t.Handoff
				out = append(out, &copy)
			}
		}
	}
	return out
}

// GetHandoffHistory returns the persisted audit log.
func (m *Manager) GetHandoffHistory() []HandoffRequest {
	return m.ensureHandoffStore().snapshot()
}

// IncomingHandoffsFor returns pending handoffs whose receiver is the
// given session. Used by the /api/office/agent/tasks payload so an
// agent polling its queue sees what's been handed to it.
func (m *Manager) IncomingHandoffsFor(sessionID string) []*HandoffRequest {
	var out []*HandoffRequest
	for _, h := range m.ListPendingHandoffs() {
		if h.ToSessionID == sessionID {
			out = append(out, h)
		}
	}
	return out
}

// SweepStaleHandoffs auto-rejects pending handoffs whose receiver has
// left the roster. Called from recovery.go on its regular tick.
// Returns the number of sweeps performed — caller can use this to
// decide whether to emit an operator notification.
//
// Unlike RejectHandoff, the sweep can't rely on resolveHandoffPair
// because the receiver is (by definition) gone — it operates directly
// on the sender's task list and skips the receiver-side plumbing
// (reject chat message, receiver audit fields).
func (m *Manager) SweepStaleHandoffs(threshold time.Duration) int {
	if threshold <= 0 {
		threshold = DefaultHandoffStale
	}
	pending := m.ListPendingHandoffs()
	now := time.Now()
	swept := 0
	for _, h := range pending {
		if now.Sub(h.CreatedAt) < threshold {
			continue
		}
		if recv := m.GetAgent(h.ToSessionID); recv != nil {
			// Receiver is still alive — long handoff is legitimate.
			continue
		}
		sender := m.GetAgent(h.FromSessionID)
		if sender == nil {
			continue
		}
		// Find the live task and clear its handoff pointer, preserving
		// the progress log entry for audit.
		var target *Task
		sender.mu.Lock()
		for _, t := range sender.taskQueue.Tasks {
			if t.ID == h.TaskID && t.Handoff != nil && t.Handoff.Status == HandoffPending {
				target = t
				break
			}
		}
		if target != nil {
			target.Handoff = nil
			target.Status = TaskInProgress
			target.ProgressLog = append(target.ProgressLog, TaskProgressEntry{
				Timestamp: now,
				Note:      "[handoff expired: receiver left roster]",
			})
			if len(target.ProgressLog) > taskProgressLogCap {
				target.ProgressLog = target.ProgressLog[len(target.ProgressLog)-taskProgressLogCap:]
			}
		}
		sender.mu.Unlock()
		if target == nil {
			continue
		}

		// Persist + broadcast.
		expired := *h
		expired.Status = HandoffExpired
		expired.ResolvedAt = now
		expired.RejectReason = "receiver left roster"
		m.ensureHandoffStore().append(expired)

		senderName := ""
		if sender.Config != nil {
			senderName = sender.Config.Name
		}
		m.auditLog("office:handoffExpired", sender.SessionID(), senderName, map[string]interface{}{
			"taskId": h.TaskID, "receiverConfigID": h.ToConfigID,
		})
		m.broadcast(EventHandoffExpired, map[string]interface{}{"handoff": &expired})
		swept++
	}
	return swept
}
