package library

// Eager experience prefetch for kickstart-time injection.
//
// Tier 2 of the self-improvement plan: at agent spawn (and at next-
// task injection), jarvis scans the experience library for the top-K
// most-relevant past tasks and renders them into the kickstart
// prompt's "What Worked Before" section. Agents still get the full
// PageIndex MCP for lazy on-demand retrieval; this eager path is
// the guaranteed-visibility layer for high-value hits.
//
// Implementation choice: Go reads the markdown files directly
// instead of round-tripping through the MCP. Reasons:
//   - The MCP is a tool surface for the LLM, not a server API. Each
//     daemon-side call would need a fresh npx subprocess + MCP
//     handshake (slow + flaky for repeated probes).
//   - Frontmatter alone gives enough signal for ranking (labels,
//     role, working dir, outcome score). We don't need the LLM-
//     driven tree search for top-K selection.
//   - The library is jarvis's own canonical store. Reading it
//     directly is the simplest, most reliable path.
//
// Scoring stack (all per-hit, multiplied):
//   - outcome_score             (anchor: only surface hits that worked)
//   - similarity                (Jaccard on labels + title token
//                                overlap + working-dir prefix match
//                                + role match bonus)
//   - recency_decay             (1.0 at 0 days → 0.0 at 180 days,
//                                linear; older hits stay searchable
//                                but rank below recent ones)
//
// Threshold: hits below combined_score = 0.20 are dropped so the
// kickstart doesn't get padded with weak matches.

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Query is the input to a library search. Empty fields are treated
// as wildcards (a query with only AgentRole set matches all tasks
// for that role regardless of labels / dir).
type Query struct {
	AgentRole  string
	TaskTitle  string
	TaskLabels []string
	WorkingDir string
	K          int // top-K to return; defaults to 3 when zero
}

// Hit is one ranked search result. Path is the file path relative
// to Dir() so callers can render a stable citation.
type Hit struct {
	Path             string
	AgentConfigID    string
	AgentName        string
	AgentRole        string
	TaskID           string
	TaskTitle        string
	TaskLabels       []string
	WorkingDir       string
	OutcomeScore     float64
	CompletionStatus string
	ApproachSummary  string
	CompletedAt      time.Time

	// Ranking components (exported for testability / debug).
	Similarity    float64
	RecencyDecay  float64
	CombinedScore float64
}

// minCombinedScore drops hits whose final rank is below this floor.
// Keeps weak matches out of the prompt budget.
const minCombinedScore = 0.20

// recencyHalfLifeDays is the linear decay window. After this many
// days the recency factor is 0.0; new hits start at 1.0.
const recencyHalfLifeDays = 180.0

// Search walks the experiences directory, scores every experience
// against q, and returns the top-K hits sorted by CombinedScore
// descending.
//
// Returns nil + nil error when the library directory is unavailable
// (graceful degradation matching every other library API). Callers
// treat nil as "no hints to show".
func Search(q Query) ([]Hit, error) {
	return searchAt(q, time.Now())
}

// searchAt is the time-injectable form of Search used by tests so
// the recency decay is deterministic regardless of wall clock drift.
func searchAt(q Query, now time.Time) ([]Hit, error) {
	root := Dir()
	if root == "" {
		return nil, nil
	}
	k := q.K
	if k <= 0 {
		k = 3
	}

	expDir := filepath.Join(root, "experiences")
	entries, err := os.ReadDir(expDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read experiences dir: %w", err)
	}

	// Approach summary lives in the body, not the frontmatter, so
	// we parse the whole doc and pull the "## Approach" section.
	hits := make([]Hit, 0, 32)
	for _, configDir := range entries {
		if !configDir.IsDir() {
			continue
		}
		dir := filepath.Join(expDir, configDir.Name())
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
				continue
			}
			path := filepath.Join(dir, f.Name())
			hit, ok := parseExperienceFile(path)
			if !ok {
				continue
			}
			hit.Similarity = computeSimilarity(q, hit)
			hit.RecencyDecay = computeRecencyDecay(now, hit.CompletedAt)
			hit.CombinedScore = hit.OutcomeScore * hit.Similarity * hit.RecencyDecay
			if hit.CombinedScore < minCombinedScore {
				continue
			}
			// Store path relative to Dir() so the rendered hint is
			// stable across machines / users.
			if rel, rerr := filepath.Rel(root, path); rerr == nil {
				hit.Path = rel
			} else {
				hit.Path = path
			}
			hits = append(hits, hit)
		}
	}

	sort.SliceStable(hits, func(i, j int) bool {
		return hits[i].CombinedScore > hits[j].CombinedScore
	})
	if len(hits) > k {
		hits = hits[:k]
	}
	return hits, nil
}

// parseExperienceFile pulls the frontmatter + approach summary out
// of an experience markdown. Returns (zero, false) on any parse
// failure so the search loop can skip silently.
func parseExperienceFile(path string) (Hit, bool) {
	f, err := os.Open(path)
	if err != nil {
		return Hit{}, false
	}
	defer f.Close()

	fm, body, ok := splitFrontmatter(f)
	if !ok {
		return Hit{}, false
	}
	hit := Hit{
		AgentConfigID:    fm["agent_config_id"],
		AgentName:        fm["agent_name"],
		AgentRole:        fm["agent_role"],
		TaskID:           fm["task_id"],
		TaskTitle:        fm["task_title"],
		TaskLabels:       parseYAMLList(fm["task_labels"]),
		WorkingDir:       fm["working_dir"],
		CompletionStatus: fm["completion_status"],
	}
	if v, err := strconv.ParseFloat(fm["outcome_score"], 64); err == nil {
		hit.OutcomeScore = v
	}
	if v, err := time.Parse(time.RFC3339, fm["completed_at"]); err == nil {
		hit.CompletedAt = v
	}
	hit.ApproachSummary = extractApproach(body)
	return hit, true
}

// splitFrontmatter reads a markdown file and returns the YAML map +
// the body. Expects the document to start with `---\n`. Returns
// (nil, "", false) when the format doesn't match.
func splitFrontmatter(r io.Reader) (map[string]string, string, bool) {
	br := bufio.NewReader(r)
	first, err := br.ReadString('\n')
	if err != nil || strings.TrimSpace(first) != "---" {
		return nil, "", false
	}
	fm := map[string]string{}
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return nil, "", false
		}
		stripped := strings.TrimRight(line, "\n\r")
		if stripped == "---" {
			break
		}
		// Parse `key: value` — value may itself contain a colon
		// (e.g. "2026-05-01T12:00:00Z"). Split on the first colon
		// only.
		idx := strings.Index(stripped, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(stripped[:idx])
		val := strings.TrimSpace(stripped[idx+1:])
		val = trimYAMLQuotes(val)
		fm[key] = val
	}
	// Body = rest of file.
	var b strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := br.Read(buf)
		if n > 0 {
			b.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	return fm, b.String(), true
}

// trimYAMLQuotes strips matching outer quotes from a scalar.
func trimYAMLQuotes(s string) string {
	if len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			inner := s[1 : len(s)-1]
			// Unescape \" + \\ for double-quoted strings.
			if first == '"' {
				inner = strings.ReplaceAll(inner, `\"`, `"`)
				inner = strings.ReplaceAll(inner, `\\`, `\`)
			}
			return inner
		}
	}
	return s
}

// parseYAMLList parses our flow-sequence form `[a, b, c]` back into
// a string slice. Tolerates surrounding quotes on each element.
func parseYAMLList(s string) []string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		return nil
	}
	inner := s[1 : len(s)-1]
	if inner == "" {
		return nil
	}
	parts := strings.Split(inner, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, trimYAMLQuotes(strings.TrimSpace(p)))
	}
	return out
}

// extractApproach pulls the content under the "## Approach" heading
// up to the next `## ` heading or end-of-body. Truncates to keep
// the rendered hint compact (~200 chars).
func extractApproach(body string) string {
	const marker = "## Approach"
	idx := strings.Index(body, marker)
	if idx < 0 {
		return ""
	}
	tail := body[idx+len(marker):]
	// Skip the newline after the heading.
	tail = strings.TrimLeft(tail, "\n")
	// Find the next ## heading (start of next section).
	if end := strings.Index(tail, "\n## "); end >= 0 {
		tail = tail[:end]
	}
	tail = strings.TrimSpace(tail)
	const maxApproachLen = 240
	return truncateOnRune(tail, maxApproachLen) + ellipsisIfTrunc(tail, maxApproachLen)
}

// truncateOnRune returns s cut to at most max bytes, backing up to a rune
// boundary so a multi-byte rune is never split (a half rune renders as a
// replacement char and can corrupt downstream markdown/YAML).
func truncateOnRune(s string, max int) string {
	if len(s) <= max {
		return s
	}
	limit := max
	for limit > 0 && !utf8.RuneStart(s[limit]) {
		limit--
	}
	return s[:limit]
}

// ellipsisIfTrunc returns the ellipsis marker only when s exceeds max bytes.
func ellipsisIfTrunc(s string, max int) string {
	if len(s) > max {
		return "…"
	}
	return ""
}

// computeSimilarity returns a [0, 1] score for how well q matches
// hit. The composition gives multiple paths to a high score so
// agents in different roles + dirs can still benefit from each
// other's experience.
//
// Weights:
//   0.50 * label_jaccard
//   0.25 * title_token_overlap
//   0.15 * workdir_prefix_match
//   0.10 * role_match
//
// Empty-on-both-sides yields 1.0 for that component (a query that
// omits labels matches anything; a hit with no labels matches a
// query that omits them).
func computeSimilarity(q Query, hit Hit) float64 {
	labels := jaccard(q.TaskLabels, hit.TaskLabels)
	title := tokenOverlap(q.TaskTitle, hit.TaskTitle)
	dir := prefixMatch(q.WorkingDir, hit.WorkingDir)
	role := 0.0
	if q.AgentRole == "" || hit.AgentRole == "" || q.AgentRole == hit.AgentRole {
		role = 1.0
	}
	return 0.50*labels + 0.25*title + 0.15*dir + 0.10*role
}

// jaccard returns |a ∩ b| / |a ∪ b| for two slices. Empty-on-both
// returns 1.0; empty-on-one returns 0.0.
func jaccard(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0
	}
	if len(a) == 0 || len(b) == 0 {
		return 0.0
	}
	set := map[string]struct{}{}
	for _, x := range a {
		set[strings.ToLower(x)] = struct{}{}
	}
	inter := 0
	for _, y := range b {
		if _, ok := set[strings.ToLower(y)]; ok {
			inter++
		}
	}
	union := len(set)
	for _, y := range b {
		if _, ok := set[strings.ToLower(y)]; !ok {
			union++
		}
	}
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// tokenOverlap returns the fraction of q's tokens (lowercased,
// non-trivial) that appear in hit. 0 if either side is empty.
func tokenOverlap(qStr, hitStr string) float64 {
	qToks := tokenizeForMatch(qStr)
	if len(qToks) == 0 {
		return 0
	}
	hitSet := map[string]struct{}{}
	for _, t := range tokenizeForMatch(hitStr) {
		hitSet[t] = struct{}{}
	}
	hits := 0
	for _, t := range qToks {
		if _, ok := hitSet[t]; ok {
			hits++
		}
	}
	return float64(hits) / float64(len(qToks))
}

// tokenizeForMatch lowercases + splits on non-alphanumeric. Drops
// tokens shorter than 3 chars and common stop-words to reduce noise.
var stopwords = map[string]struct{}{
	"the": {}, "and": {}, "for": {}, "with": {}, "from": {},
	"add": {}, "fix": {}, "use": {}, "set": {}, "get": {},
	"a": {}, "an": {}, "of": {}, "in": {}, "to": {}, "on": {},
}

func tokenizeForMatch(s string) []string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= '0' && r <= '9':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		default:
			b.WriteRune(' ')
		}
	}
	parts := strings.Fields(b.String())
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if len(p) < 3 {
			continue
		}
		if _, drop := stopwords[p]; drop {
			continue
		}
		out = append(out, p)
	}
	return out
}

// prefixMatch returns 1.0 when hit's working dir is the same or a
// descendant of q's, 0.5 for partial overlap, 0 for unrelated paths.
// Empty on either side returns 0.5 (mild match — better than 0 but
// not preferred over a real prefix hit).
func prefixMatch(qDir, hitDir string) float64 {
	if qDir == "" || hitDir == "" {
		return 0.5
	}
	q := filepath.Clean(qDir)
	h := filepath.Clean(hitDir)
	if q == h {
		return 1.0
	}
	if strings.HasPrefix(h, q+string(filepath.Separator)) ||
		strings.HasPrefix(q, h+string(filepath.Separator)) {
		return 0.75
	}
	// Partial: shared first directory component (e.g.
	// /repo/internal/auth vs /repo/internal/api both share "internal").
	qParts := strings.Split(q, string(filepath.Separator))
	hParts := strings.Split(h, string(filepath.Separator))
	common := 0
	for i := 0; i < len(qParts) && i < len(hParts); i++ {
		if qParts[i] == hParts[i] {
			common++
		} else {
			break
		}
	}
	if common >= 2 {
		return 0.5
	}
	return 0
}

// computeRecencyDecay linearly decays from 1.0 (now) to 0.0 (180+
// days ago). Zero-time completedAt returns 0.5 so unsorted records
// don't get a free pass to the top.
func computeRecencyDecay(now, completedAt time.Time) float64 {
	if completedAt.IsZero() {
		return 0.5
	}
	ageDays := now.Sub(completedAt).Hours() / 24
	if ageDays < 0 {
		return 1.0
	}
	if ageDays >= recencyHalfLifeDays {
		return 0
	}
	return 1.0 - ageDays/recencyHalfLifeDays
}
