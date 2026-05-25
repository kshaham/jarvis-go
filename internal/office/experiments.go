package office

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/claudeman/jarvis-go/internal/library"
	"github.com/claudeman/jarvis-go/internal/safego"
)

// ExperimentRecord captures the outcome of one team session (spawn to shutdown).
type ExperimentRecord struct {
	Timestamp       time.Time `json:"timestamp"`
	TeamTemplate    string    `json:"teamTemplate"`
	Mission         string    `json:"mission"`
	TaskCount       int       `json:"taskCount"`
	CompletedCount  int       `json:"completedCount"`
	AvgScore        float64   `json:"avgScore"`
	Duration        string    `json:"duration"`
	TopPerformer    string    `json:"topPerformer"`
	BottomPerformer string    `json:"bottomPerformer"`
}

// experimentsPath returns the file path for the experiment log.
func experimentsPath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".jarvis", "office-memory", "experiments.jsonl")
}

// RecordExperiment appends a record to the experiment log (append-only JSONL).
// Also mirrors the record into the knowledge library as a markdown
// projection so PageIndex can index team-level outcomes alongside
// per-task experiences. The mirror runs in a safego goroutine so a
// library failure can't affect the canonical jsonl write.
func RecordExperiment(record ExperimentRecord) error {
	path := experimentsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}

	// Mirror to library. Wrapped so failure here can't surface as
	// an error to the canonical record's caller — the jsonl write
	// already succeeded.
	safego.Go("library.writeExperiment", func() {
		libRec := library.ExperimentRecord{
			ID:              experimentIDFor(record.Timestamp),
			TeamTemplate:    record.TeamTemplate,
			Mission:         record.Mission,
			TaskCount:       record.TaskCount,
			CompletedCount:  record.CompletedCount,
			AvgScore:        record.AvgScore,
			DurationMS:      durationMillis(record.Duration),
			TopPerformer:    record.TopPerformer,
			BottomPerformer: record.BottomPerformer,
			Timestamp:       record.Timestamp,
		}
		if werr := library.WriteExperiment(libRec); werr != nil {
			slog.Debug("library experiment write skipped", "err", werr)
		}
	})
	return nil
}

// experimentIDFor builds a stable slug from the record timestamp.
// Format: exp-YYYYMMDD-HHMMSS. Library writes overwrite when the
// same instant produces the same ID — fine because RecordExperiment
// is only called once per shutdown.
func experimentIDFor(t time.Time) string {
	return "exp-" + t.UTC().Format("20060102-150405")
}

// durationMillis converts the human-readable duration string the
// JSONL record carries back into milliseconds for the library
// projection. Tolerant of parse failures — a bad string just yields
// 0 instead of failing the whole mirror.
func durationMillis(s string) int64 {
	if s == "" {
		return 0
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0
	}
	return d.Milliseconds()
}


// LoadExperimentHistory reads all experiment records from the JSONL log.
// Returns an empty slice if the file is missing or unreadable.
func LoadExperimentHistory() []ExperimentRecord {
	f, err := os.Open(experimentsPath())
	if err != nil {
		return []ExperimentRecord{}
	}
	defer f.Close()

	var records []ExperimentRecord
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec ExperimentRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			slog.Warn("Failed to parse experiment record", "err", err)
			continue
		}
		records = append(records, rec)
	}
	return records
}

// BuildExperimentRecord constructs an ExperimentRecord from current manager state.
func (m *Manager) BuildExperimentRecord(spawnedAt time.Time) ExperimentRecord {
	scores := m.CalculateTeamScores()

	totalTasks := 0
	completedTasks := 0
	for _, a := range m.GetRoster() {
		for _, t := range a.GetTasks() {
			totalTasks++
			if t.Status == TaskCompleted {
				completedTasks++
			}
		}
	}

	avgScore := 0.0
	topPerformer := ""
	bottomPerformer := ""
	if len(scores) > 0 {
		sum := 0.0
		for _, s := range scores {
			sum += s.OverallScore
		}
		avgScore = sum / float64(len(scores))

		top := GetTopPerformers(scores, 1)
		if len(top) > 0 {
			topPerformer = top[0].Name
		}
		bottom := GetBottomPerformers(scores, 1)
		if len(bottom) > 0 {
			bottomPerformer = bottom[0].Name
		}
	}

	duration := ""
	if !spawnedAt.IsZero() {
		duration = time.Since(spawnedAt).Round(time.Second).String()
	}

	return ExperimentRecord{
		Timestamp:       time.Now(),
		Mission:         m.GetTeamMission(),
		TaskCount:       totalTasks,
		CompletedCount:  completedTasks,
		AvgScore:        avgScore,
		Duration:        duration,
		TopPerformer:    topPerformer,
		BottomPerformer: bottomPerformer,
	}
}
