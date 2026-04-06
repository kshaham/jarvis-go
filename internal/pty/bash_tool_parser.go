package pty

import (
	"regexp"
	"strings"
)

// ToolInvocation represents a detected tool invocation from terminal output.
type ToolInvocation struct {
	Name   string `json:"name"`
	Status string `json:"status"` // "started" or "completed"
}

// Tool invocation patterns from Claude CLI terminal output.
// Claude CLI shows tool activity with spinner/check patterns:
//   - Started: hourglass emoji followed by tool name and parens
//   - Completed: checkmark emoji followed by tool name and parens
//   - Also matches "Running: tool_name" patterns
var (
	// Matches patterns like: "Bash(...)" "Read(...)" "Write(...)" "Edit(...)" "Glob(...)" "Grep(...)"
	toolStartedPattern   = regexp.MustCompile(`(?:⏳|⠋|⠙|⠹|⠸|⠼|⠴|⠦|⠧)\s*(\w+)\s*\(`)
	toolCompletedPattern = regexp.MustCompile(`(?:✓|✔|✅)\s*(\w+)\s*\(`)
	runningToolPattern   = regexp.MustCompile(`Running:\s*(\w+)`)
)

// Known tool names from Claude CLI.
var knownTools = map[string]bool{
	"Bash":       true,
	"Read":       true,
	"Write":      true,
	"Edit":       true,
	"Glob":       true,
	"Grep":       true,
	"WebFetch":   true,
	"WebSearch":  true,
	"TodoRead":   true,
	"TodoWrite":  true,
	"Agent":      true,
	"Task":       true,
	"Skill":      true,
	"LSP":        true,
}

// ParseToolInvocations extracts tool invocation events from terminal output text.
// Detects both started (spinner) and completed (checkmark) tool invocations.
func ParseToolInvocations(text string) []ToolInvocation {
	if text == "" {
		return nil
	}

	var invocations []ToolInvocation

	// Check for started tools
	for _, match := range toolStartedPattern.FindAllStringSubmatch(text, -1) {
		if len(match) >= 2 {
			name := strings.TrimSpace(match[1])
			if isKnownTool(name) {
				invocations = append(invocations, ToolInvocation{
					Name:   name,
					Status: "started",
				})
			}
		}
	}

	// Check for completed tools
	for _, match := range toolCompletedPattern.FindAllStringSubmatch(text, -1) {
		if len(match) >= 2 {
			name := strings.TrimSpace(match[1])
			if isKnownTool(name) {
				invocations = append(invocations, ToolInvocation{
					Name:   name,
					Status: "completed",
				})
			}
		}
	}

	// Check for "Running: tool_name" pattern
	for _, match := range runningToolPattern.FindAllStringSubmatch(text, -1) {
		if len(match) >= 2 {
			name := strings.TrimSpace(match[1])
			if isKnownTool(name) {
				invocations = append(invocations, ToolInvocation{
					Name:   name,
					Status: "started",
				})
			}
		}
	}

	return invocations
}

// isKnownTool checks if the tool name is a recognized Claude CLI tool.
func isKnownTool(name string) bool {
	return knownTools[name]
}
