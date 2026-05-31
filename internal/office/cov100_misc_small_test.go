package office

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestBuildCodeGraphSnapshot_DeepFilePrunedNotDir drives the non-dir
// past-max-depth `return nil` branch (codegraph_snapshot.go ~122). A file
// at depth 4 whose parent dir is at depth 3 (not skipped) is visited by
// Walk but pruned because its own depth exceeds codeGraphMaxDepth (3).
func TestBuildCodeGraphSnapshot_DeepFilePrunedNotDir(t *testing.T) {
	root := t.TempDir()
	// root/a/b/c at depths 1,2,3 (c == max depth, not skipped).
	cDir := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(cDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A FILE at depth 4 inside c => visited but pruned by the depth filter.
	if err := os.WriteFile(filepath.Join(cDir, "depth4.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	snap := BuildCodeGraphSnapshot(root)
	m := snap.(map[string]interface{})
	entries := m["entries"].([]interface{})
	for _, e := range entries {
		if e.(map[string]interface{})["name"] == "depth4.txt" {
			t.Error("depth-4 file should be pruned by the depth cap")
		}
	}
}

// TestBuildCodeGraphSnapshot_WalkErrorSkipped drives the Walk-callback
// err!=nil branch (codegraph_snapshot.go ~103-105). An unreadable
// subdirectory makes filepath.Walk invoke the callback with a non-nil
// error for that entry, which the callback swallows (returns nil) so the
// rest of the tree is still walked.
func TestBuildCodeGraphSnapshot_WalkErrorSkipped(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses directory permission checks")
	}
	root := t.TempDir()
	// A readable file at the root.
	if err := os.WriteFile(filepath.Join(root, "ok.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// An unreadable subdirectory: Walk descends, fails to list it, and
	// invokes the callback with a non-nil error for the contents.
	bad := filepath.Join(root, "locked")
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, "inner.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(bad, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(bad, 0o755) })

	// Must not panic; the snapshot still contains the readable root file.
	snap := BuildCodeGraphSnapshot(root)
	m, ok := snap.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map snapshot, got %T", snap)
	}
	entries := m["entries"].([]interface{})
	sawOK := false
	for _, e := range entries {
		if e.(map[string]interface{})["name"] == "ok.txt" {
			sawOK = true
		}
	}
	if !sawOK {
		t.Error("expected the readable root file in the snapshot despite the locked subdir")
	}
}

// TestMetricsSnapshot_AvgCompletionRate drives the totalStandups>0
// average-rate computation in Metrics.Snapshot (metrics.go ~66-68).
func TestMetricsSnapshot_AvgCompletionRate(t *testing.T) {
	mtr := NewMetrics()
	mtr.TotalStandups.Add(4)
	mtr.TotalStandupsComplete.Add(3)

	snap := mtr.Snapshot()
	if snap.TotalStandups != 4 || snap.TotalStandupsCompleted != 3 {
		t.Fatalf("unexpected counters: %+v", snap)
	}
	if snap.AverageStandupResponseRate < 0.74 || snap.AverageStandupResponseRate > 0.76 {
		t.Errorf("expected avg response rate ~0.75, got %v", snap.AverageStandupResponseRate)
	}
}
