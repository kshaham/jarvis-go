// Package mockups stores the AI-generated UI mockup images the office
// team produces per project (the "Mockups" page). Layout mirrors the
// task-screenshot store: PNGs live under
// ~/.jarvis/office-memory/mockups/<projectID>/<index>-<unix>.png, served
// back to the SPA via /api/mockups/image/<projectID>/<filename>. A run
// replaces the project's previous set (the latest 6 are what's shown).
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

// MockupMaxBytes caps one uploaded mockup PNG (gpt-image-1 1024px PNGs are
// well under this; the headroom covers larger sizes).
const MockupMaxBytes = 8 * 1024 * 1024

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

func projectDir(projectID string) (string, error) {
	if !ValidateProjectID(projectID) {
		return "", fmt.Errorf("invalid project id")
	}
	base := baseDir()
	if base == "" {
		return "", fmt.Errorf("no home directory")
	}
	return filepath.Join(base, projectID), nil
}

// ClearProject removes a project's existing mockups so a new run starts
// from a clean set of 6.
func ClearProject(projectID string) error {
	dir, err := projectDir(projectID)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("clear mockups: %w", err)
	}
	return nil
}

// Save writes one mockup PNG for (projectID, index). Returns the relative
// filename (for building the image URL).
func Save(projectID string, index int, png []byte, nowUnix int64) (string, error) {
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
	dir, err := projectDir(projectID)
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

// List returns a project's mockup filenames ordered by slot index (0..5).
func List(projectID string) ([]string, error) {
	dir, err := projectDir(projectID)
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

// Load returns a stored mockup's bytes, with path containment.
func Load(projectID, filename string) ([]byte, error) {
	if !ValidateFilename(filename) {
		return nil, fmt.Errorf("invalid mockup filename")
	}
	dir, err := projectDir(projectID)
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

// ── UI detection ───────────────────────────────────────────────────────

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
