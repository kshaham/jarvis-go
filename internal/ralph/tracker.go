package ralph

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Configuration constants matching the Node.js implementation.
const (
	maxTodoItems           = 50
	todoExpiryDuration     = 1 * time.Hour
	cleanupThrottle        = 30 * time.Second
	todoSimilarityThreshold = 0.85
	eventDebounceDuration  = 50 * time.Millisecond
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
	Enabled            bool                `json:"enabled"`
	CompletionPhrase   string              `json:"completionPhrase"`
	LoopIteration      int                 `json:"loopIteration"`
	MaxIterations      int                 `json:"maxIterations"`
	Todos              []TodoItem          `json:"todos"`
	LastActivityAt     time.Time           `json:"lastActivityAt"`
	CompletionDetected bool                `json:"completionDetected"`
	CircuitBreaker     CircuitBreakerStatus `json:"circuitBreaker"`
	LastRalphStatus    *RalphStatus        `json:"lastRalphStatus,omitempty"`
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

// ExitGateMet is emitted when the circuit breaker trips.
type ExitGateMet struct {
	CompletionIndicators int  `json:"completionIndicators"`
	ExitSignal           bool `json:"exitSignal"`
}

func (ExitGateMet) trackerEventType() string { return "exit_gate_met" }

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

	// RALPH_STATUS block parsing state
	inStatusBlock    bool
	statusBlockLines []string
	lastRalphStatus  *RalphStatus

	// Cleanup throttle
	lastCleanupTime time.Time

	// Event debouncing
	pendingEvents   bool
	debounceTimer   *time.Timer
	debounceStopper chan struct{}

	// Event channel
	events chan TrackerEvent

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
		if t.onCompletion != nil {
			cb := t.onCompletion
			phrase := t.state.CompletionPhrase
			go cb(phrase)
		}

		// Check circuit breaker after completion
		if t.circuitBreaker.ShouldBreak() {
			t.emitEvent(ExitGateMet{
				CompletionIndicators: t.circuitBreaker.Status().CompletionIndicators,
				ExitSignal:           t.circuitBreaker.Status().ExitSignalSeen,
			})
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

// processStatusBlocks scans for RALPH_STATUS blocks in the data.
// Must be called with t.mu held.
func (t *Tracker) processStatusBlocks(data string) {
	lines := strings.Split(data, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if statusStartPattern.MatchString(trimmed) {
			t.inStatusBlock = true
			t.statusBlockLines = nil
			continue
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
			continue
		}

		if t.inStatusBlock {
			t.statusBlockLines = append(t.statusBlockLines, trimmed)
		}
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
		go cb(todos)
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
func (t *Tracker) emitEvent(event TrackerEvent) {
	select {
	case t.events <- event:
	default:
		// Channel full; drop event to avoid blocking
	}
}

// scheduleDebouncedStateEvent schedules a debounced state change event.
// Must be called with t.mu held.
func (t *Tracker) scheduleDebouncedStateEvent() {
	if t.pendingEvents {
		return // already scheduled
	}
	t.pendingEvents = true

	// Fire asynchronously after debounce window
	go func() {
		select {
		case <-time.After(eventDebounceDuration):
			t.mu.Lock()
			t.pendingEvents = false
			state := t.snapshotStateLocked()
			t.mu.Unlock()
			t.emitEvent(StateChanged{State: state})
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
