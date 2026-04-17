package office

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// projectDocsCacheTTL bounds how often we re-read CLAUDE.md / README.md /
// ARCHITECTURE.md from disk. A 5-minute window means a team spawn doesn't
// re-parse N times, but a developer editing the docs sees the change within
// the next respawn cycle.
const projectDocsCacheTTL = 5 * time.Minute

// projectDocsPerFileCap limits how many bytes we extract from any single
// candidate file so a pathological README doesn't balloon the kickstart.
const projectDocsPerFileCap = 1200

// projectDocsTotalCap caps the total injected block across all files.
const projectDocsTotalCap = 2500

// projectDocCandidates names files we scan for, in priority order. The first
// non-empty extracted section wins; additional files fill remaining budget.
var projectDocCandidates = []string{
	"CLAUDE.md",
	"README.md",
	"ARCHITECTURE.md",
	"docs/ARCHITECTURE.md",
}

type projectDocsEntry struct {
	content string
	fetched time.Time
}

var (
	projectDocsMu    sync.RWMutex
	projectDocsCache = map[string]projectDocsEntry{}
)

// BuildProjectContext returns a compact summary of project-level documentation
// found in workingDir. Returns an empty string when no relevant file exists.
// Cached per-workingDir with a 5-minute TTL.
func BuildProjectContext(workingDir string) string {
	if workingDir == "" {
		return ""
	}

	projectDocsMu.RLock()
	if cached, ok := projectDocsCache[workingDir]; ok && time.Since(cached.fetched) < projectDocsCacheTTL {
		projectDocsMu.RUnlock()
		return cached.content
	}
	projectDocsMu.RUnlock()

	content := buildProjectContextUncached(workingDir)

	projectDocsMu.Lock()
	projectDocsCache[workingDir] = projectDocsEntry{content: content, fetched: time.Now()}
	projectDocsMu.Unlock()

	return content
}

func buildProjectContextUncached(workingDir string) string {
	var out strings.Builder
	remaining := projectDocsTotalCap

	for _, name := range projectDocCandidates {
		if remaining <= 200 {
			break
		}
		path := filepath.Join(workingDir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		excerpt := extractDocExcerpt(string(data))
		if excerpt == "" {
			continue
		}
		if len(excerpt) > projectDocsPerFileCap {
			excerpt = excerpt[:projectDocsPerFileCap] + "\n..."
		}
		if len(excerpt) > remaining {
			excerpt = excerpt[:remaining] + "\n..."
		}
		out.WriteString("### ")
		out.WriteString(name)
		out.WriteString("\n")
		out.WriteString(excerpt)
		out.WriteString("\n\n")
		remaining -= len(excerpt) + len(name) + 10
	}
	return strings.TrimSpace(out.String())
}

// markdownImageRe matches `![...](...)` and `[![..](..)](..)` patterns that
// bloat the excerpt without adding context.
var markdownImageRe = regexp.MustCompile(`\[?!\[[^\]]*\]\([^)]*\)(?:\([^)]*\))?`)

// extractDocExcerpt pulls the first meaningful prose block out of a markdown
// file: skips the title, strips code fences / image badges, and returns the
// first ~1200 chars of body text. Heuristic but robust enough for the common
// CLAUDE.md / README.md shape.
func extractDocExcerpt(md string) string {
	md = markdownImageRe.ReplaceAllString(md, "")

	lines := strings.Split(md, "\n")
	var out strings.Builder
	inCode := false
	seenProse := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inCode = !inCode
			continue
		}
		if inCode {
			continue
		}
		// Skip the very first H1 (title) so the excerpt is the first real
		// paragraph, not just "# Project Name".
		if !seenProse && strings.HasPrefix(trimmed, "# ") {
			continue
		}
		// Skip pure badge / link lines (common README preamble).
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			seenProse = true
		}
		out.WriteString(line)
		out.WriteString("\n")
		if out.Len() > projectDocsPerFileCap {
			break
		}
	}
	return strings.TrimSpace(out.String())
}
