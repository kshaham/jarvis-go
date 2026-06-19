package mockups

import (
	"encoding/json"
	"strings"
	"time"
)

// statusKey holds per-(project,kind) generation status in the shared settings.
const statusKey = "mockupsStatus"

// appliedKey holds the per-project applied UI-design selection (projectID ->
// mockup slot index) in the shared settings. Applying a design is a UI concept
// only, so the map is keyed by projectID alone (no kind component).
const appliedKey = "mockupsAppliedDesign"

// MockupRunStaleTimeout is how long a running mockup run may go without
// any upload advancing it before the sweep re-drives or fails it. Sized
// above the observed worst case where a busy perpetual lead takes a few
// minutes to finish its current turn before servicing the injected
// mockup prompt (measured ~4.5m), so the sweep does not re-drive a run
// the lead is about to start.
const MockupRunStaleTimeout = 8 * time.Minute

// MockupRunMaxRedrives caps how many times the sweep re-injects the
// prompt before giving up and marking the run errored.
const MockupRunMaxRedrives = 2

// Run/result statuses.
const (
	StatusIdle    = "idle"
	StatusRunning = "running"
	StatusDone    = "done"
	StatusError   = "error"
)

// SSE event names (mirrored in the SPA's sse.ts). Payloads carry "kind".
const (
	EventProgress = "mockups:progress"
	EventReady    = "mockups:ready"
)

// Status is one project+kind's mockup-generation progress.
type Status struct {
	Status    string `json:"status"` // idle|running|done|error
	Expected  int    `json:"expected"`
	Received  int    `json:"received"`
	StartedAt string `json:"startedAt,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
	Error     string `json:"error,omitempty"`
	Attempts  int    `json:"attempts,omitempty"` // auto re-drives performed by the sweep
}

// RunRef is one running run keyed back to its (projectID, kind).
type RunRef struct {
	ProjectID string
	Kind      string
	Status    Status
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

// Manager owns per-project, per-kind generation status. The actual image work
// is done by the team's agents (which upload via RecordUpload); the Manager
// tracks progress, persists it, and broadcasts SSE.
type Manager struct {
	store SettingsStore
	bus   Broadcaster
}

// NewManager wires the settings store + SSE bus.
func NewManager(store SettingsStore, bus Broadcaster) *Manager {
	return &Manager{store: store, bus: bus}
}

func statusMapKey(projectID, kind string) string { return projectID + "|" + kind }

// snapshot returns the raw keyed (projectID|kind -> Status) map.
func (m *Manager) snapshot() map[string]Status {
	out := map[string]Status{}
	raw, ok := m.store.GetSettings()[statusKey]
	if ok && raw != nil {
		if b, err := json.Marshal(raw); err == nil {
			_ = json.Unmarshal(b, &out)
		}
	}
	return out
}

// Status returns one project+kind's status (idle if none recorded).
func (m *Manager) Status(projectID, kind string) Status {
	s := m.snapshot()[statusMapKey(projectID, kind)]
	if s.Status == "" {
		s.Status = StatusIdle
	}
	return s
}

func (m *Manager) setStatus(projectID, kind string, s Status) {
	all := m.snapshot()
	all[statusMapKey(projectID, kind)] = s
	m.store.UpdateSettings(map[string]interface{}{statusKey: all})
}

// appliedSnapshot returns the raw keyed (projectID -> slot index) map.
func (m *Manager) appliedSnapshot() map[string]int {
	out := map[string]int{}
	raw, ok := m.store.GetSettings()[appliedKey]
	if ok && raw != nil {
		if b, err := json.Marshal(raw); err == nil {
			_ = json.Unmarshal(b, &out)
		}
	}
	return out
}

// SetAppliedDesign records that the operator selected mockup slot index as the
// target UI design direction for a project, persisting it to shared settings.
func (m *Manager) SetAppliedDesign(projectID string, index int) {
	all := m.appliedSnapshot()
	all[projectID] = index
	m.store.UpdateSettings(map[string]interface{}{appliedKey: all})
}

// AppliedDesign returns the project's applied UI-design slot index, if any.
func (m *Manager) AppliedDesign(projectID string) (int, bool) {
	idx, ok := m.appliedSnapshot()[projectID]
	return idx, ok
}

// StartRun clears the project+kind's prior mockups and marks the run running.
func (m *Manager) StartRun(projectID, kind string) error {
	if err := ClearProject(projectID, kind); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	s := Status{Status: StatusRunning, Expected: MockupCount, Received: 0, StartedAt: now, UpdatedAt: now}
	m.setStatus(projectID, kind, s)
	m.bus.Broadcast(EventProgress, map[string]any{"projectId": projectID, "kind": kind, "status": s})
	return nil
}

// RecordUpload saves one uploaded mockup and advances the run's progress.
func (m *Manager) RecordUpload(projectID, kind string, index int, png []byte) (string, error) {
	filename, err := Save(projectID, kind, index, png, time.Now().Unix())
	if err != nil {
		return "", err
	}
	list, _ := List(projectID, kind) // recount from disk (last-write-wins per slot)
	s := m.Status(projectID, kind)
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
	m.setStatus(projectID, kind, s)
	m.bus.Broadcast(EventReady, map[string]any{
		"projectId": projectID, "kind": kind, "index": index, "filename": filename, "status": s,
	})
	return filename, nil
}

// Fail marks a run as errored (e.g. dispatch failed).
func (m *Manager) Fail(projectID, kind, msg string) {
	s := m.Status(projectID, kind)
	s.Status = StatusError
	s.Error = msg
	s.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	m.setStatus(projectID, kind, s)
	m.bus.Broadcast(EventProgress, map[string]any{"projectId": projectID, "kind": kind, "status": s})
}

// RunningRuns snapshots every run currently in the running state, keyed back
// to its projectID + kind. Order is unspecified.
func (m *Manager) RunningRuns() []RunRef {
	all := m.snapshot()
	refs := make([]RunRef, 0, len(all))
	for key, s := range all {
		if s.Status != StatusRunning {
			continue
		}
		// statusMapKey joins projectID+"|"+kind; projectIDs are validated to
		// exclude "|", so the first "|" splits the pair unambiguously.
		parts := strings.SplitN(key, "|", 2)
		if len(parts) != 2 {
			continue
		}
		refs = append(refs, RunRef{ProjectID: parts[0], Kind: parts[1], Status: s})
	}
	return refs
}

// Redrive records that the sweep re-injected the prompt for a stalled run: it
// bumps Attempts and UpdatedAt while keeping the run in the running state.
func (m *Manager) Redrive(projectID, kind string) {
	s := m.Status(projectID, kind)
	s.Status = StatusRunning
	s.Attempts++
	s.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	m.setStatus(projectID, kind, s)
	m.bus.Broadcast(EventProgress, map[string]any{"projectId": projectID, "kind": kind, "status": s})
}

// Cancel clears a stuck run back to idle without deleting already-uploaded
// images (operators use this to unblock a stranded tile).
func (m *Manager) Cancel(projectID, kind string) {
	s := Status{Status: StatusIdle}
	m.setStatus(projectID, kind, s)
	m.bus.Broadcast(EventProgress, map[string]any{"projectId": projectID, "kind": kind, "status": s})
}
