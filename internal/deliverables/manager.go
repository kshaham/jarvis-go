package deliverables

import (
	"encoding/json"
	"time"
)

// draftingKey holds per-project "the team is drafting" status in settings.
const draftingKey = "deliverablesDrafting"

// SSE event name (mirrored in the SPA's sse.ts).
const EventUpdated = "deliverables:updated"

// Drafting is a light per-project in-flight indicator: which deliverable type
// the team is currently writing (cleared on the next upload).
type Drafting struct {
	Type      string `json:"type"`
	Title     string `json:"title,omitempty"`
	StartedAt string `json:"startedAt"`
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

// Manager owns the lightweight drafting status + SSE. The documents themselves
// are written by the team's agents (uploaded via SaveDoc).
type Manager struct {
	store SettingsStore
	bus   Broadcaster
}

// NewManager wires the settings store + SSE bus.
func NewManager(store SettingsStore, bus Broadcaster) *Manager {
	return &Manager{store: store, bus: bus}
}

func (m *Manager) draftingMap() map[string]Drafting {
	out := map[string]Drafting{}
	raw, ok := m.store.GetSettings()[draftingKey]
	if ok && raw != nil {
		if b, err := json.Marshal(raw); err == nil {
			_ = json.Unmarshal(b, &out)
		}
	}
	return out
}

// Drafting returns the in-flight drafting status for a project (zero if none).
func (m *Manager) Drafting(projectID string) Drafting {
	return m.draftingMap()[projectID]
}

func (m *Manager) setDrafting(projectID string, d *Drafting) {
	all := m.draftingMap()
	if d == nil {
		delete(all, projectID)
	} else {
		all[projectID] = *d
	}
	m.store.UpdateSettings(map[string]interface{}{draftingKey: all})
}

// StartDrafting marks that the team is writing a deliverable and notifies the UI.
func (m *Manager) StartDrafting(projectID, docType, title string) {
	d := Drafting{Type: docType, Title: title, StartedAt: time.Now().UTC().Format(time.RFC3339)}
	m.setDrafting(projectID, &d)
	m.bus.Broadcast(EventUpdated, map[string]any{"projectId": projectID, "drafting": d})
}

// SaveDoc persists one uploaded deliverable version, clears the drafting flag,
// and broadcasts a refresh.
func (m *Manager) SaveDoc(projectID, title, docType, content string) (string, error) {
	slug, err := SaveVersion(projectID, title, docType, content, time.Now().Unix())
	if err != nil {
		return "", err
	}
	m.setDrafting(projectID, nil) // delivered → clear in-flight indicator
	m.bus.Broadcast(EventUpdated, map[string]any{
		"projectId": projectID, "slug": slug, "title": title, "type": docType,
	})
	return slug, nil
}
