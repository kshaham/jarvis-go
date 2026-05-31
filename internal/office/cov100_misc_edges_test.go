package office

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGenerateWrapUpReport_WhiteboardSortComparator drives the whiteboard
// entries sort comparator: two agents each leave a whiteboard note, so the
// name-sort comparator runs and the report lists them alphabetically.
func TestGenerateWrapUpReport_WhiteboardSortComparator(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.RegisterAgent("sess-z", "zcfg", &TeamMemberConfig{ID: "zcfg", Name: "Zeta", Tier: "worker"})
	m.RegisterAgent("sess-a", "acfg", &TeamMemberConfig{ID: "acfg", Name: "Alpha", Tier: "worker"})

	// Two whiteboard notes so the sort.Slice comparator actually compares.
	if !m.SetWhiteboardNote("sess-z", "zeta finished the parser") {
		t.Fatal("expected whiteboard note write to succeed")
	}
	if !m.SetWhiteboardNote("sess-a", "alpha wired the API") {
		t.Fatal("expected whiteboard note write to succeed")
	}

	meta, err := GenerateWrapUpReport(m, "manual", "")
	if err != nil {
		t.Fatalf("GenerateWrapUpReport: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(WrapUpReportsDir(), meta.Filename))
	if err != nil {
		t.Fatal(err)
	}
	report := string(data)
	// Alpha must appear before Zeta (name-sorted).
	ai := strings.Index(report, "Alpha")
	zi := strings.Index(report, "Zeta")
	if ai < 0 || zi < 0 {
		t.Fatalf("expected both agents in the whiteboard section, got:\n%s", report)
	}
	if ai > zi {
		t.Errorf("expected Alpha before Zeta in name-sorted whiteboard, got Alpha@%d Zeta@%d", ai, zi)
	}
}

// TestAllocateZAIModels_DefaultSlotLimitWhenRateMissing drives the
// `limit == 0 -> defaultModelSlotLimit` branch by temporarily removing a
// model's entry from ZAIRateLimits so its lookup returns the zero value.
func TestAllocateZAIModels_DefaultSlotLimitWhenRateMissing(t *testing.T) {
	const model = "glm-5.1" // top of the downgrade path
	orig, had := ZAIRateLimits[model]
	delete(ZAIRateLimits, model) // now ZAIRateLimits[model] == 0
	t.Cleanup(func() {
		if had {
			ZAIRateLimits[model] = orig
		}
	})

	// defaultModelSlotLimit (3) agents should all fit on the top model now
	// that its limit comes from the default rather than a missing rate entry.
	members := []string{"a", "b", "c"}
	req := map[string]string{"a": model, "b": model, "c": model}
	got := AllocateZAIModels(req, members)

	count := 0
	for _, mdl := range got {
		if mdl == model {
			count++
		}
	}
	if count != 3 {
		t.Errorf("expected all 3 agents on %s via default slot limit, got %d (%v)", model, count, got)
	}
}

// TestBuildCodeGraphSnapshot_HitsMaxEntries drives the
// `count >= codeGraphMaxEntries -> SkipAll` branch by creating more than the
// cap of files so the walk stops early.
func TestBuildCodeGraphSnapshot_HitsMaxEntries(t *testing.T) {
	root := t.TempDir()
	// Create well over codeGraphMaxEntries (500) plain files.
	for i := 0; i < codeGraphMaxEntries+50; i++ {
		name := filepath.Join(root, fmt.Sprintf("file_%04d.go", i))
		if err := os.WriteFile(name, []byte("package x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	snap := BuildCodeGraphSnapshot(root)
	if snap == nil {
		t.Fatal("expected a non-nil snapshot")
	}
	snapMap, ok := snap.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map snapshot, got %T", snap)
	}
	entries, ok := snapMap["entries"].([]interface{})
	if !ok {
		t.Fatalf("expected entries slice, got %T", snapMap["entries"])
	}
	// The walk must have stopped at the cap (not enumerated all 550).
	if len(entries) > codeGraphMaxEntries {
		t.Errorf("expected at most %d entries (SkipAll cap), got %d", codeGraphMaxEntries, len(entries))
	}
}
