package ralph

import (
	"regexp"
	"strconv"
	"strings"
)

// RalphStatusValue represents the overall status of a Ralph iteration.
type RalphStatusValue string

const (
	StatusInProgress RalphStatusValue = "IN_PROGRESS"
	StatusComplete   RalphStatusValue = "COMPLETE"
	StatusBlocked    RalphStatusValue = "BLOCKED"
)

// TestsStatusValue represents the tests status reported by a RALPH_STATUS block.
type TestsStatusValue string

const (
	TestsPassing TestsStatusValue = "PASSING"
	TestsFailing TestsStatusValue = "FAILING"
	TestsNotRun  TestsStatusValue = "NOT_RUN"
)

// WorkTypeValue represents the type of work being done in the current iteration.
type WorkTypeValue string

const (
	WorkImplementation WorkTypeValue = "IMPLEMENTATION"
	WorkTesting        WorkTypeValue = "TESTING"
	WorkDocumentation  WorkTypeValue = "DOCUMENTATION"
	WorkRefactoring    WorkTypeValue = "REFACTORING"
)

// RalphStatus is the structured result of parsing a RALPH_STATUS block from
// terminal output. These blocks are emitted by the Ralph Wiggum plugin:
//
//	---RALPH_STATUS---
//	STATUS: IN_PROGRESS
//	TASKS_COMPLETED_THIS_LOOP: 3
//	FILES_MODIFIED: 12
//	TESTS_STATUS: PASSING
//	WORK_TYPE: IMPLEMENTATION
//	EXIT_SIGNAL: false
//	RECOMMENDATION: Continue with remaining tasks
//	---END_RALPH_STATUS---
type RalphStatus struct {
	Status         RalphStatusValue `json:"status"`
	TasksCompleted int              `json:"tasksCompleted"`
	FilesModified  int              `json:"filesModified"`
	TestsStatus    TestsStatusValue `json:"testsStatus"`
	WorkType       WorkTypeValue    `json:"workType"`
	ExitSignal     bool             `json:"exitSignal"`
	Recommendation string           `json:"recommendation"`
}

// statusStartMarker is the literal RALPH_STATUS start marker, used to detect a
// start marker that may be split across PTY read boundaries (HasPrefix check).
const statusStartMarker = "---RALPH_STATUS---"

// Pre-compiled patterns for RALPH_STATUS block fields.
var (
	statusStartPattern    = regexp.MustCompile(`^---RALPH_STATUS---\s*$`)
	statusEndPattern      = regexp.MustCompile(`^---END_RALPH_STATUS---\s*$`)
	statusFieldPattern    = regexp.MustCompile(`(?i)^STATUS:\s*(IN_PROGRESS|COMPLETE|BLOCKED)\s*$`)
	tasksCompletedPattern = regexp.MustCompile(`(?i)^TASKS_COMPLETED_THIS_LOOP:\s*(\d+)\s*$`)
	filesModifiedPattern  = regexp.MustCompile(`(?i)^FILES_MODIFIED:\s*(\d+)\s*$`)
	testsStatusPattern    = regexp.MustCompile(`(?i)^TESTS_STATUS:\s*(PASSING|FAILING|NOT_RUN)\s*$`)
	workTypePattern       = regexp.MustCompile(`(?i)^WORK_TYPE:\s*(IMPLEMENTATION|TESTING|DOCUMENTATION|REFACTORING)\s*$`)
	exitSignalPattern     = regexp.MustCompile(`(?i)^EXIT_SIGNAL:\s*(true|false)\s*$`)
	recommendationPattern = regexp.MustCompile(`(?i)^RECOMMENDATION:\s*(.+)$`)
)

// ParseRalphStatusBlock extracts a RalphStatus from the lines between
// ---RALPH_STATUS--- and ---END_RALPH_STATUS--- markers.
// The lines parameter should NOT include the marker lines themselves.
func ParseRalphStatusBlock(lines []string) RalphStatus {
	status := RalphStatus{
		TestsStatus: TestsNotRun,
	}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if m := statusFieldPattern.FindStringSubmatch(line); len(m) > 1 {
			status.Status = RalphStatusValue(strings.ToUpper(m[1]))
			continue
		}
		if m := tasksCompletedPattern.FindStringSubmatch(line); len(m) > 1 {
			if n, err := strconv.Atoi(m[1]); err == nil {
				status.TasksCompleted = n
			}
			continue
		}
		if m := filesModifiedPattern.FindStringSubmatch(line); len(m) > 1 {
			if n, err := strconv.Atoi(m[1]); err == nil {
				status.FilesModified = n
			}
			continue
		}
		if m := testsStatusPattern.FindStringSubmatch(line); len(m) > 1 {
			status.TestsStatus = TestsStatusValue(strings.ToUpper(m[1]))
			continue
		}
		if m := workTypePattern.FindStringSubmatch(line); len(m) > 1 {
			status.WorkType = WorkTypeValue(strings.ToUpper(m[1]))
			continue
		}
		if m := exitSignalPattern.FindStringSubmatch(line); len(m) > 1 {
			status.ExitSignal = strings.EqualFold(m[1], "true")
			continue
		}
		if m := recommendationPattern.FindStringSubmatch(line); len(m) > 1 {
			status.Recommendation = m[1]
			continue
		}
	}
	return status
}

// ExtractRalphStatusBlocks scans text for one or more RALPH_STATUS blocks
// and returns parsed results. This is useful for processing a chunk of
// terminal output that may contain multiple status reports.
func ExtractRalphStatusBlocks(text string) []RalphStatus {
	var results []RalphStatus
	lines := strings.Split(text, "\n")

	var blockLines []string
	inBlock := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if statusStartPattern.MatchString(trimmed) {
			inBlock = true
			blockLines = nil
			continue
		}
		if statusEndPattern.MatchString(trimmed) {
			if inBlock && len(blockLines) > 0 {
				results = append(results, ParseRalphStatusBlock(blockLines))
			}
			inBlock = false
			blockLines = nil
			continue
		}
		if inBlock {
			blockLines = append(blockLines, trimmed)
		}
	}
	return results
}
