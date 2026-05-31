// Package library owns the markdown corpus the PageIndex MCP server
// indexes. Every meaningful event in a jarvis run — task completed,
// ultrareview ran, workflow consolidated, standup transcribed —
// renders into a deterministic markdown file under ~/.jarvis/
// office-memory/library/. PageIndex builds its tree index over that
// directory; agents query past experiences via mcp__pageindex__*
// tools wired into their SDK session.
//
// Design contract:
//   - All writes are atomic (tmp file + rename) — mirrors
//     persistMessages in internal/office/manager.go so an interrupted
//     write can't leave a half-rendered doc that confuses PageIndex's
//     tree extractor.
//   - YAML frontmatter is rendered deterministically (sorted keys,
//     stable formatting) so the same record produces byte-identical
//     output across runs. Lets PageIndex cache index entries safely.
//   - Library types are decoupled from office package types — the
//     office caller converts at the boundary. This keeps library a
//     leaf utility (no cycles) and lets the schema evolve without
//     dragging office along.
//   - No goroutines spawned here. Every public function is
//     synchronous; callers wrap in safego.Go when invoking from
//     background paths.
package library

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// libDir caches the resolved library root; resolved marks whether the
// one-time resolution (or a test override) has run. Both are guarded by
// dirMu: the office idle-monitor goroutine calls Dir() (a read)
// concurrently with tests that repoint the root via SetDirForTest (a
// write), so unsynchronized access here is a real data race. The mutex
// makes that read/write pair race-free under -race. (Replaces an
// earlier sync.Once + pointer-swap scheme whose dirOnce/libDir reads
// and writes were unsynchronized.)
var (
	dirMu    sync.RWMutex
	libDir   string
	resolved bool
)

// Dir returns the library root path (creating it + standard
// subdirectories if missing). Subsequent calls reuse the cached path.
//
// Layout:
//
//	<root>/experiences/<configID>/<taskID>.md
//	<root>/workflows/<slug>.md
//	<root>/standups/<id>.md
//	<root>/experiments/<id>.md
//	<root>/.promotion-ledger.json
//
// On any filesystem failure (HOME unset, permission denied), Dir
// returns "" and callers MUST treat write attempts as no-ops. This
// matches the "graceful degradation" pattern jarvis uses for every
// optional subsystem (telegram, codegraph, ECC, etc.).
func Dir() string {
	// Fast path: already resolved (or test-overridden).
	dirMu.RLock()
	if resolved {
		d := libDir
		dirMu.RUnlock()
		return d
	}
	dirMu.RUnlock()

	// Slow path: resolve exactly once under the write lock. Success or
	// failure is cached (resolved=true), matching the previous
	// sync.Once semantics — on a filesystem failure Dir() degrades to
	// "" permanently rather than retrying every call.
	dirMu.Lock()
	defer dirMu.Unlock()
	if resolved {
		return libDir // another goroutine resolved while we waited
	}
	resolved = true
	home, err := os.UserHomeDir()
	if err != nil {
		return libDir // ""
	}
	root := filepath.Join(home, ".jarvis", "office-memory", "library")
	for _, sub := range []string{
		"experiences", "workflows", "standups", "experiments", "anti-patterns",
	} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			return libDir // "" — cached, no retry
		}
	}
	libDir = root
	return libDir
}

// SetDirForTest overrides the cached library root. Returns a restore
// function suitable for t.Cleanup. Tests use this to point library
// writes at a temp dir without touching the operator's real library.
func SetDirForTest(path string) (restore func()) {
	dirMu.Lock()
	prevDir, prevResolved := libDir, resolved
	libDir = path
	resolved = true // mark as resolved so Dir returns libDir directly
	dirMu.Unlock()
	return func() {
		dirMu.Lock()
		libDir = prevDir
		resolved = prevResolved
		dirMu.Unlock()
	}
}

// ExperienceRecord is the canonical "one task happened, here's how
// it went" payload. Rendered into a single markdown file under
// experiences/<configID>/<taskID>.md.
//
// All fields are inputs to the YAML frontmatter or the body sections.
// Render is stable: same inputs → byte-identical output.
type ExperienceRecord struct {
	// Identity
	AgentConfigID string
	AgentName     string
	AgentRole     string
	TaskID        string
	TaskTitle     string
	TaskLabels    []string
	WorkingDir    string

	// Outcome
	CompletionStatus    string // "completed" | "failed" | "abandoned"
	OutcomeScore        float64
	UltrareviewSeverity string // "none" | "nit" | "warning" | "blocker"
	RecoveryAttempts    int

	// Cost / duration
	StartedAt   time.Time
	CompletedAt time.Time
	DurationMS  int64
	CostUSD     float64

	// Narrative (distilled — not the raw progress log)
	ApproachSummary string
	Learnings       []string
	FilesModified   []string
}

// Finding mirrors the ultrareview finding shape without importing
// the office package (would create a cycle).
type Finding struct {
	Severity    string
	File        string
	Line        int
	Title       string
	Description string
	Category    string
}

// UltrareviewAppendRecord is the payload AppendUltrareview consumes.
// Adds a "## Ultrareview" section to the experience file matching
// the TaskID — does NOT replace, so multiple runs of ultrareview
// stack chronologically.
type UltrareviewAppendRecord struct {
	AgentConfigID string // needed to locate the experience file
	TaskID        string
	Findings      []Finding
	Summary       string
	Took          time.Duration
	Timestamp     time.Time
}

// Workflow is a consolidated reusable pattern derived from N
// high-outcome experiences via the memify pipeline. Rendered to
// workflows/<slug>.md with the derived-from + avg-outcome fields
// in frontmatter so PageIndex can surface them by structure.
type Workflow struct {
	Slug                 string
	Title                string
	DerivedFromTaskIDs   []string
	AvgOutcome           float64
	ReuseCountAtCreation int
	AgentRole            string
	TaskLabels           []string
	WorkingDirHints      []string
	Steps                []string
	KnownPitfalls        []string
	CreatedAt            time.Time
}

// StandupTranscript is the rendered form of a completed standup.
type StandupTranscript struct {
	ID        string
	StartedAt time.Time
	EndedAt   time.Time
	Topic     string
	Messages  []StandupMessage
}

// StandupMessage mirrors office.StandupMessage without the import.
type StandupMessage struct {
	SessionID string
	Speaker   string
	Content   string
	Type      string // "opening" | "status_report" | "closing" | "takeaway"
	Timestamp time.Time
}

// ExperimentRecord mirrors office.ExperimentRecord for the library
// rendering. The jsonl file in office-memory/ stays as the source
// of truth; this is a markdown projection for PageIndex.
type ExperimentRecord struct {
	ID             string
	TeamTemplate   string
	Mission        string
	TaskCount      int
	CompletedCount int
	AvgScore       float64
	DurationMS     int64
	TopPerformer   string
	BottomPerformer string
	Timestamp      time.Time
}

// AntiPattern is the parallel-to-Workflow shape for failure
// lessons. Memify derives these from clusters of LOW-outcome
// experiences (avg < lowOutcomeThreshold) so PageIndex can surface
// "don't do this" guidance alongside the positive workflow it has
// for the same (role, labels).
//
// Slug matches the corresponding workflow slug so an agent looking
// up "auth" via the MCP gets BOTH "what worked" and "what to
// avoid" in the same tree branch.
type AntiPattern struct {
	Slug                 string
	Title                string
	DerivedFromTaskIDs   []string
	AvgOutcome           float64 // expect < lowOutcomeThreshold
	ObservationCount     int     // size of the low-outcome cluster
	AgentRole            string
	TaskLabels           []string
	WorkingDirHints      []string
	WhatWentWrong        []string // observations from the cluster
	WhatToAvoid          []string // actionable guidance
	WhyItFails           string   // root-cause explanation
	CreatedAt            time.Time
}

// experiencePath returns the absolute path for the given (configID,
// taskID) pair. Caller MUST verify root != "" before using the
// returned path.
func experiencePath(root, configID, taskID string) string {
	return filepath.Join(root, "experiences", sanitize(configID), sanitize(taskID)+".md")
}

// workflowPath returns the absolute path for a workflow doc.
func workflowPath(root, slug string) string {
	return filepath.Join(root, "workflows", sanitize(slug)+".md")
}

// standupPath returns the absolute path for a standup transcript.
func standupPath(root, id string) string {
	return filepath.Join(root, "standups", sanitize(id)+".md")
}

// experimentPath returns the absolute path for an experiment doc.
func experimentPath(root, id string) string {
	return filepath.Join(root, "experiments", sanitize(id)+".md")
}

// antiPatternPath returns the absolute path for an anti-pattern.
func antiPatternPath(root, slug string) string {
	return filepath.Join(root, "anti-patterns", sanitize(slug)+".md")
}

// sanitize replaces every character outside [a-zA-Z0-9_-] with '-'.
// Dots are deliberately rejected — config IDs, task IDs, workflow
// slugs, standup IDs all naturally avoid them, and stripping dots
// eliminates the `..` traversal class entirely without needing a
// separate filter pass.
//
// NOT a security boundary on its own — the caller MUST also verify
// the resolved abs path stays inside Dir().
func sanitize(s string) string {
	if s == "" {
		return "unknown"
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := b.String()
	if out == "" || strings.Trim(out, "-") == "" {
		return "unknown"
	}
	return out
}

// writeAtomic writes content to path via a tmp file + rename. Mirror
// of internal/office/manager.go:persistMessages so a partial write
// can't leave a half-rendered markdown doc visible to PageIndex.
//
// Creates the parent directory if missing (one-shot mkdir; the
// per-subdir setup happens in Dir but a stray configID needs its own
// experiences/<configID>/ folder).
func writeAtomic(path string, content []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir parent: %w", err)
	}
	// Unique tmp name so two concurrent writes — even to the same final path —
	// can't collide on a shared "<path>.tmp" (one renaming a tmp the other is
	// mid-writing). os.CreateTemp guarantees a unique name; the last successful
	// rename wins, which is the intended atomic-write semantics.
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create tmp: %w", err)
	}
	tmpName := tmp.Name()
	if _, werr := tmp.Write(content); werr != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("write tmp: %w", werr)
	}
	if cerr := tmp.Close(); cerr != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close tmp: %w", cerr)
	}
	if cerr := os.Chmod(tmpName, 0o644); cerr != nil { // match prior 0644 perms
		_ = os.Remove(tmpName)
		return fmt.Errorf("chmod tmp: %w", cerr)
	}
	if rerr := os.Rename(tmpName, path); rerr != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename: %w", rerr)
	}
	return nil
}

// WriteExperience renders rec into experiences/<configID>/<taskID>.md
// atomically. Returns an error on any filesystem failure so the
// caller (typically wrapped in safego.Go) can log it at warn level.
//
// Overwrites any existing file at the same path — re-running a task
// (e.g. via restart) is treated as a corrected experience, not a
// duplicate.
func WriteExperience(rec ExperienceRecord) error {
	root := Dir()
	if root == "" {
		return fmt.Errorf("library root unavailable")
	}
	path := experiencePath(root, rec.AgentConfigID, rec.TaskID)
	body := renderExperience(rec)
	return writeAtomic(path, []byte(body))
}

// AppendUltrareview adds a "## Ultrareview <timestamp>" section to
// the experience file matching rec.AgentConfigID + rec.TaskID. If
// the experience file doesn't exist yet (ultrareview fired before
// the experience write — possible race), the function returns a
// "no experience file" error that the caller logs and ignores.
func AppendUltrareview(rec UltrareviewAppendRecord) error {
	root := Dir()
	if root == "" {
		return fmt.Errorf("library root unavailable")
	}
	path := experiencePath(root, rec.AgentConfigID, rec.TaskID)
	existing, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no experience file for task %s", rec.TaskID)
		}
		return fmt.Errorf("read existing: %w", err)
	}
	appended := string(existing) + "\n" + renderUltrareviewSection(rec)
	return writeAtomic(path, []byte(appended))
}

// WriteWorkflow renders w into workflows/<slug>.md atomically.
// Workflows overwrite freely — memify rebuilds them on each tick as
// derived-from sets change.
func WriteWorkflow(w Workflow) error {
	root := Dir()
	if root == "" {
		return fmt.Errorf("library root unavailable")
	}
	path := workflowPath(root, w.Slug)
	body := renderWorkflow(w)
	return writeAtomic(path, []byte(body))
}

// RenderStandup writes a standup transcript to standups/<id>.md.
// Idempotent — re-rendering the same transcript produces identical
// output.
func RenderStandup(s StandupTranscript) error {
	root := Dir()
	if root == "" {
		return fmt.Errorf("library root unavailable")
	}
	path := standupPath(root, s.ID)
	body := renderStandup(s)
	return writeAtomic(path, []byte(body))
}

// WriteExperiment writes an experiment record (run-level outcome
// summary) to experiments/<id>.md. The canonical JSONL store in
// office-memory/experiments.jsonl stays unchanged — this is a
// markdown projection so PageIndex can index it alongside per-task
// experiences.
func WriteExperiment(rec ExperimentRecord) error {
	root := Dir()
	if root == "" {
		return fmt.Errorf("library root unavailable")
	}
	path := experimentPath(root, rec.ID)
	body := renderExperiment(rec)
	return writeAtomic(path, []byte(body))
}

// WriteAntiPattern renders ap into anti-patterns/<slug>.md
// atomically. Same overwrite-freely semantics as WriteWorkflow —
// memify rebuilds these on each tick as the low-outcome cluster
// composition changes.
func WriteAntiPattern(ap AntiPattern) error {
	root := Dir()
	if root == "" {
		return fmt.Errorf("library root unavailable")
	}
	path := antiPatternPath(root, ap.Slug)
	body := renderAntiPattern(ap)
	return writeAtomic(path, []byte(body))
}
