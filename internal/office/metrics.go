package office

import (
	"sync/atomic"
	"time"
)

// Metrics tracks office-level counters.
type Metrics struct {
	TotalStandups         atomic.Int64
	TotalStandupsComplete atomic.Int64
	TotalTasks            atomic.Int64
	TotalTasksCompleted   atomic.Int64
	TotalBugs             atomic.Int64
	TotalBugTasksCreated  atomic.Int64
	TotalBugsResolved     atomic.Int64
	TotalMessages         atomic.Int64
	TotalReflections      atomic.Int64
	TotalCuriosity        atomic.Int64
	TotalLearnings        atomic.Int64
	TotalRecoveryAttempts atomic.Int64
	TotalStuckAlerts      atomic.Int64
	TotalSkillsDiscovered atomic.Int64
	StartedAt             time.Time
}

// NewMetrics creates a new metrics tracker.
func NewMetrics() *Metrics {
	return &Metrics{StartedAt: time.Now()}
}

// Uptime returns how long the office has been running.
func (m *Metrics) Uptime() time.Duration {
	return time.Since(m.StartedAt)
}

// MetricsSnapshot returns a point-in-time copy of all metrics.
type MetricsSnapshot struct {
	TotalStandups              int64            `json:"totalStandups"`
	TotalStandupsCompleted     int64            `json:"totalStandupsCompleted"`
	TotalTasks                 int64            `json:"totalTasks"`
	TotalTasksCompleted        int64            `json:"totalTasksCompleted"`
	TotalBugs                  int64            `json:"totalBugs"`
	TotalBugTasksCreated       int64            `json:"totalBugTasksCreated"`
	TotalBugTasksResolved      int64            `json:"totalBugTasksResolved"`
	TotalMessages              int64            `json:"totalMessages"`
	TotalReflections           int64            `json:"totalReflections"`
	TotalCuriosity             int64            `json:"totalCuriosity"`
	TotalLearnings             int64            `json:"totalLearnings"`
	TotalRecoveryAttempts      int64            `json:"totalRecoveryAttempts"`
	TotalStuckAlerts           int64            `json:"totalStuckAlerts"`
	TotalSkillsDiscovered      int64            `json:"totalSkillsDiscovered"`
	AverageStandupResponseRate float64          `json:"averageStandupResponseRate"`
	TotalCost                  float64          `json:"totalCost"`
	TotalInputTokens           int64            `json:"totalInputTokens"`
	TotalOutputTokens          int64            `json:"totalOutputTokens"`
	AgentUptime                map[string]int64 `json:"agentUptime"`
	Uptime                     string           `json:"uptime"`
}

// Snapshot returns a point-in-time copy of all metrics.
func (m *Metrics) Snapshot() MetricsSnapshot {
	totalStandups := m.TotalStandups.Load()
	completed := m.TotalStandupsComplete.Load()
	var avgRate float64
	if totalStandups > 0 {
		avgRate = float64(completed) / float64(totalStandups)
	}

	return MetricsSnapshot{
		TotalStandups:              totalStandups,
		TotalStandupsCompleted:     completed,
		TotalTasks:                 m.TotalTasks.Load(),
		TotalTasksCompleted:        m.TotalTasksCompleted.Load(),
		TotalBugs:                  m.TotalBugs.Load(),
		TotalBugTasksCreated:       m.TotalBugTasksCreated.Load(),
		TotalBugTasksResolved:      m.TotalBugsResolved.Load(),
		TotalMessages:              m.TotalMessages.Load(),
		TotalReflections:           m.TotalReflections.Load(),
		TotalCuriosity:             m.TotalCuriosity.Load(),
		TotalLearnings:             m.TotalLearnings.Load(),
		TotalRecoveryAttempts:      m.TotalRecoveryAttempts.Load(),
		TotalStuckAlerts:           m.TotalStuckAlerts.Load(),
		TotalSkillsDiscovered:      m.TotalSkillsDiscovered.Load(),
		AverageStandupResponseRate: avgRate,
		TotalCost:                  0,
		TotalInputTokens:           0,
		TotalOutputTokens:          0,
		AgentUptime:                make(map[string]int64),
		Uptime:                     m.Uptime().String(),
	}
}
