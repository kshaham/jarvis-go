package office

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"text/template"
)

//go:embed seeds/new_project_claude.md.tmpl
var newProjectClaudeTemplate string

// seedProjectTemplate is parsed once at init. Parse errors would be a
// build-time bug in the embedded template, not a runtime condition.
var seedProjectTemplate = template.Must(
	template.New("new-project-claude").Parse(newProjectClaudeTemplate),
)

// seedProjectData is the template context.
type seedProjectData struct {
	Name    string
	Mission string
}

// SeedNewProjectCLAUDEMd writes a CLAUDE.md into a freshly-created project
// directory. It is idempotent: an existing CLAUDE.md in the target dir is
// left untouched so operators who pick an existing name don't lose notes.
// Returns nil on success OR on skip (the file already existed).
func SeedNewProjectCLAUDEMd(dir, name, mission string) error {
	if dir == "" {
		return fmt.Errorf("seed CLAUDE.md: directory is required")
	}
	if name == "" {
		return fmt.Errorf("seed CLAUDE.md: project name is required")
	}
	target := filepath.Join(dir, "CLAUDE.md")

	// Idempotent: never overwrite an existing CLAUDE.md. Covers the case
	// where the operator reuses a project name that already has notes.
	if _, err := os.Stat(target); err == nil {
		return nil
	}

	// Ensure the dir exists. Typically the handler has already created it
	// with MkdirAll, but belt-and-suspenders keeps this helper usable
	// standalone (e.g. in tests).
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("seed CLAUDE.md: mkdir %q: %w", dir, err)
	}

	// Write atomically via a tmpfile + rename so a partial write can't
	// leave a half-rendered CLAUDE.md behind.
	tmp := target + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("seed CLAUDE.md: create %q: %w", tmp, err)
	}
	if execErr := seedProjectTemplate.Execute(f, seedProjectData{Name: name, Mission: mission}); execErr != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("seed CLAUDE.md: render template: %w", execErr)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("seed CLAUDE.md: close %q: %w", tmp, err)
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("seed CLAUDE.md: rename: %w", err)
	}
	return nil
}
