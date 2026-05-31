package mockups

import (
	"encoding/json"
	"time"
)

// statusKey holds the per-project generation status in the shared settings.
const statusKey = "mockupsStatus"

// Run/result statuses.
const (
	StatusIdle    = "idle"
	StatusRunning = "running"
	StatusDone    = "done"
	StatusError   = "error"
)

// SSE event names (mirrored in the SPA's sse.ts).
const (
	EventProgress = "mockups:progress"
	EventReady    = "mockups:ready"
)

// Status is one project's mockup-generation progress.
type Status struct {
	Status    string `json:"status"` // idle|running|done|error
	Expected  int    `json:"expected"`
	Received  int    `json:"received"`
	StartedAt string `json:"startedAt,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
	Error     string `json:"error,omitempty"`
}

// SettingsStore is the minimal settings surface (same shape projects uses).
type SettingsStore interface {
	GetSettings() map[string]interface{}
	UpdateSettings(updates map[string]interface{})
}

// Broadcaster is the SSE surface (satisfied by *sse.Broker).
type Broadcaster interface {
	Broadcast(event string, data interface{})
}

// Manager owns per-project generation status. The actual image work is done
// by the team's agents (which upload via RecordUpload); the Manager tracks
// progress, persists it, and broadcasts SSE.
type Manager struct {
	store SettingsStore
	bus   Broadcaster
}

// NewManager wires the settings store + SSE bus.
func NewManager(store SettingsStore, bus Broadcaster) *Manager {
	return &Manager{store: store, bus: bus}
}

// Snapshot returns the per-project status map.
func (m *Manager) Snapshot() map[string]Status {
	out := map[string]Status{}
	raw, ok := m.store.GetSettings()[statusKey]
	if ok && raw != nil {
		if b, err := json.Marshal(raw); err == nil {
			_ = json.Unmarshal(b, &out)
		}
	}
	return out
}

func (m *Manager) setStatus(projectID string, s Status) {
	all := m.Snapshot()
	all[projectID] = s
	m.store.UpdateSettings(map[string]interface{}{statusKey: all})
}

// StartRun clears the project's prior mockups and marks the run running.
// The team's agents then generate + upload the 6 images via RecordUpload.
func (m *Manager) StartRun(projectID string) error {
	if err := ClearProject(projectID); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	s := Status{Status: StatusRunning, Expected: MockupCount, Received: 0, StartedAt: now, UpdatedAt: now}
	m.setStatus(projectID, s)
	m.bus.Broadcast(EventProgress, map[string]any{"projectId": projectID, "status": s})
	return nil
}

// RecordUpload saves one uploaded mockup and advances the run's progress.
func (m *Manager) RecordUpload(projectID string, index int, png []byte) (string, error) {
	filename, err := Save(projectID, index, png, time.Now().Unix())
	if err != nil {
		return "", err
	}
	list, _ := List(projectID) // recount from disk (last-write-wins per slot)
	s := m.Snapshot()[projectID]
	if s.Expected == 0 {
		s.Expected = MockupCount
	}
	s.Received = len(list)
	s.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if s.Received >= s.Expected {
		s.Status = StatusDone
	} else {
		s.Status = StatusRunning
	}
	m.setStatus(projectID, s)
	m.bus.Broadcast(EventReady, map[string]any{
		"projectId": projectID, "index": index, "filename": filename, "status": s,
	})
	return filename, nil
}

// Fail marks a run as errored (e.g. dispatch failed).
func (m *Manager) Fail(projectID, msg string) {
	s := m.Snapshot()[projectID]
	s.Status = StatusError
	s.Error = msg
	s.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	m.setStatus(projectID, s)
	m.bus.Broadcast(EventProgress, map[string]any{"projectId": projectID, "status": s})
}
