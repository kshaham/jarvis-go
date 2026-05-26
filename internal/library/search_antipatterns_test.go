package library

import (
	"strings"
	"testing"
	"time"
)

func seedAntiPatternForSearch(t *testing.T, slug, role string, labels, dirs []string, avg float64, createdAt time.Time) {
	t.Helper()
	ap := AntiPattern{
		Slug:               slug,
		Title:              "Avoid: " + slug,
		DerivedFromTaskIDs: []string{"t1", "t2", "t3"},
		AvgOutcome:         avg,
		ObservationCount:   3,
		AgentRole:          role,
		TaskLabels:         labels,
		WorkingDirHints:    dirs,
		WhyItFails:         "root cause for " + slug,
		WhatWentWrong:      []string{"observation 1", "observation 2"},
		WhatToAvoid:        []string{"avoid foo", "always bar"},
		CreatedAt:          createdAt,
	}
	if err := WriteAntiPattern(ap); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func TestSearchAntiPatterns_EmptyLibraryReturnsNoHits(t *testing.T) {
	withTempLibrary(t)
	hits, err := SearchAntiPatterns(Query{AgentRole: "backend"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Errorf("got %d hits, want 0", len(hits))
	}
}

func TestSearchAntiPatterns_RanksByCombinedScore(t *testing.T) {
	withTempLibrary(t)
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	// "worst" anti-pattern: very low avg outcome → higher
	// outcomeWeight → higher combined score, all else equal.
	seedAntiPatternForSearch(t, "worst", "backend", []string{"auth"},
		[]string{"/repo/auth"}, 0.05, now)
	seedAntiPatternForSearch(t, "milder", "backend", []string{"auth"},
		[]string{"/repo/auth"}, 0.35, now)
	// Off-role/labels should be filtered.
	seedAntiPatternForSearch(t, "ui-stuff", "frontend", []string{"ui"},
		[]string{"/repo/web"}, 0.10, now)

	hits, err := searchAntiPatternsAt(Query{
		AgentRole:  "backend",
		TaskLabels: []string{"auth"},
		WorkingDir: "/repo/auth",
		K:          5,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) < 1 {
		t.Fatal("expected at least 1 hit")
	}
	if hits[0].Slug != "worst" {
		t.Errorf("top hit=%q want worst (lowest outcome → strongest lesson)", hits[0].Slug)
	}
	for _, h := range hits {
		if h.Slug == "ui-stuff" {
			t.Errorf("off-role anti-pattern leaked: %+v", h)
		}
	}
}

func TestSearchAntiPatterns_PopulatesBodyFields(t *testing.T) {
	withTempLibrary(t)
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	seedAntiPatternForSearch(t, "auth", "backend", []string{"auth"},
		[]string{"/repo/auth"}, 0.20, now)

	hits, err := searchAntiPatternsAt(Query{
		AgentRole:  "backend",
		TaskLabels: []string{"auth"},
		WorkingDir: "/repo/auth",
		K:          1,
	}, now)
	if err != nil || len(hits) != 1 {
		t.Fatalf("hits=%d err=%v", len(hits), err)
	}
	h := hits[0]
	if h.WhyItFails == "" {
		t.Errorf("WhyItFails not populated: %+v", h)
	}
	if len(h.WhatWentWrong) != 2 {
		t.Errorf("WhatWentWrong=%v want 2 items", h.WhatWentWrong)
	}
	if len(h.WhatToAvoid) != 2 {
		t.Errorf("WhatToAvoid=%v want 2 items", h.WhatToAvoid)
	}
	if !strings.HasPrefix(h.Path, "anti-patterns/") {
		t.Errorf("Path=%q want anti-patterns/ prefix", h.Path)
	}
}

func TestSearchAntiPatterns_LimitsToK(t *testing.T) {
	withTempLibrary(t)
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		seedAntiPatternForSearch(t, "ap-"+itoa(i), "backend",
			[]string{"auth"}, []string{"/repo/auth"}, 0.15, now)
	}
	hits, _ := searchAntiPatternsAt(Query{
		AgentRole:  "backend",
		TaskLabels: []string{"auth"},
		WorkingDir: "/repo/auth",
		K:          2,
	}, now)
	if len(hits) != 2 {
		t.Errorf("got %d hits, want K=2", len(hits))
	}
}

func TestSearchAntiPatterns_NonAntiPatternKindFiltered(t *testing.T) {
	// Manually drop a Workflow-shape file into anti-patterns/
	// (e.g. a future schema change). The "kind" frontmatter check
	// should reject it so workflows can't leak into anti-pattern
	// searches.
	root := withTempLibrary(t)
	body := "---\nkind: workflow\ntitle: I am a workflow\n---\n# x"
	path := root + "/anti-patterns/sneaky.md"
	if err := writeAtomic(path, []byte(body)); err != nil {
		t.Fatal(err)
	}
	hits, _ := SearchAntiPatterns(Query{K: 5})
	if len(hits) != 0 {
		t.Errorf("workflow-kind file leaked into anti-pattern hits: %v", hits)
	}
}

func TestExtractListSection_HappyPath(t *testing.T) {
	body := `# Title

## What to avoid

- first
- second
- third

## Next heading
- not-this
`
	got := extractListSection(body, "## What to avoid")
	if len(got) != 3 || got[0] != "first" || got[2] != "third" {
		t.Errorf("got %v want [first second third]", got)
	}
}

func TestExtractSection_TruncatesLongContent(t *testing.T) {
	long := strings.Repeat("a", 1000)
	body := "## Why it fails\n\n" + long + "\n\n## Next\n"
	got := extractSection(body, "## Why it fails")
	if len(got) > 250 {
		t.Errorf("section not truncated; got %d chars", len(got))
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("expected ellipsis at truncation; got %q", got[len(got)-10:])
	}
}
