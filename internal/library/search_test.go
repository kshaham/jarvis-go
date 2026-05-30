package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// seedExperience writes a sample experience to the test library so
// search can find it. Returns the file path (for assertions).
func seedExperience(t *testing.T, configID, taskID, title string, labels []string, workingDir, role string, outcome float64, completedAt time.Time) {
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
		ApproachSummary:  "Built " + title + " carefully. Tested with curl.",
	}
	if err := WriteExperience(rec); err != nil {
		t.Fatalf("seed %s: %v", taskID, err)
	}
}

func TestSearch_EmptyLibraryReturnsNoHits(t *testing.T) {
	withTempLibrary(t)
	hits, err := Search(Query{AgentRole: "backend"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("got %d hits on empty library, want 0", len(hits))
	}
}

func TestSearch_RanksByCombinedScore(t *testing.T) {
	withTempLibrary(t)
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

	// Three candidates, all completed recently:
	// - "auth-strong": same role, same labels, high outcome → top
	// - "auth-weak": same labels but failed outcome → low
	// - "ui-unrelated": different role + labels → filtered out
	seedExperience(t, "vex", "task-strong", "Add JWT refresh",
		[]string{"auth", "jwt"}, "/repo/internal/auth", "backend-engineer",
		0.90, now.Add(-1*24*time.Hour))
	seedExperience(t, "iris", "task-weak", "Botch JWT refresh",
		[]string{"auth", "jwt"}, "/repo/internal/auth", "backend-engineer",
		0.35, now.Add(-1*24*time.Hour))
	seedExperience(t, "kit", "task-ui", "Style the button",
		[]string{"ui", "css"}, "/repo/web/static", "frontend-engineer",
		0.85, now.Add(-1*24*time.Hour))

	hits, err := Search(Query{
		AgentRole:  "backend-engineer",
		TaskTitle:  "Add JWT refresh",
		TaskLabels: []string{"auth", "jwt"},
		WorkingDir: "/repo/internal/auth",
		K:          3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("expected at least 1 hit")
	}
	if hits[0].TaskID != "task-strong" {
		t.Errorf("top hit=%q want task-strong", hits[0].TaskID)
	}
	if hits[0].CombinedScore <= 0 {
		t.Errorf("top hit has zero combined score: %+v", hits[0])
	}
	// Unrelated UI task should be filtered out (different labels,
	// different role, different dir → similarity well below threshold).
	for _, h := range hits {
		if h.TaskID == "task-ui" {
			t.Errorf("UI hit leaked into backend results: %+v", h)
		}
	}
}

func TestSearch_LimitsToK(t *testing.T) {
	withTempLibrary(t)
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 10; i++ {
		seedExperience(t, "vex", "task-"+itoa(i), "Add JWT refresh "+itoa(i),
			[]string{"auth", "jwt"}, "/repo/internal/auth", "backend-engineer",
			0.8, now.Add(-time.Duration(i)*24*time.Hour))
	}
	hits, err := Search(Query{
		AgentRole:  "backend-engineer",
		TaskLabels: []string{"auth", "jwt"},
		WorkingDir: "/repo/internal/auth",
		K:          3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 3 {
		t.Errorf("got %d hits, want exactly K=3", len(hits))
	}
}

func TestSearch_DropsBelowThreshold(t *testing.T) {
	// Even a high-outcome hit gets dropped when similarity is too
	// low (no matching labels, no matching dir, different role).
	withTempLibrary(t)
	seedExperience(t, "vex", "task-1", "Something else entirely",
		[]string{"ui"}, "/other/path", "frontend",
		0.95, time.Now().Add(-time.Hour))

	hits, err := Search(Query{
		AgentRole:  "backend",
		TaskTitle:  "Add JWT refresh",
		TaskLabels: []string{"auth", "jwt"},
		WorkingDir: "/repo/internal/auth",
		K:          5,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		if h.CombinedScore < minCombinedScore {
			t.Errorf("hit below threshold leaked: %+v", h)
		}
	}
}

func TestSearch_RecencyDecayPreservesOrder(t *testing.T) {
	// Two identical hits except for age. Recent wins. Use the
	// time-injectable searchAt so the test stays deterministic
	// regardless of wall clock drift.
	withTempLibrary(t)
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	seedExperience(t, "vex", "task-old", "Add JWT refresh",
		[]string{"auth"}, "/repo/internal/auth", "backend",
		0.85, now.Add(-100*24*time.Hour))
	seedExperience(t, "vex", "task-new", "Add JWT refresh",
		[]string{"auth"}, "/repo/internal/auth", "backend",
		0.85, now.Add(-1*24*time.Hour))

	hits, err := searchAt(Query{
		AgentRole:  "backend",
		TaskLabels: []string{"auth"},
		WorkingDir: "/repo/internal/auth",
		K:          2,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("got %d hits, want 2", len(hits))
	}
	if hits[0].TaskID != "task-new" {
		t.Errorf("top hit=%q want task-new (recency should rank above old)", hits[0].TaskID)
	}
}

func TestSearch_ApproachSummaryExtracted(t *testing.T) {
	withTempLibrary(t)
	seedExperience(t, "vex", "task-1", "Add JWT refresh",
		[]string{"auth"}, "/repo/internal/auth", "backend",
		0.85, time.Now().Add(-time.Hour))
	hits, err := Search(Query{AgentRole: "backend", TaskLabels: []string{"auth"}, K: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatal("expected 1 hit")
	}
	if !strings.Contains(hits[0].ApproachSummary, "carefully") {
		t.Errorf("approach summary missing seeded content: %q", hits[0].ApproachSummary)
	}
}

func TestSearch_PathIsRelativeToLibraryRoot(t *testing.T) {
	root := withTempLibrary(t)
	seedExperience(t, "vex", "task-1", "Add JWT refresh",
		[]string{"auth"}, "/repo/internal/auth", "backend",
		0.85, time.Now().Add(-time.Hour))
	hits, _ := Search(Query{AgentRole: "backend", TaskLabels: []string{"auth"}, K: 1})
	if len(hits) == 0 {
		t.Fatal("no hits")
	}
	if strings.HasPrefix(hits[0].Path, root) {
		t.Errorf("hit path leaks absolute root: %q (root=%q)", hits[0].Path, root)
	}
	if !strings.HasPrefix(hits[0].Path, "experiences/") {
		t.Errorf("hit path=%q want prefix experiences/", hits[0].Path)
	}
}

func TestJaccard_HappyPath(t *testing.T) {
	if got := jaccard([]string{"a", "b"}, []string{"b", "c"}); got != 1.0/3.0 {
		t.Errorf("jaccard {a,b} {b,c}=%v want 0.333", got)
	}
	if got := jaccard([]string{"a"}, []string{"a"}); got != 1.0 {
		t.Errorf("identical sets jaccard=%v want 1.0", got)
	}
	if got := jaccard(nil, nil); got != 1.0 {
		t.Errorf("empty-empty jaccard=%v want 1.0", got)
	}
	if got := jaccard([]string{"a"}, nil); got != 0.0 {
		t.Errorf("one-empty jaccard=%v want 0.0", got)
	}
}

func TestTokenOverlap_DropsStopwords(t *testing.T) {
	// "add the auth" → only "auth" survives tokenization (stopwords
	// + too-short tokens dropped). Should fully match "auth refresh".
	got := tokenOverlap("add the auth", "auth refresh middleware")
	if got != 1.0 {
		t.Errorf("got %v want 1.0 (auth fully covered)", got)
	}
}

func TestPrefixMatch(t *testing.T) {
	cases := []struct {
		a, b string
		want float64
	}{
		{"/repo/internal/auth", "/repo/internal/auth", 1.0},
		{"/repo/internal", "/repo/internal/auth", 0.75},    // descendant
		{"/repo/internal/auth", "/repo/internal", 0.75},    // ancestor
		{"/repo/internal/auth", "/repo/internal/api", 0.5}, // shared prefix
		{"/elsewhere", "/repo/internal", 0.0},
		{"", "/anything", 0.5},
		{"/anything", "", 0.5},
	}
	for _, tc := range cases {
		if got := prefixMatch(tc.a, tc.b); got != tc.want {
			t.Errorf("prefixMatch(%q,%q)=%v want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestRecencyDecay(t *testing.T) {
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	// 0 days → 1.0
	if got := computeRecencyDecay(now, now); got != 1.0 {
		t.Errorf("0d decay=%v want 1.0", got)
	}
	// 180 days → 0
	if got := computeRecencyDecay(now, now.Add(-180*24*time.Hour)); got > 1e-9 {
		t.Errorf("180d decay=%v want 0", got)
	}
	// Mid-window: half decay
	mid := computeRecencyDecay(now, now.Add(-90*24*time.Hour))
	if mid < 0.49 || mid > 0.51 {
		t.Errorf("90d decay=%v want ~0.5", mid)
	}
	// Older than window: 0
	if got := computeRecencyDecay(now, now.Add(-365*24*time.Hour)); got != 0 {
		t.Errorf("365d decay=%v want 0", got)
	}
	// Zero time: 0.5 (degraded but not zero)
	if got := computeRecencyDecay(now, time.Time{}); got != 0.5 {
		t.Errorf("zero-time decay=%v want 0.5", got)
	}
}

func TestParseYAMLList_HappyPath(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"[a, b, c]", []string{"a", "b", "c"}},
		{`["with space", quoted]`, []string{"with space", "quoted"}},
		{"[]", nil},
		{"not-a-list", nil},
	}
	for _, tc := range cases {
		got := parseYAMLList(tc.in)
		if !sliceEq(got, tc.want) {
			t.Errorf("parseYAMLList(%q)=%v want %v", tc.in, got, tc.want)
		}
	}
}

func TestSplitFrontmatter_RoundTripsAnExperience(t *testing.T) {
	// Round-trip: write a known record, parse it back, assert fields.
	withTempLibrary(t)
	rec := ExperienceRecord{
		AgentConfigID:    "vex",
		AgentName:        "Vex",
		TaskID:           "task-rt",
		TaskTitle:        "Round trip test",
		TaskLabels:       []string{"alpha", "beta"},
		CompletionStatus: "completed",
		OutcomeScore:     0.77,
		CompletedAt:      time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
		ApproachSummary:  "Just worked.",
	}
	if err := WriteExperience(rec); err != nil {
		t.Fatal(err)
	}
	hits, err := Search(Query{TaskLabels: []string{"alpha"}, K: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("got %d hits, want 1", len(hits))
	}
	h := hits[0]
	if h.TaskID != "task-rt" || h.TaskTitle != "Round trip test" {
		t.Errorf("identity drift: %+v", h)
	}
	if h.OutcomeScore < 0.76 || h.OutcomeScore > 0.78 {
		t.Errorf("outcome drift: %v want 0.77", h.OutcomeScore)
	}
	if !sliceEq(h.TaskLabels, []string{"alpha", "beta"}) {
		t.Errorf("labels drift: %v", h.TaskLabels)
	}
}

func sliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestSearchCachesWithinTTL verifies that a second Search call within the TTL
// returns the cached result even when the on-disk files have been altered.
// After resetLibrarySearchCacheForTest() the walk is repeated and picks up
// the new (altered) state.
func TestSearchCachesWithinTTL(t *testing.T) {
	root := withTempLibrary(t)
	resetLibrarySearchCacheForTest()

	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	q := Query{
		AgentRole:  "backend",
		TaskLabels: []string{"auth"},
		WorkingDir: "/repo/internal/auth",
		K:          3,
	}

	// Seed one experience and do the first search (cold cache).
	seedExperience(t, "vex", "task-cache-1", "Cache test task",
		[]string{"auth"}, "/repo/internal/auth", "backend",
		0.90, now.Add(-time.Hour))

	first, err := searchAt(q, now)
	if err != nil {
		t.Fatalf("first search: %v", err)
	}
	if len(first) == 0 {
		t.Fatal("expected at least one hit on first search")
	}

	// Delete the experience files so a fresh disk walk would return nothing.
	expDir := filepath.Join(root, "experiences")
	if err := os.RemoveAll(expDir); err != nil {
		t.Fatalf("remove experiences dir: %v", err)
	}
	if err := os.MkdirAll(expDir, 0o755); err != nil {
		t.Fatalf("recreate experiences dir: %v", err)
	}

	// Second search within TTL (same now) must return cached result.
	second, err := searchAt(q, now)
	if err != nil {
		t.Fatalf("second search: %v", err)
	}
	if len(second) != len(first) {
		t.Errorf("within-TTL cache miss: got %d hits, want %d (cached)", len(second), len(first))
	}
	if len(second) > 0 && second[0].TaskID != first[0].TaskID {
		t.Errorf("within-TTL: different hit returned; got %q want %q", second[0].TaskID, first[0].TaskID)
	}

	// After resetting the cache, the walk re-reads from disk (now empty)
	// and returns no hits.
	resetLibrarySearchCacheForTest()
	third, err := searchAt(q, now)
	if err != nil {
		t.Fatalf("third search: %v", err)
	}
	if len(third) != 0 {
		t.Errorf("after cache reset: expected 0 hits (empty dir), got %d", len(third))
	}
}

// TestSearchAntiPatternsCachesWithinTTL is the parallel test for
// SearchAntiPatterns — verifies cache hit within TTL and reset seam.
func TestSearchAntiPatternsCachesWithinTTL(t *testing.T) {
	root := withTempLibrary(t)
	// withTempLibrary only creates experiences/workflows/standups/experiments;
	// ensure anti-patterns/ exists too.
	if err := os.MkdirAll(filepath.Join(root, "anti-patterns"), 0o755); err != nil {
		t.Fatalf("mkdir anti-patterns: %v", err)
	}
	resetAntiPatternSearchCacheForTest()

	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	q := Query{
		AgentRole:  "backend",
		TaskLabels: []string{"auth"},
		WorkingDir: "/repo/auth",
		K:          3,
	}

	seedAntiPatternForSearch(t, "ap-cache", "backend", []string{"auth"},
		[]string{"/repo/auth"}, 0.10, now)

	first, err := searchAntiPatternsAt(q, now)
	if err != nil {
		t.Fatalf("first search: %v", err)
	}
	if len(first) == 0 {
		t.Fatal("expected at least one hit on first search")
	}

	// Remove all anti-pattern files.
	apDir := filepath.Join(root, "anti-patterns")
	if err := os.RemoveAll(apDir); err != nil {
		t.Fatalf("remove anti-patterns dir: %v", err)
	}
	if err := os.MkdirAll(apDir, 0o755); err != nil {
		t.Fatalf("recreate anti-patterns dir: %v", err)
	}

	// Within-TTL: must still return cached result.
	second, err := searchAntiPatternsAt(q, now)
	if err != nil {
		t.Fatalf("second search: %v", err)
	}
	if len(second) != len(first) {
		t.Errorf("within-TTL cache miss: got %d hits, want %d", len(second), len(first))
	}

	// After reset: empty dir → no hits.
	resetAntiPatternSearchCacheForTest()
	third, err := searchAntiPatternsAt(q, now)
	if err != nil {
		t.Fatalf("third search: %v", err)
	}
	if len(third) != 0 {
		t.Errorf("after reset: expected 0 hits, got %d", len(third))
	}
}
