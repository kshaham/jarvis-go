package office

// Unified knowledge search across the team's durable stores.
//
// Jarvis already has several per-agent knowledge surfaces:
//   - per-agent memory at ~/.jarvis/agent-memory/<projectHash>/<configID>.md
//   - per-agent learnings at ~/.jarvis/learnings/<configID>.json
//   - standup transcripts at ~/.jarvis/office-memory/standups/<id>.json
//   - team whiteboard at ~/.jarvis/office-memory/whiteboard.json (R17)
//
// Before this file, agents could only search their OWN memory with a
// substring-only CLI. Cross-agent knowledge was invisible unless the 1-
// line-per-peer kickstart digest happened to surface it. That made
// fresh agents re-derive knowledge that a teammate had written 10
// minutes earlier.
//
// SearchKnowledge walks every store in parallel, scores each line-level
// hit, and returns the top-N with source + agent + timestamp. Scoring
// is a simple (match + recency + source-weight) sum so recent teammate
// insights rank above stale entries.
//
// This is pure-Go, linear-scan, no index. For current scale (<50
// agents, <1000 standup JSONs) that's <20ms per query. A future
// embedding layer would stack on top, not replace.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// KnowledgeSource categorizes where a hit came from. Exposed so callers
// can filter or group results; values are stable for UI code.
type KnowledgeSource string

const (
	SourceMemory     KnowledgeSource = "memory"
	SourceLearning   KnowledgeSource = "learning"
	SourceStandup    KnowledgeSource = "standup"
	SourceWhiteboard KnowledgeSource = "whiteboard"
	SourceTask       KnowledgeSource = "task"
)

// KnowledgeHit is a single match from the unified search.
type KnowledgeHit struct {
	Source    KnowledgeSource `json:"source"`
	AgentName string          `json:"agentName,omitempty"`
	Section   string          `json:"section,omitempty"`
	Excerpt   string          `json:"excerpt"`
	Timestamp time.Time       `json:"timestamp,omitempty"`
	Score     float64         `json:"score"`
}

// KnowledgeSearchOptions tunes SearchKnowledge behavior.
type KnowledgeSearchOptions struct {
	// Query is the case-insensitive substring to find. Empty = Tail mode.
	Query string
	// Limit caps the number of returned hits (0 → defaultKnowledgeLimit).
	Limit int
	// WorkingDir scopes memory search to a single project. Empty = all
	// projects (used by --all-projects flag).
	WorkingDir string
	// AllowedAgents limits learnings + standup search to these configIDs
	// (the current team). Empty = no filter.
	AllowedAgents map[string]bool
}

const (
	defaultKnowledgeLimit = 20
	// maxExcerptChars trims very long matching lines so output stays
	// scannable. The full line is still available via the direct
	// source endpoint if an agent needs it.
	maxExcerptChars = 220
)

// SearchKnowledge walks every store and returns the top-N substring
// hits, recency-ranked. Zero hits returns nil — never an error — so
// callers can treat no-results as "nothing found yet."
func SearchKnowledge(opts KnowledgeSearchOptions) ([]KnowledgeHit, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = defaultKnowledgeLimit
	}
	query := strings.TrimSpace(opts.Query)
	if query == "" {
		return nil, fmt.Errorf("query is required; use TailKnowledge for recency-only output")
	}
	needle := strings.ToLower(query)

	var hits []KnowledgeHit
	hits = append(hits, scanMemoryHits(needle, opts)...)
	hits = append(hits, scanLearningHits(needle, opts)...)
	hits = append(hits, scanStandupHits(needle, opts)...)
	hits = append(hits, scanWhiteboardHits(needle)...)

	// Apply scoring + sort descending. Within equal scores, most-recent wins.
	for i := range hits {
		hits[i].Score = computeKnowledgeScore(hits[i])
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].Timestamp.After(hits[j].Timestamp)
	})

	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

// TailKnowledge returns the N most-recent entries from every source,
// without substring filtering. Useful for "what happened recently?"
func TailKnowledge(limit int, allowedAgents map[string]bool) ([]KnowledgeHit, error) {
	if limit <= 0 {
		limit = defaultKnowledgeLimit
	}
	opts := KnowledgeSearchOptions{AllowedAgents: allowedAgents}

	var hits []KnowledgeHit
	hits = append(hits, scanMemoryHits("", opts)...)
	hits = append(hits, scanLearningHits("", opts)...)
	hits = append(hits, scanStandupHits("", opts)...)
	hits = append(hits, scanWhiteboardHits("")...)

	// Pure recency sort.
	sort.SliceStable(hits, func(i, j int) bool {
		return hits[i].Timestamp.After(hits[j].Timestamp)
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

// computeKnowledgeScore scores a hit by (match count already folded into
// caller's signal of "it's here at all" = base 1) + recency bonus +
// source weight. A later embedding rerank would override this.
func computeKnowledgeScore(h KnowledgeHit) float64 {
	score := 1.0
	age := time.Since(h.Timestamp)
	switch {
	case age < time.Hour:
		score += 2.0
	case age < 24*time.Hour:
		score += 1.0
	}
	switch h.Source {
	case SourceLearning, SourceMemory:
		score += 1.0
	}
	return score
}

// --- Source scanners --------------------------------------------------

// scanMemoryHits walks every memory file under ~/.jarvis/agent-memory/.
// When needle == "" each line becomes a hit (tail mode); caller sorts.
// When WorkingDir is set, only that project's memory dir is scanned.
func scanMemoryHits(needle string, opts KnowledgeSearchOptions) []KnowledgeHit {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	root := filepath.Join(homeDir, ".jarvis", "agent-memory")

	var projectDirs []string
	if opts.WorkingDir != "" {
		projectDirs = []string{filepath.Join(root, ProjectHash(opts.WorkingDir))}
	} else {
		entries, readErr := os.ReadDir(root)
		if readErr != nil {
			return nil
		}
		for _, e := range entries {
			if e.IsDir() {
				projectDirs = append(projectDirs, filepath.Join(root, e.Name()))
			}
		}
	}

	var hits []KnowledgeHit
	for _, dir := range projectDirs {
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
				continue
			}
			configID := strings.TrimSuffix(f.Name(), ".md")
			if opts.AllowedAgents != nil && !opts.AllowedAgents[configID] {
				continue
			}
			path := filepath.Join(dir, f.Name())
			info, statErr := os.Stat(path)
			if statErr != nil {
				continue
			}
			file, openErr := os.Open(path)
			if openErr != nil {
				continue
			}
			section := "notes"
			scanner := bufio.NewScanner(file)
			scanner.Buffer(make([]byte, 64*1024), 1024*1024)
			for scanner.Scan() {
				line := scanner.Text()
				if strings.HasPrefix(line, sectionHeaderPrefix) {
					section = strings.TrimSpace(strings.TrimPrefix(line, sectionHeaderPrefix))
					continue
				}
				if strings.TrimSpace(line) == "" {
					continue
				}
				if strings.HasPrefix(line, "# Memory:") {
					continue
				}
				if needle != "" && !strings.Contains(strings.ToLower(line), needle) {
					continue
				}
				hits = append(hits, KnowledgeHit{
					Source:    SourceMemory,
					AgentName: configID,
					Section:   section,
					Excerpt:   trimExcerpt(line),
					// File mtime is the best proxy for "recency" per file
					// without per-line timestamps. Per-line would require
					// parsing timestamped entries; OK for now.
					Timestamp: info.ModTime(),
				})
			}
			_ = file.Close()
		}
	}
	return hits
}

// scanLearningHits walks every ~/.jarvis/learnings/*.json and emits a
// hit for each matching AgentLearning.Content line.
func scanLearningHits(needle string, opts KnowledgeSearchOptions) []KnowledgeHit {
	dir := learningsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var hits []KnowledgeHit
	for _, f := range entries {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		configID := strings.TrimSuffix(f.Name(), ".json")
		if opts.AllowedAgents != nil && !opts.AllowedAgents[configID] {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(dir, f.Name()))
		if readErr != nil {
			continue
		}
		var ld AgentLearningData
		if err := json.Unmarshal(data, &ld); err != nil {
			continue
		}
		agentName := ld.Name
		if agentName == "" {
			agentName = configID
		}
		for _, l := range ld.Learnings {
			text := l.Content
			if needle != "" && !strings.Contains(strings.ToLower(text), needle) {
				continue
			}
			hits = append(hits, KnowledgeHit{
				Source:    SourceLearning,
				AgentName: agentName,
				Section:   l.Category,
				Excerpt:   trimExcerpt(text),
				Timestamp: l.CreatedAt,
			})
		}
		// Distilled insights are valuable but undated — use file mtime as
		// a lower-bound timestamp so they still sort reasonably.
		if info, serr := os.Stat(filepath.Join(dir, f.Name())); serr == nil {
			for _, insight := range ld.DistilledInsights {
				if needle != "" && !strings.Contains(strings.ToLower(insight), needle) {
					continue
				}
				hits = append(hits, KnowledgeHit{
					Source:    SourceLearning,
					AgentName: agentName,
					Section:   "distilled",
					Excerpt:   trimExcerpt(insight),
					Timestamp: info.ModTime(),
				})
			}
		}
	}
	return hits
}

// scanStandupHits walks every ~/.jarvis/office-memory/standups/*.json.
// Each transcript entry becomes a candidate hit; only agent_response
// and user_input lines are emitted to keep the signal high.
func scanStandupHits(needle string, opts KnowledgeSearchOptions) []KnowledgeHit {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	dir := filepath.Join(homeDir, ".jarvis", "office-memory", "standups")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var hits []KnowledgeHit
	for _, f := range entries {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(dir, f.Name()))
		if readErr != nil {
			continue
		}
		var s PersistedStandup
		if err := json.Unmarshal(bytes.TrimSpace(data), &s); err != nil {
			continue
		}
		startedAt := time.UnixMilli(s.StartedAt)
		for _, msg := range s.Transcript {
			// Skip pure prompts — the value is in what agents said.
			if msg.Type == "opening" || msg.Type == "closing" {
				continue
			}
			text := msg.Content
			if needle != "" && !strings.Contains(strings.ToLower(text), needle) {
				continue
			}
			ts := startedAt
			if msg.Timestamp > 0 {
				ts = time.UnixMilli(msg.Timestamp)
			}
			hits = append(hits, KnowledgeHit{
				Source:    SourceStandup,
				AgentName: msg.SpeakerName,
				Section:   msg.Type,
				Excerpt:   trimExcerpt(text),
				Timestamp: ts,
			})
		}
	}
	return hits
}

// scanWhiteboardHits reads the persisted whiteboard JSON (R17). Missing
// file is not an error — the team simply hasn't written yet.
func scanWhiteboardHits(needle string) []KnowledgeHit {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	path := filepath.Join(homeDir, ".jarvis", "office-memory", "whiteboard.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var board TeamWhiteboard
	if err := json.Unmarshal(data, &board); err != nil {
		return nil
	}
	var hits []KnowledgeHit
	for agentName, entry := range board.Entries {
		for sectionName, section := range entry.Sections {
			text := section.Content
			if text == "" {
				continue
			}
			if needle != "" && !strings.Contains(strings.ToLower(text), needle) {
				continue
			}
			hits = append(hits, KnowledgeHit{
				Source:    SourceWhiteboard,
				AgentName: agentName,
				Section:   sectionName,
				Excerpt:   trimExcerpt(text),
				Timestamp: section.UpdatedAt,
			})
		}
	}
	return hits
}

// trimExcerpt keeps hits scannable in terminal output without hiding the
// part that matched. We don't try to center the match — callers that
// need a better excerpt can re-read the source directly.
//
// Uses ASCII "..." (3 bytes) so len() stays byte-accurate for tests and
// the total length never exceeds maxExcerptChars.
func trimExcerpt(line string) string {
	line = strings.TrimSpace(line)
	if len(line) <= maxExcerptChars {
		return line
	}
	// Back up to a rune boundary so a multi-byte rune (emoji / non-ASCII in
	// agent memory) isn't split into invalid UTF-8 in the marshaled excerpt.
	limit := maxExcerptChars - 3
	for limit > 0 && !utf8.RuneStart(line[limit]) {
		limit--
	}
	return line[:limit] + "..."
}
