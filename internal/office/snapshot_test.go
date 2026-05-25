package office

// R25 · Snapshot round-trip + name validation tests.

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestValidateSnapshotName(t *testing.T) {
	cases := []struct {
		name    string
		wantErr bool
	}{
		{"basic", false},
		{"with-dash", false},
		{"with_underscore", false},
		{"Mix-123", false},
		{"", true},
		{"has space", true},
		{"has/slash", true},
		{"has.dot", true},
		{"..", true},
		{strings.Repeat("a", 65), true},
	}
	for _, tc := range cases {
		err := ValidateSnapshotName(tc.name)
		if (err != nil) != tc.wantErr {
			t.Errorf("ValidateSnapshotName(%q) err=%v, wantErr=%v", tc.name, err, tc.wantErr)
		}
	}
}

func TestCreateListLoadDeleteSnapshot_RoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Cleanup(func() { time.Sleep(60 * time.Millisecond) })

	store := &recordingSessionStore{}
	var mu sync.Mutex
	events := []string{}
	m := NewManager(store, func(event string, _ interface{}) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, event)
	}, nil)
	m.SetTeamSpawned(true)
	m.SetTeamMission("do the thing")
	m.SetPerpetualMode(true)
	m.RegisterAgent("sess-1", "cfg-1", &TeamMemberConfig{ID: "cfg-1", Name: "Ada", Role: "Lead", Tier: "strategic"})
	m.RegisterAgent("sess-2", "cfg-2", &TeamMemberConfig{ID: "cfg-2", Name: "Babbage", Role: "Engineer", Tier: "worker"})

	a1 := m.GetAgent("sess-1")
	a1.AddTask(&Task{ID: "t-1", Title: "design it", Status: TaskInProgress})
	a1.AddTask(&Task{ID: "t-2", Title: "write it", Status: TaskPending})

	snap, err := m.CreateSnapshot("first-snap", "/tmp/work", map[string]string{"cfg-1": "claude-sid-1"})
	if err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}
	if snap.Name != "first-snap" || len(snap.Members) != 2 {
		t.Fatalf("unexpected snap: %+v", snap)
	}

	// File should exist under the test HOME's snapshots dir.
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".jarvis", "office-memory", "snapshots", "first-snap.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected snapshot file at %s, got err: %v", path, err)
	}

	// List should include it with the right member count + mission.
	list, err := ListSnapshots()
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(list) != 1 || list[0].Name != "first-snap" || list[0].MemberCount != 2 {
		t.Fatalf("unexpected list: %+v", list)
	}
	if list[0].TeamMission != "do the thing" {
		t.Errorf("TeamMission: got %q", list[0].TeamMission)
	}

	// Load round-trips everything.
	loaded, err := LoadSnapshot("first-snap")
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	if loaded.PerpetualMode != true {
		t.Error("expected PerpetualMode=true preserved")
	}
	if loaded.WorkingDir != "/tmp/work" {
		t.Errorf("WorkingDir: got %q", loaded.WorkingDir)
	}
	if len(loaded.Members) != 2 {
		t.Fatalf("expected 2 members, got %d", len(loaded.Members))
	}
	var ada *SnapshotMemberEntry
	for i := range loaded.Members {
		if loaded.Members[i].ConfigID == "cfg-1" {
			ada = &loaded.Members[i]
		}
	}
	if ada == nil {
		t.Fatal("expected cfg-1 (Ada) in snapshot members")
	}
	if len(ada.Tasks) != 2 {
		t.Fatalf("expected 2 tasks preserved, got %d", len(ada.Tasks))
	}

	// Delete then confirm it's gone.
	if err := DeleteSnapshot("first-snap"); err != nil {
		t.Fatalf("DeleteSnapshot: %v", err)
	}
	list2, _ := ListSnapshots()
	if len(list2) != 0 {
		t.Fatalf("expected empty list after delete, got %+v", list2)
	}
}

func TestCreateSnapshot_RefusesWhenTeamNotSpawned(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Cleanup(func() { time.Sleep(60 * time.Millisecond) })
	store := &recordingSessionStore{}
	m := NewManager(store, func(string, interface{}) {}, nil)
	// teamSpawned defaults to false
	_, err := m.CreateSnapshot("will-fail", "/tmp", nil)
	if err == nil {
		t.Fatal("expected error when team not spawned")
	}
}

func TestApplyPostSpawnTaskQueues_ResetsInProgressTasks(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Cleanup(func() { time.Sleep(60 * time.Millisecond) })
	store := &recordingSessionStore{}
	m := NewManager(store, func(string, interface{}) {}, nil)
	m.RegisterAgent("sess-new", "cfg-ada", &TeamMemberConfig{ID: "cfg-ada", Name: "Ada", Tier: "strategic"})
	snap := &TeamSnapshot{
		Members: []SnapshotMemberEntry{
			{
				ConfigID: "cfg-ada",
				Tasks: []*Task{
					{ID: "t-a", Title: "a", Status: TaskInProgress},
					{ID: "t-b", Title: "b", Status: TaskPending},
				},
			},
			// configId that doesn't match any live agent — gets skipped
			{ConfigID: "cfg-absent", Tasks: []*Task{{ID: "ghost", Title: "ghost", Status: TaskPending}}},
		},
	}
	seeded := m.ApplyPostSpawnTaskQueues(snap)
	if seeded != 2 {
		t.Fatalf("expected 2 tasks seeded, got %d", seeded)
	}
	ada := m.GetAgent("sess-new")
	tasks := ada.GetTasks()
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks on Ada, got %d", len(tasks))
	}
	// R25 invariant: in_progress tasks from the snapshot must be
	// reset to pending since no agent was actively working them.
	for _, tk := range tasks {
		if tk.Status == TaskInProgress {
			t.Errorf("task %s still in_progress after restore", tk.ID)
		}
	}
}
