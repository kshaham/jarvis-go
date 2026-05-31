package office

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestBuildSprintSummary_UptimeAndEmptyRoster covers the uptime block (hours +
// minutes) and the empty-roster branch.
func TestBuildSprintSummary_UptimeAndEmptyRoster(t *testing.T) {
	m, _, _ := newCtrlManager(t, DefaultConfig())

	// Empty roster branch.
	m.mu.Lock()
	m.teamSpawnedAt = time.Now().Add(-90 * time.Minute) // 1h 30m uptime
	m.mu.Unlock()

	out := m.buildSprintSummary()
	if !strings.Contains(out, "Uptime: 1h 30m") {
		t.Errorf("expected hours+minutes uptime, got %q", out)
	}
	if !strings.Contains(out, "No agents registered") {
		t.Errorf("expected empty-roster line, got %q", out)
	}

	// Minutes-only uptime branch.
	m.mu.Lock()
	m.teamSpawnedAt = time.Now().Add(-12 * time.Minute)
	m.mu.Unlock()
	out2 := m.buildSprintSummary()
	if !strings.Contains(out2, "Uptime: 12m") || strings.Contains(out2, "0h") {
		t.Errorf("expected minutes-only uptime, got %q", out2)
	}
}

// TestPersistMetrics_WritesFile verifies PersistMetrics writes metrics.json.
func TestPersistMetrics_WritesFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	m := newTestManager()

	m.PersistMetrics()

	metricsFile := filepath.Join(home, ".jarvis", "office-memory", "metrics.json")
	if _, err := os.Stat(metricsFile); err != nil {
		t.Errorf("expected metrics.json written, got %v", err)
	}
	// Tmp file must have been renamed away.
	if _, err := os.Stat(metricsFile + ".tmp"); !os.IsNotExist(err) {
		t.Error("expected metrics tmp file removed after rename")
	}
}

// TestBuildProjectContext_ExtractsSeededDocs seeds CLAUDE.md + README.md and
// asserts the project-context block includes excerpts from both.
func TestBuildProjectContext_ExtractsSeededDocs(t *testing.T) {
	dir := t.TempDir()
	claude := "# My Project\n\nThis project automates the office and ships fast.\n"
	readme := "# README\n\nThe readme prose explains how to run the thing locally.\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(claude), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(readme), 0o644); err != nil {
		t.Fatal(err)
	}

	got := buildProjectContextUncached(dir)
	if !strings.Contains(got, "### CLAUDE.md") {
		t.Errorf("expected CLAUDE.md section header, got %q", got)
	}
	if !strings.Contains(got, "automates the office") {
		t.Errorf("expected CLAUDE.md excerpt, got %q", got)
	}
	if !strings.Contains(got, "### README.md") {
		t.Errorf("expected README.md section header, got %q", got)
	}
}

// TestBuildProjectContext_PerFileCap verifies an oversized single doc is
// truncated to the per-file cap with a continuation marker.
func TestBuildProjectContext_PerFileCap(t *testing.T) {
	dir := t.TempDir()
	bigBody := strings.Repeat("alpha beta gamma delta. ", 200) // well over the per-file cap
	doc := "# Title\n\n" + bigBody + "\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	got := buildProjectContextUncached(dir)
	if !strings.Contains(got, "...") {
		t.Errorf("expected a truncation marker for an oversized doc, got tail %q", got[max(0, len(got)-40):])
	}
	if len(got) > projectDocsTotalCap+200 {
		t.Errorf("expected total output bounded near %d, got %d", projectDocsTotalCap, len(got))
	}
}

// TestSeedNewProjectCLAUDEMd covers the validation errors, the idempotent
// skip, and the happy-path render.
func TestSeedNewProjectCLAUDEMd(t *testing.T) {
	// Validation errors.
	if err := SeedNewProjectCLAUDEMd("", "name", "mission"); err == nil {
		t.Error("expected error for empty dir")
	}
	if err := SeedNewProjectCLAUDEMd(t.TempDir(), "", "mission"); err == nil {
		t.Error("expected error for empty name")
	}

	// Happy path renders the project name + mission into CLAUDE.md.
	dir := t.TempDir()
	if err := SeedNewProjectCLAUDEMd(dir, "Phoenix", "rise from ashes"); err != nil {
		t.Fatalf("SeedNewProjectCLAUDEMd: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read seeded CLAUDE.md: %v", err)
	}
	if !strings.Contains(string(data), "Phoenix") {
		t.Errorf("expected project name in CLAUDE.md, got %q", string(data))
	}

	// Idempotent: a second call must NOT overwrite the existing file.
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("operator edits"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SeedNewProjectCLAUDEMd(dir, "Phoenix", "different mission"); err != nil {
		t.Fatalf("idempotent seed: %v", err)
	}
	again, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if string(again) != "operator edits" {
		t.Errorf("expected existing CLAUDE.md preserved, got %q", string(again))
	}
}

// TestWhiteboardStore_RoundTrip exercises load (missing file) + Set/Append +
// saveLocked + Snapshot + reload-from-disk.
func TestWhiteboardStore_RoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	store := NewWhiteboardStore()
	store.Set("Vex", "notes", "first note")
	store.Append("Vex", "notes", "second line")
	store.Set("", "notes", "ignored") // empty agent name -> no-op

	snap := store.Snapshot()
	entry, ok := snap.Entries["Vex"]
	if !ok {
		t.Fatalf("expected a Vex entry, got %+v", snap.Entries)
	}
	notes := entry.Sections["notes"].Content
	if !strings.Contains(notes, "first note") || !strings.Contains(notes, "second line") {
		t.Errorf("expected both note lines, got %q", notes)
	}

	// A fresh store reloads the persisted state via load().
	store2 := NewWhiteboardStore()
	snap2 := store2.Snapshot()
	if _, ok := snap2.Entries["Vex"]; !ok {
		t.Error("expected whiteboard state reloaded from disk")
	}
}

// TestPrependBudgetWarning covers both the under-threshold (no warning) and
// over-threshold (warning prepended) branches.
func TestPrependBudgetWarning(t *testing.T) {
	small := "a short kickstart prompt"
	if got := prependBudgetWarning(small, "Vex"); got != small {
		t.Errorf("expected no warning for small prompt, got %q", got)
	}

	// estimatedTokens = len/4; threshold is 50k tokens -> 200k+ chars triggers.
	big := strings.Repeat("x", 220_000)
	got := prependBudgetWarning(big, "Vex")
	if !strings.HasPrefix(got, "[NOTICE]") {
		t.Errorf("expected a [NOTICE] budget warning prepended, got prefix %q", got[:min(40, len(got))])
	}
	if !strings.Contains(got, big) {
		t.Error("expected the original prompt preserved after the warning")
	}
}
