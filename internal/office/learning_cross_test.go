package office

import (
	"strings"
	"testing"
)

// TestBuildCrossAgentContextCompact_SummarizesPeers verifies that the
// compact cross-agent context lists one most-recent learning per peer,
// excludes the requesting agent, skips peers with no learnings, and
// truncates long content.
func TestBuildCrossAgentContextCompact_SummarizesPeers(t *testing.T) {
	_, cleanup := withTempLearningsDir(t)
	defer cleanup()

	// Peer "vex" has two learnings; the most recent should be surfaced.
	if err := AddLearningEntry("vex", "technique", "old finding", "t1", "T1"); err != nil {
		t.Fatalf("seed vex 1: %v", err)
	}
	if err := AddLearningEntry("vex", "technique", "newest finding from vex", "t2", "T2"); err != nil {
		t.Fatalf("seed vex 2: %v", err)
	}
	// Peer "bolt" has one learning with very long content (>100 chars) to
	// exercise the truncation branch.
	longContent := strings.Repeat("z", 150)
	if err := AddLearningEntry("bolt", "gotcha", longContent, "t3", "T3"); err != nil {
		t.Fatalf("seed bolt: %v", err)
	}
	// The requesting agent "self" has a learning that must be EXCLUDED.
	if err := AddLearningEntry("self", "technique", "should be excluded", "t4", "T4"); err != nil {
		t.Fatalf("seed self: %v", err)
	}

	out := BuildCrossAgentContextCompact("self")

	if !strings.Contains(out, "Recent teammate findings") {
		t.Errorf("expected header, got %q", out)
	}
	if !strings.Contains(out, "newest finding from vex") {
		t.Errorf("expected vex's most recent learning, got %q", out)
	}
	if strings.Contains(out, "old finding") {
		t.Errorf("expected only the newest vex learning, got %q", out)
	}
	if strings.Contains(out, "should be excluded") {
		t.Errorf("expected requesting agent excluded, got %q", out)
	}
	// Long content truncated with ellipsis.
	if !strings.Contains(out, "...") {
		t.Errorf("expected truncation ellipsis for long content, got %q", out)
	}
}

// TestBuildCrossAgentContextCompact_EmptyWhenNoPeers verifies an empty
// string is returned when there are no other agents' learnings.
func TestBuildCrossAgentContextCompact_EmptyWhenNoPeers(t *testing.T) {
	_, cleanup := withTempLearningsDir(t)
	defer cleanup()

	// Only the requesting agent has learnings.
	if err := AddLearningEntry("solo", "technique", "private note", "t1", "T1"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if got := BuildCrossAgentContextCompact("solo"); got != "" {
		t.Errorf("expected empty cross-agent context, got %q", got)
	}
}
