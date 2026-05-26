package library

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// seedFailedExperience writes a failed-status experience. Used by
// anti-pattern tests since clusterExperiences(true) admits failed
// entries.
func seedFailedExperience(t *testing.T, configID, taskID, title string, labels []string, workingDir, role string, outcome float64, completedAt time.Time) {
	t.Helper()
	rec := ExperienceRecord{
		AgentConfigID:    configID,
		AgentName:        configID,
		AgentRole:        role,
		TaskID:           taskID,
		TaskTitle:        title,
		TaskLabels:       labels,
		WorkingDir:       workingDir,
		CompletionStatus: "failed",
		OutcomeScore:     outcome,
		StartedAt:        completedAt.Add(-15 * time.Minute),
		CompletedAt:      completedAt,
		ApproachSummary:  "Tried " + title + " but it broke.",
	}
	if err := WriteExperience(rec); err != nil {
		t.Fatalf("seed %s: %v", taskID, err)
	}
}

func TestRunMemify_LowOutcomeClusterProducesAntiPattern(t *testing.T) {
	withTempLibrary(t)
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		seedFailedExperience(t, "vex", "fail-"+itoa(i), "Auth refresh",
			[]string{"auth"}, "/repo/internal/auth", "backend",
			0.20, now.Add(-time.Duration(i)*time.Hour))
	}

	res, err := RunMemify(MemifyOptions{NowFunc: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if res.AntiPatternsWritten != 1 {
		t.Errorf("AntiPatternsWritten=%d want 1", res.AntiPatternsWritten)
	}
	if len(res.AntiPatternSlugs) != 1 || res.AntiPatternSlugs[0] != "auth" {
		t.Errorf("AntiPatternSlugs=%v want [auth]", res.AntiPatternSlugs)
	}

	path := filepath.Join(Dir(), "anti-patterns", "auth.md")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("anti-pattern not on disk: %v", err)
	}
	bs := string(body)
	for _, want := range []string{
		"kind: anti-pattern",
		"anti_pattern_slug: auth",
		"observation_count: 3",
		"agent_role: backend",
		"# Anti-pattern: Avoid: Auth task pitfalls",
		"## What went wrong",
		"Auth refresh", // appears in the cited observation
	} {
		if !strings.Contains(bs, want) {
			t.Errorf("body missing %q\n--- body ---\n%s", want, bs)
		}
	}
}

func TestRunMemify_MediocreClusterProducesNeitherWorkflowNorAntiPattern(t *testing.T) {
	// Cluster avg sits in the [0.40, 0.70) no-mans-land — neither
	// good enough to codify nor bad enough to warn about.
	withTempLibrary(t)
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		seedExperienceAt(t, "vex", "mid-"+itoa(i), "Some task",
			[]string{"meh"}, "/repo", "backend", 0.55, now)
	}

	res, _ := RunMemify(MemifyOptions{NowFunc: func() time.Time { return now }})
	if res.WorkflowsWritten != 0 || res.AntiPatternsWritten != 0 {
		t.Errorf("mediocre cluster produced output; workflows=%d anti=%d",
			res.WorkflowsWritten, res.AntiPatternsWritten)
	}
}

func TestRunMemify_BothWorkflowAndAntiPatternOnSplitOutcomes(t *testing.T) {
	// One cluster of high-outcome auth experiences → workflow.
	// One cluster of failed ui experiences → anti-pattern.
	// The two paths are independent and both should fire.
	withTempLibrary(t)
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		seedExperienceAt(t, "vex", "auth-good-"+itoa(i), "Auth good",
			[]string{"auth"}, "/repo/auth", "backend", 0.90, now)
	}
	for i := 0; i < 3; i++ {
		seedFailedExperience(t, "kit", "ui-bad-"+itoa(i), "UI bad",
			[]string{"ui"}, "/repo/web", "frontend", 0.15, now)
	}

	res, _ := RunMemify(MemifyOptions{NowFunc: func() time.Time { return now }})
	if res.WorkflowsWritten != 1 {
		t.Errorf("WorkflowsWritten=%d want 1", res.WorkflowsWritten)
	}
	if res.AntiPatternsWritten != 1 {
		t.Errorf("AntiPatternsWritten=%d want 1", res.AntiPatternsWritten)
	}
}

func TestRunMemify_AntiPatternLLMEnrichmentSucceeds(t *testing.T) {
	withTempLibrary(t)
	installLLMStub(t, `{"result":"{\"title\":\"LLM-anti-pattern\",\"why_it_fails\":\"forgot the cookie clear\",\"what_to_avoid\":[\"never bypass middleware\",\"always clear refresh cookie\"],\"what_went_wrong\":[\"agents skipped cookie clearing\"]}"}`)

	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		seedFailedExperience(t, "vex", "f-"+itoa(i), "Auth fail",
			[]string{"auth"}, "/repo", "backend", 0.20, now)
	}

	res, err := RunMemify(MemifyOptions{
		NowFunc:   func() time.Time { return now },
		LLMBudget: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.AntiPatternsWritten != 1 {
		t.Fatalf("AntiPatternsWritten=%d want 1", res.AntiPatternsWritten)
	}
	if res.LLMEnriched < 1 {
		t.Errorf("LLMEnriched=%d want >=1 (anti-pattern should consume budget)", res.LLMEnriched)
	}

	body, _ := os.ReadFile(filepath.Join(Dir(), "anti-patterns", "auth.md"))
	bs := string(body)
	for _, want := range []string{
		"LLM-anti-pattern",
		"forgot the cookie clear",
		"never bypass middleware",
		"## What to avoid",
	} {
		if !strings.Contains(bs, want) {
			t.Errorf("body missing %q\n--- body ---\n%s", want, bs)
		}
	}
}

func TestRunMemify_LLMBudgetSharedBetweenWorkflowsAndAntiPatterns(t *testing.T) {
	// Workflow extraction runs first; a budget of 1 means the
	// workflow consumes the slot and the anti-pattern falls back
	// to the heuristic.
	withTempLibrary(t)
	installLLMStub(t, `{"result":"{\"title\":\"shared-budget-target\",\"steps\":[\"x\"]}"}`)

	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		seedExperienceAt(t, "vex", "good-"+itoa(i), "Good task",
			[]string{"auth"}, "/repo/auth", "backend", 0.90, now)
	}
	for i := 0; i < 3; i++ {
		seedFailedExperience(t, "kit", "bad-"+itoa(i), "Bad task",
			[]string{"ui"}, "/repo/web", "frontend", 0.15, now)
	}

	res, err := RunMemify(MemifyOptions{
		NowFunc:   func() time.Time { return now },
		LLMBudget: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.LLMEnriched != 1 {
		t.Errorf("LLMEnriched=%d want exactly 1 (shared budget cap)", res.LLMEnriched)
	}
	// Workflow path runs first; the LLM-enriched title appears in
	// the workflow doc, NOT the anti-pattern doc.
	wfBody, _ := os.ReadFile(filepath.Join(Dir(), "workflows", "auth.md"))
	if !strings.Contains(string(wfBody), "shared-budget-target") {
		t.Errorf("workflow didn't claim the budget slot; body:\n%s", wfBody)
	}
	apBody, _ := os.ReadFile(filepath.Join(Dir(), "anti-patterns", "ui.md"))
	if strings.Contains(string(apBody), "shared-budget-target") {
		t.Errorf("anti-pattern unexpectedly LLM-enriched; body:\n%s", apBody)
	}
}

func TestRunMemify_AntiPatternLLMFailureFallsBackToHeuristic(t *testing.T) {
	withTempLibrary(t)
	installLLMFailingStub(t)

	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		seedFailedExperience(t, "vex", "f-"+itoa(i), "Auth fail",
			[]string{"auth"}, "/repo", "backend", 0.20, now)
	}

	res, _ := RunMemify(MemifyOptions{
		NowFunc:   func() time.Time { return now },
		LLMBudget: 5,
	})
	if res.AntiPatternsWritten != 1 {
		t.Errorf("AntiPatternsWritten=%d want 1 (heuristic fallback)", res.AntiPatternsWritten)
	}
	if res.LLMFailed < 1 {
		t.Errorf("LLMFailed=%d want >=1", res.LLMFailed)
	}
	body, _ := os.ReadFile(filepath.Join(Dir(), "anti-patterns", "auth.md"))
	if !strings.Contains(string(body), "Auth fail") {
		t.Errorf("heuristic observations missing from anti-pattern body:\n%s", body)
	}
}

func TestExtractAntiPatternWithLLM_HappyPath(t *testing.T) {
	installLLMStub(t, `{"result":"{\"title\":\"Cookie pitfall\",\"why_it_fails\":\"missed clear\",\"what_to_avoid\":[\"clear cookie\"],\"what_went_wrong\":[\"observed skipped clear\"]}"}`)

	cluster := &experienceCluster{
		role:   "backend",
		labels: []string{"auth"},
		entries: []*experienceMeta{
			{TaskTitle: "Botch refresh", OutcomeScore: 0.15, CompletionStatus: "failed", ApproachSummary: "skipped clearing"},
		},
	}
	ext, err := extractAntiPatternWithLLM(context.Background(), cluster, "")
	if err != nil {
		t.Fatal(err)
	}
	if ext.Title != "Cookie pitfall" {
		t.Errorf("title=%q", ext.Title)
	}
	if len(ext.WhatToAvoid) != 1 {
		t.Errorf("what_to_avoid=%d want 1", len(ext.WhatToAvoid))
	}
}

func TestBuildAntiPatternPrompt_IncludesFailureContext(t *testing.T) {
	cluster := &experienceCluster{
		role:   "backend",
		labels: []string{"auth"},
		entries: []*experienceMeta{
			{TaskTitle: "Botch", OutcomeScore: 0.10, CompletionStatus: "failed", ApproachSummary: "tried hack"},
		},
	}
	prompt := buildAntiPatternPrompt(cluster)
	for _, want := range []string{
		"FAILED or LOW-OUTCOME",
		"Agent role: backend",
		"Botch",
		"outcome 0.10",
		"What was tried: tried hack",
		"what_to_avoid",
		"why_it_fails",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestMergeIntoAntiPattern_OverridesAuthoredFieldsOnly(t *testing.T) {
	base := AntiPattern{
		Slug:               "auth",
		Title:              "heuristic title",
		DerivedFromTaskIDs: []string{"t1"},
		AvgOutcome:         0.20,
		ObservationCount:   3,
		AgentRole:          "backend",
		TaskLabels:         []string{"auth"},
		WhatWentWrong:      []string{"heuristic observation"},
	}
	ext := &ExtractedAntiPattern{
		Title:         "llm title",
		WhyItFails:    "the root cause",
		WhatToAvoid:   []string{"llm advice"},
		WhatWentWrong: []string{"llm observation"},
	}
	got := mergeIntoAntiPattern(base, ext)
	if got.Slug != "auth" || got.ObservationCount != 3 {
		t.Errorf("identity fields drifted: %+v", got)
	}
	if got.Title != "llm title" {
		t.Errorf("title=%q want llm title", got.Title)
	}
	if got.WhyItFails != "the root cause" {
		t.Errorf("why_it_fails=%q", got.WhyItFails)
	}
	if len(got.WhatToAvoid) != 1 {
		t.Errorf("what_to_avoid=%v want [llm advice]", got.WhatToAvoid)
	}
}
