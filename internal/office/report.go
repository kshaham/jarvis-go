package office

// R33 · Wrap-up / post-mortem report generator.
//
// Builds a structured Markdown document summarizing a team run --
// mission, per-agent task history, standup highlights, handoff log,
// cost + token totals, whiteboard state. Two trigger paths:
//
//  1. Automatic on wrap-up (handler/office.go:OfficePostWrapUp hooks
//     a fire-and-forget call).
//  2. Manual via POST /api/office/reports/generate.
//
// Persisted atomically to ~/.jarvis/office-memory/reports/ with a
// UTC-stamped filename. Old reports front-drop past 100 entries.
//
// Reuses existing accessors (GetRoster, LoadAgentMemory,
// LoadLearningData, GetHandoffHistory, GetWhiteboard, Metrics
// snapshot) -- no duplicate data paths.

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// WrapUpReportMeta is the report index entry exposed via
// GET /api/office/reports. Excludes the Markdown body so list
// responses stay small.
type WrapUpReportMeta struct {
	Filename    string    `json:"filename"`
	GeneratedAt time.Time `json:"generatedAt"`
	Trigger     string    `json:"trigger"` // "wrap-up" or "manual"
	Reason      string    `json:"reason,omitempty"`
	TeamMission string    `json:"teamMission,omitempty"`
	SizeBytes   int       `json:"sizeBytes"`
}

const (
	// WrapUpReportsMaxKeep caps the reports directory. Matches the
	// HandoffAuditCap pattern -- a handful of MB even for heavy use.
	WrapUpReportsMaxKeep = 100
)

// reportFilenameRE enforces `wrap-up-YYYYMMDD-HHMMSS.md`. Used by
// the HTTP handler's path validation to block traversal attempts
// before we ever touch the filesystem.
var reportFilenameRE = regexp.MustCompile(`^wrap-up-\d{8}-\d{6}\.md$`)

// ValidateReportFilename reports whether the given string is a safe
// report filename. Rejects path traversal, unexpected extensions,
// and anything that doesn't match the generated naming convention.
func ValidateReportFilename(name string) bool {
	return reportFilenameRE.MatchString(name)
}

// WrapUpReportsDir returns the canonical reports directory path.
func WrapUpReportsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".jarvis", "office-memory", "reports")
}

// GenerateAndBroadcastWrapUpReport runs GenerateWrapUpReport and
// emits a single SSE event on success, or logs the failure on
// error. Used by the wrap-up handler's fire-and-forget goroutine
// so the result-shape + broadcast logic doesn't have to live
// inline.
//
// broadcast may be nil — tests use that path to verify the report
// generation works without an SSE broker present.
//
// Failures are logged at error level (not returned) because the
// caller is a goroutine — there's nowhere to return an error to.
// Operators can always retry via POST /api/office/reports/generate.
func GenerateAndBroadcastWrapUpReport(m *Manager, trigger, reason string, broadcast SSEBroadcaster) {
	meta, err := GenerateWrapUpReport(m, trigger, reason)
	if err != nil {
		slog.Error("auto-generate wrap-up report failed", "err", err, "trigger", trigger)
		return
	}
	if broadcast != nil {
		broadcast("office:reportGenerated", meta)
	}
	slog.Info("wrap-up report generated", "filename", meta.Filename, "bytes", meta.SizeBytes)
}

// GenerateWrapUpReport builds a structured Markdown report for the
// current team state, writes it to disk atomically, prunes old
// reports to WrapUpReportsMaxKeep, and returns the metadata. Safe to
// call during or outside a wrap-up; operator decides when.
func GenerateWrapUpReport(m *Manager, trigger, reason string) (*WrapUpReportMeta, error) {
	now := time.Now().UTC()
	filename := fmt.Sprintf("wrap-up-%s.md", now.Format("20060102-150405"))

	body := buildReportBody(m, now, trigger, reason)

	dir := WrapUpReportsDir()
	if dir == "" {
		return nil, fmt.Errorf("no home directory")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create reports dir: %w", err)
	}
	fullPath := filepath.Join(dir, filename)
	tmp := fullPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
		return nil, fmt.Errorf("write report tmp: %w", err)
	}
	if err := os.Rename(tmp, fullPath); err != nil {
		_ = os.Remove(tmp)
		return nil, fmt.Errorf("rename report: %w", err)
	}

	// Front-drop old reports. A failure here is non-fatal -- the
	// newly-written report is fine on its own.
	if err := pruneOldReports(dir, WrapUpReportsMaxKeep); err != nil {
		slog.Warn("prune old reports failed", "err", err)
	}

	return &WrapUpReportMeta{
		Filename:    filename,
		GeneratedAt: now,
		Trigger:     trigger,
		Reason:      reason,
		TeamMission: truncateForSnippet(m.GetTeamMission(), 200),
		SizeBytes:   len(body),
	}, nil
}

// ListWrapUpReports returns every persisted report's metadata,
// newest-first. Missing directory = empty list, not an error.
func ListWrapUpReports() ([]WrapUpReportMeta, error) {
	dir := WrapUpReportsDir()
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]WrapUpReportMeta, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !ValidateReportFilename(e.Name()) {
			continue
		}
		meta, err := readReportMeta(dir, e.Name())
		if err != nil {
			slog.Warn("skip unparseable report", "name", e.Name(), "err", err)
			continue
		}
		out = append(out, meta)
	}
	// Newest first (sort by generatedAt descending; filename suffices
	// because it carries a UTC timestamp).
	sort.Slice(out, func(i, j int) bool { return out[i].Filename > out[j].Filename })
	return out, nil
}

// LoadWrapUpReport returns the raw Markdown body for the given
// filename. Caller MUST pass ValidateReportFilename-approved input;
// this function re-checks defensively.
func LoadWrapUpReport(filename string) (string, error) {
	if !ValidateReportFilename(filename) {
		return "", fmt.Errorf("invalid report filename")
	}
	dir := WrapUpReportsDir()
	if dir == "" {
		return "", fmt.Errorf("no home directory")
	}
	full := filepath.Join(dir, filename)
	// Resolve symlinks + enforce the file is inside dir (R27 pattern).
	// Resolve both sides so symlinked parents (macOS /var -> /private/var)
	// don't trip the prefix check.
	resolved, err := filepath.EvalSymlinks(full)
	if err != nil {
		return "", err
	}
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(resolved, resolvedDir+string(filepath.Separator)) {
		return "", fmt.Errorf("report path escaped reports dir")
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ---------------------------------------------------------------------------
// Internals
// ---------------------------------------------------------------------------

// readReportMeta reads the first ~1 KB of a report file, parses the
// header block, and returns metadata. Keeps list responses fast by
// not loading the full body.
func readReportMeta(dir, filename string) (WrapUpReportMeta, error) {
	full := filepath.Join(dir, filename)
	info, err := os.Stat(full)
	if err != nil {
		return WrapUpReportMeta{}, err
	}
	f, err := os.Open(full)
	if err != nil {
		return WrapUpReportMeta{}, err
	}
	defer f.Close()
	head := make([]byte, 2048)
	n, _ := f.Read(head)
	head = head[:n]

	meta := WrapUpReportMeta{
		Filename:  filename,
		SizeBytes: int(info.Size()),
	}
	// Parse the top-level header lines we write in buildReportBody:
	// "**Trigger:** ...", "**Reason:** ...", plus the first H1 line
	// that carries the timestamp.
	for _, line := range strings.Split(string(head), "\n") {
		switch {
		case strings.HasPrefix(line, "**Trigger:** "):
			meta.Trigger = strings.TrimPrefix(line, "**Trigger:** ")
		case strings.HasPrefix(line, "**Reason:** "):
			meta.Reason = strings.TrimPrefix(line, "**Reason:** ")
		case strings.HasPrefix(line, "**Mission:** "):
			meta.TeamMission = strings.TrimPrefix(line, "**Mission:** ")
		}
	}
	// Generated-at comes from the filename (canonical) rather than a
	// best-effort header parse.
	if t, err := parseFilenameTimestamp(filename); err == nil {
		meta.GeneratedAt = t
	} else {
		meta.GeneratedAt = info.ModTime().UTC()
	}
	return meta, nil
}

func parseFilenameTimestamp(filename string) (time.Time, error) {
	// wrap-up-YYYYMMDD-HHMMSS.md
	const prefix = "wrap-up-"
	const suffix = ".md"
	if !strings.HasPrefix(filename, prefix) || !strings.HasSuffix(filename, suffix) {
		return time.Time{}, fmt.Errorf("bad name")
	}
	core := strings.TrimSuffix(strings.TrimPrefix(filename, prefix), suffix)
	return time.ParseInLocation("20060102-150405", core, time.UTC)
}

func pruneOldReports(dir string, keep int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var valid []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if ValidateReportFilename(e.Name()) {
			valid = append(valid, e.Name())
		}
	}
	if len(valid) <= keep {
		return nil
	}
	// Names sort lexicographically = chronologically thanks to
	// UTC-padded timestamps. Keep the newest `keep`.
	sort.Strings(valid)
	drop := valid[:len(valid)-keep]
	for _, name := range drop {
		_ = os.Remove(filepath.Join(dir, name))
	}
	return nil
}

func truncateForSnippet(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

// ---------------------------------------------------------------------------
// Markdown rendering
// ---------------------------------------------------------------------------

// buildReportBody is deliberately one long function — every section
// reads a distinct data source, and splitting into N helpers spreads
// the report structure across the file. Easier to read + evolve the
// layout as a single template.
func buildReportBody(m *Manager, generatedAt time.Time, trigger, reason string) string {
	var b strings.Builder

	// Header
	fmt.Fprintf(&b, "# Team Wrap-Up Report — %s\n\n",
		generatedAt.Format("2006-01-02 15:04:05 UTC"))
	fmt.Fprintf(&b, "**Trigger:** %s\n", trigger)
	if reason != "" {
		fmt.Fprintf(&b, "**Reason:** %s\n", reason)
	}

	mission := m.GetTeamMission()
	missionSnippet := truncateForSnippet(mission, 200)
	if missionSnippet != "" {
		fmt.Fprintf(&b, "**Mission:** %s\n", missionSnippet)
	}

	// Working dir + template (the Manager tracks the last template
	// and working dir for the spawned team).
	lastTemplate, lastWorkdir := m.GetSpawnContext()
	if lastWorkdir != "" {
		fmt.Fprintf(&b, "**Working dir:** `%s`\n", lastWorkdir)
	}
	if lastTemplate != "" {
		fmt.Fprintf(&b, "**Template:** `%s`\n", lastTemplate)
	}

	// Uptime
	if spawnedAt := m.GetTeamSpawnedAt(); !spawnedAt.IsZero() {
		up := generatedAt.Sub(spawnedAt)
		fmt.Fprintf(&b, "**Team uptime:** %s\n", formatDuration(up))
	}
	b.WriteString("\n")

	// Full mission (if over snippet length)
	if mission != "" && mission != missionSnippet {
		b.WriteString("## Mission\n")
		b.WriteString(mission)
		b.WriteString("\n\n")
	}

	roster := m.GetRoster()
	metrics := m.GetMetricsSnapshot()
	handoffHistory := m.GetHandoffHistory()

	// Executive summary
	b.WriteString("## Executive Summary\n")
	completedTotal := 0
	for _, a := range roster {
		for _, t := range a.GetTasks() {
			if t.Status == TaskCompleted {
				completedTotal++
			}
		}
	}
	fmt.Fprintf(&b, "- %d tasks completed across %d agents\n",
		completedTotal, len(roster))
	fmt.Fprintf(&b, "- Total cost: $%.4f (%s input / %s output tokens)\n",
		metrics.TotalCost,
		formatTokenCount(metrics.TotalInputTokens),
		formatTokenCount(metrics.TotalOutputTokens))

	accepted, rejected, cancelled, expired := 0, 0, 0, 0
	for _, h := range handoffHistory {
		switch h.Status {
		case HandoffAccepted:
			accepted++
		case HandoffRejected:
			rejected++
		case HandoffCancelled:
			cancelled++
		case HandoffExpired:
			expired++
		}
	}
	fmt.Fprintf(&b, "- %d handoffs (%d accepted, %d rejected, %d cancelled, %d expired)\n",
		len(handoffHistory), accepted, rejected, cancelled, expired)
	fmt.Fprintf(&b, "- %d stuck alerts, %d recovery attempts\n",
		metrics.TotalStuckAlerts, metrics.TotalRecoveryAttempts)
	b.WriteString("\n")

	// Per-agent summary
	b.WriteString("## Per-Agent Summary\n\n")
	if len(roster) == 0 {
		b.WriteString("_No agents in roster._\n\n")
	} else {
		// Deterministic ordering by name so reports diff nicely.
		agents := make([]*AgentState, len(roster))
		copy(agents, roster)
		sort.Slice(agents, func(i, j int) bool {
			ni, nj := "", ""
			if agents[i].Config != nil {
				ni = agents[i].Config.Name
			}
			if agents[j].Config != nil {
				nj = agents[j].Config.Name
			}
			return ni < nj
		})
		for _, a := range agents {
			writeAgentSection(&b, a, lastWorkdir)
		}
	}

	// Handoff log
	if len(handoffHistory) > 0 {
		b.WriteString("## Handoff Log\n\n")
		// Newest first.
		sorted := make([]HandoffRequest, len(handoffHistory))
		copy(sorted, handoffHistory)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].CreatedAt.After(sorted[j].CreatedAt)
		})
		for _, h := range sorted {
			writeHandoffLine(&b, &h)
		}
		b.WriteString("\n")
	}

	// Whiteboard final state (legacy per-agent whiteboard).
	whiteboard := m.GetWhiteboard()
	if len(whiteboard) > 0 {
		b.WriteString("## Whiteboard Final State\n\n")
		// Build a deterministic name-sorted list.
		type wbEntry struct {
			name    string
			content string
		}
		var entries []wbEntry
		for sid, note := range whiteboard {
			if note == nil || strings.TrimSpace(note.Content) == "" {
				continue
			}
			name := sid
			if a := m.GetAgent(sid); a != nil && a.Config != nil {
				name = a.Config.Name
			}
			entries = append(entries, wbEntry{name: name, content: note.Content})
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
		for _, e := range entries {
			fmt.Fprintf(&b, "**%s:**\n\n%s\n\n",
				e.name, indentBlock(e.content, "> "))
		}
	}

	// Knowledge index -- point operators at the canonical per-agent
	// memory files on disk.
	b.WriteString("## Knowledge Index\n\n")
	b.WriteString("Per-agent memory files:\n\n")
	for _, a := range roster {
		path := GetAgentMemoryPath(a.ConfigID, lastWorkdir)
		sizeHint := ""
		if info, err := os.Stat(path); err == nil {
			sizeHint = fmt.Sprintf(" (%d bytes)", info.Size())
		} else {
			sizeHint = " (not found)"
		}
		agentName := a.ConfigID
		if a.Config != nil {
			agentName = a.Config.Name
		}
		fmt.Fprintf(&b, "- **%s:** `%s`%s\n", agentName, path, sizeHint)
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "---\nGenerated by Jarvis on %s.\n", generatedAt.Format(time.RFC3339))
	return b.String()
}

func writeAgentSection(b *strings.Builder, a *AgentState, workdir string) {
	name := a.ConfigID
	role := ""
	tier := ""
	if a.Config != nil {
		name = a.Config.Name
		role = a.Config.Role
		tier = a.Config.Tier
	}

	// Completed + in-flight task summary.
	tasks := a.GetTasks()
	completed := make([]*Task, 0)
	inflight := make([]*Task, 0)
	for _, t := range tasks {
		switch t.Status {
		case TaskCompleted:
			completed = append(completed, t)
		case TaskPending, TaskAcknowledged, TaskInProgress, TaskBlocked, TaskHandoffPending:
			inflight = append(inflight, t)
		}
	}

	fmt.Fprintf(b, "### %s", name)
	if role != "" {
		fmt.Fprintf(b, " (%s", role)
		if tier != "" {
			fmt.Fprintf(b, ", %s tier", tier)
		}
		b.WriteString(")")
	}
	fmt.Fprintf(b, " — %d task%s completed\n\n", len(completed), pluralS(len(completed)))

	if a.TotalCostUSD > 0 || a.TotalInputTokens > 0 || a.TotalOutputTokens > 0 {
		fmt.Fprintf(b, "**Usage:** $%.4f · %s input / %s output tokens\n\n",
			a.TotalCostUSD,
			formatTokenCount(a.TotalInputTokens),
			formatTokenCount(a.TotalOutputTokens))
	}

	if len(completed) > 0 {
		b.WriteString("**Task history:**\n\n")
		// Sort completed newest-first.
		sorted := make([]*Task, len(completed))
		copy(sorted, completed)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].CompletedAt.After(sorted[j].CompletedAt)
		})
		for _, t := range sorted {
			ts := ""
			if !t.CompletedAt.IsZero() {
				ts = " · " + t.CompletedAt.Format("15:04")
			}
			fmt.Fprintf(b, "- ✅ %s%s\n", t.Title, ts)
		}
		b.WriteString("\n")
	}
	if len(inflight) > 0 {
		b.WriteString("**Still in flight at wrap-up:**\n\n")
		for _, t := range inflight {
			fmt.Fprintf(b, "- [%s] %s\n", t.Status, t.Title)
		}
		b.WriteString("\n")
	}

	// Memory sections -- current_focus and decisions carry the most
	// signal; everything else is in the knowledge index link below.
	if focus, err := MemoryReadSection(a.ConfigID, workdir, "current_focus"); err == nil && strings.TrimSpace(focus) != "" {
		b.WriteString("**Current focus:**\n\n")
		b.WriteString(indentBlock(focus, "> "))
		b.WriteString("\n\n")
	}
	if decisions, err := MemoryReadSection(a.ConfigID, workdir, "decisions"); err == nil && strings.TrimSpace(decisions) != "" {
		b.WriteString("**Decisions:**\n\n")
		b.WriteString(indentBlock(decisions, "> "))
		b.WriteString("\n\n")
	}

	// Distilled learnings.
	if data := LoadLearningData(a.ConfigID); data != nil && len(data.DistilledInsights) > 0 {
		b.WriteString("**Distilled learnings:**\n\n")
		for _, ins := range data.DistilledInsights {
			fmt.Fprintf(b, "- %s\n", ins)
		}
		b.WriteString("\n")
	}
}

func writeHandoffLine(b *strings.Builder, h *HandoffRequest) {
	ts := h.CreatedAt.Format("15:04")
	fmt.Fprintf(b, "- %s %s → %s: %q — %s",
		ts, handoffName(h.FromName, h.FromConfigID),
		handoffName(h.ToName, h.ToConfigID),
		h.TaskTitle, h.Status)
	if h.RejectReason != "" && (h.Status == HandoffRejected || h.Status == HandoffCancelled || h.Status == HandoffExpired) {
		fmt.Fprintf(b, " (%s)", h.RejectReason)
	}
	b.WriteString("\n")
}

func handoffName(name, fallback string) string {
	if name != "" {
		return name
	}
	return fallback
}

// indentBlock prefixes every line of s with the given prefix. Used
// for blockquotes of memory sections.
func indentBlock(s, prefix string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, ln := range lines {
		lines[i] = prefix + ln
	}
	return strings.Join(lines, "\n")
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func formatTokenCount(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

