package office

import (
	"strings"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/library"
)

// TestNextTaskAntiPatternHint_EmptyTitleAndManyAvoids drives two branches in
// nextTaskAntiPatternHint:
//   - title == "" -> fall back to the anti-pattern Slug
//   - the WhatToAvoid loop's `i >= 2` break (only the first two bullets render)
func TestNextTaskAntiPatternHint_EmptyTitleAndManyAvoids(t *testing.T) {
	withTempLibraryForOffice(t)

	ap := library.AntiPattern{
		Slug:            "slug-fallback-name",
		Title:           "", // empty -> Slug is used as the title
		AvgOutcome:      0.1,
		AgentRole:       "backend-engineer",
		TaskLabels:      []string{"auth"},
		WorkingDirHints: []string{"/repo/internal/auth"},
		WhyItFails:      "leaks state",
		WhatToAvoid: []string{
			"first avoid item",
			"second avoid item",
			"third avoid item (must be dropped by the i>=2 break)",
		},
		CreatedAt: time.Now().Add(-time.Hour),
	}
	if err := library.WriteAntiPattern(ap); err != nil {
		t.Fatal(err)
	}

	rec := &nextTaskHintRecordingStore{}
	m := NewManager(rec, func(string, interface{}) {}, nil)
	m.RegisterAgent("sess-a", "iris",
		&TeamMemberConfig{ID: "iris", Name: "Iris", Role: "backend-engineer"})
	agent := m.GetAgent("sess-a")
	agent.SetWorktreePath("/repo/internal/auth")
	agent.AddTask(&Task{
		ID:     "t-new",
		Title:  "Add auth thing",
		Status: TaskPending,
		Labels: []string{"auth"},
	})

	m.PromptNextTask("sess-a", "Add auth thing", "t-new")

	prompt := rec.lastPrompt
	if !strings.Contains(prompt, "[AVOID]") {
		t.Fatalf("expected an [AVOID] hint, got:\n%s", prompt)
	}
	// Slug used because Title was empty.
	if !strings.Contains(prompt, "slug-fallback-name") {
		t.Errorf("expected slug fallback in the hint title, got:\n%s", prompt)
	}
	// The first two avoid items render; the third must be dropped by i>=2.
	if !strings.Contains(prompt, "first avoid item") || !strings.Contains(prompt, "second avoid item") {
		t.Errorf("expected the first two avoid items, got:\n%s", prompt)
	}
	if strings.Contains(prompt, "third avoid item") {
		t.Errorf("expected the third avoid item to be dropped by the i>=2 break, got:\n%s", prompt)
	}
}
