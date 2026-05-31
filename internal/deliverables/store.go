// Package deliverables stores versioned markdown documents the office team
// produces per project (the "Deliverables" page). Each document has a slug
// (sanitized title), a type (research-brief, sop, etc.), and one or more
// version files. Layout:
//   ~/.jarvis/office-memory/deliverables/<projectID>/<slug>/meta.json
//   ~/.jarvis/office-memory/deliverables/<projectID>/<slug>/v<unix>.md
// Reading and writing use atomic tmp+rename. Path containment is enforced.
package deliverables

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// MaxContentBytes caps one deliverable version (4 MB for large research docs).
const MaxContentBytes = 4 * 1024 * 1024

// MaxVersionsPerSlug keeps at most this many versions of one doc (GC oldest).
const MaxVersionsPerSlug = 20

var (
	projectIDRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)
	slugRE      = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,95}$`)
	// v<unix>.md  — unix 10-19 digits
	versionFileRE = regexp.MustCompile(`^v(\d{10,19})\.md$`)
)

// ValidateProjectID guards the per-project subdirectory name.
func ValidateProjectID(id string) bool { return projectIDRE.MatchString(id) }

// ValidateSlug guards a document slug.
func ValidateSlug(slug string) bool { return slugRE.MatchString(slug) }

// TitleToSlug converts a document title to a stable filesystem slug.
func TitleToSlug(title string) string {
	s := strings.ToLower(strings.TrimSpace(title))
	var b strings.Builder
	prev := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prev = false
		} else if !prev && b.Len() > 0 {
			b.WriteByte('-')
			prev = true
		}
	}
	slug := strings.TrimRight(b.String(), "-")
	if len(slug) > 96 {
		slug = slug[:96]
	}
	if slug == "" {
		slug = "doc"
	}
	return slug
}

// Meta is the per-slug metadata file.
type Meta struct {
	Title string `json:"title"`
	Type  string `json:"type"`
}

// Deliverable is one doc entry returned in the list.
type Deliverable struct {
	Slug      string  `json:"slug"`
	Title     string  `json:"title"`
	Type      string  `json:"type"`
	Versions  []int64 `json:"versions"` // unix timestamps, newest first
	UpdatedAt string  `json:"updatedAt,omitempty"`
}

func baseDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".jarvis", "office-memory", "deliverables")
}

func slugDir(projectID, slug string) (string, error) {
	if !ValidateProjectID(projectID) {
		return "", fmt.Errorf("invalid project id")
	}
	if !ValidateSlug(slug) {
		return "", fmt.Errorf("invalid slug")
	}
	base := baseDir()
	if base == "" {
		return "", fmt.Errorf("no home directory")
	}
	return filepath.Join(base, projectID, slug), nil
}

// SaveVersion writes a new version of a document. Returns the slug used.
func SaveVersion(projectID, title, docType, content string, nowUnix int64) (string, error) {
	if projectID == "" || title == "" {
		return "", fmt.Errorf("projectId and title are required")
	}
	if len(content) == 0 {
		return "", fmt.Errorf("empty content")
	}
	if len(content) > MaxContentBytes {
		return "", fmt.Errorf("content too large: %d bytes (max %d)", len(content), MaxContentBytes)
	}
	slug := TitleToSlug(title)
	dir, err := slugDir(projectID, slug)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir deliverables: %w", err)
	}
	// Write or update the meta.json (idempotent: first write wins for title).
	metaPath := filepath.Join(dir, "meta.json")
	if _, err := os.Stat(metaPath); os.IsNotExist(err) {
		m := Meta{Title: title, Type: docType}
		if b, err := json.Marshal(m); err == nil {
			_ = atomicWrite(metaPath, b)
		}
	}
	// Write the version file.
	vPath := filepath.Join(dir, fmt.Sprintf("v%d.md", nowUnix))
	if err := atomicWrite(vPath, []byte(content)); err != nil {
		return "", fmt.Errorf("write version: %w", err)
	}
	pruneVersions(dir)
	return slug, nil
}

// List returns all deliverables for a project, newest-updated first.
func List(projectID string) ([]Deliverable, error) {
	if !ValidateProjectID(projectID) {
		return nil, fmt.Errorf("invalid project id")
	}
	base := baseDir()
	if base == "" {
		return nil, nil
	}
	projDir := filepath.Join(base, projectID)
	entries, err := os.ReadDir(projDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Deliverable
	for _, e := range entries {
		if !e.IsDir() || !ValidateSlug(e.Name()) {
			continue
		}
		d, err := readDeliverable(projectID, e.Name())
		if err != nil {
			continue
		}
		out = append(out, d)
	}
	// Newest-updated first.
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].Versions) == 0 {
			return false
		}
		if len(out[j].Versions) == 0 {
			return true
		}
		return out[i].Versions[0] > out[j].Versions[0]
	})
	return out, nil
}

// Get returns the content of a specific version (0 = latest).
func Get(projectID, slug string, version int64) (content string, meta Meta, err error) {
	dir, err := slugDir(projectID, slug)
	if err != nil {
		return "", Meta{}, err
	}
	// Containment check.
	resolvedDir, err2 := filepath.EvalSymlinks(dir)
	if err2 != nil {
		return "", Meta{}, fmt.Errorf("slug dir not found")
	}
	baseResolved, _ := filepath.EvalSymlinks(filepath.Join(baseDir(), projectID))
	if !strings.HasPrefix(resolvedDir, baseResolved+string(filepath.Separator)) {
		return "", Meta{}, fmt.Errorf("slug path escaped deliverables dir")
	}
	// Load meta.
	if b, e := os.ReadFile(filepath.Join(dir, "meta.json")); e == nil {
		_ = json.Unmarshal(b, &meta)
	}
	// Resolve version.
	var vPath string
	if version == 0 {
		d, _ := readDeliverable(projectID, slug)
		if len(d.Versions) == 0 {
			return "", meta, fmt.Errorf("no versions found")
		}
		vPath = filepath.Join(dir, fmt.Sprintf("v%d.md", d.Versions[0]))
	} else {
		vPath = filepath.Join(dir, fmt.Sprintf("v%d.md", version))
	}
	b, err := os.ReadFile(vPath)
	if err != nil {
		return "", meta, fmt.Errorf("version not found")
	}
	return string(b), meta, nil
}

// ── internals ────────────────────────────────────────────────────────────

func readDeliverable(projectID, slug string) (Deliverable, error) {
	dir, err := slugDir(projectID, slug)
	if err != nil {
		return Deliverable{}, err
	}
	var m Meta
	if b, e := os.ReadFile(filepath.Join(dir, "meta.json")); e == nil {
		_ = json.Unmarshal(b, &m)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Deliverable{}, err
	}
	var versions []int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		sm := versionFileRE.FindStringSubmatch(e.Name())
		if sm == nil {
			continue
		}
		var ts int64
		for _, c := range sm[1] {
			ts = ts*10 + int64(c-'0')
		}
		versions = append(versions, ts)
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i] > versions[j] })
	d := Deliverable{Slug: slug, Title: m.Title, Type: m.Type, Versions: versions}
	if len(versions) > 0 {
		d.UpdatedAt = time.Unix(versions[0], 0).UTC().Format(time.RFC3339)
	}
	return d, nil
}

func atomicWrite(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func pruneVersions(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && versionFileRE.MatchString(e.Name()) {
			files = append(files, e.Name())
		}
	}
	if len(files) <= MaxVersionsPerSlug {
		return
	}
	sort.Strings(files) // lexical ≈ chronological for same-digit unix timestamps
	drop := files[:len(files)-MaxVersionsPerSlug]
	for _, f := range drop {
		_ = os.Remove(filepath.Join(dir, f))
	}
}
