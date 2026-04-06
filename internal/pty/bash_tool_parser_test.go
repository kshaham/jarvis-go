package pty

import (
	"testing"
)

func TestParseToolInvocations_Started(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []ToolInvocation
	}{
		{
			name:  "spinner Bash started",
			input: "\u23f3 Bash(ls -la)",
			expected: []ToolInvocation{
				{Name: "Bash", Status: "started"},
			},
		},
		{
			name:  "braille spinner Read started",
			input: "\u280b Read(/path/to/file)",
			expected: []ToolInvocation{
				{Name: "Read", Status: "started"},
			},
		},
		{
			name:  "Running pattern",
			input: "Running: Bash",
			expected: []ToolInvocation{
				{Name: "Bash", Status: "started"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseToolInvocations(tt.input)
			if len(result) != len(tt.expected) {
				t.Fatalf("expected %d invocations, got %d", len(tt.expected), len(result))
			}
			for i, inv := range result {
				if inv.Name != tt.expected[i].Name {
					t.Errorf("invocation %d: expected name %s, got %s", i, tt.expected[i].Name, inv.Name)
				}
				if inv.Status != tt.expected[i].Status {
					t.Errorf("invocation %d: expected status %s, got %s", i, tt.expected[i].Status, inv.Status)
				}
			}
		})
	}
}

func TestParseToolInvocations_Completed(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []ToolInvocation
	}{
		{
			name:  "checkmark Bash completed",
			input: "\u2713 Bash(ls -la)",
			expected: []ToolInvocation{
				{Name: "Bash", Status: "completed"},
			},
		},
		{
			name:  "heavy check Write completed",
			input: "\u2714 Write(/path/to/file)",
			expected: []ToolInvocation{
				{Name: "Write", Status: "completed"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseToolInvocations(tt.input)
			if len(result) != len(tt.expected) {
				t.Fatalf("expected %d invocations, got %d", len(tt.expected), len(result))
			}
			for i, inv := range result {
				if inv.Name != tt.expected[i].Name {
					t.Errorf("invocation %d: expected name %s, got %s", i, tt.expected[i].Name, inv.Name)
				}
				if inv.Status != tt.expected[i].Status {
					t.Errorf("invocation %d: expected status %s, got %s", i, tt.expected[i].Status, inv.Status)
				}
			}
		})
	}
}

func TestParseToolInvocations_EmptyInput(t *testing.T) {
	result := ParseToolInvocations("")
	if result != nil {
		t.Errorf("expected nil for empty input, got %v", result)
	}
}

func TestParseToolInvocations_NoTools(t *testing.T) {
	result := ParseToolInvocations("just regular terminal output with no tools")
	if result != nil {
		t.Errorf("expected nil for no tools, got %v", result)
	}
}

func TestParseToolInvocations_UnknownTool(t *testing.T) {
	// Unknown tool name should be filtered out
	result := ParseToolInvocations("\u23f3 FakeTool(args)")
	if result != nil {
		t.Errorf("expected nil for unknown tool, got %v", result)
	}
}

func TestParseToolInvocations_MultipleTools(t *testing.T) {
	input := "\u23f3 Bash(ls)\n\u2713 Read(/file)\n\u23f3 Write(/out)"
	result := ParseToolInvocations(input)
	if len(result) != 3 {
		t.Fatalf("expected 3 invocations, got %d", len(result))
	}

	// Build lookup maps since started and completed tools are scanned in separate passes
	startedTools := make(map[string]bool)
	completedTools := make(map[string]bool)
	for _, inv := range result {
		if inv.Status == "started" {
			startedTools[inv.Name] = true
		} else if inv.Status == "completed" {
			completedTools[inv.Name] = true
		}
	}

	if !startedTools["Bash"] {
		t.Error("expected Bash started")
	}
	if !startedTools["Write"] {
		t.Error("expected Write started")
	}
	if !completedTools["Read"] {
		t.Error("expected Read completed")
	}
}

func TestIsKnownTool(t *testing.T) {
	known := []string{"Bash", "Read", "Write", "Edit", "Glob", "Grep", "Agent", "Task"}
	for _, name := range known {
		if !isKnownTool(name) {
			t.Errorf("expected %s to be a known tool", name)
		}
	}

	unknown := []string{"FakeTool", "bash", "read", ""}
	for _, name := range unknown {
		if isKnownTool(name) {
			t.Errorf("expected %s to NOT be a known tool", name)
		}
	}
}
