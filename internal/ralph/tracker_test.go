package ralph

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// --- Tracker basic tests ---

func TestTracker_NewTracker(t *testing.T) {
	tr := NewTracker()
	state := tr.GetState()
	if state.Enabled {
		t.Error("expected tracker to start disabled")
	}
	if state.LoopIteration != 0 {
		t.Errorf("expected iteration 0, got %d", state.LoopIteration)
	}
	if len(state.Todos) != 0 {
		t.Errorf("expected 0 todos, got %d", len(state.Todos))
	}
	tr.Stop()
}

// TestTracker_EmitEvent_CountsDropsWhenBufferFull verifies that events shed
// because the consumer isn't draining are COUNTED (not silently lost), so a
// lagging consumer is observable via DroppedEvents().
func TestTracker_EmitEvent_CountsDropsWhenBufferFull(t *testing.T) {
	tr := NewTracker()
	defer tr.Stop()
	tr.mu.Lock()
	// Fill the 64-slot buffer with no consumer draining it.
	for i := 0; i < cap(tr.events); i++ {
		tr.emitEvent(TodosUpdated{})
	}
	// The next emit has nowhere to go -> must be dropped AND counted.
	tr.emitEvent(TodosUpdated{})
	tr.mu.Unlock()
	if got := tr.DroppedEvents(); got < 1 {
		t.Errorf("expected >=1 dropped event when buffer full, got %d", got)
	}
}

func TestTracker_Configure(t *testing.T) {
	tr := NewTracker()
	defer tr.Stop()
	tr.Configure("ALL_DONE", 10)
	state := tr.GetState()
	if !state.Enabled {
		t.Error("expected enabled after Configure with non-empty phrase")
	}
	if state.CompletionPhrase != "ALL_DONE" {
		t.Errorf("expected phrase ALL_DONE, got %s", state.CompletionPhrase)
	}
	if state.MaxIterations != 10 {
		t.Errorf("expected max iterations 10, got %d", state.MaxIterations)
	}
}

func TestTracker_CompletionDetection(t *testing.T) {
	tr := NewTracker()
	defer tr.Stop()

	completed := make(chan string, 1)
	tr.SetCallbacks(func(phrase string) {
		completed <- phrase
	}, nil)

	tr.Configure("TASK_COMPLETE", 50)
	tr.ProcessOutput("The output says TASK_COMPLETE here.")

	select {
	case phrase := <-completed:
		if phrase != "TASK_COMPLETE" {
			t.Errorf("expected TASK_COMPLETE, got %s", phrase)
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for completion callback")
	}

	state := tr.GetState()
	if !state.CompletionDetected {
		t.Error("expected CompletionDetected to be true")
	}
	if state.LoopIteration != 1 {
		t.Errorf("expected iteration 1, got %d", state.LoopIteration)
	}
}

func TestTracker_LoopIterationIncrement(t *testing.T) {
	tr := NewTracker()
	defer tr.Stop()
	tr.Configure("DONE", 100)

	for i := 1; i <= 5; i++ {
		tr.ProcessOutput("Iteration complete: DONE")
		state := tr.GetState()
		if state.LoopIteration != i {
			t.Errorf("after %d completions: expected iteration %d, got %d", i, i, state.LoopIteration)
		}
	}
}

func TestTracker_PromiseTagDetection(t *testing.T) {
	tr := NewTracker()
	defer tr.Stop()

	tr.ProcessOutput("Some output <promise>MY_PHRASE</promise> more output")

	state := tr.GetState()
	if state.CompletionPhrase != "MY_PHRASE" {
		t.Errorf("expected MY_PHRASE, got %s", state.CompletionPhrase)
	}
	if !state.Enabled {
		t.Error("expected tracker to auto-enable on promise tag detection")
	}
}

func TestTracker_PromiseTagCaseInsensitive(t *testing.T) {
	tr := NewTracker()
	defer tr.Stop()

	tr.ProcessOutput("Output with <Promise> SPECIAL_PHRASE </Promise> here")

	state := tr.GetState()
	if state.CompletionPhrase != "SPECIAL_PHRASE" {
		t.Errorf("expected SPECIAL_PHRASE, got %s", state.CompletionPhrase)
	}
}

// --- Todo tests ---

func TestTracker_TodoParsing(t *testing.T) {
	tr := NewTracker()
	defer tr.Stop()

	tr.ProcessOutput("- [ ] First task\n- [x] Second task\n- [ ] Third task")

	state := tr.GetState()
	if len(state.Todos) != 3 {
		t.Fatalf("expected 3 todos, got %d", len(state.Todos))
	}
	if state.Todos[0].Content != "First task" || state.Todos[0].Status != "pending" {
		t.Errorf("todo 0: %+v", state.Todos[0])
	}
	if state.Todos[1].Content != "Second task" || state.Todos[1].Status != "completed" {
		t.Errorf("todo 1: %+v", state.Todos[1])
	}
	if state.Todos[2].Content != "Third task" || state.Todos[2].Status != "pending" {
		t.Errorf("todo 2: %+v", state.Todos[2])
	}
}

func TestTracker_TodoDeduplication_ExactMatch(t *testing.T) {
	tr := NewTracker()
	defer tr.Stop()

	tr.ProcessOutput("- [ ] Build the widget")
	tr.ProcessOutput("- [x] Build the widget")

	state := tr.GetState()
	if len(state.Todos) != 1 {
		t.Fatalf("expected 1 todo (deduped), got %d", len(state.Todos))
	}
	if state.Todos[0].Status != "completed" {
		t.Errorf("expected status completed after update, got %s", state.Todos[0].Status)
	}
}

func TestTracker_TodoDeduplication_SimilarContent(t *testing.T) {
	tr := NewTracker()
	defer tr.Stop()

	tr.ProcessOutput("- [ ] Implement the authentication handler")
	tr.ProcessOutput("- [x] Implement the authentication handlers") // very similar

	state := tr.GetState()
	if len(state.Todos) != 1 {
		t.Fatalf("expected 1 todo (similar dedup), got %d", len(state.Todos))
	}
	if state.Todos[0].Status != "completed" {
		t.Errorf("expected status completed, got %s", state.Todos[0].Status)
	}
}

func TestTracker_TodoExpiration(t *testing.T) {
	tr := NewTracker()
	defer tr.Stop()

	// Manually add an old todo
	tr.mu.Lock()
	tr.state.Todos = append(tr.state.Todos, TodoItem{
		ID:        "todo-old",
		Content:   "Old task",
		Status:    "pending",
		CreatedAt: time.Now().Add(-2 * time.Hour),
	})
	tr.lastCleanupTime = time.Time{} // force cleanup to run
	tr.mu.Unlock()

	// Process output triggers cleanup
	tr.ProcessOutput("- [ ] New task")

	state := tr.GetState()
	found := false
	for _, todo := range state.Todos {
		if todo.Content == "Old task" {
			found = true
			if todo.Status != "expired" {
				t.Errorf("expected old todo to be expired, got %s", todo.Status)
			}
		}
	}
	if !found {
		t.Error("expected old todo to still be present (just marked expired)")
	}
}

func TestTracker_TodoMaxLimit(t *testing.T) {
	tr := NewTracker()
	defer tr.Stop()

	// Add maxTodoItems + 5 todos
	for i := 0; i < maxTodoItems+5; i++ {
		tr.ProcessOutput(fmt.Sprintf("- [ ] Task number %d", i))
	}

	state := tr.GetState()
	if len(state.Todos) > maxTodoItems {
		t.Errorf("expected at most %d todos, got %d", maxTodoItems, len(state.Todos))
	}
}

func TestTracker_Reset(t *testing.T) {
	tr := NewTracker()
	defer tr.Stop()

	tr.Configure("DONE", 5)
	tr.ProcessOutput("- [ ] Task\nDONE")
	tr.Reset()

	state := tr.GetState()
	if state.CompletionDetected {
		t.Error("expected CompletionDetected false after reset")
	}
	if len(state.Todos) != 0 {
		t.Errorf("expected 0 todos after reset, got %d", len(state.Todos))
	}
	// Configuration should be preserved
	if !state.Enabled || state.CompletionPhrase != "DONE" {
		t.Error("expected configuration to be preserved after reset")
	}
}

// --- Loop event channel tests ---

func TestTracker_EventChannel(t *testing.T) {
	tr := NewTracker()
	defer tr.Stop()
	tr.Configure("LOOP_END", 10)

	tr.ProcessOutput("Output: LOOP_END")

	// Drain events and look for LoopCompleted
	timeout := time.After(time.Second)
	for {
		select {
		case ev := <-tr.Events():
			if lc, ok := ev.(LoopCompleted); ok {
				if lc.Iteration != 1 {
					t.Errorf("expected iteration 1, got %d", lc.Iteration)
				}
				return
			}
		case <-timeout:
			t.Fatal("timeout waiting for LoopCompleted event")
		}
	}
}

// --- Circuit breaker tests ---

func TestCircuitBreaker_NotTrippedWithoutBothConditions(t *testing.T) {
	cb := NewCircuitBreaker()

	// Only completion
	cb.RecordCompletion()
	cb.RecordCompletion()
	if cb.ShouldBreak() {
		t.Error("should not break with only completions, no exit signal")
	}

	// Only exit signal on fresh breaker
	cb2 := NewCircuitBreaker()
	cb2.RecordExitSignal()
	if cb2.ShouldBreak() {
		t.Error("should not break with only exit signal, no completions")
	}
}

func TestCircuitBreaker_TripsOnDualCondition(t *testing.T) {
	cb := NewCircuitBreaker()
	cb.RecordCompletion()
	cb.RecordCompletion()
	cb.RecordExitSignal()

	if !cb.ShouldBreak() {
		t.Error("expected breaker to trip with dual condition met")
	}
}

func TestCircuitBreaker_StaysTripped(t *testing.T) {
	cb := NewCircuitBreaker()
	cb.RecordCompletion()
	cb.RecordCompletion()
	cb.RecordExitSignal()
	cb.ShouldBreak() // trips

	// Should still be tripped
	if !cb.ShouldBreak() {
		t.Error("expected breaker to stay tripped")
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	cb := NewCircuitBreaker()
	cb.RecordCompletion()
	cb.RecordCompletion()
	cb.RecordExitSignal()
	cb.ShouldBreak()

	ok := cb.Reset()
	if !ok {
		t.Error("expected reset to succeed")
	}
	if cb.ShouldBreak() {
		t.Error("expected breaker to be clear after reset")
	}
}

func TestCircuitBreaker_MaxResets(t *testing.T) {
	cb := NewCircuitBreaker()
	for i := 0; i < maxAutoResets; i++ {
		cb.RecordCompletion()
		cb.RecordCompletion()
		cb.RecordExitSignal()
		cb.ShouldBreak()
		ok := cb.Reset()
		if !ok {
			t.Errorf("reset %d should succeed", i+1)
		}
	}

	// One more reset should fail
	cb.RecordCompletion()
	cb.RecordCompletion()
	cb.RecordExitSignal()
	cb.ShouldBreak()
	if cb.Reset() {
		t.Error("expected reset to fail after max resets")
	}
}

func TestCircuitBreaker_WindowExpiry(t *testing.T) {
	cb := NewCircuitBreakerWithWindow(1 * time.Millisecond)
	cb.RecordCompletion()
	cb.RecordCompletion()

	// Wait for window to expire
	time.Sleep(10 * time.Millisecond)

	cb.RecordExitSignal()
	if cb.ShouldBreak() {
		t.Error("expected breaker NOT to trip when completion is outside window")
	}
}

func TestCircuitBreaker_Status(t *testing.T) {
	cb := NewCircuitBreaker()
	cb.RecordCompletion()
	cb.RecordExitSignal()

	status := cb.Status()
	if status.CompletionIndicators != 1 {
		t.Errorf("expected 1 indicator, got %d", status.CompletionIndicators)
	}
	if !status.ExitSignalSeen {
		t.Error("expected exit signal seen")
	}
	if status.Tripped {
		t.Error("should not be tripped with only 1 completion")
	}
}

// --- Status parser tests ---

func TestParseRalphStatusBlock(t *testing.T) {
	lines := []string{
		"STATUS: IN_PROGRESS",
		"TASKS_COMPLETED_THIS_LOOP: 3",
		"FILES_MODIFIED: 12",
		"TESTS_STATUS: PASSING",
		"WORK_TYPE: IMPLEMENTATION",
		"EXIT_SIGNAL: false",
		"RECOMMENDATION: Continue with remaining tasks",
	}

	status := ParseRalphStatusBlock(lines)

	if status.Status != StatusInProgress {
		t.Errorf("expected IN_PROGRESS, got %s", status.Status)
	}
	if status.TasksCompleted != 3 {
		t.Errorf("expected 3 tasks, got %d", status.TasksCompleted)
	}
	if status.FilesModified != 12 {
		t.Errorf("expected 12 files, got %d", status.FilesModified)
	}
	if status.TestsStatus != TestsPassing {
		t.Errorf("expected PASSING, got %s", status.TestsStatus)
	}
	if status.WorkType != WorkImplementation {
		t.Errorf("expected IMPLEMENTATION, got %s", status.WorkType)
	}
	if status.ExitSignal {
		t.Error("expected exit signal false")
	}
	if status.Recommendation != "Continue with remaining tasks" {
		t.Errorf("unexpected recommendation: %s", status.Recommendation)
	}
}

func TestParseRalphStatusBlock_Complete(t *testing.T) {
	lines := []string{
		"STATUS: COMPLETE",
		"TASKS_COMPLETED_THIS_LOOP: 8",
		"FILES_MODIFIED: 25",
		"TESTS_STATUS: PASSING",
		"WORK_TYPE: TESTING",
		"EXIT_SIGNAL: true",
		"RECOMMENDATION: All work is done",
	}

	status := ParseRalphStatusBlock(lines)

	if status.Status != StatusComplete {
		t.Errorf("expected COMPLETE, got %s", status.Status)
	}
	if !status.ExitSignal {
		t.Error("expected exit signal true")
	}
	if status.WorkType != WorkTesting {
		t.Errorf("expected TESTING, got %s", status.WorkType)
	}
}

func TestParseRalphStatusBlock_Blocked(t *testing.T) {
	lines := []string{
		"STATUS: BLOCKED",
		"TASKS_COMPLETED_THIS_LOOP: 0",
		"FILES_MODIFIED: 0",
		"TESTS_STATUS: FAILING",
		"WORK_TYPE: DOCUMENTATION",
		"EXIT_SIGNAL: false",
		"RECOMMENDATION: Waiting for external dependency",
	}

	status := ParseRalphStatusBlock(lines)
	if status.Status != StatusBlocked {
		t.Errorf("expected BLOCKED, got %s", status.Status)
	}
	if status.TestsStatus != TestsFailing {
		t.Errorf("expected FAILING, got %s", status.TestsStatus)
	}
}

func TestParseRalphStatusBlock_EmptyLines(t *testing.T) {
	lines := []string{
		"",
		"STATUS: COMPLETE",
		"",
		"EXIT_SIGNAL: true",
		"",
	}

	status := ParseRalphStatusBlock(lines)
	if status.Status != StatusComplete {
		t.Errorf("expected COMPLETE, got %s", status.Status)
	}
	if !status.ExitSignal {
		t.Error("expected exit signal true")
	}
}

func TestExtractRalphStatusBlocks(t *testing.T) {
	text := `Some output
---RALPH_STATUS---
STATUS: IN_PROGRESS
TASKS_COMPLETED_THIS_LOOP: 2
FILES_MODIFIED: 5
TESTS_STATUS: PASSING
WORK_TYPE: IMPLEMENTATION
EXIT_SIGNAL: false
RECOMMENDATION: Keep going
---END_RALPH_STATUS---
More output
---RALPH_STATUS---
STATUS: COMPLETE
TASKS_COMPLETED_THIS_LOOP: 8
FILES_MODIFIED: 15
TESTS_STATUS: PASSING
WORK_TYPE: TESTING
EXIT_SIGNAL: true
RECOMMENDATION: Done
---END_RALPH_STATUS---
Final output`

	blocks := ExtractRalphStatusBlocks(text)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if blocks[0].Status != StatusInProgress {
		t.Errorf("block 0: expected IN_PROGRESS, got %s", blocks[0].Status)
	}
	if blocks[1].Status != StatusComplete {
		t.Errorf("block 1: expected COMPLETE, got %s", blocks[1].Status)
	}
	if !blocks[1].ExitSignal {
		t.Error("block 1: expected exit signal true")
	}
}

func TestExtractRalphStatusBlocks_NoBlocks(t *testing.T) {
	text := "Just regular output with no status blocks"
	blocks := ExtractRalphStatusBlocks(text)
	if len(blocks) != 0 {
		t.Errorf("expected 0 blocks, got %d", len(blocks))
	}
}

func TestExtractRalphStatusBlocks_IncompleteBlock(t *testing.T) {
	text := `---RALPH_STATUS---
STATUS: IN_PROGRESS
` // No end marker

	blocks := ExtractRalphStatusBlocks(text)
	if len(blocks) != 0 {
		t.Errorf("expected 0 blocks (incomplete), got %d", len(blocks))
	}
}

// --- Tracker integration with status blocks ---

func TestTracker_StatusBlockIntegration(t *testing.T) {
	tr := NewTracker()
	defer tr.Stop()

	statusBlock := `---RALPH_STATUS---
STATUS: COMPLETE
TASKS_COMPLETED_THIS_LOOP: 5
FILES_MODIFIED: 10
TESTS_STATUS: PASSING
WORK_TYPE: IMPLEMENTATION
EXIT_SIGNAL: true
RECOMMENDATION: All tasks done
---END_RALPH_STATUS---`

	tr.ProcessOutput(statusBlock)

	state := tr.GetState()
	if state.LastRalphStatus == nil {
		t.Fatal("expected last ralph status to be set")
	}
	if state.LastRalphStatus.Status != StatusComplete {
		t.Errorf("expected COMPLETE, got %s", state.LastRalphStatus.Status)
	}
	if !state.LastRalphStatus.ExitSignal {
		t.Error("expected exit signal true")
	}
}

func TestTracker_CircuitBreakerIntegration(t *testing.T) {
	tr := NewTracker()
	defer tr.Stop()

	// Send completion indicators
	tr.ProcessOutput("all tasks are completed and done")
	tr.ProcessOutput("everything is done now")

	// Send RALPH_STATUS with EXIT_SIGNAL: true
	statusBlock := `---RALPH_STATUS---
STATUS: COMPLETE
TASKS_COMPLETED_THIS_LOOP: 5
FILES_MODIFIED: 10
TESTS_STATUS: PASSING
WORK_TYPE: IMPLEMENTATION
EXIT_SIGNAL: true
RECOMMENDATION: All tasks done
---END_RALPH_STATUS---`
	tr.ProcessOutput(statusBlock)

	cbStatus := tr.GetCircuitBreakerStatus()
	if cbStatus.CompletionIndicators < 2 {
		t.Errorf("expected at least 2 completion indicators, got %d", cbStatus.CompletionIndicators)
	}
	if !cbStatus.ExitSignalSeen {
		t.Error("expected exit signal seen")
	}
}

// --- String similarity tests ---

func TestStringSimilarity(t *testing.T) {
	tests := []struct {
		a, b   string
		minSim float64
		maxSim float64
	}{
		{"hello", "hello", 1.0, 1.0},
		{"", "", 0.0, 0.0},
		{"abc", "", 0.0, 0.0},
		{"Implement auth handler", "Implement authentication handler", 0.7, 1.0},
		{"completely different", "xyz abc 123", 0.0, 0.4},
		{"Build the widget", "Build the widgets", 0.85, 1.0},
	}

	for _, tc := range tests {
		sim := stringSimilarity(tc.a, tc.b)
		if sim < tc.minSim || sim > tc.maxSim {
			t.Errorf("stringSimilarity(%q, %q) = %.3f, expected [%.3f, %.3f]",
				tc.a, tc.b, sim, tc.minSim, tc.maxSim)
		}
	}
}

// --- Completion indicator detection ---

func TestTracker_CompletionIndicatorDetection(t *testing.T) {
	tr := NewTracker()
	defer tr.Stop()

	indicators := []string{
		"all tasks are completed",
		"nothing left to do",
		"no more tasks",
		"everything is done",
		"project is completed",
		"finished all tasks",
	}

	for _, indicator := range indicators {
		cb := NewCircuitBreaker()
		// Process through a fresh tracker each time to test patterns
		for _, re := range completionIndicatorRegexps {
			if re.MatchString(indicator) {
				cb.RecordCompletion()
				break
			}
		}
		if cb.Status().CompletionIndicators != 1 {
			t.Errorf("indicator %q not detected", indicator)
		}
	}

	// Non-matching text should not trigger
	cb := NewCircuitBreaker()
	for _, re := range completionIndicatorRegexps {
		if re.MatchString("just working on some tasks") {
			cb.RecordCompletion()
			break
		}
	}
	if cb.Status().CompletionIndicators != 0 {
		t.Error("non-completion text should not trigger indicator")
	}

	_ = tr // use tr to avoid unused warning
}

// --- Event debouncing test ---

func TestTracker_EventDebouncing(t *testing.T) {
	tr := NewTracker()
	defer tr.Stop()

	// Rapidly send multiple outputs
	for i := 0; i < 10; i++ {
		tr.ProcessOutput(fmt.Sprintf("- [ ] Task %d", i))
	}

	// Collect events over a short window
	var stateEvents int
	timeout := time.After(200 * time.Millisecond)
	for done := false; !done; {
		select {
		case ev := <-tr.Events():
			if _, ok := ev.(StateChanged); ok {
				stateEvents++
			}
		case <-timeout:
			done = true
		}
	}

	// Due to debouncing, we should see fewer StateChanged events than raw calls
	if stateEvents > 5 {
		t.Errorf("expected debounced state events (got %d, expected fewer than 5)", stateEvents)
	}
}

// --- Edge cases ---

func TestTracker_EmptyOutput(t *testing.T) {
	tr := NewTracker()
	defer tr.Stop()
	tr.ProcessOutput("")
	tr.ProcessOutput("   ")
	state := tr.GetState()
	if len(state.Todos) != 0 {
		t.Errorf("expected 0 todos from empty output, got %d", len(state.Todos))
	}
}

func TestTracker_MixedContent(t *testing.T) {
	tr := NewTracker()
	defer tr.Stop()
	tr.Configure("ALL_TESTS_PASS", 20)

	mixed := strings.Join([]string{
		"Starting work...",
		"- [ ] Write unit tests",
		"- [ ] Run integration tests",
		"<promise>ALL_TESTS_PASS</promise>",
		"ALL_TESTS_PASS",
		"- [x] Write unit tests",
	}, "\n")

	tr.ProcessOutput(mixed)

	state := tr.GetState()
	if !state.CompletionDetected {
		t.Error("expected completion detected")
	}
	if state.LoopIteration != 1 {
		t.Errorf("expected iteration 1, got %d", state.LoopIteration)
	}
	if len(state.Todos) < 2 {
		t.Errorf("expected at least 2 todos, got %d", len(state.Todos))
	}
}
