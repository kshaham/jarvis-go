package office

// R25 · Team snapshot + restore. Saves the team's "shape" — member
// configs, mission, perpetual mode, pause state, whiteboard notes,
// per-agent task queues — into a single named JSON file. Restoring
// hands the configs back to the spawn flow so a fresh team comes up
// with identical membership and mid-run state.
//
// Out of scope on purpose:
//   - Agent memory + learnings: already persisted separately on disk
//     keyed by configID (learning.go, memory). A restored agent
//     with the same configID automatically re-reads them on kickstart.
//   - Active meetings / standups: transient, regenerated on new spawn.
//   - File locks: session-bound; dropped on restore since new
//     sessions get new IDs.
//
// Storage layout: ~/.jarvis/office-memory/snapshots/<NAME>.json.
// Name is validated the same way as new-project slugs (letters,
// numbers, underscore, hyphen; 1–64 chars) so no path traversal.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// snapshotNameRe mirrors projectNameRe from office.go — kept local
// to avoid cross-package coupling. Same constraint: no dots, no
// slashes, so <name>.json resolves cleanly inside the snapshots dir.
var snapshotNameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// SnapshotMemberEntry captures one member's config + live state worth
// preserving across a restore. Memory + learnings are NOT embedded —
// they live in their own files keyed by configID and survive
// independently.
type SnapshotMemberEntry struct {
	ConfigID   string           `json:"configId"`
	Config     TeamMemberConfig `json:"config"`
	Tasks      []*Task          `json:"tasks,omitempty"`
	Whiteboard *WhiteboardNote  `json:"whiteboard,omitempty"`
}

// TeamSnapshot is the root snapshot record.
type TeamSnapshot struct {
	Name          string                `json:"name"`
	CreatedAt     time.Time             `json:"createdAt"`
	TeamMission   string                `json:"teamMission,omitempty"`
	WorkingDir    string                `json:"workingDir,omitempty"`
	PerpetualMode bool                  `json:"perpetualMode"`
	Paused        bool                  `json:"paused,omitempty"`
	PauseReason   string                `json:"pauseReason,omitempty"`
	Members       []SnapshotMemberEntry `json:"members"`
}

// SnapshotSummary is what `GET /api/office/snapshots` returns — no
// per-member detail, just enough for a dashboard listing.
type SnapshotSummary struct {
	Name        string    `json:"name"`
	CreatedAt   time.Time `json:"createdAt"`
	MemberCount int       `json:"memberCount"`
	TeamMission string    `json:"teamMission,omitempty"`
}

// snapshotDir resolves the directory once. Returns "" if the home
// directory isn't accessible (tests, sandboxes).
func snapshotDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".jarvis", "office-memory", "snapshots")
}

// ValidateSnapshotName returns nil if the name is safe to write.
func ValidateSnapshotName(name string) error {
	if !snapshotNameRe.MatchString(name) {
		return fmt.Errorf("snapshot name must match [a-zA-Z0-9_-]{1,64}")
	}
	return nil
}

// CreateSnapshot captures the current team state into a named JSON
// file under ~/.jarvis/office-memory/snapshots/<name>.json. Fails
// fast if the team isn't spawned (nothing to snapshot).
func (m *Manager) CreateSnapshot(name, workingDir string) (*TeamSnapshot, error) {
	if err := ValidateSnapshotName(name); err != nil {
		return nil, err
	}
	dir := snapshotDir()
	if dir == "" {
		return nil, fmt.Errorf("snapshot dir unavailable (UserHomeDir failed)")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir snapshots: %w", err)
	}

	m.mu.RLock()
	spawned := m.teamSpawned
	mission := m.teamMission
	perpetual := m.perpetualMode
	paused := m.paused
	pauseReason := m.pauseReason
	m.mu.RUnlock()
	if !spawned {
		return nil, fmt.Errorf("no team is currently spawned")
	}

	whiteboard := m.GetWhiteboard()
	roster := m.GetRoster()
	members := make([]SnapshotMemberEntry, 0, len(roster))
	for _, a := range roster {
		if a == nil || a.Config == nil {
			continue
		}
		entry := SnapshotMemberEntry{
			ConfigID: a.ConfigID,
			Config:   *a.Config,
			Tasks:    a.GetTasks(),
		}
		if wb, ok := whiteboard[a.SessionID]; ok && wb != nil {
			copied := *wb
			entry.Whiteboard = &copied
		}
		members = append(members, entry)
	}

	snap := &TeamSnapshot{
		Name:          name,
		CreatedAt:     time.Now(),
		TeamMission:   mission,
		WorkingDir:    workingDir,
		PerpetualMode: perpetual,
		Paused:        paused,
		PauseReason:   pauseReason,
		Members:       members,
	}

	// Atomic write via temp-file + rename so a crash mid-write can't
	// leave a half-serialized snapshot that fails to load later.
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	target := filepath.Join(dir, name+".json")
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return nil, fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, target); err != nil {
		os.Remove(tmp)
		return nil, fmt.Errorf("rename: %w", err)
	}
	return snap, nil
}

// ListSnapshots returns metadata for every snapshot file. Corrupt /
// unparseable files are skipped with a best-effort filename-only
// summary so one bad file doesn't hide the rest of the list.
func ListSnapshots() ([]SnapshotSummary, error) {
	dir := snapshotDir()
	if dir == "" {
		return nil, fmt.Errorf("snapshot dir unavailable")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]SnapshotSummary, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".json")
		if !snapshotNameRe.MatchString(name) {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(dir, e.Name()))
		if readErr != nil {
			continue
		}
		var snap TeamSnapshot
		if json.Unmarshal(data, &snap) != nil {
			out = append(out, SnapshotSummary{Name: name})
			continue
		}
		out = append(out, SnapshotSummary{
			Name:        snap.Name,
			CreatedAt:   snap.CreatedAt,
			MemberCount: len(snap.Members),
			TeamMission: snap.TeamMission,
		})
	}
	// Sort newest first — most recent snapshot is usually the one
	// the operator wants to restore.
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

// LoadSnapshot reads a single snapshot by name.
func LoadSnapshot(name string) (*TeamSnapshot, error) {
	if err := ValidateSnapshotName(name); err != nil {
		return nil, err
	}
	dir := snapshotDir()
	if dir == "" {
		return nil, fmt.Errorf("snapshot dir unavailable")
	}
	data, err := os.ReadFile(filepath.Join(dir, name+".json"))
	if err != nil {
		return nil, err
	}
	var snap TeamSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return &snap, nil
}

// DeleteSnapshot removes a snapshot file. Idempotent: succeeds
// silently when the file doesn't exist.
func DeleteSnapshot(name string) error {
	if err := ValidateSnapshotName(name); err != nil {
		return err
	}
	dir := snapshotDir()
	if dir == "" {
		return fmt.Errorf("snapshot dir unavailable")
	}
	err := os.Remove(filepath.Join(dir, name+".json"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ApplyPostSpawnTaskQueues seeds task queues onto freshly-spawned
// agents from a snapshot. Called by the restore handler after the
// fresh team is running — walks the snapshot members by configID
// and pushes their saved tasks onto the matching live agent. Tasks
// with status "in_progress" are reset to "pending" since the prior
// agent's progress didn't survive the session boundary.
func (m *Manager) ApplyPostSpawnTaskQueues(snap *TeamSnapshot) int {
	if snap == nil {
		return 0
	}
	var seeded int
	for _, entry := range snap.Members {
		if len(entry.Tasks) == 0 {
			continue
		}
		live := m.GetAgentByConfig(entry.ConfigID)
		if live == nil {
			continue
		}
		for _, t := range entry.Tasks {
			if t == nil {
				continue
			}
			if t.Status == TaskInProgress {
				t.Status = TaskPending
			}
			live.AddTask(t)
			seeded++
		}
	}
	return seeded
}
