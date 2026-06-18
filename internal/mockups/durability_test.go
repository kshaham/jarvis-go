package mockups

import (
	"testing"
	"time"
)

// memStore is an in-memory SettingsStore for Manager tests (no disk, no SSE
// side effects). Updates merge into the same map the manager reads back.
type memStore struct {
	settings map[string]interface{}
}

func newMemStore() *memStore { return &memStore{settings: map[string]interface{}{}} }

func (m *memStore) GetSettings() map[string]interface{} { return m.settings }

func (m *memStore) UpdateSettings(updates map[string]interface{}) {
	for k, v := range updates {
		m.settings[k] = v
	}
}

// recordBus captures broadcast events so tests can assert they fire.
type recordBus struct {
	events []string
}

func (b *recordBus) Broadcast(event string, _ interface{}) {
	b.events = append(b.events, event)
}

// seedStatus writes a status directly (bypassing StartRun's disk clear) so
// tests can pin exact timestamps + attempts deterministically.
func seedStatus(m *Manager, projectID, kind string, s Status) {
	m.setStatus(projectID, kind, s)
}

func TestRunningRuns(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	cases := []struct {
		name      string
		seed      map[string]Status // key is statusMapKey(projectID, kind)
		wantPairs map[string]string // projectID -> kind for every expected running ref
	}{
		{
			name: "only running entries are returned",
			seed: map[string]Status{
				statusMapKey("proj-1", "ui"):           {Status: StatusRunning, UpdatedAt: now},
				statusMapKey("proj-2", "presentation"): {Status: StatusDone, UpdatedAt: now},
				statusMapKey("proj-3", "ui"):           {Status: StatusError, UpdatedAt: now},
				statusMapKey("proj-4", "presentation"): {Status: StatusIdle},
			},
			wantPairs: map[string]string{"proj-1": "ui"},
		},
		{
			name: "multiple running entries parse projectID and kind",
			seed: map[string]Status{
				statusMapKey("proj-a", "ui"):           {Status: StatusRunning, UpdatedAt: now},
				statusMapKey("proj-b", "presentation"): {Status: StatusRunning, UpdatedAt: now},
			},
			wantPairs: map[string]string{"proj-a": "ui", "proj-b": "presentation"},
		},
		{
			name:      "no running entries returns empty",
			seed:      map[string]Status{statusMapKey("proj-x", "ui"): {Status: StatusDone, UpdatedAt: now}},
			wantPairs: map[string]string{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewManager(newMemStore(), &recordBus{})
			for key, s := range tc.seed {
				// Seed via setStatus by reconstructing project/kind from the key.
				parts := splitKey(key)
				seedStatus(m, parts[0], parts[1], s)
			}
			refs := m.RunningRuns()
			if len(refs) != len(tc.wantPairs) {
				t.Fatalf("RunningRuns returned %d refs, want %d: %+v", len(refs), len(tc.wantPairs), refs)
			}
			for _, ref := range refs {
				wantKind, okPair := tc.wantPairs[ref.ProjectID]
				if !okPair {
					t.Errorf("unexpected running ref for projectID %q", ref.ProjectID)
					continue
				}
				if ref.Kind != wantKind {
					t.Errorf("projectID %q: kind = %q, want %q", ref.ProjectID, ref.Kind, wantKind)
				}
				if ref.Status.Status != StatusRunning {
					t.Errorf("projectID %q: status = %q, want running", ref.ProjectID, ref.Status.Status)
				}
			}
		})
	}
}

func TestRedrive(t *testing.T) {
	bus := &recordBus{}
	m := NewManager(newMemStore(), bus)
	old := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	seedStatus(m, "proj-1", "ui", Status{Status: StatusRunning, Expected: MockupCount, Attempts: 1, UpdatedAt: old})

	m.Redrive("proj-1", "ui")

	got := m.Status("proj-1", "ui")
	if got.Status != StatusRunning {
		t.Errorf("status = %q, want running", got.Status)
	}
	if got.Attempts != 2 {
		t.Errorf("attempts = %d, want 2", got.Attempts)
	}
	if got.UpdatedAt == old {
		t.Error("UpdatedAt should have advanced past the seeded timestamp")
	}
	if len(bus.events) != 1 || bus.events[0] != EventProgress {
		t.Errorf("events = %v, want [%s]", bus.events, EventProgress)
	}
}

func TestCancel(t *testing.T) {
	bus := &recordBus{}
	m := NewManager(newMemStore(), bus)
	seedStatus(m, "proj-1", "presentation", Status{Status: StatusRunning, Expected: MockupCount, Received: 3, Attempts: 2})

	m.Cancel("proj-1", "presentation")

	got := m.Status("proj-1", "presentation")
	if got.Status != StatusIdle {
		t.Errorf("status = %q, want idle", got.Status)
	}
	if got.Received != 0 || got.Attempts != 0 || got.Error != "" {
		t.Errorf("cancel should reset to a fresh idle status, got %+v", got)
	}
	if len(bus.events) != 1 || bus.events[0] != EventProgress {
		t.Errorf("events = %v, want [%s]", bus.events, EventProgress)
	}
}

// splitKey mirrors RunningRuns' key parse for test seeding (projectID|kind).
func splitKey(key string) [2]string {
	for i := 0; i < len(key); i++ {
		if key[i] == '|' {
			return [2]string{key[:i], key[i+1:]}
		}
	}
	return [2]string{key, ""}
}
