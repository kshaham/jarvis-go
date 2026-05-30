package office

import (
	"os"
	"path/filepath"
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
