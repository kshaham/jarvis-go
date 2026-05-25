package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// seedExperienceAt is like seedExperience but takes explicit
// completedAt + lets the test inject a custom outcome score. Kept
// here rather than in search_test.go so memify_test.go is self-
// contained.
func seedExperienceAt(t *testing.T, configID, taskID, title string, labels []string, workingDir, role string, outcome float64, completedAt time.Time) {
	t.Helper()
	rec := ExperienceRecord{
		AgentConfigID:    configID,
		AgentName:        configID,
		AgentRole:        role,
		TaskID:           taskID,
		TaskTitle:        title,
		TaskLabels:       labels,
		WorkingDir:       workingDir,
		CompletionStatus: "completed",
		OutcomeScore:     outcome,
		StartedAt:        completedAt.Add(-15 * time.Minute),
		CompletedAt:      completedAt,
		ApproachSummary:  "Built " + title + " carefully.",
	}
	if err := WriteExperience(rec); err != nil {
		t.Fatalf("seed %s: %v", taskID, err)
	}
}

func TestRunMemify_EmptyLibraryIsNoOp(t *testing.T) {
	withTempLibrary(t)
	got, err := RunMemify(MemifyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkflowsWritten != 0 {
		t.Errorf("WorkflowsWritten=%d want 0", got.WorkflowsWritten)
	}
}

func TestRunMemify_ClustersOfThreeProduceWorkflow(t *testing.T) {
	withTempLibrary(t)
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		seedExperienceAt(t, "vex", "t-"+itoa(i), "Add JWT refresh",
			[]string{"auth", "jwt"}, "/repo/internal/auth", "backend-engineer",
			0.85, now.Add(-time.Duration(i)*time.Hour))
	}

	res, err := RunMemify(MemifyOptions{NowFunc: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if res.WorkflowsWritten != 1 {
		t.Errorf("WorkflowsWritten=%d want 1", res.WorkflowsWritten)
	}
	if len(res.WrittenSlugs) != 1 {
		t.Fatalf("len(WrittenSlugs)=%d want 1", len(res.WrittenSlugs))
	}
	// Slug = first label = "auth".
	if res.WrittenSlugs[0] != "auth" {
		t.Errorf("slug=%q want auth", res.WrittenSlugs[0])
	}

	path := filepath.Join(Dir(), "workflows", "auth.md")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("workflow not on disk: %v", err)
	}
	bs := string(body)
	for _, want := range []string{
		"workflow_slug: auth",
		"avg_outcome: 0.8500",
		"reuse_count_at_creation: 3",
		"agent_role: backend-engineer",
		"task_labels: [auth, jwt]",
		"working_dir_hints: [/repo/internal/auth]",
		"Add JWT refresh", // appears in step content
	} {
		if !strings.Contains(bs, want) {
			t.Errorf("workflow body missing %q\n--- body ---\n%s", want, bs)
		}
	}
}

func TestRunMemify_ClustersBelowMinSizeSkipped(t *testing.T) {
	withTempLibrary(t)
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	// Only 2 experiences — below default MinClusterSize=3.
	seedExperienceAt(t, "vex", "t1", "Add JWT refresh",
		[]string{"auth"}, "/repo/internal/auth", "backend", 0.85, now)
	seedExperienceAt(t, "vex", "t2", "Add JWT refresh",
		[]string{"auth"}, "/repo/internal/auth", "backend", 0.85, now)

	res, _ := RunMemify(MemifyOptions{NowFunc: func() time.Time { return now }})
	if res.WorkflowsWritten != 0 {
		t.Errorf("WorkflowsWritten=%d want 0 (below min cluster size)", res.WorkflowsWritten)
	}
}

func TestRunMemify_LowOutcomeClusterSkipped(t *testing.T) {
	withTempLibrary(t)
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	// Three experiences but their avg outcome is 0.5 — below
	// default MinAvgOutcome=0.70.
	for i, score := range []float64{0.5, 0.5, 0.5} {
		seedExperienceAt(t, "vex", "t"+itoa(i), "Botch JWT refresh",
			[]string{"auth"}, "/repo/internal/auth", "backend", score, now)
	}

	res, _ := RunMemify(MemifyOptions{NowFunc: func() time.Time { return now }})
	if res.WorkflowsWritten != 0 {
		t.Errorf("WorkflowsWritten=%d want 0 (avg outcome below threshold)", res.WorkflowsWritten)
	}
}

func TestRunMemify_AbandonedExperiencesExcluded(t *testing.T) {
	// Non-"completed" experiences must NOT enter any cluster — they
	// could pull the avg outcome below threshold and block
	// promotion of an otherwise-strong cluster.
	withTempLibrary(t)
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		seedExperienceAt(t, "vex", "t-good-"+itoa(i), "Ship feature",
			[]string{"feat"}, "/repo", "backend", 0.85, now)
	}
	// Inject a failed task that, if counted, would drag the avg
	// down to (0.85*3 + 0.1)/4 = 0.65.
	rec := ExperienceRecord{
		AgentConfigID:    "vex",
		AgentRole:        "backend",
		TaskID:           "t-failed",
		TaskTitle:        "Ship feature",
		TaskLabels:       []string{"feat"},
		WorkingDir:       "/repo",
		CompletionStatus: "failed",
		OutcomeScore:     0.10,
		CompletedAt:      now,
	}
	if err := WriteExperience(rec); err != nil {
		t.Fatal(err)
	}

	res, _ := RunMemify(MemifyOptions{NowFunc: func() time.Time { return now }})
	if res.WorkflowsWritten != 1 {
		t.Errorf("WorkflowsWritten=%d want 1 (failed task should be excluded)", res.WorkflowsWritten)
	}
}

func TestRunMemify_StableAcrossRuns(t *testing.T) {
	// Same inputs → same workflow output. Critical for determinism
	// + PageIndex cache stability.
	withTempLibrary(t)
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		seedExperienceAt(t, "vex", "t"+itoa(i), "Add JWT refresh",
			[]string{"auth", "jwt"}, "/repo/internal/auth", "backend", 0.85, now)
	}
	_, _ = RunMemify(MemifyOptions{NowFunc: func() time.Time { return now }})
	first, _ := os.ReadFile(filepath.Join(Dir(), "workflows", "auth.md"))
	_, _ = RunMemify(MemifyOptions{NowFunc: func() time.Time { return now }})
	second, _ := os.ReadFile(filepath.Join(Dir(), "workflows", "auth.md"))
	if string(first) != string(second) {
		t.Errorf("workflow output drifted across runs\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
}

func TestRunMemify_DecaysOldExperiences(t *testing.T) {
	withTempLibrary(t)
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	// One ancient (180d) + one recent. Only the ancient should get
	// the valid_until stamp.
	seedExperienceAt(t, "vex", "t-old", "Ancient task",
		[]string{"misc"}, "/repo", "backend", 0.8, now.Add(-180*24*time.Hour))
	seedExperienceAt(t, "vex", "t-new", "Fresh task",
		[]string{"misc"}, "/repo", "backend", 0.8, now.Add(-1*time.Hour))

	_, err := RunMemify(MemifyOptions{
		NowFunc:         func() time.Time { return now },
		DecayCutoffDays: 90,
	})
	if err != nil {
		t.Fatal(err)
	}

	oldBody, _ := os.ReadFile(filepath.Join(Dir(), "experiences", "vex", "t-old.md"))
	newBody, _ := os.ReadFile(filepath.Join(Dir(), "experiences", "vex", "t-new.md"))

	if !strings.Contains(string(oldBody), "valid_until:") {
		t.Errorf("old experience missing valid_until stamp:\n%s", oldBody)
	}
	if strings.Contains(string(newBody), "valid_until:") {
		t.Errorf("recent experience incorrectly stamped:\n%s", newBody)
	}
}

func TestRunMemify_DecayIsIdempotent(t *testing.T) {
	withTempLibrary(t)
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	seedExperienceAt(t, "vex", "t-old", "Old", []string{"x"}, "/r", "backend",
		0.8, now.Add(-180*24*time.Hour))

	// Run memify twice. The second pass MUST NOT stamp a second
	// valid_until line.
	_, _ = RunMemify(MemifyOptions{NowFunc: func() time.Time { return now }, DecayCutoffDays: 90})
	_, _ = RunMemify(MemifyOptions{NowFunc: func() time.Time { return now }, DecayCutoffDays: 90})

	body, _ := os.ReadFile(filepath.Join(Dir(), "experiences", "vex", "t-old.md"))
	count := strings.Count(string(body), "valid_until:")
	if count != 1 {
		t.Errorf("valid_until appears %d times, want exactly 1\n--- body ---\n%s",
			count, body)
	}
}

func TestRunMemify_DifferentLabelsetsClusterIndependently(t *testing.T) {
	// Three "auth" experiences + three "ui" experiences should
	// produce two distinct workflows.
	withTempLibrary(t)
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		seedExperienceAt(t, "vex", "auth-"+itoa(i), "Auth task",
			[]string{"auth"}, "/repo/auth", "backend", 0.85, now)
		seedExperienceAt(t, "kit", "ui-"+itoa(i), "UI task",
			[]string{"ui"}, "/repo/web", "frontend", 0.85, now)
	}

	res, _ := RunMemify(MemifyOptions{NowFunc: func() time.Time { return now }})
	if res.WorkflowsWritten != 2 {
		t.Errorf("WorkflowsWritten=%d want 2", res.WorkflowsWritten)
	}
	gotSlugs := map[string]bool{}
	for _, s := range res.WrittenSlugs {
		gotSlugs[s] = true
	}
	for _, want := range []string{"auth", "ui"} {
		if !gotSlugs[want] {
			t.Errorf("missing slug %q in %v", want, res.WrittenSlugs)
		}
	}
}

func TestClusterKey_DeterministicAcrossLabelOrder(t *testing.T) {
	// [auth, jwt] and [jwt, auth] must produce the same key so
	// reordered labels don't fragment a cluster.
	k1, _ := clusterKey("backend", []string{"auth", "jwt"})
	k2, _ := clusterKey("backend", []string{"jwt", "auth"})
	if k1 != k2 {
		t.Errorf("cluster keys differ for reordered labels: %q vs %q", k1, k2)
	}
}

func TestClusterKey_EmptyInputsCollapseToPlaceholder(t *testing.T) {
	k, labels := clusterKey("", nil)
	if !strings.Contains(k, "(any)") || !strings.Contains(k, "(no-labels)") {
		t.Errorf("empty-key=%q want placeholders", k)
	}
	if len(labels) != 0 {
		t.Errorf("labels=%v want empty", labels)
	}
}
