package library

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Markdown rendering helpers.
//
// Each renderXxx function takes a typed record and produces a
// deterministic markdown document with YAML frontmatter. Determinism
// matters because PageIndex caches its tree index by content hash —
// non-deterministic output (e.g. unsorted map iteration) would
// invalidate cache entries on every write even when the underlying
// record didn't change.
//
// Rules:
//   - Frontmatter keys are emitted in fixed lexical order.
//   - Slice values are emitted in their natural order (caller
//     responsibility to pre-sort if a canonical order is needed).
//   - Times are rendered in RFC3339 UTC so two daemons in different
//     timezones produce identical output for the same instant.
//   - Empty optional fields are omitted from frontmatter to keep
//     the docs scannable.

// renderExperience produces the markdown for an ExperienceRecord.
// Frontmatter carries every structured field PageIndex can use to
// filter; the body's ## sections are what an agent reads when it
// drills in via mcp__pageindex__get_page_content.
func renderExperience(rec ExperienceRecord) string {
	var b strings.Builder

	// Frontmatter
	fm := orderedFrontmatter{}
	fm.add("agent_config_id", rec.AgentConfigID)
	fm.add("agent_name", rec.AgentName)
	fm.add("agent_role", rec.AgentRole)
	fm.add("task_id", rec.TaskID)
	fm.add("task_title", rec.TaskTitle)
	fm.addStringSlice("task_labels", rec.TaskLabels)
	fm.add("working_dir", rec.WorkingDir)
	fm.add("completion_status", rec.CompletionStatus)
	fm.addFloat("outcome_score", rec.OutcomeScore)
	fm.add("ultrareview_severity", rec.UltrareviewSeverity)
	fm.addInt("recovery_attempts", rec.RecoveryAttempts)
	fm.addTime("started_at", rec.StartedAt)
	fm.addTime("completed_at", rec.CompletedAt)
	fm.addInt64("duration_ms", rec.DurationMS)
	fm.addFloat("cost_usd", rec.CostUSD)
	b.WriteString(fm.render())

	// Body
	b.WriteString("# ")
	b.WriteString(orPlaceholder(rec.TaskTitle, "(untitled task)"))
	b.WriteString("\n\n")

	if rec.ApproachSummary != "" {
		b.WriteString("## Approach\n\n")
		b.WriteString(rec.ApproachSummary)
		b.WriteString("\n\n")
	}

	if len(rec.FilesModified) > 0 {
		b.WriteString("## Files modified\n\n")
		for _, f := range rec.FilesModified {
			b.WriteString("- `")
			b.WriteString(f)
			b.WriteString("`\n")
		}
		b.WriteString("\n")
	}

	if len(rec.Learnings) > 0 {
		b.WriteString("## Learnings\n\n")
		for _, l := range rec.Learnings {
			b.WriteString("- ")
			b.WriteString(l)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	return b.String()
}

// renderUltrareviewSection produces the section appended to an
// existing experience file when ultrareview runs.
func renderUltrareviewSection(rec UltrareviewAppendRecord) string {
	var b strings.Builder
	b.WriteString("## Ultrareview ")
	b.WriteString(rec.Timestamp.UTC().Format(time.RFC3339))
	b.WriteString("\n\n")
	if rec.Summary != "" {
		b.WriteString(rec.Summary)
		b.WriteString("\n\n")
	}
	if len(rec.Findings) == 0 {
		b.WriteString("_No findings._\n\n")
		return b.String()
	}
	// Sort findings by severity for stable rendering. Severity order:
	// blocker > warning > nit > info > other.
	sorted := make([]Finding, len(rec.Findings))
	copy(sorted, rec.Findings)
	sort.SliceStable(sorted, func(i, j int) bool {
		return severityRank(sorted[i].Severity) < severityRank(sorted[j].Severity)
	})
	for _, f := range sorted {
		b.WriteString("### [")
		b.WriteString(orPlaceholder(f.Severity, "info"))
		b.WriteString("] ")
		b.WriteString(orPlaceholder(f.Title, "(untitled finding)"))
		b.WriteString("\n\n")
		if f.File != "" {
			b.WriteString("- File: `")
			b.WriteString(f.File)
			if f.Line > 0 {
				b.WriteString(fmt.Sprintf(":%d", f.Line))
			}
			b.WriteString("`\n")
		}
		if f.Category != "" {
			b.WriteString("- Category: ")
			b.WriteString(f.Category)
			b.WriteString("\n")
		}
		if f.Description != "" {
			b.WriteString("\n")
			b.WriteString(f.Description)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// severityRank gives a sort key for finding severities.
func severityRank(s string) int {
	switch strings.ToLower(s) {
	case "blocker":
		return 0
	case "warning":
		return 1
	case "nit":
		return 2
	case "info":
		return 3
	default:
		return 4
	}
}

// renderWorkflow produces the markdown for a consolidated workflow.
func renderWorkflow(w Workflow) string {
	var b strings.Builder

	fm := orderedFrontmatter{}
	fm.add("workflow_slug", w.Slug)
	fm.add("title", w.Title)
	fm.addStringSlice("derived_from", w.DerivedFromTaskIDs)
	fm.addFloat("avg_outcome", w.AvgOutcome)
	fm.addInt("reuse_count_at_creation", w.ReuseCountAtCreation)
	fm.add("agent_role", w.AgentRole)
	fm.addStringSlice("task_labels", w.TaskLabels)
	fm.addStringSlice("working_dir_hints", w.WorkingDirHints)
	fm.addTime("created_at", w.CreatedAt)
	b.WriteString(fm.render())

	b.WriteString("# Workflow: ")
	b.WriteString(orPlaceholder(w.Title, w.Slug))
	b.WriteString("\n\n")

	b.WriteString("## When this applies\n\n")
	if w.AgentRole != "" {
		b.WriteString("- Agent role: ")
		b.WriteString(w.AgentRole)
		b.WriteString("\n")
	}
	if len(w.TaskLabels) > 0 {
		b.WriteString("- Task labels: ")
		b.WriteString(strings.Join(w.TaskLabels, ", "))
		b.WriteString("\n")
	}
	if len(w.WorkingDirHints) > 0 {
		b.WriteString("- Working dir hints: ")
		b.WriteString(strings.Join(w.WorkingDirHints, ", "))
		b.WriteString("\n")
	}
	b.WriteString("\n")

	if len(w.Steps) > 0 {
		b.WriteString("## Steps that worked\n\n")
		for i, s := range w.Steps {
			b.WriteString(fmt.Sprintf("%d. %s\n", i+1, s))
		}
		b.WriteString("\n")
	}

	if len(w.KnownPitfalls) > 0 {
		b.WriteString("## Known pitfalls\n\n")
		for _, p := range w.KnownPitfalls {
			b.WriteString("- ")
			b.WriteString(p)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	return b.String()
}

// renderStandup produces the markdown for a standup transcript.
func renderStandup(s StandupTranscript) string {
	var b strings.Builder

	fm := orderedFrontmatter{}
	fm.add("standup_id", s.ID)
	fm.add("topic", s.Topic)
	fm.addTime("started_at", s.StartedAt)
	fm.addTime("ended_at", s.EndedAt)
	fm.addInt("message_count", len(s.Messages))
	b.WriteString(fm.render())

	b.WriteString("# Standup ")
	b.WriteString(s.ID)
	b.WriteString("\n\n")
	if s.Topic != "" {
		b.WriteString("**Topic:** ")
		b.WriteString(s.Topic)
		b.WriteString("\n\n")
	}

	// Group messages by type for navigability via PageIndex.
	groups := map[string][]StandupMessage{}
	order := []string{}
	for _, m := range s.Messages {
		key := orPlaceholder(m.Type, "other")
		if _, seen := groups[key]; !seen {
			order = append(order, key)
		}
		groups[key] = append(groups[key], m)
	}
	// Stable ordering within the doc — sort by canonical type order
	// then by appearance.
	sort.SliceStable(order, func(i, j int) bool {
		return standupTypeRank(order[i]) < standupTypeRank(order[j])
	})
	for _, typ := range order {
		b.WriteString("## ")
		b.WriteString(strings.Title(strings.ReplaceAll(typ, "_", " ")))
		b.WriteString("\n\n")
		for _, m := range groups[typ] {
			b.WriteString("### ")
			b.WriteString(orPlaceholder(m.Speaker, "(anonymous)"))
			b.WriteString(" — ")
			b.WriteString(m.Timestamp.UTC().Format(time.RFC3339))
			b.WriteString("\n\n")
			b.WriteString(m.Content)
			b.WriteString("\n\n")
		}
	}

	return b.String()
}

func standupTypeRank(t string) int {
	switch t {
	case "opening":
		return 0
	case "status_report":
		return 1
	case "takeaway":
		return 2
	case "closing":
		return 3
	default:
		return 4
	}
}

// renderExperiment produces the markdown for a run-level experiment.
func renderExperiment(rec ExperimentRecord) string {
	var b strings.Builder

	fm := orderedFrontmatter{}
	fm.add("experiment_id", rec.ID)
	fm.add("team_template", rec.TeamTemplate)
	fm.add("mission", rec.Mission)
	fm.addInt("task_count", rec.TaskCount)
	fm.addInt("completed_count", rec.CompletedCount)
	fm.addFloat("avg_score", rec.AvgScore)
	fm.addInt64("duration_ms", rec.DurationMS)
	fm.add("top_performer", rec.TopPerformer)
	fm.add("bottom_performer", rec.BottomPerformer)
	fm.addTime("timestamp", rec.Timestamp)
	b.WriteString(fm.render())

	b.WriteString("# Experiment ")
	b.WriteString(rec.ID)
	b.WriteString("\n\n")
	b.WriteString("Team template: `")
	b.WriteString(orPlaceholder(rec.TeamTemplate, "(none)"))
	b.WriteString("`  \n")
	b.WriteString("Mission: ")
	b.WriteString(orPlaceholder(rec.Mission, "(none)"))
	b.WriteString("\n\n")
	b.WriteString(fmt.Sprintf("Completed %d/%d tasks at avg score %.2f over %d ms.\n",
		rec.CompletedCount, rec.TaskCount, rec.AvgScore, rec.DurationMS))
	if rec.TopPerformer != "" {
		b.WriteString("Top performer: " + rec.TopPerformer + "  \n")
	}
	if rec.BottomPerformer != "" {
		b.WriteString("Bottom performer: " + rec.BottomPerformer + "\n")
	}

	return b.String()
}

// orPlaceholder returns s if non-empty, else the placeholder.
// Keeps frontmatter rendering tolerant of partial records.
func orPlaceholder(s, placeholder string) string {
	if s == "" {
		return placeholder
	}
	return s
}

// orderedFrontmatter accumulates key/value pairs in insertion order,
// then renders them as YAML between `---` delimiters. Empty values
// are dropped at render time so optional fields stay out of the doc
// when unset.
type orderedFrontmatter struct {
	keys   []string
	values map[string]string
}

func (o *orderedFrontmatter) ensure() {
	if o.values == nil {
		o.values = map[string]string{}
	}
}

func (o *orderedFrontmatter) add(key, value string) {
	o.ensure()
	if value == "" {
		return
	}
	if _, seen := o.values[key]; !seen {
		o.keys = append(o.keys, key)
	}
	o.values[key] = yamlScalarString(value)
}

func (o *orderedFrontmatter) addFloat(key string, v float64) {
	o.add(key, fmt.Sprintf("%.4f", v))
}

func (o *orderedFrontmatter) addInt(key string, v int) {
	if v == 0 {
		return
	}
	o.add(key, fmt.Sprintf("%d", v))
}

func (o *orderedFrontmatter) addInt64(key string, v int64) {
	if v == 0 {
		return
	}
	o.add(key, fmt.Sprintf("%d", v))
}

func (o *orderedFrontmatter) addTime(key string, t time.Time) {
	if t.IsZero() {
		return
	}
	o.add(key, t.UTC().Format(time.RFC3339))
}

func (o *orderedFrontmatter) addStringSlice(key string, vs []string) {
	if len(vs) == 0 {
		return
	}
	// Render as YAML flow sequence on one line — keeps the
	// frontmatter compact + scannable. Each element is YAML-quoted
	// if needed; the surrounding brackets are NOT passed through
	// yamlScalarString (which would double-quote the whole thing).
	o.ensure()
	if _, seen := o.values[key]; !seen {
		o.keys = append(o.keys, key)
	}
	quoted := make([]string, 0, len(vs))
	for _, v := range vs {
		quoted = append(quoted, yamlScalarString(v))
	}
	o.values[key] = "[" + strings.Join(quoted, ", ") + "]"
}

func (o *orderedFrontmatter) render() string {
	if len(o.keys) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("---\n")
	for _, k := range o.keys {
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(o.values[k])
		b.WriteString("\n")
	}
	b.WriteString("---\n\n")
	return b.String()
}

// yamlScalarString returns a YAML-safe scalar. Quotes the value when
// it contains characters that would confuse the YAML parser (colon,
// brackets, quotes, leading dash, etc.). Otherwise returns the bare
// string. Keeps the common case (plain words / paths) unquoted for
// readability.
func yamlScalarString(s string) string {
	if s == "" {
		return `""`
	}
	// Always quote if any of these chars appear OR if the string
	// already looks like a YAML structural token.
	if strings.ContainsAny(s, ":#\n\r\"'[]{}|&*!%@`") ||
		strings.HasPrefix(s, "-") ||
		strings.HasPrefix(s, " ") ||
		strings.HasSuffix(s, " ") {
		// Use double quotes; escape backslash + double-quote.
		escaped := strings.ReplaceAll(s, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `"`, `\"`)
		return `"` + escaped + `"`
	}
	return s
}
