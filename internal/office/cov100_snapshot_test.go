package office

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSnapshotDir_NoHome covers the UserHomeDir error → "".
func TestSnapshotDir_NoHome(t *testing.T) {
	withNoHome(t)
	if d := snapshotDir(); d != "" {
		t.Errorf("expected empty snapshot dir with no HOME, got %q", d)
	}
}

// TestCreateSnapshot_InvalidName covers the name-validation guard.
func TestCreateSnapshot_InvalidName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	if _, err := m.CreateSnapshot("../bad name", "/wd", nil); err == nil {
		t.Fatal("expected invalid-name error")
	}
}

// TestCreateSnapshot_NoHome covers the dir=="" branch.
func TestCreateSnapshot_NoHome(t *testing.T) {
	m := newTestManager()
	withNoHome(t)
	if _, err := m.CreateSnapshot("snap", "/wd", nil); err == nil {
		t.Fatal("expected error with no HOME")
	}
}

// TestCreateSnapshot_MkdirFails makes the snapshots parent a file.
func TestCreateSnapshot_MkdirFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	mem := filepath.Join(home, ".jarvis", "office-memory")
	if err := os.MkdirAll(filepath.Dir(mem), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mem, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTestManager()
	if _, err := m.CreateSnapshot("snap", "/wd", nil); err == nil {
		t.Fatal("expected mkdir error")
	}
}

// TestCreateSnapshot_WriteFails makes the snapshot .tmp path a directory.
func TestCreateSnapshot_WriteFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := snapshotDir()
	if err := os.MkdirAll(filepath.Join(dir, "snap.json.tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := newTestManager()
	m.SetTeamSpawned(true)
	m.RegisterAgent("s1", "c1", &TeamMemberConfig{ID: "c1", Name: "C1"})
	if _, err := m.CreateSnapshot("snap", "/wd", nil); err == nil {
		t.Fatal("expected write-tmp error")
	}
}

// TestCreateSnapshot_RenameFails makes the target a non-empty directory.
func TestCreateSnapshot_RenameFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := snapshotDir()
	if err := os.MkdirAll(filepath.Join(dir, "snap.json", "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := newTestManager()
	m.SetTeamSpawned(true)
	m.RegisterAgent("s1", "c1", &TeamMemberConfig{ID: "c1", Name: "C1"})
	if _, err := m.CreateSnapshot("snap", "/wd", nil); err == nil {
		t.Fatal("expected rename error")
	}
}

// TestCreateSnapshot_SkipsNilConfigMember covers the nil-config member skip
// and the claudeSessionIDsByConfigID mapping.
func TestCreateSnapshot_SkipsNilConfigMember(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	m := newTestManager()
	m.SetTeamSpawned(true)
	m.RegisterAgent("s-good", "good", &TeamMemberConfig{ID: "good", Name: "Good"})
	// Inject nil-config agent.
	m.mu.Lock()
	m.agents["s-nil"] = NewAgentState("s-nil", "nilcfg", nil)
	m.mu.Unlock()

	snap, err := m.CreateSnapshot("snap", "/wd", map[string]string{"good": "claude-123"})
	if err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}
	if len(snap.Members) != 1 || snap.Members[0].ConfigID != "good" {
		t.Errorf("expected only the good member, got %+v", snap.Members)
	}
	if snap.Members[0].ClaudeSessionID != "claude-123" {
		t.Errorf("expected claude session id embedded, got %q", snap.Members[0].ClaudeSessionID)
	}
}

// TestListSnapshots_NoHome covers the dir=="" error branch.
func TestListSnapshots_NoHome(t *testing.T) {
	withNoHome(t)
	if _, err := ListSnapshots(); err == nil {
		t.Fatal("expected error with no HOME")
	}
}

// TestListSnapshots_MissingDir covers the IsNotExist → nil,nil branch.
func TestListSnapshots_MissingDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	out, err := ListSnapshots()
	if err != nil || out != nil {
		t.Errorf("expected nil,nil for missing dir, got %v,%v", out, err)
	}
}

// TestListSnapshots_ReadDirError makes the snapshots path a file.
func TestListSnapshots_ReadDirError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := snapshotDir()
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ListSnapshots(); err == nil {
		t.Fatal("expected ReadDir error when snapshots path is a file")
	}
}

// TestListSnapshots_CorruptSkipped lists a corrupt snapshot (filename-only
// summary) alongside valid skips.
func TestListSnapshots_CorruptSummary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := snapshotDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Corrupt JSON with a valid name → filename-only summary.
	if err := os.WriteFile(filepath.Join(dir, "corrupt.json"), []byte("{bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Non-json + subdir + invalid-name → skipped.
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "sub.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad name!.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := ListSnapshots()
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Name != "corrupt" {
		t.Errorf("expected one filename-only summary, got %+v", out)
	}
}

// TestLoadSnapshot_Errors covers invalid-name, no-home, read-error, and
// unmarshal-error branches.
func TestLoadSnapshot_Errors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := LoadSnapshot("../bad"); err == nil {
		t.Error("expected invalid-name error")
	}
	if _, err := LoadSnapshot("nonexistent"); err == nil {
		t.Error("expected read error for missing file")
	}
	// Corrupt file → unmarshal error.
	dir := snapshotDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "corrupt.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSnapshot("corrupt"); err == nil {
		t.Error("expected unmarshal error for corrupt file")
	}
}

// TestLoadSnapshot_NoHome covers the dir=="" branch.
func TestLoadSnapshot_NoHome(t *testing.T) {
	withNoHome(t)
	if _, err := LoadSnapshot("snap"); err == nil {
		t.Fatal("expected error with no HOME")
	}
}

// TestDeleteSnapshot_Errors covers invalid-name, no-home, and the
// idempotent missing-file path.
func TestDeleteSnapshot_Errors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := DeleteSnapshot("../bad"); err == nil {
		t.Error("expected invalid-name error")
	}
	// Missing file → idempotent success.
	if err := DeleteSnapshot("missing"); err != nil {
		t.Errorf("expected idempotent success for missing file, got %v", err)
	}
}

// TestDeleteSnapshot_NoHome covers the dir=="" branch.
func TestDeleteSnapshot_NoHome(t *testing.T) {
	withNoHome(t)
	if err := DeleteSnapshot("snap"); err == nil {
		t.Fatal("expected error with no HOME")
	}
}

// TestApplyPostSpawnTaskQueues_Edges covers nil snapshot, empty-task member,
// no-live-agent, and nil-task skips.
func TestApplyPostSpawnTaskQueues_Edges(t *testing.T) {
	m := newTestManager()
	// nil snapshot → 0.
	if n := m.ApplyPostSpawnTaskQueues(nil); n != 0 {
		t.Errorf("expected 0 for nil snapshot, got %d", n)
	}
	m.RegisterAgent("s1", "live", &TeamMemberConfig{ID: "live", Name: "Live"})
	snap := &TeamSnapshot{Members: []SnapshotMemberEntry{
		{ConfigID: "live", Tasks: []*Task{nil, {ID: "t1", Status: TaskInProgress}}}, // nil-task skip + in_progress reset
		{ConfigID: "empty", Tasks: nil},                                            // empty-task skip
		{ConfigID: "gone", Tasks: []*Task{{ID: "t2"}}},                             // no live agent skip
	}}
	seeded := m.ApplyPostSpawnTaskQueues(snap)
	if seeded != 1 {
		t.Errorf("expected 1 seeded task, got %d", seeded)
	}
	// The seeded in_progress task should be reset to pending.
	tasks := m.GetAgentByConfig("live").GetTasksCopy()
	if len(tasks) != 1 || tasks[0].Status != TaskPending {
		t.Errorf("expected one pending task on live agent, got %+v", tasks)
	}
}
