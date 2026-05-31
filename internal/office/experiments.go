package office

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/kshaham/jarvis/internal/library"
	"github.com/kshaham/jarvis/internal/safego"
)

// experimentsOpenFile indirects the append-open syscall so tests can inject
// a writer whose Write / Close fail, exercising the disk-full / EIO branches
// that can't otherwise be reached through the filesystem. Production opens
// the real JSONL file in append mode.
var experimentsOpenFile = func(path string) (io.WriteCloser, error) {
	return os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
}

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

	f, err := experimentsOpenFile(path)
	if err != nil {
		return err
	}
	// On the write path we need the close error: a buffered append that
	// only surfaces a disk-full / EIO at flush time would otherwise be
	// silently dropped by `defer f.Close()`.
	data, err := json.Marshal(record)
	if err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close experiment log: %w", err)
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

// experimentHistoryCap is the maximum number of experiment records returned
// by LoadExperimentHistory per call. The UI shows recent experiments, so
// returning the last N from the JSONL tail is sufficient. Older records
// are omitted rather than loaded and discarded.
const experimentHistoryCap = 200

// LoadExperimentHistory reads the most recent experimentHistoryCap experiment
// records from the JSONL log. Returns an empty slice if the file is missing or
// unreadable.
//
// To avoid reading the entire file on every call, the scanner consumes all
// lines but only retains the last experimentHistoryCap in a ring buffer —
// this is O(N) in lines but O(cap) in allocations and avoids a seek/stat
// loop for a file that is append-only and typically small.
func LoadExperimentHistory() []ExperimentRecord {
	f, err := os.Open(experimentsPath())
	if err != nil {
		return []ExperimentRecord{}
	}
	defer f.Close()

	// Ring buffer: keep only the last experimentHistoryCap records to bound
	// memory and avoid returning thousands of stale entries to the UI.
	ring := make([]ExperimentRecord, experimentHistoryCap)
	head := 0
	count := 0

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
		ring[head%experimentHistoryCap] = rec
		head++
		count++
	}

	if count == 0 {
		return []ExperimentRecord{}
	}

	// Reconstruct in chronological order (oldest-surviving → newest).
	n := count
	if n > experimentHistoryCap {
		n = experimentHistoryCap
	}
	out := make([]ExperimentRecord, n)
	start := head - n
	for i := 0; i < n; i++ {
		out[i] = ring[(start+i)%experimentHistoryCap]
	}
	return out
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
