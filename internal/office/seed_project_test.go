package office

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSeedNewProjectCLAUDEMd_WritesTemplate(t *testing.T) {
	dir := t.TempDir()
	if err := SeedNewProjectCLAUDEMd(dir, "my-app", "Build a landing page"); err != nil {
		t.Fatalf("seed failed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("CLAUDE.md not written: %v", err)
	}
	content := string(data)

	// Header + mission + design tokens should all appear.
	for _, want := range []string{
		"# my-app",
		"Freshly-created project",
		"Build a landing page",
		"#06080C", // canvas color
		"linear-gradient(to right bottom, #53617A, #3A445C)",
		"cm dev announce",      // team-dev-instance pointer
		"browser verification", // screenshot rule
	} {
		if !strings.Contains(content, want) {
			t.Errorf("CLAUDE.md missing %q", want)
		}
	}
}

func TestSeedNewProjectCLAUDEMd_SkipsExisting(t *testing.T) {
	dir := t.TempDir()
	existing := []byte("# kept\n")
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), existing, 0o644); err != nil {
		t.Fatalf("pre-seed: %v", err)
	}
	if err := SeedNewProjectCLAUDEMd(dir, "my-app", "anything"); err != nil {
		t.Fatalf("seed failed: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if string(got) != string(existing) {
		t.Errorf("existing CLAUDE.md was overwritten; want %q, got %q", existing, got)
	}
}

func TestSeedNewProjectCLAUDEMd_NoMissionRendersCleanly(t *testing.T) {
	dir := t.TempDir()
	if err := SeedNewProjectCLAUDEMd(dir, "bare", ""); err != nil {
		t.Fatalf("seed failed: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if strings.Contains(string(data), "## Mission") {
		t.Errorf("empty mission should omit the Mission section")
	}
}

func TestSeedNewProjectCLAUDEMd_RequiresInputs(t *testing.T) {
	if err := SeedNewProjectCLAUDEMd("", "x", "y"); err == nil {
		t.Error("expected error on empty dir")
	}
	if err := SeedNewProjectCLAUDEMd(t.TempDir(), "", "y"); err == nil {
		t.Error("expected error on empty name")
	}
}
