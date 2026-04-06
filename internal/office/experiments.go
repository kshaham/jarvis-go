package office

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"time"
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
	_, err = f.Write(append(data, '\n'))
	return err
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
