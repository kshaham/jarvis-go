package library

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// withTempLibrary points Dir() at a temp directory and returns the
// absolute path. Test cleanup via t.TempDir() handles deletion;
// SetDirForTest cleanup restores the previous cached path.
func withTempLibrary(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, sub := range []string{"experiences", "workflows", "standups", "experiments"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
	}
	restore := SetDirForTest(root)
	t.Cleanup(restore)
	return root
}

// sampleExperience returns a deterministic ExperienceRecord for
// frontmatter + body assertions. All time fields use a fixed
// instant so the rendered string is byte-stable.
func sampleExperience() ExperienceRecord {
	t0 := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	return ExperienceRecord{
		AgentConfigID:       "vex",
		AgentName:           "Vex",
		AgentRole:           "backend-engineer",
		TaskID:              "task-001",
		TaskTitle:           "Add JWT refresh",
		TaskLabels:          []string{"auth", "jwt"},
		WorkingDir:          "/repo/internal/auth",
		CompletionStatus:    "completed",
		OutcomeScore:        0.87,
		UltrareviewSeverity: "nit",
		RecoveryAttempts:    1,
		StartedAt:           t0,
		CompletedAt:         t0.Add(20 * time.Minute),
		DurationMS:          1_200_000,
		CostUSD:             0.42,
		ApproachSummary:     "Built middleware first, wired in handler last.",
		Learnings:           []string{"keep middleware pure", "verify via curl before claiming done"},
		FilesModified:       []string{"internal/auth/middleware.go", "internal/auth/handler.go"},
	}
}

func TestWriteExperience_CreatesFileAtExpectedPath(t *testing.T) {
	root := withTempLibrary(t)
	rec := sampleExperience()
	if err := WriteExperience(rec); err != nil {
		t.Fatalf("WriteExperience: %v", err)
	}
	want := filepath.Join(root, "experiences", "vex", "task-001.md")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected file at %s: %v", want, err)
	}
}

func TestWriteExperience_RendersExpectedFrontmatterAndSections(t *testing.T) {
	withTempLibrary(t)
	rec := sampleExperience()
	if err := WriteExperience(rec); err != nil {
		t.Fatal(err)
	}
	body := readExperience(t, "vex", "task-001")

	// Frontmatter markers
	if !strings.HasPrefix(body, "---\n") {
		t.Errorf("missing opening frontmatter delimiter; got: %q", body[:min(50, len(body))])
	}
	if !strings.Contains(body, "\n---\n\n") {
		t.Error("missing closing frontmatter delimiter")
	}

	// Key fields land
	for _, want := range []string{
		"agent_config_id: vex",
		"task_id: task-001",
		"task_title: Add JWT refresh", // bare scalar, spaces ok in YAML
		"task_labels: [auth, jwt]",
		"completion_status: completed",
		"outcome_score: 0.8700",
		"# Add JWT refresh",
		"## Approach",
		"## Files modified",
		"## Learnings",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q\n--- body ---\n%s", want, body)
		}
	}
}

func TestWriteExperience_DeterministicAcrossRuns(t *testing.T) {
	// Same input → byte-identical output (PageIndex cache depends
	// on this).
	withTempLibrary(t)
	rec := sampleExperience()
	if err := WriteExperience(rec); err != nil {
		t.Fatal(err)
	}
	first := readExperience(t, "vex", "task-001")

	// Re-render to a fresh library to remove filesystem mtime
	// effects.
	withTempLibrary(t)
	if err := WriteExperience(rec); err != nil {
		t.Fatal(err)
	}
	second := readExperience(t, "vex", "task-001")

	if first != second {
		t.Errorf("rendering is not deterministic\n--- first ---\n%s\n--- second ---\n%s",
			first, second)
	}
}

func TestWriteExperience_AtomicWriteLeavesNoTmpOnSuccess(t *testing.T) {
	// After a successful write, no .tmp file should remain — the
	// atomic-rename pattern must not leak filesystem cruft.
	root := withTempLibrary(t)
	if err := WriteExperience(sampleExperience()); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "experiences", "vex"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("leftover tmp file: %s", e.Name())
		}
	}
}

func TestWriteExperience_OverwritesExistingFile(t *testing.T) {
	// Re-running a task should produce a corrected experience, not
	// stack duplicates.
	withTempLibrary(t)
	rec := sampleExperience()
	if err := WriteExperience(rec); err != nil {
		t.Fatal(err)
	}
	rec.OutcomeScore = 0.50 // simulate a corrected re-run
	if err := WriteExperience(rec); err != nil {
		t.Fatal(err)
	}
	body := readExperience(t, "vex", "task-001")
	if !strings.Contains(body, "outcome_score: 0.5000") {
		t.Errorf("corrected score not present in re-write; body:\n%s", body)
	}
	// Old score must NOT linger.
	if strings.Contains(body, "outcome_score: 0.8700") {
		t.Errorf("old score still present after overwrite")
	}
}

func TestAppendUltrareview_AddsSectionToExisting(t *testing.T) {
	withTempLibrary(t)
	rec := sampleExperience()
	if err := WriteExperience(rec); err != nil {
		t.Fatal(err)
	}
	ur := UltrareviewAppendRecord{
		AgentConfigID: "vex",
		TaskID:        "task-001",
		Findings: []Finding{
			{Severity: "blocker", Title: "Race in writePump", File: "internal/web/sse/client.go", Line: 67, Description: "closed-channel send"},
			{Severity: "nit", Title: "Naming nit"},
		},
		Summary:   "1 blocker, 1 nit.",
		Took:      300 * time.Millisecond,
		Timestamp: time.Date(2026, 5, 1, 12, 30, 0, 0, time.UTC),
	}
	if err := AppendUltrareview(ur); err != nil {
		t.Fatalf("AppendUltrareview: %v", err)
	}
	body := readExperience(t, "vex", "task-001")
	if !strings.Contains(body, "## Ultrareview 2026-05-01T12:30:00Z") {
		t.Error("ultrareview section header missing")
	}
	// Blocker should appear before the nit (severity ordering).
	blockerIdx := strings.Index(body, "Race in writePump")
	nitIdx := strings.Index(body, "Naming nit")
	if blockerIdx < 0 || nitIdx < 0 || blockerIdx > nitIdx {
		t.Errorf("severity ordering broken: blockerIdx=%d nitIdx=%d", blockerIdx, nitIdx)
	}
}

func TestAppendUltrareview_MissingExperienceReturnsError(t *testing.T) {
	withTempLibrary(t)
	err := AppendUltrareview(UltrareviewAppendRecord{
		AgentConfigID: "vex",
		TaskID:        "ghost-task",
		Findings:      []Finding{{Severity: "nit", Title: "x"}},
		Timestamp:     time.Now(),
	})
	if err == nil {
		t.Error("expected error when experience file is missing")
	}
}

func TestWriteWorkflow_RendersWithDerivedFromFrontmatter(t *testing.T) {
	withTempLibrary(t)
	w := Workflow{
		Slug:                 "auth-jwt-refresh",
		Title:                "Add JWT refresh",
		DerivedFromTaskIDs:   []string{"task-001", "task-002", "task-003"},
		AvgOutcome:           0.89,
		ReuseCountAtCreation: 3,
		AgentRole:            "backend-engineer",
		TaskLabels:           []string{"auth", "jwt"},
		WorkingDirHints:      []string{"internal/auth/"},
		Steps:                []string{"Build middleware first", "Wire in handler last", "Verify via curl"},
		KnownPitfalls:        []string{"Watch the closed-channel race in writePump"},
		CreatedAt:            time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
	}
	if err := WriteWorkflow(w); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(Dir(), "workflows", "auth-jwt-refresh.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read workflow: %v", err)
	}
	body := string(data)
	for _, want := range []string{
		"derived_from: [task-001, task-002, task-003]",
		"avg_outcome: 0.8900",
		"# Workflow: Add JWT refresh",
		"1. Build middleware first",
		"## Known pitfalls",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("workflow body missing %q\nbody:\n%s", want, body)
		}
	}
}

func TestRenderStandup_GroupsByType(t *testing.T) {
	withTempLibrary(t)
	t0 := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	s := StandupTranscript{
		ID:        "standup-001",
		StartedAt: t0,
		EndedAt:   t0.Add(10 * time.Minute),
		Topic:     "Sprint kickoff",
		Messages: []StandupMessage{
			{SessionID: "s1", Speaker: "Alpha", Content: "Hi.", Type: "opening", Timestamp: t0},
			{SessionID: "s2", Speaker: "Beta", Content: "Shipped X.", Type: "status_report", Timestamp: t0.Add(time.Minute)},
			{SessionID: "s3", Speaker: "Alpha", Content: "Let's go.", Type: "closing", Timestamp: t0.Add(2 * time.Minute)},
		},
	}
	if err := RenderStandup(s); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(Dir(), "standups", "standup-001.md")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	openingIdx := strings.Index(string(body), "## Opening")
	statusIdx := strings.Index(string(body), "## Status Report")
	closingIdx := strings.Index(string(body), "## Closing")
	if openingIdx < 0 || statusIdx < 0 || closingIdx < 0 {
		t.Fatalf("missing one of the type sections; body:\n%s", body)
	}
	if !(openingIdx < statusIdx && statusIdx < closingIdx) {
		t.Errorf("type sections out of order: opening=%d status=%d closing=%d",
			openingIdx, statusIdx, closingIdx)
	}
}

func TestWriteExperiment_HappyPath(t *testing.T) {
	withTempLibrary(t)
	rec := ExperimentRecord{
		ID:              "exp-001",
		TeamTemplate:    "backend-strike",
		Mission:         "ship auth refresh",
		TaskCount:       5,
		CompletedCount:  4,
		AvgScore:        0.82,
		DurationMS:      3600000,
		TopPerformer:    "Vex",
		BottomPerformer: "Iris",
		Timestamp:       time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
	}
	if err := WriteExperiment(rec); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(Dir(), "experiments", "exp-001.md")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"experiment_id: exp-001",
		"team_template: backend-strike",
		"Top performer: Vex",
		"Completed 4/5 tasks at avg score 0.82",
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("experiment body missing %q\nbody:\n%s", want, body)
		}
	}
}

func TestSanitize_RejectsPathTraversal(t *testing.T) {
	cases := []struct{ in, want string }{
		{"normal", "normal"},
		{"with space", "with-space"},
		{"../../etc/passwd", "------etc-passwd"}, // ../../etc/ = 6 unsafe chars → 6 dashes
		{"/abs/path", "-abs-path"},
		{".hidden", "-hidden"}, // dots become dashes
		{"", "unknown"},
		{"...", "unknown"}, // all-dot path → all-dash → trimmed → unknown
		{"weird?<>chars", "weird---chars"},
	}
	for _, tc := range cases {
		if got := sanitize(tc.in); got != tc.want {
			t.Errorf("sanitize(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestWriteExperience_StaysInsideRoot(t *testing.T) {
	// Defense-in-depth: a crafted configID/taskID must not escape
	// the library root. sanitize() handles char-level stripping but
	// we also assert the resolved abs path stays under Dir().
	root := withTempLibrary(t)
	rec := sampleExperience()
	rec.AgentConfigID = "../../../etc"
	rec.TaskID = "passwd"
	if err := WriteExperience(rec); err != nil {
		t.Fatal(err)
	}
	// The file MUST land under root, not at /etc/passwd.
	var found string
	_ = filepath.Walk(root, func(p string, _ os.FileInfo, _ error) error {
		if strings.HasSuffix(p, "passwd.md") {
			found = p
		}
		return nil
	})
	if found == "" {
		t.Fatal("no passwd.md found under root — write was suppressed?")
	}
	abs, _ := filepath.Abs(found)
	rootAbs, _ := filepath.Abs(root)
	if !strings.HasPrefix(abs, rootAbs) {
		t.Errorf("file escaped root: %s not under %s", abs, rootAbs)
	}
}

func TestConcurrentWritesDoNotCorrupt(t *testing.T) {
	// 50 concurrent writers to 10 different experience paths.
	// After the storm, every file must parse: starts with `---`,
	// ends with a complete section. No half-written content
	// (atomic rename guarantees this).
	withTempLibrary(t)
	const writers = 50
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rec := sampleExperience()
			rec.TaskID = "task-" + itoa(i%10)
			rec.OutcomeScore = float64(i) / 100
			_ = WriteExperience(rec)
		}(i)
	}
	wg.Wait()

	// Validate every produced file.
	dir := filepath.Join(Dir(), "experiences", "vex")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("leftover tmp file: %s", e.Name())
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(string(body), "---\n") {
			t.Errorf("%s: missing frontmatter start", e.Name())
		}
		if !strings.Contains(string(body), "# Add JWT refresh") {
			t.Errorf("%s: missing body heading", e.Name())
		}
	}
}

// --- helpers ---

func readExperience(t *testing.T, configID, taskID string) string {
	t.Helper()
	path := filepath.Join(Dir(), "experiences", configID, taskID+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var s string
	neg := i < 0
	if neg {
		i = -i
	}
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	if neg {
		s = "-" + s
	}
	return s
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
