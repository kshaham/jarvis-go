// Package scheduler provides a durable cron-style job runner for jarvis.
//
// Jobs are defined as `Job` structs (name, cron expression, action type,
// action-specific config). They persist to a JSON file with the same
// atomic-rename + backup-rotation pattern used by state.store, so a
// server restart resumes on-schedule work.
//
// Actions (spawn_team, one_shot, webhook) are dispatched via a pluggable
// ExecutorFunc injected from the handler layer so this package stays free
// of web-specific imports and is easy to unit-test.
package scheduler

import (
	"context"
	"encoding/json"
	"time"
)

// ActionType names the built-in job kinds.
const (
	ActionSpawnTeam = "spawn_team"
	ActionOneShot   = "one_shot"
	ActionWebhook   = "webhook"
)

// Job represents a scheduled task. The Config field carries action-specific
// parameters and is stored as raw JSON so each executor can decode its own
// shape without the scheduler package knowing about team templates, session
// configs, etc.
type Job struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Cron       string          `json:"cron"`
	Action     string          `json:"action"`
	Config     json.RawMessage `json:"config,omitempty"`
	Enabled    bool            `json:"enabled"`
	CreatedAt  time.Time       `json:"createdAt"`
	UpdatedAt  time.Time       `json:"updatedAt"`
	LastRunAt  *time.Time      `json:"lastRunAt,omitempty"`
	NextRunAt  *time.Time      `json:"nextRunAt,omitempty"`
	LastStatus string          `json:"lastStatus,omitempty"`
	LastError  string          `json:"lastError,omitempty"`
}

// RunRecord is a single historical execution entry. Kept in memory per job
// (bounded) and surfaced via the /history endpoint.
type RunRecord struct {
	StartedAt time.Time `json:"startedAt"`
	EndedAt   time.Time `json:"endedAt"`
	Status    string    `json:"status"` // "ok" | "error"
	Error     string    `json:"error,omitempty"`
	Output    string    `json:"output,omitempty"`
}

// ExecutorFunc dispatches a job's action. Called from a goroutine so it may
// block; implementations should respect ctx for cancellation. Returns an
// optional short output string (persisted to LastStatus / history) and an
// error if execution failed.
type ExecutorFunc func(ctx context.Context, job *Job) (string, error)
