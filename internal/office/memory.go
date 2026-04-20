package office

// Sectioned per-agent memory.
//
// On disk, each agent has one memory file at
//   ~/.jarvis/agent-memory/<projectHash>/<configID>.md
// (see GetAgentMemoryPath in manager.go).
//
// Sectioned format looks like:
//
//   # Memory: <agent>
//
//   ## section: current_focus
//   <content>
//
//   ## section: decisions
//   <content>
//
//   ## section: notes
//   <content>
//
// Legacy files (written before this format existed) have no
// "## section:" headers; the parser treats the entire file as the
// "notes" section. The first sectioned write migrates the file
// transparently.
//
// The legacy LoadAgentMemory/SaveAgentMemory/AppendAgentMemory in
// manager.go still work the way they always did: they read or replace
// the entire file as a flat blob. The new helpers below operate on
// individual sections.

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

const (
	sectionHeaderPrefix = "## section: "
	maxMemoryQueryHits  = 20
)

// memoryFileLocks serializes writes to the same agent memory file.
// R27 state review · AppendAgentMemory and MemoryWriteSection used to
// write the same file path from different goroutines with no
// coordination. A concurrent section rewrite could overwrite a fresh
// append, or vice versa, leaving the file internally inconsistent.
// Now every write path acquires the per-path lock before touching
// the file.
//
// Lock discipline: ALWAYS lock the per-path mutex BEFORE opening or
// truncating the file, release AFTER rename / close. A single mutex
// per memory file is sufficient — there's one writer logical concept
// per agent file, operations just need to serialize.
var memoryFileLocks sync.Map // map[string]*sync.Mutex

// memoryFileLock returns the mutex for the given memory file path,
// creating it on first use. Callers must Lock() before touching the
// file and Unlock() after.
func memoryFileLock(path string) *sync.Mutex {
	if v, ok := memoryFileLocks.Load(path); ok {
		return v.(*sync.Mutex)
	}
	mu := &sync.Mutex{}
	actual, _ := memoryFileLocks.LoadOrStore(path, mu)
	return actual.(*sync.Mutex)
}

var memorySectionNameRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

// MemoryHit is a single line-level match returned by MemoryQuery.
type MemoryHit struct {
	Section string `json:"section"`
	Line    int    `json:"line"`
	Text    string `json:"text"`
}

// IsValidMemorySectionName reports whether the given name is safe to use
// as a memory section identifier (no path traversal, no weird chars).
func IsValidMemorySectionName(name string) bool {
	return memorySectionNameRE.MatchString(name)
}

// MemoryReadSection returns the content of the named section, or "" if
// the section does not exist. Returns an error only on file read errors
// other than not-exist.
func MemoryReadSection(configID, workingDir, name string) (string, error) {
	if !IsValidMemorySectionName(name) {
		return "", fmt.Errorf("invalid section name: %q", name)
	}
	sections, _, err := loadMemorySections(configID, workingDir)
	if err != nil {
		return "", err
	}
	return sections[name], nil
}

// MemoryWriteSection overwrites (or creates) the named section's
// content. Other sections are preserved.
func MemoryWriteSection(configID, workingDir, name, content string) error {
	if !IsValidMemorySectionName(name) {
		return fmt.Errorf("invalid section name: %q", name)
	}
	sections, header, err := loadMemorySections(configID, workingDir)
	if err != nil {
		return err
	}
	sections[name] = strings.TrimRight(content, "\n")
	return writeMemorySections(configID, workingDir, header, sections)
}

// MemoryAppendSection appends a single line (or block) to the named
// section. The section is created if it does not exist. The appended
// text is separated from existing content with a single newline.
func MemoryAppendSection(configID, workingDir, name, line string) error {
	if !IsValidMemorySectionName(name) {
		return fmt.Errorf("invalid section name: %q", name)
	}
	sections, header, err := loadMemorySections(configID, workingDir)
	if err != nil {
		return err
	}
	existing := sections[name]
	if existing == "" {
		sections[name] = strings.TrimRight(line, "\n")
	} else {
		sections[name] = strings.TrimRight(existing, "\n") + "\n" + strings.TrimRight(line, "\n")
	}
	return writeMemorySections(configID, workingDir, header, sections)
}

// MemoryQuery does a line-level case-insensitive substring search across
// all sections of the agent's memory file. Returns up to maxMemoryQueryHits
// hits with section + line number + text, sorted by line number descending
// (newest-first) — appended entries live at the end of sections, so later
// lines are typically newer lessons.
// Returns nil if the file does not exist.
func MemoryQuery(configID, workingDir, keyword string) ([]MemoryHit, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, fmt.Errorf("empty keyword")
	}
	path := GetAgentMemoryPath(configID, workingDir)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	needle := strings.ToLower(keyword)
	var hits []MemoryHit
	currentSection := "notes" // legacy default for any prelude
	lineNo := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	// First pass: collect ALL matches so we can sort before truncating.
	// A large memory file with thousands of lines still scans quickly
	// (single linear walk, substring test per line).
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if strings.HasPrefix(line, sectionHeaderPrefix) {
			currentSection = strings.TrimSpace(strings.TrimPrefix(line, sectionHeaderPrefix))
			continue
		}
		if strings.Contains(strings.ToLower(line), needle) {
			hits = append(hits, MemoryHit{
				Section: currentSection,
				Line:    lineNo,
				Text:    line,
			})
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	// Newest-first (higher line number ~ later in file ~ more recent).
	sort.SliceStable(hits, func(i, j int) bool {
		return hits[i].Line > hits[j].Line
	})
	if len(hits) > maxMemoryQueryHits {
		hits = hits[:maxMemoryQueryHits]
	}
	return hits, nil
}

// ---------------------------------------------------------------------------
// Internal: parse + serialize sectioned memory files
// ---------------------------------------------------------------------------

// loadMemorySections reads the agent's memory file and parses it into a
// map of section name -> content. Lines before the first "## section:"
// header (or the entire file if there are no headers) become the "notes"
// section. The optional "header" return value is the file's leading
// "# Memory: ..." title line, if present, so writes can preserve it.
func loadMemorySections(configID, workingDir string) (map[string]string, string, error) {
	path := GetAgentMemoryPath(configID, workingDir)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, "", nil
		}
		return nil, "", err
	}

	sections := make(map[string]string)
	header := ""

	lines := strings.Split(string(data), "\n")
	currentSection := ""
	var prelude []string
	var current []string

	flush := func() {
		if currentSection != "" {
			// Trim trailing blank lines for cleanliness.
			for len(current) > 0 && strings.TrimSpace(current[len(current)-1]) == "" {
				current = current[:len(current)-1]
			}
			sections[currentSection] = strings.Join(current, "\n")
		}
		current = nil
	}

	for i, line := range lines {
		if i == 0 && strings.HasPrefix(line, "# Memory:") {
			header = line
			continue
		}
		if strings.HasPrefix(line, sectionHeaderPrefix) {
			flush()
			currentSection = strings.TrimSpace(strings.TrimPrefix(line, sectionHeaderPrefix))
			continue
		}
		if currentSection == "" {
			prelude = append(prelude, line)
		} else {
			current = append(current, line)
		}
	}
	flush()

	// Legacy migration: if there were no section headers, the prelude IS
	// the notes section.
	if len(sections) == 0 && len(prelude) > 0 {
		// Trim leading + trailing blank lines.
		for len(prelude) > 0 && strings.TrimSpace(prelude[0]) == "" {
			prelude = prelude[1:]
		}
		for len(prelude) > 0 && strings.TrimSpace(prelude[len(prelude)-1]) == "" {
			prelude = prelude[:len(prelude)-1]
		}
		if len(prelude) > 0 {
			sections["notes"] = strings.Join(prelude, "\n")
		}
	}

	return sections, header, nil
}

// writeMemorySections serializes a section map back to disk in the
// canonical sectioned format. R27 · Serializes with AppendAgentMemory
// against concurrent writes on the same file via memoryFileLock.
func writeMemorySections(configID, workingDir, header string, sections map[string]string) error {
	path := GetAgentMemoryPath(configID, workingDir)
	lock := memoryFileLock(path)
	lock.Lock()
	defer lock.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	var b strings.Builder
	if header == "" {
		b.WriteString("# Memory: ")
		b.WriteString(configID)
		b.WriteString("\n\n")
	} else {
		b.WriteString(header)
		b.WriteString("\n\n")
	}

	// Stable section ordering: known sections first in a fixed order, then
	// any other sections alphabetically. This makes diffs reviewable.
	preferred := []string{"current_focus", "decisions", "blockers", "files_owned", "onboarding", "task_history", "notes"}
	written := make(map[string]bool, len(sections))

	writeOne := func(name string) {
		content, ok := sections[name]
		if !ok {
			return
		}
		b.WriteString(sectionHeaderPrefix)
		b.WriteString(name)
		b.WriteString("\n")
		if content != "" {
			b.WriteString(content)
			b.WriteString("\n")
		}
		b.WriteString("\n")
		written[name] = true
	}
	for _, name := range preferred {
		writeOne(name)
	}
	// Any remaining sections alphabetically.
	var rest []string
	for name := range sections {
		if !written[name] {
			rest = append(rest, name)
		}
	}
	sort.Strings(rest)
	for _, name := range rest {
		writeOne(name)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
