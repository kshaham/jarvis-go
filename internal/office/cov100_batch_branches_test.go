package office

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLoadFloat64File_ValidNonNegative drives the happy return path of
// loadFloat64File (persistence.go:137): a file holding a valid,
// non-negative float returns (value, true).
func TestLoadFloat64File_ValidNonNegative(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "score.txt")
	if err := os.WriteFile(path, []byte("0.875"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := loadFloat64File(path, nil)
	if !ok {
		t.Fatal("expected loaded=true for a valid float file")
	}
	if got != 0.875 {
		t.Errorf("value=%v want 0.875", got)
	}
}

// TestGetLocks_SkipsNilLockEntry is a regression test for a nil-deref bug:
// before the fix, GetLocks' cleanup loop dereferenced lock.ExpiresAt
// without a nil-check (panicking on a nil map entry), even though the
// very next copy loop already guarded against nil. The fix drops nil
// entries in the cleanup loop. This test asserts GetLocks no longer
// panics on a nil entry, removes it, and still returns live locks.
func TestGetLocks_SkipsNilLockEntry(t *testing.T) {
	m := newTestManager()
	m.mu.Lock()
	m.fileLocks["good.go"] = &FileLock{
		Path:      "good.go",
		Owner:     "vex",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	m.fileLocks["nilentry.go"] = nil // the branch under test
	m.mu.Unlock()

	locks := m.GetLocks() // must not panic
	if len(locks) != 1 {
		t.Fatalf("expected exactly 1 lock (nil entry dropped), got %d", len(locks))
	}
	if locks[0].Path != "good.go" {
		t.Errorf("expected the live lock, got %+v", locks[0])
	}
	// The nil entry must have been cleaned out of the map.
	m.mu.Lock()
	_, stillThere := m.fileLocks["nilentry.go"]
	m.mu.Unlock()
	if stillThere {
		t.Error("nil lock entry should have been removed by the cleanup loop")
	}
}

// TestInjectOptimizationSuggestions_NilSessionsReturns drives the
// sessions==nil early return (optimizer.go:66-68).
func TestInjectOptimizationSuggestions_NilSessionsReturns(t *testing.T) {
	// NewManager with nil SessionStore so m.sessions is nil.
	m := NewManager(nil, func(string, interface{}) {}, nil)
	// Non-empty opts so we get past the len()==0 guard and reach the
	// sessions==nil check. Must not panic.
	m.InjectOptimizationSuggestions([]PromptOptimization{
		{TargetConfigID: "x", Suggestion: "tighten your loop"},
	})
}

// TestBuildKickstartPrompt_ShortMemoryAndOwnLearnings drives two
// previously-uncovered kickstart branches:
//   - the short-memory (non-truncated) arm (kickstart.go:63-66)
//   - the own-agent Learned Knowledge section (kickstart.go:70-74)
func TestBuildKickstartPrompt_ShortMemoryAndOwnLearnings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Seed the calling agent's OWN learning file so
	// BuildLearningsPromptSection("me") returns non-empty.
	learnDir := filepath.Join(home, ".jarvis", "learnings")
	if err := os.MkdirAll(learnDir, 0o755); err != nil {
		t.Fatal(err)
	}
	own := AgentLearningData{
		ConfigID:  "me",
		Name:      "Me",
		Learnings: []AgentLearning{{Category: "tech", Content: "always memoize the heavy probe"}},
	}
	odata, _ := json.Marshal(own)
	if err := os.WriteFile(filepath.Join(learnDir, "me.json"), odata, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &TeamMemberConfig{ID: "me", Name: "Me", Role: "Backend", Tier: "worker"}
	shortMemory := "short prior-work note, well under the cap"
	prompt := BuildKickstartPrompt(cfg, t.TempDir(), nil, "", shortMemory, true, nil)

	// Short memory: included verbatim, NOT truncated.
	if !strings.Contains(prompt, "## Previous Work Memory") {
		t.Error("expected Previous Work Memory header")
	}
	if !strings.Contains(prompt, "short prior-work note") {
		t.Error("short memory should be embedded verbatim")
	}
	if strings.Contains(prompt, "(truncated") {
		t.Error("short memory should NOT be marked truncated")
	}
	// Own learnings section present.
	if !strings.Contains(prompt, "## Learned Knowledge") {
		t.Error("expected Learned Knowledge section from own learnings")
	}
	if !strings.Contains(prompt, "always memoize the heavy probe") {
		t.Error("expected own learning content embedded")
	}
}

// TestBuildKickstartPrompt_LongMemoryTruncated drives the truncation arm
// (kickstart.go:60-62): memory longer than memoryInjectMaxChars is cut
// and annotated.
func TestBuildKickstartPrompt_LongMemoryTruncated(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := &TeamMemberConfig{ID: "solo", Name: "Solo", Role: "Backend", Tier: "worker"}
	// deduplicateMemory collapses identical content lines, so each line
	// carries a unique incrementing index to survive dedup and push the
	// total well past memoryInjectMaxChars.
	var sb strings.Builder
	for i := 0; sb.Len() <= memoryInjectMaxChars*2; i++ {
		fmt.Fprintf(&sb, "unique-memory-line-%d-%s\n", i, strings.Repeat("y", 40))
	}
	prompt := BuildKickstartPrompt(cfg, t.TempDir(), nil, "", sb.String(), true, nil)
	if !strings.Contains(prompt, "## Previous Work Memory") {
		t.Fatal("expected Previous Work Memory header")
	}
	if !strings.Contains(prompt, "(truncated") {
		t.Errorf("expected truncation annotation for over-long memory\n%s", prompt)
	}
}

// TestRemoveStatusLine_OperatorOutputStyleSkipped drives the
// operator-authored output-style skip (statusline.go:114-115): a
// .claude/output-style file NOT authored by jarvis is left in place.
func TestRemoveStatusLine_OperatorOutputStyleSkipped(t *testing.T) {
	cwd := t.TempDir()
	claudeDir := filepath.Join(cwd, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stylePath := filepath.Join(claudeDir, outputStyleFileName)
	operator := []byte("operator's own output style, no jarvis marker\n")
	if err := os.WriteFile(stylePath, operator, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := RemoveStatusLine(cwd); err != nil {
		t.Fatalf("RemoveStatusLine: %v", err)
	}
	// Operator file must survive untouched.
	got, err := os.ReadFile(stylePath)
	if err != nil {
		t.Fatalf("operator output-style should survive: %v", err)
	}
	if string(got) != string(operator) {
		t.Errorf("operator output-style was modified: %q", got)
	}
}

// TestListSnapshots_SortsTwoValidNewestFirst drives the sort comparator
// (snapshot.go:220-222) by listing two VALID snapshots with distinct
// CreatedAt timestamps; the result must be newest-first.
func TestListSnapshots_SortsTwoValidNewestFirst(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := snapshotDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	older := TeamSnapshot{
		Name:      "older",
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Members:   []SnapshotMemberEntry{{ConfigID: "a"}},
	}
	newer := TeamSnapshot{
		Name:      "newer",
		CreatedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Members:   []SnapshotMemberEntry{{ConfigID: "b"}},
	}
	for _, snap := range []TeamSnapshot{older, newer} {
		data, _ := json.MarshalIndent(snap, "", "  ")
		if err := os.WriteFile(filepath.Join(dir, snap.Name+".json"), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	out, err := ListSnapshots()
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 summaries, got %d: %+v", len(out), out)
	}
	if out[0].Name != "newer" || out[1].Name != "older" {
		t.Errorf("expected newest-first order [newer, older], got [%s, %s]",
			out[0].Name, out[1].Name)
	}
}
