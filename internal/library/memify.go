package library

// Memify — periodic consolidation of raw experiences into reusable
// workflows. Borrowed from Cognee's "memify" terminology and the
// Agent Workflow Memory pattern from the 2026 research: group
// repeat high-outcome experiences with the same shape (role +
// labels) and distill them into a single workflow.md that future
// agents can read instead of re-discovering the approach from
// scratch.
//
// Cadence: invoked from the idle-monitor tick in
// internal/office/recovery.go — every 50 completed tasks OR every
// 6h, whichever first. Cheap (O(experiences) walk + write); safe
// to run mid-busy team.
//
// What it does NOT do:
//   - No LLM extraction in this pass. Pure heuristic clustering on
//     (agent_role, sorted_labels). Tier 3.3 may add a bounded
//     claude-haiku call to extract richer workflows; that's an
//     additive layer, not a replacement for the heuristic path.
//   - No deletion of source experiences. Memify only PRODUCES new
//     workflow docs and MARKS old experiences stale via the
//     valid_until frontmatter. Operators can still inspect raw
//     history.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// MemifyOptions controls a single memify pass.
type MemifyOptions struct {
	// MinClusterSize is the minimum number of experiences with the
	// same (role, labels) shape before they get distilled into a
	// workflow. Default 3 — three repeats establishes a pattern,
	// two is coincidence.
	MinClusterSize int

	// MinAvgOutcome is the minimum cluster mean outcome for the
	// cluster to graduate to a workflow. Default 0.70 — don't
	// codify mediocre approaches as canonical workflows.
	MinAvgOutcome float64

	// MaxAvgOutcomeForAntiPattern is the upper cluster-mean
	// threshold for anti-pattern generation. Clusters whose avg
	// outcome is BELOW this value (and which meet MinClusterSize
	// + include enough completed-or-failed experiences) graduate
	// to anti-patterns/<slug>.md. Default 0.40 — significantly
	// worse than the workflow floor, leaving a "mediocre"
	// no-mans-land in [0.40, 0.70) that produces neither.
	MaxAvgOutcomeForAntiPattern float64

	// DecayCutoffDays marks experiences older than this with a
	// valid_until frontmatter field on the next memify pass. The
	// experience file isn't deleted; future search results just
	// rank it lower (and Tier 4 dashboards can hide stale entries).
	// Default 90 days.
	DecayCutoffDays int

	// LLMBudget caps how many clusters per memify pass go through
	// the claude -p enrichment path. Each successful call
	// decrements the budget; failed calls don't count. Zero =
	// LLM extraction disabled (heuristic-only). Default 0 — the
	// operator opts in via the team setting.
	LLMBudget int

	// LLMModel is the --model flag passed to claude -p. Empty
	// means "use upstream default" (claude-sonnet for subscription
	// auth, the operator's ANTHROPIC_DEFAULT_HAIKU_MODEL for haiku
	// routing). Memify-time defaults to "" so the operator's
	// existing config governs.
	LLMModel string

	// NowFunc lets tests inject a deterministic clock. nil →
	// time.Now.
	NowFunc func() time.Time
}

// MemifyResult bundles what changed in a memify pass.
type MemifyResult struct {
	ClustersEvaluated   int
	WorkflowsWritten    int // new + updated combined
	AntiPatternsWritten int // anti-patterns emitted from low-outcome clusters
	ExperiencesScanned  int
	LLMEnriched         int // workflows + anti-patterns whose content came from the LLM
	LLMFailed           int // LLM attempts that fell back to heuristic
	// Slugs of workflows written this pass, in the order they
	// were emitted. Useful for caller telemetry / SSE broadcasts.
	WrittenSlugs []string
	// AntiPatternSlugs of anti-patterns emitted this pass.
	AntiPatternSlugs []string
}

// defaultMemifyOptions returns the canonical settings used when
// the idle-monitor tick fires.
func defaultMemifyOptions() MemifyOptions {
	return MemifyOptions{
		MinClusterSize:              3,
		MinAvgOutcome:               0.70,
		MaxAvgOutcomeForAntiPattern: 0.40,
		DecayCutoffDays:             90,
	}
}

// applyDefaults fills zero fields in opts with the canonical
// defaults so callers can pass MemifyOptions{} for the standard run.
func (o *MemifyOptions) applyDefaults() {
	d := defaultMemifyOptions()
	if o.MinClusterSize <= 0 {
		o.MinClusterSize = d.MinClusterSize
	}
	if o.MinAvgOutcome <= 0 {
		o.MinAvgOutcome = d.MinAvgOutcome
	}
	if o.MaxAvgOutcomeForAntiPattern <= 0 {
		o.MaxAvgOutcomeForAntiPattern = d.MaxAvgOutcomeForAntiPattern
	}
	if o.DecayCutoffDays <= 0 {
		o.DecayCutoffDays = d.DecayCutoffDays
	}
	if o.NowFunc == nil {
		o.NowFunc = time.Now
	}
}

// RunMemify executes a single memify pass over the experience
// library. Idempotent — running twice in a row produces the same
// workflow files (same content, same paths) as long as the
// underlying experiences haven't changed.
//
// Returns a result summary suitable for telemetry. Errors are
// returned for catastrophic failures (root dir missing); per-cluster
// write failures are logged via the returned WrittenSlugs gap (the
// slug won't appear if its write failed).
func RunMemify(opts MemifyOptions) (MemifyResult, error) {
	opts.applyDefaults()
	root := Dir()
	if root == "" {
		return MemifyResult{}, nil
	}

	expRoot := filepath.Join(root, "experiences")
	now := opts.NowFunc()

	// Phase 1: walk experiences + collect.
	all, err := collectAllExperiences(expRoot)
	if err != nil {
		return MemifyResult{}, err
	}
	result := MemifyResult{ExperiencesScanned: len(all)}

	// Phase 2a: cluster completed experiences for workflow extraction.
	// Failed experiences excluded so they can't drag a cluster's avg
	// outcome below the workflow threshold.
	workflowClusters := clusterExperiences(all, false)
	result.ClustersEvaluated = len(workflowClusters)

	// Phase 3: emit workflows for high-outcome clusters. LLM
	// enrichment is bounded by opts.LLMBudget — workflow clusters
	// take precedence; remaining budget flows to anti-patterns.
	llmBudget := opts.LLMBudget
	for _, cluster := range workflowClusters {
		if len(cluster.entries) < opts.MinClusterSize {
			continue
		}
		avg := clusterAvgOutcome(cluster)
		if avg < opts.MinAvgOutcome {
			continue
		}
		wf := buildWorkflowFromCluster(cluster, avg, now)

		// Optional LLM enrichment. Only attempted when budget
		// allows; any failure is silent + falls through to the
		// heuristic workflow (already populated in wf).
		if llmBudget > 0 {
			ctx, cancel := context.WithTimeout(context.Background(), llmExtractTimeout)
			ext, lerr := extractWorkflowWithLLM(ctx, cluster, opts.LLMModel)
			cancel()
			if lerr == nil && ext != nil {
				wf = mergeIntoWorkflow(wf, ext)
				result.LLMEnriched++
				llmBudget-- // count only successful calls
			} else {
				result.LLMFailed++
			}
		}

		if err := WriteWorkflow(wf); err != nil {
			continue
		}
		result.WorkflowsWritten++
		result.WrittenSlugs = append(result.WrittenSlugs, wf.Slug)
	}

	// Phase 4: emit anti-patterns from LOW-outcome clusters.
	// Re-cluster including failed experiences so the failure signal
	// has weight. The same slug naturally pairs with any workflow
	// of the same (role, labels) — agents looking up "auth" via
	// PageIndex see both surfaces.
	antiClusters := clusterExperiences(all, true)
	for _, cluster := range antiClusters {
		if len(cluster.entries) < opts.MinClusterSize {
			continue
		}
		avg := clusterAvgOutcome(cluster)
		if avg >= opts.MaxAvgOutcomeForAntiPattern {
			continue
		}
		ap := buildAntiPatternFromCluster(cluster, avg, now)

		// LLM enrichment, sharing the same budget as workflows.
		if llmBudget > 0 {
			ctx, cancel := context.WithTimeout(context.Background(), llmExtractTimeout)
			ext, lerr := extractAntiPatternWithLLM(ctx, cluster, opts.LLMModel)
			cancel()
			if lerr == nil && ext != nil {
				ap = mergeIntoAntiPattern(ap, ext)
				result.LLMEnriched++
				llmBudget--
			} else {
				result.LLMFailed++
			}
		}

		if err := WriteAntiPattern(ap); err != nil {
			continue
		}
		result.AntiPatternsWritten++
		result.AntiPatternSlugs = append(result.AntiPatternSlugs, ap.Slug)
	}

	// Phase 4: decay old experiences.
	cutoff := now.Add(-time.Duration(opts.DecayCutoffDays) * 24 * time.Hour)
	for _, e := range all {
		if e.CompletedAt.IsZero() || e.CompletedAt.After(cutoff) {
			continue
		}
		decayExperience(e.path, now.Add(24*time.Hour))
	}

	return result, nil
}

// experienceMeta is the minimal projection memify needs from an
// experience file. Avoids carrying the full markdown body around
// when we only need frontmatter + the file path.
type experienceMeta struct {
	path             string // absolute path
	relPath          string // path under library root for citations
	AgentConfigID    string
	AgentRole        string
	TaskID           string
	TaskTitle        string
	TaskLabels       []string
	WorkingDir       string
	OutcomeScore     float64
	CompletionStatus string
	CompletedAt      time.Time
	ApproachSummary  string
}

// collectAllExperiences walks the experiences root and parses every
// .md file's frontmatter. Files we can't parse are silently
// skipped — memify is best-effort consolidation, not a validator.
func collectAllExperiences(expRoot string) ([]*experienceMeta, error) {
	out := []*experienceMeta{}
	root := Dir()
	configDirs, err := os.ReadDir(expRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, fmt.Errorf("read experiences root: %w", err)
	}
	for _, cd := range configDirs {
		if !cd.IsDir() {
			continue
		}
		dir := filepath.Join(expRoot, cd.Name())
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
				continue
			}
			full := filepath.Join(dir, f.Name())
			hit, ok := parseExperienceFile(full)
			if !ok {
				continue
			}
			meta := &experienceMeta{
				path:             full,
				AgentConfigID:    hit.AgentConfigID,
				AgentRole:        hit.AgentRole,
				TaskID:           hit.TaskID,
				TaskTitle:        hit.TaskTitle,
				TaskLabels:       hit.TaskLabels,
				WorkingDir:       hit.WorkingDir,
				OutcomeScore:     hit.OutcomeScore,
				CompletionStatus: hit.CompletionStatus,
				CompletedAt:      hit.CompletedAt,
				ApproachSummary:  hit.ApproachSummary,
			}
			if rel, rerr := filepath.Rel(root, full); rerr == nil {
				meta.relPath = rel
			} else {
				meta.relPath = full
			}
			out = append(out, meta)
		}
	}
	return out, nil
}

// experienceCluster is a group of experiences sharing the same
// (role, sorted_labels) shape. Order within a cluster is by
// CompletedAt descending (most recent first) — when we render the
// workflow we cite the top 3 most-recent contributors.
type experienceCluster struct {
	role    string
	labels  []string // sorted, lowercase
	entries []*experienceMeta
}

// clusterKey returns the deterministic key for grouping. role +
// "|" + sorted_label_csv. Empty role/labels collapse to "(any)" /
// "(no-labels)" so empty-shape experiences still form a cluster.
func clusterKey(role string, labels []string) (string, []string) {
	r := strings.ToLower(strings.TrimSpace(role))
	if r == "" {
		r = "(any)"
	}
	norm := make([]string, 0, len(labels))
	for _, l := range labels {
		l = strings.ToLower(strings.TrimSpace(l))
		if l != "" {
			norm = append(norm, l)
		}
	}
	sort.Strings(norm)
	if len(norm) == 0 {
		return r + "|(no-labels)", norm
	}
	return r + "|" + strings.Join(norm, ","), norm
}

// clusterExperiences groups by clusterKey. Within each cluster,
// entries are sorted by CompletedAt descending so workflow
// rendering pulls the most recent + most informative contributors.
//
// includeFailed selects whether failed experiences enter the
// clusters. Workflow building passes false (failures shouldn't drag
// down a high-outcome cluster's avg); anti-pattern building passes
// true (failures ARE the signal we want to cluster on).
//
// Returns clusters in deterministic order (sorted by key) so the
// memify pass produces stable output across runs.
func clusterExperiences(all []*experienceMeta, includeFailed bool) []*experienceCluster {
	byKey := map[string]*experienceCluster{}
	for _, e := range all {
		switch e.CompletionStatus {
		case "completed":
			// always in
		case "failed":
			if !includeFailed {
				continue
			}
		default:
			// abandoned / in-progress / unknown — exclude
			continue
		}
		key, sorted := clusterKey(e.AgentRole, e.TaskLabels)
		c, ok := byKey[key]
		if !ok {
			c = &experienceCluster{role: e.AgentRole, labels: sorted}
			byKey[key] = c
		}
		c.entries = append(c.entries, e)
	}
	out := make([]*experienceCluster, 0, len(byKey))
	keys := make([]string, 0, len(byKey))
	for k := range byKey {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		c := byKey[k]
		sort.SliceStable(c.entries, func(i, j int) bool {
			return c.entries[i].CompletedAt.After(c.entries[j].CompletedAt)
		})
		out = append(out, c)
	}
	return out
}

// clusterAvgOutcome returns the arithmetic mean outcome score over
// the cluster's entries.
func clusterAvgOutcome(c *experienceCluster) float64 {
	if len(c.entries) == 0 {
		return 0
	}
	sum := 0.0
	for _, e := range c.entries {
		sum += e.OutcomeScore
	}
	return sum / float64(len(c.entries))
}

// buildWorkflowFromCluster projects a cluster into a Workflow doc.
// Top-3 most-recent entries become the "Steps that worked" + cite
// list. Pulls task titles + approach summaries — agents reading the
// workflow get a quick "what was tried" view; the cited experiences
// are linked for full detail via PageIndex.
//
// Slug = lowercase-kebab of (first label OR role OR "team").
// Workflow titles deduplicate by slug so re-running memify on the
// same cluster updates in place.
func buildWorkflowFromCluster(c *experienceCluster, avg float64, now time.Time) Workflow {
	slug := workflowSlugFor(c)
	title := workflowTitleFor(c)

	// Pull top-3 most recent contributors.
	contribTaskIDs := []string{}
	workingDirSet := map[string]struct{}{}
	steps := []string{}
	for i, e := range c.entries {
		contribTaskIDs = append(contribTaskIDs, e.TaskID)
		if e.WorkingDir != "" {
			workingDirSet[e.WorkingDir] = struct{}{}
		}
		if i < 3 && e.ApproachSummary != "" {
			steps = append(steps, fmt.Sprintf("%s — %s", e.TaskTitle, e.ApproachSummary))
		}
	}
	workingDirs := make([]string, 0, len(workingDirSet))
	for d := range workingDirSet {
		workingDirs = append(workingDirs, d)
	}
	sort.Strings(workingDirs)

	return Workflow{
		Slug:                 slug,
		Title:                title,
		DerivedFromTaskIDs:   contribTaskIDs,
		AvgOutcome:           avg,
		ReuseCountAtCreation: len(c.entries),
		AgentRole:            c.role,
		TaskLabels:           c.labels,
		WorkingDirHints:      workingDirs,
		Steps:                steps,
		CreatedAt:            now,
	}
}

// buildAntiPatternFromCluster projects a low-outcome cluster into
// an AntiPattern doc. Slug matches the corresponding workflow slug
// (same workflowSlugFor() function) so PageIndex pairs them.
//
// Heuristic content: WhatWentWrong = top-3 most-recent task titles
// + their approach summaries (the things that didn't work).
// WhatToAvoid is left empty for the heuristic path — only the LLM
// enrichment fills this with actionable guidance.
func buildAntiPatternFromCluster(c *experienceCluster, avg float64, now time.Time) AntiPattern {
	slug := workflowSlugFor(c)
	title := "Avoid: " + antiPatternTitleSuffix(c)

	contribTaskIDs := []string{}
	workingDirSet := map[string]struct{}{}
	observations := []string{}
	for i, e := range c.entries {
		contribTaskIDs = append(contribTaskIDs, e.TaskID)
		if e.WorkingDir != "" {
			workingDirSet[e.WorkingDir] = struct{}{}
		}
		if i < 3 && e.ApproachSummary != "" {
			observations = append(observations,
				fmt.Sprintf("%s (outcome %.2f) — %s",
					e.TaskTitle, e.OutcomeScore, e.ApproachSummary))
		}
	}
	workingDirs := make([]string, 0, len(workingDirSet))
	for d := range workingDirSet {
		workingDirs = append(workingDirs, d)
	}
	sort.Strings(workingDirs)

	return AntiPattern{
		Slug:               slug,
		Title:              title,
		DerivedFromTaskIDs: contribTaskIDs,
		AvgOutcome:         avg,
		ObservationCount:   len(c.entries),
		AgentRole:          c.role,
		TaskLabels:         c.labels,
		WorkingDirHints:    workingDirs,
		WhatWentWrong:      observations,
		CreatedAt:          now,
	}
}

// antiPatternTitleSuffix builds the descriptive half of an
// anti-pattern title (the "Avoid: <suffix>" sentence).
func antiPatternTitleSuffix(c *experienceCluster) string {
	if len(c.labels) > 0 {
		return strings.Title(c.labels[0]) + " task pitfalls"
	}
	if c.role != "" {
		return strings.Title(c.role) + " pitfalls"
	}
	return "common team pitfalls"
}

// workflowSlugFor builds a stable filename slug for the cluster.
// First label > role > "team" fallback. Lowercased, sanitized.
func workflowSlugFor(c *experienceCluster) string {
	switch {
	case len(c.labels) > 0:
		return sanitize(c.labels[0])
	case c.role != "":
		return sanitize(strings.ToLower(c.role))
	default:
		return "team"
	}
}

// workflowTitleFor builds a human-readable title. Reuses the slug
// logic but title-cases the result.
func workflowTitleFor(c *experienceCluster) string {
	if len(c.labels) > 0 {
		return "Workflow for " + strings.Title(c.labels[0]) + " tasks"
	}
	if c.role != "" {
		return "Workflow for " + strings.Title(c.role)
	}
	return "Workflow for the team"
}

// decayExperience appends a valid_until field to an experience
// file's frontmatter. The file content stays — only the marker
// changes — so PageIndex's tree index doesn't need a full rebuild.
//
// Idempotent: re-applying decay to an already-decayed file is a
// no-op (we check for valid_until before stamping). validity is set
// 24h in the future from now so search consumers have a small grace
// window to surface the experience one last time.
func decayExperience(path string, validUntil time.Time) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	body := string(data)
	if strings.Contains(body, "valid_until:") {
		return // already decayed
	}
	// Insert valid_until before the closing `---\n` of the
	// frontmatter. If the doc doesn't have frontmatter we silently
	// give up — memify won't manufacture one.
	const fmEnd = "\n---\n"
	idx := strings.Index(body, fmEnd)
	if idx < 0 {
		return
	}
	stamped := body[:idx] + "\nvalid_until: " + validUntil.UTC().Format(time.RFC3339) + body[idx:]
	_ = writeAtomic(path, []byte(stamped))
}
