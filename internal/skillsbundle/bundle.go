// Package skillsbundle embeds the curated office/agent skill set into the
// jarvis binary so a standalone deploy ships its team's skills with no
// external skills/ directory. The bundle under skills/ is the
// self-contained, authoritative source of every skill name referenced by
// the kickstart prompt (internal/office/skills.go).
//
// Materialize is called once at daemon startup to write the embedded files
// into ~/.claude/skills, where agents read them at runtime via
// `cm skill read <name>` and internal/office/skills.go. It only writes the
// paths it ships and never deletes files it does not ship, so operator- or
// agent-authored skills in the same directory are preserved.
package skillsbundle

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// The all: prefix includes dotfiles and nested directories (e.g.
// references/) under the skills subtree.
//
//go:embed all:skills
var bundle embed.FS

// FS returns a filesystem rooted at the embedded skills directory, so its
// top-level entries are the individual skill directories. On the (in
// practice impossible) sub-FS error it falls back to the raw embed.FS.
func FS() fs.FS {
	sub, err := fs.Sub(bundle, "skills")
	if err != nil {
		return bundle
	}
	return sub
}

// Names returns the sorted list of top-level skill directory names in the
// embedded bundle.
func Names() ([]string, error) {
	entries, err := fs.ReadDir(FS(), ".")
	if err != nil {
		return nil, fmt.Errorf("read embedded skills root: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// Materialize writes every embedded skill file into destSkillsRoot,
// overwriting any same-named files (the bundled copy is authoritative for
// the paths it ships). It never deletes files outside the paths it writes,
// so unrelated skills already present in destSkillsRoot are left intact.
// It returns the number of files written and the first error encountered.
func Materialize(destSkillsRoot string) (int, error) {
	if err := os.MkdirAll(destSkillsRoot, 0o755); err != nil {
		return 0, fmt.Errorf("create skills root %q: %w", destSkillsRoot, err)
	}

	src := FS()
	written := 0
	err := fs.WalkDir(src, ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(src, path)
		if err != nil {
			return fmt.Errorf("read embedded skill %q: %w", path, err)
		}
		dest := filepath.Join(destSkillsRoot, path)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return fmt.Errorf("create dir for %q: %w", dest, err)
		}
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			return fmt.Errorf("write skill %q: %w", dest, err)
		}
		written++
		return nil
	})
	if err != nil {
		return written, err
	}
	return written, nil
}
