package ralph

import (
	"fmt"
	"log/slog"
	"regexp"
	"runtime/debug"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/kshaham/jarvis/internal/safego"
)

// Configuration constants matching the Node.js implementation.
const (
	maxTodoItems            = 50
	todoExpiryDuration      = 1 * time.Hour
	cleanupThrottle         = 30 * time.Second
	todoSimilarityThreshold = 0.85
	eventDebounceDuration   = 50 * time.Millisecond
	// maxStatusBlockLines caps how many lines an in-progress RALPH_STATUS block
	// may accumulate before we abort it. Without this, an unclosed block (start
	// marker printed but the end marker never arrives in recognizable form)
	// would accumulate every subsequent line of agent-controlled terminal
	// output for the whole session -- unbounded memory growth.
	maxStatusBlockLines = 64
	// maxStatusRemainder bounds the cross-chunk line buffer so a pathological
	// newline-less stream can't grow it without limit.
	maxStatusRemainder = 64 * 1024
	// gateMaxConsecutiveFailures is how many times the objective gate may fail
	// in a row before the tracker flags escalation (Escalate=true on
	// GateCheckFailed). Mirrors the loop-engineering rule that a loop which
	// cannot pass its own gate is stuck and needs a human, not another retry.
	gateMaxConsecutiveFailures = 3
)

// TodoItem represents a single todo item detected from terminal output.
type TodoItem struct {
	ID        string    `json:"id"`
	Content   string    `json:"content"`
	Status    string    `json:"status"` // pending, in_progress, completed, failed, expired
	CreatedAt time.Time `json:"createdAt"`
}

// TrackerState holds the current state of the Ralph tracker for a session.
type TrackerState struct {
	Enabled            bool                 `json:"enabled"`
	CompletionPhrase   string               `json:"completionPhrase"`
	LoopIteration      int                  `json:"loopIteration"`
	MaxIterations      int                  `json:"maxIterations"`
	Todos              []TodoItem           `json:"todos"`
	LastActivityAt     time.Time            `json:"lastActivityAt"`
	CompletionDetected bool                 `json:"completionDetected"`
	CircuitBreaker     CircuitBreakerStatus `json:"circuitBreaker"`
	LastRalphStatus    *RalphStatus         `json:"lastRalphStatus,omitempty"`
	// LastGateResult is the most recent objective-gate outcome. nil until the
	// gate has run at least once. Surfaced so operators can spot-check that the
	// gate actually rejects failing work (loop-engineering: gates rot).
	LastGateResult *GateResult `json:"lastGateResult,omitempty"`
	// GateFailureCount is the number of consecutive objective-gate failures
	// since the last pass. Drives escalation to stuck-agent recovery.
	GateFailureCount int `json:"gateFailureCount"`
}

// TrackerEvent represents an event emitted by the Ralph tracker.
type TrackerEvent interface {
	trackerEventType() string
}

// LoopCompleted is emitted when a completion phrase is detected.
type LoopCompleted struct {
	Phrase    string `json:"phrase"`
	Iteration int    `json:"iteration"`
}

func (LoopCompleted) trackerEventType() string { return "loop_completed" }

// StatusBlockDetected is emitted when a RALPH_STATUS block is parsed.
type StatusBlockDetected struct {
	Status RalphStatus `json:"status"`
}

func (StatusBlockDetected) trackerEventType() string { return "status_block" }

// ExitGateMet is emitted when the dual-condition circuit breaker trips AND the
// objective gate passes (or no gate is configured). It is the only signal that
// legitimately ends a loop.
type ExitGateMet struct {
	CompletionIndicators int  `json:"completionIndicators"`
	ExitSignal           bool `json:"exitSignal"`
}

func (ExitGateMet) trackerEventType() string { return "exit_gate_met" }

// GateCheckFailed is emitted when the dual-condition circuit breaker tripped but
// the objective verify gate (tests/build/lint) failed. The loop must NOT exit:
// the circuit breaker is reset so the agent keeps working until the gate passes.
// This is the core Ralph-Wiggum defense -- a completion phrase cannot override a
// failing test suite. Escalate is set once ConsecutiveFails crosses the cap.
type GateCheckFailed struct {
	Reason           string `json:"reason"`
	Command          string `json:"command"`
	ExitCode         int    `json:"exitCode"`
	ConsecutiveFails int    `json:"consecutiveFails"`
	Escalate         bool   `json:"escalate"`
}

func (GateCheckFailed) trackerEventType() string { return "gate_check_failed" }

// LoopLimitReached is emitted when the loop has completed MaxIterations cycles.
// It is a HARD STOP: the loop has run its configured budget and should pause for
// human review rather than keep spending. Distinct from ExitGateMet (success) --
// reaching the iteration cap is not a success signal.
type LoopLimitReached struct {
	Iteration     int `json:"iteration"`
	MaxIterations int `json:"maxIterations"`
}

func (LoopLimitReached) trackerEventType() string { return "loop_limit_reached" }

// TodosUpdated is emitted when the todo list changes.
type TodosUpdated struct {
	Todos []TodoItem `json:"todos"`
}

func (TodosUpdated) trackerEventType() string { return "todos_updated" }

// StateChanged is emitted when overall tracker state changes.
type StateChanged struct {
	State TrackerState `json:"state"`
}

func (StateChanged) trackerEventType() string { return "state_changed" }

// Pre-compiled regex patterns for completion indicator detection.
var completionIndicatorRegexps []*regexp.Regexp

func init() {
	for _, pat := range completionIndicatorPatterns {
		completionIndicatorRegexps = append(completionIndicatorRegexps, regexp.MustCompile(pat))
	}
}

// Tracker monitors terminal output for completion phrases, todo items,
// loop iterations, RALPH_STATUS blocks, and working/idle state changes.
type Tracker struct {
	mu    sync.Mutex
	state TrackerState

	// Monotonic counter for unique todo IDs
	nextTodoID int

	// Patterns
	completionPattern *regexp.Regexp
	todoPattern       *regexp.Regexp
	promisePattern    *regexp.Regexp

	// Circuit breaker for dual-condition exit
	circuitBreaker *CircuitBreaker

	// Objective gate: when set, the dual-condition circuit breaker tripping is
	// necessary but NOT sufficient to exit -- the verify command must also pass.
	// gateWorkingDir is where the gate runs; gateInFlight guards against
	// launching overlapping checks; gateFailures counts consecutive failures.
	gate           ObjectiveGate
	gateWorkingDir string
	gateInFlight   bool
	gateFailures   int

	// RALPH_STATUS block parsing state
	inStatusBlock    bool
	statusBlockLines []string
	lastRalphStatus  *RalphStatus
	// statusRemainder holds the trailing partial line carried over between
	// ProcessOutput calls. EventTerminal delivers raw PTY read chunks, not whole
	// lines, so a marker or field line ("EXIT_SIGNAL: true", "---END_RALPH...")
	// can be split across two chunks; buffering the partial line and only
	// parsing COMPLETE \n-terminated lines keeps the anchored regexes matching.
	statusRemainder string

	// Cleanup throttle
	lastCleanupTime time.Time

	// Event debouncing
	pendingEvents   bool
	debounceStopper chan struct{}

	// Event channel
	events chan TrackerEvent

	// droppedEvents counts events shed by emitEvent when the consumer can't keep
	// up (the buffer is full). Surfaced so a silently-lagging consumer is visible
	// instead of the tracker appearing to hang. Guarded by t.mu (emitEvent holds it).
	droppedEvents int
	droppedWarned bool

	// Legacy callbacks (kept for backward compatibility)
	onCompletion func(phrase string)
	onTodoUpdate func(todos []TodoItem)
}

// NewTracker creates a new Ralph tracker with default patterns.
func NewTracker() *Tracker {
	t := &Tracker{
		state: TrackerState{
			Todos: make([]TodoItem, 0),
		},
		todoPattern:     regexp.MustCompile(`(?i)- \[([ xX` + "\u2713\u2717" + `])\] (.+)`),
		promisePattern:  regexp.MustCompile(`(?i)<promise>\s*([^<]+?)\s*</promise>`),
		circuitBreaker:  NewCircuitBreaker(),
		events:          make(chan TrackerEvent, 64),
		debounceStopper: make(chan struct{}),
	}
	return t
}

// Events returns the event channel for consuming tracker events.
func (t *Tracker) Events() <-chan TrackerEvent {
	return t.events
}

// Configure sets the completion phrase and max iterations for the tracker.
// If phrase is non-empty, the tracker is enabled.
func (t *Tracker) Configure(phrase string, maxIterations int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state.Enabled = phrase != ""
	t.state.CompletionPhrase = phrase
	t.state.MaxIterations = maxIterations
	if phrase != "" {
		t.completionPattern = regexp.MustCompile(regexp.QuoteMeta(phrase))
	} else {
		t.completionPattern = nil
	}
}

// SetGate attaches an objective verify gate. Once set, the dual-condition
// circuit breaker tripping kicks off an asynchronous gate check (the verify
// command runs in workingDir); ExitGateMet is emitted only if the gate passes.
// Passing a nil gate clears it and restores the original phrase-only behavior.
func (t *Tracker) SetGate(gate ObjectiveGate, workingDir string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.gate = gate
	t.gateWorkingDir = workingDir
	// gateFailures counts CONSECUTIVE failures of the CURRENT gate, so a new
	// gate starts with a clean budget -- otherwise swapping a failing gate for
	// a fresh one would inherit the old count and falsely escalate on the new
	// gate's first failure.
	t.gateFailures = 0
	t.state.GateFailureCount = 0
}

// maybeRunGateLocked decides what happens when the circuit breaker has tripped.
// With no gate configured it preserves the legacy behavior (emit ExitGateMet
// immediately). With a gate configured it launches a single async verify check;
// ExitGateMet (pass) or GateCheckFailed (fail) is emitted from the goroutine.
// Must be called with t.mu held.
func (t *Tracker) maybeRunGateLocked() {
	if t.gate == nil {
		t.emitEvent(ExitGateMet{
			CompletionIndicators: t.circuitBreaker.Status().CompletionIndicators,
			ExitSignal:           t.circuitBreaker.Status().ExitSignalSeen,
		})
		return
	}
	if t.gateInFlight {
		return // a check is already running; don't stack verify subprocesses
	}
	t.gateInFlight = true
	gate := t.gate
	workingDir := t.gateWorkingDir

	// Run the (slow) verify command off the lock so ProcessOutput never blocks
	// on a test suite. safego converts any panic into a logged event instead of
	// crashing the daemon.
	safego.Go("ralph.objectiveGate", func() {
		result := gate.Check(workingDir)
		t.applyGateResult(result)
	})
}

// applyGateResult records an objective-gate outcome and emits the appropriate
// terminal event. A pass (or a skip -- an un-gateable project falls back to the
// dual-condition decision) emits ExitGateMet. A real failure resets the circuit
// breaker so the loop continues and emits GateCheckFailed, flagging escalation
// once consecutive failures cross the cap.
func (t *Tracker) applyGateResult(result GateResult) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.gateInFlight = false
	res := result
	t.state.LastGateResult = &res

	if result.Pass || result.Skipped {
		t.gateFailures = 0
		t.state.GateFailureCount = 0
		// Read the breaker status FRESH here (not a snapshot captured before the
		// async gate ran) so ExitGateMet reports state at fire time, matching the
		// synchronous no-gate path.
		cbStatus := t.circuitBreaker.Status()
		t.emitEvent(ExitGateMet{
			CompletionIndicators: cbStatus.CompletionIndicators,
			ExitSignal:           cbStatus.ExitSignalSeen,
		})
		return
	}

	// Real gate failure: the agent claimed completion but the verify command
	// rejected the work. Keep the loop alive -- reset the breaker so a later
	// genuine completion can re-trip it once the work actually passes. Reset
	// returns false once the auto-reset cap is exhausted; if so the breaker
	// stays tripped and ExitGateMet can never fire again, so surface it loudly
	// rather than silently stalling the loop.
	t.gateFailures++
	t.state.GateFailureCount = t.gateFailures
	if !t.circuitBreaker.Reset() {
		slog.Warn("ralph: circuit breaker exhausted its auto-reset budget after a gate failure; loop is stuck-tripped and needs operator attention",
			"gateFailures", t.gateFailures)
	}

	reason := "verify command failed"
	if result.TimedOut {
		reason = "verify command timed out"
	} else if result.Err != "" {
		reason = result.Err
	}
	t.emitEvent(GateCheckFailed{
		Reason:           reason,
		Command:          result.Command,
		ExitCode:         result.ExitCode,
		ConsecutiveFails: t.gateFailures,
		Escalate:         t.gateFailures >= gateMaxConsecutiveFailures,
	})
}

// ProcessOutput scans terminal output data for completion phrases,
// promise tags, todo items, RALPH_STATUS blocks, and completion indicators.
func (t *Tracker) ProcessOutput(data string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state.LastActivityAt = time.Now()

	// Run periodic cleanup
	t.maybeCleanupLocked()

	// Process RALPH_STATUS blocks line by line
	t.processStatusBlocks(data)

	// Check for completion indicators (natural-language phrases)
	t.checkCompletionIndicators(data)

	// Check for completion phrase
	if t.completionPattern != nil && t.completionPattern.MatchString(data) {
		t.state.CompletionDetected = true
		t.state.LoopIteration++
		t.emitEvent(LoopCompleted{
			Phrase:    t.state.CompletionPhrase,
			Iteration: t.state.LoopIteration,
		})

		// Hard stop: enforce the iteration cap that was previously parsed but
		// never compared. Fire LoopLimitReached EXACTLY ONCE -- at the moment
		// the cap is first reached (==, not >=, so later completions past the
		// cap don't re-broadcast the one-shot hard-stop signal over SSE).
		if t.state.MaxIterations > 0 && t.state.LoopIteration == t.state.MaxIterations {
			t.emitEvent(LoopLimitReached{
				Iteration:     t.state.LoopIteration,
				MaxIterations: t.state.MaxIterations,
			})
		}
		if t.onCompletion != nil {
			cb := t.onCompletion
			phrase := t.state.CompletionPhrase
			// safego: callback is operator-supplied; a panic there
			// would otherwise crash the daemon.
			safego.Go("ralph.onCompletion", func() { cb(phrase) })
		}

		// Check circuit breaker after completion. The dual condition is
		// necessary but, when an objective gate is configured, not sufficient:
		// the verify command must also pass before the loop may exit.
		if t.circuitBreaker.ShouldBreak() {
			t.maybeRunGateLocked()
		}
	}

	// Check for promise tags: <promise>PHRASE</promise>
	if strings.Contains(data, "<promise>") || strings.Contains(data, "<Promise>") || strings.Contains(data, "<PROMISE>") {
		if matches := t.promisePattern.FindStringSubmatch(data); len(matches) > 1 {
			phrase := strings.TrimSpace(matches[1])
			if phrase != "" {
				t.state.CompletionPhrase = phrase
				t.state.Enabled = true
				t.completionPattern = regexp.MustCompile(regexp.QuoteMeta(phrase))
			}
		}
	}

	// Parse todo items from checkbox-style lines
	for _, match := range t.todoPattern.FindAllStringSubmatch(data, -1) {
		if len(match) >= 3 {
			status := "pending"
			marker := strings.TrimSpace(match[1])
			switch marker {
			case "x", "X", "\u2713":
				status = "completed"
			case "\u2717":
				status = "failed"
			}
			t.updateOrAddTodo(match[2], status)
		}
	}

	// Update circuit breaker status in state
	t.state.CircuitBreaker = t.circuitBreaker.Status()
	t.state.LastRalphStatus = t.lastRalphStatus

	// Debounced state change notification
	t.scheduleDebouncedStateEvent()
}

// processStatusBlocks scans for RALPH_STATUS blocks in the data. Only COMPLETE
// (\n-terminated) lines are parsed; the trailing partial line is buffered in
// t.statusRemainder and prepended to the next chunk, so markers/field lines
// split across PTY read boundaries are still matched as whole lines.
// Must be called with t.mu held.
func (t *Tracker) processStatusBlocks(data string) {
	buf := t.statusRemainder + data
	for {
		idx := strings.IndexByte(buf, '\n')
		if idx < 0 {
			break
		}
		line := buf[:idx]
		buf = buf[idx+1:]
		t.processStatusLine(strings.TrimSpace(line))
	}

	// buf is now the trailing line with no terminating newline. Peek it for a
	// COMPLETE start/end marker: the anchored patterns only match a whole
	// marker, so a terminal marker that is the final line (end-of-output, no
	// trailing newline) still closes/opens the block.
	trimmed := strings.TrimSpace(buf)
	if statusStartPattern.MatchString(trimmed) || statusEndPattern.MatchString(trimmed) {
		t.processStatusLine(trimmed)
		t.statusRemainder = ""
		return
	}
	// Retain the trailing partial line when it's relevant to status parsing:
	// mid-block (a split field/end-marker line) OR when it could still grow into
	// a START marker (a split start marker). Anything else is ordinary output
	// and is discarded so it can't pollute the next chunk's first line or
	// accumulate unboundedly between blocks.
	if t.inStatusBlock || (trimmed != "" && strings.HasPrefix(statusStartMarker, trimmed)) {
		if len(buf) > maxStatusRemainder {
			buf = buf[len(buf)-maxStatusRemainder:]
		}
		t.statusRemainder = buf
	} else {
		t.statusRemainder = ""
	}
}

// processStatusLine applies one complete (trimmed) line to the RALPH_STATUS
// state machine. Must be called with t.mu held.
func (t *Tracker) processStatusLine(trimmed string) {
	if statusStartPattern.MatchString(trimmed) {
		t.inStatusBlock = true
		t.statusBlockLines = nil
		return
	}

	if statusEndPattern.MatchString(trimmed) {
		if t.inStatusBlock && len(t.statusBlockLines) > 0 {
			status := ParseRalphStatusBlock(t.statusBlockLines)
			t.lastRalphStatus = &status

			// Feed circuit breaker
			if status.ExitSignal {
				t.circuitBreaker.RecordExitSignal()
			}

			t.emitEvent(StatusBlockDetected{Status: status})
		}
		t.inStatusBlock = false
		t.statusBlockLines = nil
		return
	}

	if t.inStatusBlock {
		// Abort a runaway/unclosed block instead of accumulating forever.
		if len(t.statusBlockLines) >= maxStatusBlockLines {
			slog.Warn("ralph: RALPH_STATUS block exceeded line cap without an end marker; aborting block",
				"cap", maxStatusBlockLines)
			t.inStatusBlock = false
			t.statusBlockLines = nil
			return
		}
		t.statusBlockLines = append(t.statusBlockLines, trimmed)
	}
}

// checkCompletionIndicators looks for natural-language completion phrases.
// Must be called with t.mu held.
func (t *Tracker) checkCompletionIndicators(data string) {
	for _, re := range completionIndicatorRegexps {
		if re.MatchString(data) {
			t.circuitBreaker.RecordCompletion()
			break // one detection per chunk is enough
		}
	}
}

// updateOrAddTodo updates an existing todo's status or appends a new one.
// Includes deduplication via similarity check and enforces max todo count.
// Must be called with t.mu held.
func (t *Tracker) updateOrAddTodo(content, status string) {
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}

	// Check exact match first
	for i, todo := range t.state.Todos {
		if todo.Content == content {
			t.state.Todos[i].Status = status
			t.notifyTodoUpdate()
			return
		}
	}

	// Check similarity-based deduplication
	for i, todo := range t.state.Todos {
		if stringSimilarity(todo.Content, content) >= todoSimilarityThreshold {
			// Update existing similar todo instead of creating duplicate
			t.state.Todos[i].Status = status
			t.notifyTodoUpdate()
			return
		}
	}

	// Enforce maximum todo count by removing oldest
	if len(t.state.Todos) >= maxTodoItems {
		t.state.Todos = t.state.Todos[1:]
	}

	t.state.Todos = append(t.state.Todos, TodoItem{
		ID:        fmt.Sprintf("todo-%d", func() int { t.nextTodoID++; return t.nextTodoID }()),
		Content:   content,
		Status:    status,
		CreatedAt: time.Now(),
	})
	t.notifyTodoUpdate()
}

// notifyTodoUpdate sends the todo update via callback and event channel.
// Must be called with t.mu held.
func (t *Tracker) notifyTodoUpdate() {
	todos := t.copyTodos()
	t.emitEvent(TodosUpdated{Todos: todos})
	if t.onTodoUpdate != nil {
		cb := t.onTodoUpdate
		// safego: callback is operator-supplied; a panic there
		// would otherwise crash the daemon.
		safego.Go("ralph.onTodoUpdate", func() { cb(todos) })
	}
}

// maybeCleanupLocked expires old todos and enforces limits.
// Must be called with t.mu held.
func (t *Tracker) maybeCleanupLocked() {
	now := time.Now()
	if now.Sub(t.lastCleanupTime) < cleanupThrottle {
		return
	}
	t.lastCleanupTime = now

	changed := false
	for i := range t.state.Todos {
		if t.state.Todos[i].Status != "completed" &&
			t.state.Todos[i].Status != "expired" &&
			now.Sub(t.state.Todos[i].CreatedAt) > todoExpiryDuration {
			t.state.Todos[i].Status = "expired"
			changed = true
		}
	}
	if changed {
		t.notifyTodoUpdate()
	}
}

// emitEvent sends an event to the channel without blocking.
// Must be called with t.mu held.
//
// The send is non-blocking by design: emitEvent runs under t.mu, so a blocking
// send to a slow/absent consumer would deadlock the tracker. When the 64-slot
// buffer is full the event is dropped, but the drop is now counted and logged
// once (rather than silently vanishing) so an operator can tell a lagging
// consumer from a genuinely quiet tracker.
func (t *Tracker) emitEvent(event TrackerEvent) {
	select {
	case t.events <- event:
	default:
		t.droppedEvents++
		if !t.droppedWarned {
			t.droppedWarned = true
			slog.Warn("ralph: tracker event dropped; consumer not draining the channel",
				"eventType", event.trackerEventType())
		}
	}
}

// DroppedEvents returns the number of tracker events shed because the consumer
// could not keep up. Zero in the healthy case.
func (t *Tracker) DroppedEvents() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.droppedEvents
}

// scheduleDebouncedStateEvent schedules a debounced state change event.
// Must be called with t.mu held.
func (t *Tracker) scheduleDebouncedStateEvent() {
	if t.pendingEvents {
		return // already scheduled
	}
	t.pendingEvents = true

	// Fire asynchronously after debounce window.
	// R26 · Panic recovery so a crash inside snapshotStateLocked /
	// emitEvent doesn't kill the goroutine before
	// pendingEvents gets cleared — would leave the tracker stuck.
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic in ralph debounce",
					"panic", rec, "stack", string(debug.Stack()))
				t.mu.Lock()
				t.pendingEvents = false
				t.mu.Unlock()
			}
		}()
		select {
		case <-time.After(eventDebounceDuration):
			// emitEvent must run with t.mu held (it touches droppedEvents/
			// droppedWarned and the non-blocking channel send is instant), so
			// keep it inside the critical section rather than after Unlock.
			t.mu.Lock()
			t.pendingEvents = false
			state := t.snapshotStateLocked()
			t.emitEvent(StateChanged{State: state})
			t.mu.Unlock()
		case <-t.debounceStopper:
			return
		}
	}()
}

// snapshotStateLocked returns a copy of the tracker state.
// Must be called with t.mu held.
func (t *Tracker) snapshotStateLocked() TrackerState {
	s := t.state
	s.Todos = make([]TodoItem, len(t.state.Todos))
	copy(s.Todos, t.state.Todos)
	return s
}

// copyTodos returns a copy of the current todos.
// Must be called with t.mu held.
func (t *Tracker) copyTodos() []TodoItem {
	todos := make([]TodoItem, len(t.state.Todos))
	copy(todos, t.state.Todos)
	return todos
}

// GetState returns a snapshot of the current tracker state.
func (t *Tracker) GetState() TrackerState {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.snapshotStateLocked()
	s.CircuitBreaker = t.circuitBreaker.Status()
	s.LastRalphStatus = t.lastRalphStatus
	return s
}

// GetCircuitBreakerStatus returns the circuit breaker status.
func (t *Tracker) GetCircuitBreakerStatus() CircuitBreakerStatus {
	return t.circuitBreaker.Status()
}

// ResetCircuitBreaker resets the circuit breaker. Returns false if the
// maximum auto-reset count has been reached.
func (t *Tracker) ResetCircuitBreaker() bool {
	return t.circuitBreaker.Reset()
}

// SetCallbacks registers optional callbacks for completion and todo events.
func (t *Tracker) SetCallbacks(onCompletion func(string), onTodoUpdate func([]TodoItem)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onCompletion = onCompletion
	t.onTodoUpdate = onTodoUpdate
}

// UpdatePlanTask updates a specific plan task's status by ID.
// Returns true if the task was found and updated, false otherwise.
func (t *Tracker) UpdatePlanTask(taskID, status, errMsg string, incrementAttempts bool) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	for i, todo := range t.state.Todos {
		if todo.ID == taskID {
			if status != "" {
				t.state.Todos[i].Status = status
			}
			if errMsg != "" && status == "failed" {
				// Append error info to content if not already present
				if !strings.Contains(t.state.Todos[i].Content, "FAILED") {
					t.state.Todos[i].Content += " (FAILED: " + errMsg + ")"
				}
			}
			t.notifyTodoUpdate()
			return true
		}
	}
	return false
}

// Reset clears the completion flag and todo list, preserving configuration.
func (t *Tracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state.CompletionDetected = false
	t.state.Todos = make([]TodoItem, 0)
	t.inStatusBlock = false
	t.statusBlockLines = nil
	t.lastRalphStatus = nil
}

// Stop cleans up any goroutines and timers.
func (t *Tracker) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	select {
	case <-t.debounceStopper:
		// already closed
	default:
		close(t.debounceStopper)
	}
}

// stringSimilarity computes a simple similarity ratio between two strings
// using the longest common subsequence approach. Returns 0.0 to 1.0.
func stringSimilarity(a, b string) float64 {
	a = strings.ToLower(strings.TrimSpace(a))
	b = strings.ToLower(strings.TrimSpace(b))

	lenA := utf8.RuneCountInString(a)
	lenB := utf8.RuneCountInString(b)
	if lenA == 0 || lenB == 0 {
		return 0.0
	}
	if a == b {
		return 1.0
	}

	runesA := []rune(a)
	runesB := []rune(b)

	// LCS length via dynamic programming
	dp := make([][]int, lenA+1)
	for i := range dp {
		dp[i] = make([]int, lenB+1)
	}
	for i := 1; i <= lenA; i++ {
		for j := 1; j <= lenB; j++ {
			if runesA[i-1] == runesB[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else {
				if dp[i-1][j] > dp[i][j-1] {
					dp[i][j] = dp[i-1][j]
				} else {
					dp[i][j] = dp[i][j-1]
				}
			}
		}
	}

	lcs := dp[lenA][lenB]
	maxLen := lenA
	if lenB > maxLen {
		maxLen = lenB
	}
	return float64(2*lcs) / float64(lenA+lenB)
}
