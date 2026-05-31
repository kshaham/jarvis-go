package office

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const maxLearnings = 50
const maxSkills = 25
const maxDistilledInsights = 10
const distillTriggerCount = 30
const distillBatchSize = 15

// AgentLearningData is the full learning store for one agent.
type AgentLearningData struct {
	ConfigID          string            `json:"configId"`
	Name              string            `json:"name"`
	Learnings         []AgentLearning   `json:"learnings"`
	Skills            []AgentSkillEntry `json:"skills"`
	DistilledInsights []string          `json:"distilledInsights"`
	Metrics           LearningMetrics   `json:"metrics"`
}

// AgentLearning is a single recorded learning entry.
type AgentLearning struct {
	Category  string    `json:"category"`
	Content   string    `json:"content"`
	TaskID    string    `json:"taskId,omitempty"`
	TaskTitle string    `json:"taskTitle,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// AgentSkillEntry tracks a reusable technique discovered by an agent.
type AgentSkillEntry struct {
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	UsageCount   int       `json:"usageCount"`
	DiscoveredAt time.Time `json:"discoveredAt"`
	LastUsedAt   time.Time `json:"lastUsedAt"`
}

// LearningMetrics tracks aggregate learning statistics.
type LearningMetrics struct {
	TotalLearnings     int   `json:"totalLearnings"`
	TotalSkills        int   `json:"totalSkills"`
	TotalCuriosity     int   `json:"totalCuriosity"`
	LastReflectionAt   int64 `json:"lastReflectionAt"`
	LastCuriosityAt    int64 `json:"lastCuriosityAt"`
	LastDistillationAt int64 `json:"lastDistillationAt"`
}

var learningMu sync.Mutex

// learningsDir returns the directory for learning files.
func learningsDir() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".jarvis", "learnings")
}

// learningPath returns the file path for an agent's learning store.
func learningPath(configID string) string {
	return filepath.Join(learningsDir(), configID+".json")
}

// LearningFileEntry describes one learning file on disk. Surfaced
// by ListLearningFiles for the dashboard's "agents I've taught
// before" picker.
type LearningFileEntry struct {
	ConfigID string `json:"configId"`
	File     string `json:"file"`
	// Modified is RFC3339-formatted; empty when stat() failed.
	Modified string `json:"modified"`
}

// ListLearningFiles scans the learnings directory and returns one
// entry per *.json file. Returns an empty slice (not nil) when the
// directory doesn't exist — the dashboard can render an empty list
// without a guard.
//
// Pure file I/O. The handler previously inlined the same walk.
func ListLearningFiles() []LearningFileEntry {
	out := []LearningFileEntry{}
	entries, err := os.ReadDir(learningsDir())
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		entry := LearningFileEntry{
			ConfigID: strings.TrimSuffix(e.Name(), ".json"),
			File:     e.Name(),
		}
		if info, infoErr := e.Info(); infoErr == nil {
			entry.Modified = info.ModTime().Format(time.RFC3339)
		}
		out = append(out, entry)
	}
	return out
}

// ErrLearningsNotFound is returned by LoadLearningsForUI when the
// file doesn't exist. The handler maps this to an empty-array
// response rather than a 404 — the dashboard treats "no learnings
// yet" as the empty state.
var ErrLearningsNotFound = errors.New("office: learnings file not found")

// LoadLearningsForUI reads a config's learning file and returns the
// canonical learnings array (the contents of the `learnings` field
// in AgentLearningData) for the dashboard to render.
//
// Distinguished from LoadLearningData by the return shape: this
// helper returns just the entries (whatever shape they're in on
// disk), while LoadLearningData returns the full typed wrapper.
//
// Returns ErrLearningsNotFound when the file doesn't exist; the
// handler maps this to the empty-state response. Other I/O / parse
// errors are returned with context.
func LoadLearningsForUI(configID string) (interface{}, error) {
	data, err := os.ReadFile(learningPath(configID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrLearningsNotFound
		}
		return nil, fmt.Errorf("read learnings file: %w", err)
	}
	var parsed struct {
		ConfigID  string      `json:"configId"`
		Name      string      `json:"name"`
		Learnings interface{} `json:"learnings"`
	}
	if jsonErr := json.Unmarshal(data, &parsed); jsonErr != nil {
		return nil, fmt.Errorf("parse learnings file: %w", jsonErr)
	}
	if parsed.Learnings == nil {
		return []interface{}{}, nil
	}
	return parsed.Learnings, nil
}

// LoadLearningData reads the full AgentLearningData for a given agent.
// Returns a zero-value struct with sensible defaults if the file is missing or unparseable.
func LoadLearningData(configID string) *AgentLearningData {
	learningMu.Lock()
	defer learningMu.Unlock()
	return loadLearningDataUnsafe(configID)
}

// loadLearningDataUnsafe reads without locking (caller must hold learningMu).
//
// R27 · On parse failure the old implementation silently returned a
// zero-value struct, which the very next AddLearningEntry call would
// then overwrite with empty data — destroying whatever was in the
// corrupt file. Now the file is quarantined (renamed with a .corrupt-
// <unix> suffix) so subsequent loads see it as "missing" and the
// fresh-start zero value is safe, while the original stays on disk
// for forensic inspection.
func loadLearningDataUnsafe(configID string) *AgentLearningData {
	path := learningPath(configID)
	data, err := os.ReadFile(path)
	if err != nil {
		return defaultLearningData(configID)
	}

	var ld AgentLearningData
	if err := json.Unmarshal(data, &ld); err != nil {
		ts := time.Now().Unix()
		corruptPath := fmt.Sprintf("%s.corrupt-%d", path, ts)
		if renameErr := os.Rename(path, corruptPath); renameErr != nil {
			slog.Warn("failed to quarantine corrupt learning file",
				"path", path, "renameErr", renameErr, "parseErr", err)
		} else {
			slog.Error("quarantined corrupt learning file",
				"path", path, "corruptPath", corruptPath, "parseErr", err)
		}
		return defaultLearningData(configID)
	}

	// Ensure slice fields are initialised.
	if ld.Learnings == nil {
		ld.Learnings = make([]AgentLearning, 0)
	}
	if ld.Skills == nil {
		ld.Skills = make([]AgentSkillEntry, 0)
	}
	if ld.DistilledInsights == nil {
		ld.DistilledInsights = make([]string, 0)
	}
	return &ld
}

// defaultLearningData returns a fresh AgentLearningData for a given configID.
func defaultLearningData(configID string) *AgentLearningData {
	return &AgentLearningData{
		ConfigID:          configID,
		Learnings:         make([]AgentLearning, 0),
		Skills:            make([]AgentSkillEntry, 0),
		DistilledInsights: make([]string, 0),
	}
}

// SaveLearningData atomically writes an AgentLearningData to disk.
func SaveLearningData(configID string, ld *AgentLearningData) error {
	learningMu.Lock()
	defer learningMu.Unlock()
	return writeLearningData(ld)
}

// writeLearningData atomically writes to disk (caller may or may not hold learningMu).
func writeLearningData(ld *AgentLearningData) error {
	dir := learningsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(ld, "", "  ")
	if err != nil {
		return err
	}

	tmpFile := learningPath(ld.ConfigID) + ".tmp"
	targetFile := learningPath(ld.ConfigID)
	if err := os.WriteFile(tmpFile, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmpFile, targetFile); err != nil {
		// R27 state review · Clean up the orphan .tmp so repeated
		// rename failures don't accumulate files on disk. Matches
		// the R26 phase 4 cleanup pattern.
		_ = os.Remove(tmpFile)
		return err
	}

	slog.Debug("Saved agent learning data", "configId", ld.ConfigID,
		"learnings", len(ld.Learnings), "skills", len(ld.Skills))
	return nil
}

// AddLearningEntry appends a new learning entry for an agent.
// Triggers automatic distillation when the learning count reaches distillTriggerCount.
func AddLearningEntry(configID, category, content, taskID, taskTitle string) error {
	learningMu.Lock()
	defer learningMu.Unlock()

	ld := loadLearningDataUnsafe(configID)

	entry := AgentLearning{
		Category:  category,
		Content:   content,
		TaskID:    taskID,
		TaskTitle: taskTitle,
		CreatedAt: time.Now(),
	}
	ld.Learnings = append(ld.Learnings, entry)

	// Cap at maxLearnings.
	if len(ld.Learnings) > maxLearnings {
		ld.Learnings = ld.Learnings[len(ld.Learnings)-maxLearnings:]
	}

	// Update metrics.
	ld.Metrics.TotalLearnings++
	ld.Metrics.LastReflectionAt = time.Now().UnixMilli()

	// Auto-distill when we hit the trigger threshold.
	if len(ld.Learnings) >= distillTriggerCount {
		distillLearningsInPlace(ld)
	}

	return writeLearningData(ld)
}

// AddSkillEntry records a discovered skill for an agent, deduplicating by name.
func AddSkillEntry(configID, name, description string) error {
	learningMu.Lock()
	defer learningMu.Unlock()

	ld := loadLearningDataUnsafe(configID)

	// Deduplicate by name (case-insensitive).
	nameLower := strings.ToLower(name)
	for i, s := range ld.Skills {
		if strings.ToLower(s.Name) == nameLower {
			// Update description and usage count if already present.
			ld.Skills[i].Description = description
			ld.Skills[i].UsageCount++
			ld.Skills[i].LastUsedAt = time.Now()
			ld.Metrics.TotalSkills = len(ld.Skills)
			return writeLearningData(ld)
		}
	}

	entry := AgentSkillEntry{
		Name:         name,
		Description:  description,
		UsageCount:   1,
		DiscoveredAt: time.Now(),
		LastUsedAt:   time.Now(),
	}
	ld.Skills = append(ld.Skills, entry)

	// Cap at maxSkills.
	if len(ld.Skills) > maxSkills {
		ld.Skills = ld.Skills[len(ld.Skills)-maxSkills:]
	}

	ld.Metrics.TotalSkills = len(ld.Skills)

	return writeLearningData(ld)
}

// DistillLearnings groups the oldest learnings by category into distilled insights.
// Called automatically by AddLearningEntry, but can also be called explicitly.
func DistillLearnings(ld *AgentLearningData) {
	learningMu.Lock()
	defer learningMu.Unlock()
	distillLearningsInPlace(ld)
}

// distillLearningsInPlace performs distillation without locking (caller must hold lock).
func distillLearningsInPlace(ld *AgentLearningData) {
	if len(ld.Learnings) < distillBatchSize {
		return
	}

	// Take the oldest batch.
	batch := ld.Learnings[:distillBatchSize]
	ld.Learnings = ld.Learnings[distillBatchSize:]

	// Group by category.
	byCategory := make(map[string][]string)
	for _, l := range batch {
		cat := l.Category
		if cat == "" {
			cat = "general"
		}
		byCategory[cat] = append(byCategory[cat], l.Content)
	}

	// Build insight strings.
	for cat, items := range byCategory {
		var sb strings.Builder
		fmt.Fprintf(&sb, "Key %s learnings: ", cat)
		for i, item := range items {
			if i > 0 {
				sb.WriteString("; ")
			}
			// Truncate long items.
			if len(item) > 80 {
				item = item[:80] + "..."
			}
			sb.WriteString(item)
		}
		insight := sb.String()
		ld.DistilledInsights = append(ld.DistilledInsights, insight)
	}

	// Cap distilled insights at maxDistilledInsights.
	if len(ld.DistilledInsights) > maxDistilledInsights {
		ld.DistilledInsights = ld.DistilledInsights[len(ld.DistilledInsights)-maxDistilledInsights:]
	}

	ld.Metrics.LastDistillationAt = time.Now().UnixMilli()
}

// BuildLearningsPromptSection returns a formatted string suitable for injection
// into the kickstart prompt. Returns "" if there is nothing to show.
func BuildLearningsPromptSection(configID string) string {
	learningMu.Lock()
	ld := loadLearningDataUnsafe(configID)
	learningMu.Unlock()

	if len(ld.Learnings) == 0 && len(ld.Skills) == 0 && len(ld.DistilledInsights) == 0 {
		return ""
	}

	var sb strings.Builder

	if len(ld.Learnings) > 0 {
		sb.WriteString("Recent learnings:\n")
		// Show up to 10 most recent.
		start := 0
		if len(ld.Learnings) > 10 {
			start = len(ld.Learnings) - 10
		}
		for _, l := range ld.Learnings[start:] {
			cat := l.Category
			if cat == "" {
				cat = "general"
			}
			fmt.Fprintf(&sb, "- [%s] %s\n", cat, l.Content)
		}
	}

	if len(ld.Skills) > 0 {
		sb.WriteString("Top skills:\n")
		// Show up to 5 skills, sorted by usage count (simple selection).
		top := topSkills(ld.Skills, 5)
		for _, s := range top {
			fmt.Fprintf(&sb, "- \"%s\" (used %d times)\n", s.Name, s.UsageCount)
		}
	}

	if len(ld.DistilledInsights) > 0 {
		sb.WriteString("Distilled insights:\n")
		for _, insight := range ld.DistilledInsights {
			fmt.Fprintf(&sb, "- %s\n", insight)
		}
	}

	return sb.String()
}

// topSkills returns up to n skills sorted by UsageCount descending.
func topSkills(skills []AgentSkillEntry, n int) []AgentSkillEntry {
	// Copy slice to avoid mutating the original.
	sorted := make([]AgentSkillEntry, len(skills))
	copy(sorted, skills)

	// Simple selection sort (small n, small slice).
	for i := 0; i < len(sorted) && i < n; i++ {
		maxIdx := i
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].UsageCount > sorted[maxIdx].UsageCount {
				maxIdx = j
			}
		}
		sorted[i], sorted[maxIdx] = sorted[maxIdx], sorted[i]
	}

	if n > len(sorted) {
		n = len(sorted)
	}
	return sorted[:n]
}

// BuildCrossAgentContext loads all agents' recent learnings except the given
// configID and returns a formatted "What your teammates learned" section.
// Returns "" if there is nothing to show.
func BuildCrossAgentContext(excludeConfigID string) string {
	learningMu.Lock()
	defer learningMu.Unlock()

	dir := learningsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	var sb strings.Builder
	hasContent := false

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		configID := strings.TrimSuffix(e.Name(), ".json")
		if configID == excludeConfigID {
			continue
		}

		ld := loadLearningDataUnsafe(configID)
		if len(ld.Learnings) == 0 {
			continue
		}

		// Take up to 3 most recent learnings.
		start := 0
		if len(ld.Learnings) > 3 {
			start = len(ld.Learnings) - 3
		}
		recent := ld.Learnings[start:]

		name := ld.Name
		if name == "" {
			name = configID
		}

		if !hasContent {
			sb.WriteString("What your teammates learned recently:\n")
			hasContent = true
		}

		fmt.Fprintf(&sb, "- %s:\n", name)
		for _, l := range recent {
			cat := l.Category
			if cat == "" {
				cat = "general"
			}
			fmt.Fprintf(&sb, "  - [%s] %s\n", cat, l.Content)
		}
	}

	return sb.String()
}

// BuildCrossAgentContextCompact returns a compact one-line-per-peer
// summary of teammates' most recent learning. Used by kickstart.go to
// keep cross-agent context cheap. Returns "" if there's nothing to show.
func BuildCrossAgentContextCompact(excludeConfigID string) string {
	learningMu.Lock()
	defer learningMu.Unlock()

	dir := learningsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	var sb strings.Builder
	hasContent := false

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		configID := strings.TrimSuffix(e.Name(), ".json")
		if configID == excludeConfigID {
			continue
		}
		ld := loadLearningDataUnsafe(configID)
		if len(ld.Learnings) == 0 {
			continue
		}
		// Take the single most recent learning.
		recent := ld.Learnings[len(ld.Learnings)-1]
		name := ld.Name
		if name == "" {
			name = configID
		}
		content := recent.Content
		if len(content) > 100 {
			content = content[:100] + "..."
		}
		if !hasContent {
			sb.WriteString("Recent teammate findings (one per peer):\n")
			hasContent = true
		}
		fmt.Fprintf(&sb, "- %s: %s\n", name, content)
	}

	return sb.String()
}

