package library

// Anti-pattern eager search.
//
// Parallel to Search() but walks library/anti-patterns/ instead of
// library/experiences/. Same Query type — agent role + task labels +
// working dir + K. Different output type (AntiPatternHit) because
// the surfaced fields are different ("what to avoid" vs "approach
// summary").
//
// Used by the kickstart hint builder to add a "## What to Avoid"
// section so agents see prior failure lessons alongside the
// "What Worked Before" workflow hints.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// antiPatternCacheTTL mirrors searchCacheTTL: anti-pattern files are
// written even less frequently than experience files (only when memify
// consolidates a low-outcome cluster), so 60 s staleness is safe.
const antiPatternCacheTTL = 60 * time.Second

type antiPatternCacheKey struct {
	role       string
	title      string
	labels     string
	workingDir string
	k          int
}

type antiPatternCacheEntry struct {
	hits     []AntiPatternHit
	loadedAt time.Time
}

var (
	antiPatternCacheMu sync.Mutex
	antiPatternCache   = map[antiPatternCacheKey]antiPatternCacheEntry{}
)

// resetAntiPatternSearchCacheForTest empties the anti-pattern result
// cache. Only for use in tests.
func resetAntiPatternSearchCacheForTest() {
	antiPatternCacheMu.Lock()
	antiPatternCache = map[antiPatternCacheKey]antiPatternCacheEntry{}
	antiPatternCacheMu.Unlock()
}

func makeAntiPatternCacheKey(q Query, k int) antiPatternCacheKey {
	labels := make([]string, len(q.TaskLabels))
	copy(labels, q.TaskLabels)
	sort.Strings(labels)
	return antiPatternCacheKey{
		role:       q.AgentRole,
		title:      q.TaskTitle,
		labels:     strings.Join(labels, ","),
		workingDir: q.WorkingDir,
		k:          k,
	}
}

// AntiPatternHit is one ranked anti-pattern result. Path is
// relative to Dir() for stable citations.
type AntiPatternHit struct {
	Path             string
	Slug             string
	Title            string
	AgentRole        string
	TaskLabels       []string
	WorkingDirHints  []string
	AvgOutcome       float64 // low by definition
	ObservationCount int
	WhyItFails       string
	WhatToAvoid      []string
	WhatWentWrong    []string
	CreatedAt        time.Time

	// Ranking components.
	Similarity    float64
	RecencyDecay  float64
	CombinedScore float64
}

// SearchAntiPatterns walks library/anti-patterns/, ranks every
// entry against q, returns top-K sorted by CombinedScore desc.
//
// Ranking: same similarity components as Search (Jaccard labels +
// title token overlap + working-dir prefix + role match), but the
// outcome dimension is inverted — lower AvgOutcome means the
// anti-pattern was learned from worse failures, which is MORE
// useful for an agent doing similar work today. We weight by
// (1 - AvgOutcome) so a 0.10-avg anti-pattern outranks a 0.35 one
// of equal similarity + recency.
func SearchAntiPatterns(q Query) ([]AntiPatternHit, error) {
	return searchAntiPatternsAt(q, time.Now())
}

func searchAntiPatternsAt(q Query, now time.Time) ([]AntiPatternHit, error) {
	root := Dir()
	if root == "" {
		return nil, nil
	}
	k := q.K
	if k <= 0 {
		k = 3
	}

	key := makeAntiPatternCacheKey(q, k)
	antiPatternCacheMu.Lock()
	if entry, ok := antiPatternCache[key]; ok && now.Sub(entry.loadedAt) < antiPatternCacheTTL {
		cached := make([]AntiPatternHit, len(entry.hits))
		copy(cached, entry.hits)
		antiPatternCacheMu.Unlock()
		return cached, nil
	}
	antiPatternCacheMu.Unlock()

	apDir := filepath.Join(root, "anti-patterns")
	files, err := os.ReadDir(apDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read anti-patterns dir: %w", err)
	}

	hits := make([]AntiPatternHit, 0, 16)
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
			continue
		}
		path := filepath.Join(apDir, f.Name())
		hit, ok := parseAntiPatternFile(path)
		if !ok {
			continue
		}
		// Reuse the Search ranking helpers — anti-patterns share the
		// same role/labels/dir shape with workflows, so similarity
		// computes the same way.
		hit.Similarity = computeAntiPatternSimilarity(q, hit)
		hit.RecencyDecay = computeRecencyDecay(now, hit.CreatedAt)
		// Outcome inversion: bad anti-patterns (low AvgOutcome)
		// rank higher because they encode a stronger lesson.
		outcomeWeight := 1.0 - hit.AvgOutcome
		if outcomeWeight < 0 {
			outcomeWeight = 0
		}
		hit.CombinedScore = outcomeWeight * hit.Similarity * hit.RecencyDecay
		if hit.CombinedScore < minCombinedScore {
			continue
		}
		if rel, rerr := filepath.Rel(root, path); rerr == nil {
			hit.Path = rel
		} else {
			hit.Path = path
		}
		hits = append(hits, hit)
	}

	sort.SliceStable(hits, func(i, j int) bool {
		return hits[i].CombinedScore > hits[j].CombinedScore
	})
	if len(hits) > k {
		hits = hits[:k]
	}

	// Populate cache.
	stored := make([]AntiPatternHit, len(hits))
	copy(stored, hits)
	antiPatternCacheMu.Lock()
	antiPatternCache[key] = antiPatternCacheEntry{hits: stored, loadedAt: now}
	antiPatternCacheMu.Unlock()

	return hits, nil
}

// parseAntiPatternFile parses an anti-pattern markdown's
// frontmatter + extracts the WhatToAvoid + WhatWentWrong + WhyItFails
// from the body. The body shape is what renderAntiPattern wrote.
func parseAntiPatternFile(path string) (AntiPatternHit, bool) {
	f, err := os.Open(path)
	if err != nil {
		return AntiPatternHit{}, false
	}
	defer f.Close()

	fm, body, ok := splitFrontmatter(f)
	if !ok {
		return AntiPatternHit{}, false
	}
	// Defensive: ensure this is an anti-pattern file. Lets us
	// share library/ subdir layouts in the future without
	// misclassifying workflows as anti-patterns.
	if k := fm["kind"]; k != "" && k != "anti-pattern" {
		return AntiPatternHit{}, false
	}

	hit := AntiPatternHit{
		Slug:            fm["anti_pattern_slug"],
		Title:           fm["title"],
		AgentRole:       fm["agent_role"],
		TaskLabels:      parseYAMLList(fm["task_labels"]),
		WorkingDirHints: parseYAMLList(fm["working_dir_hints"]),
	}
	if v, err := strconv.ParseFloat(fm["avg_outcome"], 64); err == nil {
		hit.AvgOutcome = v
	}
	if v, err := strconv.Atoi(fm["observation_count"]); err == nil {
		hit.ObservationCount = v
	}
	if v, err := time.Parse(time.RFC3339, fm["created_at"]); err == nil {
		hit.CreatedAt = v
	}

	// Pull the body sections.
	hit.WhyItFails = extractSection(body, "## Why it fails")
	hit.WhatWentWrong = extractListSection(body, "## What went wrong")
	hit.WhatToAvoid = extractListSection(body, "## What to avoid")

	return hit, true
}

// computeAntiPatternSimilarity mirrors computeSimilarity for the
// AntiPattern field set. Slightly different working-dir reasoning
// — we use the cluster's working_dir_hints (multiple) rather than
// a single WorkingDir. Picks the best per-hint score.
//
// Two modes:
//
//   - **Specific query** (labels OR title set): use the full
//     weighted similarity. Off-label / off-title hits get a 0 for
//     that component and rank correctly below relevant ones.
//   - **Sparse query** (both labels AND title empty — kickstart-
//     time case where the agent doesn't have a specific task yet):
//     re-weight to use ONLY role + dir, since those are the only
//     signals the caller has provided. Without this, the kickstart
//     hint never surfaces any anti-patterns because labels=0
//     dominates the formula.
//
// Either way an off-role + off-dir hit stays below threshold —
// the bar for "this anti-pattern is relevant to me" is met by at
// least one positive dimension.
func computeAntiPatternSimilarity(q Query, hit AntiPatternHit) float64 {
	role := 0.0
	if q.AgentRole == "" || hit.AgentRole == "" || q.AgentRole == hit.AgentRole {
		role = 1.0
	}

	// Best-of working-dir prefix across the cluster's hints.
	dirScore := 0.0
	if q.WorkingDir == "" || len(hit.WorkingDirHints) == 0 {
		dirScore = 0.5
	} else {
		for _, h := range hit.WorkingDirHints {
			if s := prefixMatch(q.WorkingDir, h); s > dirScore {
				dirScore = s
			}
		}
	}

	// Sparse-query fast path.
	if len(q.TaskLabels) == 0 && q.TaskTitle == "" {
		return 0.60*role + 0.40*dirScore
	}

	labels := 0.0
	if len(q.TaskLabels) > 0 {
		labels = jaccard(q.TaskLabels, hit.TaskLabels)
	}
	title := 0.0
	if q.TaskTitle != "" {
		title = tokenOverlap(q.TaskTitle, hit.Title)
	}
	return 0.50*labels + 0.25*title + 0.15*dirScore + 0.10*role
}

// extractSection pulls the body content under a `## Heading` until
// the next `## ` or end-of-body. Truncates to keep the rendered
// hint compact.
func extractSection(body, heading string) string {
	idx := strings.Index(body, heading)
	if idx < 0 {
		return ""
	}
	tail := body[idx+len(heading):]
	tail = strings.TrimLeft(tail, "\n")
	if end := strings.Index(tail, "\n## "); end >= 0 {
		tail = tail[:end]
	}
	tail = strings.TrimSpace(tail)
	const max = 240
	return truncateOnRune(tail, max) + ellipsisIfTrunc(tail, max)
}

// extractListSection pulls bullet list items under a `## Heading`.
// Recognises lines starting with "- " (matches renderAntiPattern
// output). Returns the items in order; empty when no list found.
func extractListSection(body, heading string) []string {
	section := extractSection(body, heading)
	if section == "" {
		return nil
	}
	lines := strings.Split(section, "\n")
	items := make([]string, 0, len(lines))
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if strings.HasPrefix(ln, "- ") {
			items = append(items, strings.TrimPrefix(ln, "- "))
		}
	}
	return items
}
