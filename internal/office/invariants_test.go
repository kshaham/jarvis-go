package office

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectInvariantsCapsAndTrims(t *testing.T) {
	m := &Manager{}
	m.SetProjectInvariants([]string{"  Rule one  ", "", "Rule two"})
	got := m.GetProjectInvariants()
	if len(got) != 2 || got[0] != "Rule one" || got[1] != "Rule two" {
		t.Fatalf("expected trimmed/non-empty 2 items, got %#v", got)
	}
}

func TestProjectInvariantsAddRemove(t *testing.T) {
	m := &Manager{}
	m.AddProjectInvariant("alpha")
	m.AddProjectInvariant("beta")
	if got := m.GetProjectInvariants(); len(got) != 2 {
		t.Fatalf("want 2, got %d", len(got))
	}
	if err := m.RemoveProjectInvariant(0); err != nil {
		t.Fatal(err)
	}
	got := m.GetProjectInvariants()
	if len(got) != 1 || got[0] != "beta" {
		t.Fatalf("want [beta], got %#v", got)
	}
	if err := m.RemoveProjectInvariant(5); err == nil {
		t.Fatalf("expected out-of-range error")
	}
}

func TestProjectInvariantsPersistRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	m := &Manager{}
	m.SetProjectInvariants([]string{"persist me"})
	if _, err := os.Stat(filepath.Join(tmp, ".jarvis", "office-memory", "project-invariants.json")); err != nil {
		t.Fatalf("invariants file not written: %v", err)
	}
	m2 := &Manager{}
	m2.loadPersistedInvariants()
	if got := m2.GetProjectInvariants(); len(got) != 1 || got[0] != "persist me" {
		t.Fatalf("round-trip failed: %#v", got)
	}
}

func TestRenderInvariantsBlock(t *testing.T) {
	if renderInvariantsBlock(nil) != "" {
		t.Fatalf("empty invariants must render empty block")
	}
	out := renderInvariantsBlock([]string{"No force-push to main", "All code has tests"})
	if !strings.Contains(out, "Project Invariants") ||
		!strings.Contains(out, "- No force-push to main") ||
		!strings.Contains(out, "- All code has tests") {
		t.Fatalf("unexpected block:\n%s", out)
	}
}

func TestKickstartIncludesInvariants(t *testing.T) {
	cfg := &TeamMemberConfig{Name: "Alice", Role: "Engineer"}
	p := BuildKickstartPrompt(cfg, "/tmp", nil, "Ship it", "", false, []string{"No force-push to main"})
	if !strings.Contains(p, "Project Invariants") || !strings.Contains(p, "No force-push to main") {
		t.Fatalf("kickstart missing invariants block")
	}
	p2 := BuildKickstartPrompt(cfg, "/tmp", nil, "Ship it", "", false, nil)
	if strings.Contains(p2, "Project Invariants") {
		t.Fatalf("empty invariants should not emit a block")
	}
}
