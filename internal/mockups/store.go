// Package mockups stores the AI-generated mockup images the office team
// produces per project (the "Mockups" page). A mockup is one of two KINDS:
// "ui" (an image-to-image restyle of the running UI) or "presentation" (a
// text-to-image slide design from the project's docs). Layout mirrors the
// task-screenshot store: PNGs live under
// ~/.jarvis/office-memory/mockups/<projectID>/<kind>/<slot>-<unix>.png,
// served via /api/mockups/image/<projectID>/<kind>/<filename>. A run
// replaces that project+kind's previous set (the latest 6 are shown).
package mockups

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// MockupCount is how many mockups a generation run produces.
const MockupCount = 6

// MockupMaxBytes caps one uploaded mockup PNG (gpt-image-1 1024-1536px PNGs
// are well under this; the headroom covers larger sizes).
const MockupMaxBytes = 8 * 1024 * 1024

// Kinds — a mockup is either a UI redesign or a presentation slide design.
const (
	KindUI           = "ui"
	KindPresentation = "presentation"
)

// Kinds lists the supported mockup kinds (for iteration in handlers).
var Kinds = []string{KindUI, KindPresentation}

// ValidateKind reports whether kind is a supported mockup kind.
func ValidateKind(kind string) bool {
	return kind == KindUI || kind == KindPresentation
}

var (
	projectIDRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)
	// <index 0-5>-<unix>.png
	filenameRE = regexp.MustCompile(`^[0-5]-\d{10,19}\.png$`)
)

// ValidateProjectID guards the per-project subdirectory name.
func ValidateProjectID(id string) bool { return projectIDRE.MatchString(id) }

// ValidateFilename guards a stored mockup filename.
func ValidateFilename(name string) bool { return filenameRE.MatchString(name) }

func baseDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".jarvis", "office-memory", "mockups")
}

// kindDir is the per-project, per-kind storage directory.
func kindDir(projectID, kind string) (string, error) {
	if !ValidateProjectID(projectID) {
		return "", fmt.Errorf("invalid project id")
	}
	if !ValidateKind(kind) {
		return "", fmt.Errorf("invalid mockup kind")
	}
	base := baseDir()
	if base == "" {
		return "", fmt.Errorf("no home directory")
	}
	return filepath.Join(base, projectID, kind), nil
}

// ClearProject removes a project's existing mockups for one kind so a new run
// starts from a clean set of 6.
func ClearProject(projectID, kind string) error {
	dir, err := kindDir(projectID, kind)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("clear mockups: %w", err)
	}
	return nil
}

// Save writes one mockup PNG for (projectID, kind, index). Returns the
// relative filename (for building the image URL).
func Save(projectID, kind string, index int, png []byte, nowUnix int64) (string, error) {
	if index < 0 || index >= MockupCount {
		return "", fmt.Errorf("index out of range 0..%d", MockupCount-1)
	}
	if len(png) == 0 {
		return "", fmt.Errorf("empty image data")
	}
	if len(png) > MockupMaxBytes {
		return "", fmt.Errorf("image too large: %d bytes (max %d)", len(png), MockupMaxBytes)
	}
	if len(png) < 8 || string(png[1:4]) != "PNG" {
		return "", fmt.Errorf("data is not a PNG")
	}
	dir, err := kindDir(projectID, kind)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir mockups: %w", err)
	}
	filename := fmt.Sprintf("%d-%d.png", index, nowUnix)
	full := filepath.Join(dir, filename)
	tmp := full + ".tmp"
	if err := os.WriteFile(tmp, png, 0o644); err != nil {
		return "", fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, full); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("rename: %w", err)
	}
	// Drop any older file for the same index (keep only the newest per slot).
	pruneIndex(dir, index, filename)
	return filename, nil
}

// List returns a project's mockup filenames for one kind, ordered by slot.
func List(projectID, kind string) ([]string, error) {
	dir, err := kindDir(projectID, kind)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && ValidateFilename(e.Name()) {
			out = append(out, e.Name())
		}
	}
	// Filename starts with the index, so a lexical sort orders by slot.
	sort.Strings(out)
	return out, nil
}

// SlotPath returns the absolute path of the stored mockup occupying the given
// slot (0..MockupCount-1) for a project+kind. Errors when the index is out of
// range or no image has been saved for that slot yet.
func SlotPath(projectID, kind string, index int) (string, error) {
	if index < 0 || index >= MockupCount {
		return "", fmt.Errorf("index out of range 0..%d", MockupCount-1)
	}
	dir, err := kindDir(projectID, kind)
	if err != nil {
		return "", err
	}
	files, err := List(projectID, kind)
	if err != nil {
		return "", err
	}
	prefix := fmt.Sprintf("%d-", index)
	for _, f := range files {
		if strings.HasPrefix(f, prefix) {
			return filepath.Join(dir, f), nil
		}
	}
	return "", fmt.Errorf("no mockup at slot %d", index)
}

// Load returns a stored mockup's bytes, with path containment.
func Load(projectID, kind, filename string) ([]byte, error) {
	if !ValidateFilename(filename) {
		return nil, fmt.Errorf("invalid mockup filename")
	}
	dir, err := kindDir(projectID, kind)
	if err != nil {
		return nil, err
	}
	full := filepath.Join(dir, filename)
	resolved, err := filepath.EvalSymlinks(full)
	if err != nil {
		return nil, err
	}
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(resolved, resolvedDir+string(filepath.Separator)) {
		return nil, fmt.Errorf("mockup path escaped project dir")
	}
	return os.ReadFile(resolved)
}

func pruneIndex(dir string, index int, keep string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	prefix := fmt.Sprintf("%d-", index)
	for _, e := range entries {
		if e.IsDir() || e.Name() == keep || !ValidateFilename(e.Name()) {
			continue
		}
		if strings.HasPrefix(e.Name(), prefix) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// ── detection ──────────────────────────────────────────────────────────

// uiDeps are package.json dependencies that mark a project as having a UI.
var uiDeps = []string{
	"react", "react-dom", "vue", "svelte", "@angular/core", "next", "vite",
	"solid-js", "preact", "@remix-run/react", "nuxt", "astro",
}

// HasUI reports whether dir looks like it has a frontend UI worth mocking
// up: a package.json with a known UI dependency, or an index.html.
func HasUI(dir string) bool {
	if b, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil {
		var pkg struct {
			Dependencies    map[string]string `json:"dependencies"`
			DevDependencies map[string]string `json:"devDependencies"`
		}
		if json.Unmarshal(b, &pkg) == nil {
			for _, d := range uiDeps {
				if _, ok := pkg.Dependencies[d]; ok {
					return true
				}
				if _, ok := pkg.DevDependencies[d]; ok {
					return true
				}
			}
		}
	}
	for _, p := range []string{"index.html", filepath.Join("public", "index.html"), filepath.Join("src", "index.html")} {
		if _, err := os.Stat(filepath.Join(dir, p)); err == nil {
			return true
		}
	}
	return false
}

// docFiles are markers of presentable docs/research content.
var docFiles = []string{
	"README.md", "README.MD", "readme.md", "README.rst",
	"ARCHITECTURE.md", "docs", "REPORT.md",
}

// HasDocs reports whether dir has docs/research content worth presenting: a
// top-level README/doc marker, or any markdown file in the root.
func HasDocs(dir string) bool {
	for _, p := range docFiles {
		if _, err := os.Stat(filepath.Join(dir, p)); err == nil {
			return true
		}
	}
	// Any *.md in the project root counts as presentable content.
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
				return true
			}
		}
	}
	return false
}
