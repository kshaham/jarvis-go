package office

// R34 · Task screenshot attachments.
//
// Agents cite a PNG path in their `cm task complete --summary` today
// (e.g. "Screenshotted at /tmp/foo.png"), but operators can't see
// the actual image from the dashboard -- verification is a text
// claim, not evidence. This store lets agents upload the PNG so the
// dashboard can render it inline on the task detail modal.
//
// Storage layout: ~/.jarvis/office-memory/task-screenshots/<filename>
// Filename convention: <taskID>-<unix>.<ext>
// Retention: last 500 files front-drop (matches the R29/R33 pattern).

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	TaskScreenshotMaxBytes = 2 * 1024 * 1024 // 2 MB
	TaskScreenshotMaxKeep  = 500
)

// evalSymlinks is the symlink resolver used by the path-containment
// checks in LoadTaskScreenshot and LoadWrapUpReport. It is a seam (not
// an inline call to filepath.EvalSymlinks) so a test can force the
// second resolve — EvalSymlinks(dir) — to fail even after the first
// resolve of the file path succeeds, exercising the otherwise-TOCTOU
// error branch deterministically. Production never reassigns it.
var evalSymlinks = filepath.EvalSymlinks

// readFile is the seam used by LoadWrapUpReport's final read so a test
// can force a read error on an already-resolved, in-bounds path without
// a real unreadable file. Production never reassigns it.
var readFile = os.ReadFile

// screenshotFilenameRE enforces <taskID>-<unix>.<ext> where taskID
// is [a-zA-Z0-9_-]+ and ext ∈ {png, jpg, jpeg, webp}.
var screenshotFilenameRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,127}-\d{10,19}\.(png|jpg|jpeg|webp)$`)

func ValidateTaskScreenshotFilename(name string) bool {
	return screenshotFilenameRE.MatchString(name)
}

func TaskScreenshotsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".jarvis", "office-memory", "task-screenshots")
}

// mimeToExt maps allowed mime types to the on-disk extension. Used
// to validate the upload and to name the file predictably.
var mimeToExt = map[string]string{
	"image/png":  "png",
	"image/jpeg": "jpg",
	"image/webp": "webp",
}

// SaveTaskScreenshot writes the given bytes to the screenshots dir
// with a filename derived from taskID + unix nanos. Returns the
// relative filename (not the absolute path) so the handler can
// build the download URL. Enforces size cap, mime whitelist, and
// front-drops old files to TaskScreenshotMaxKeep.
func SaveTaskScreenshot(taskID, mimeType string, data []byte, nowUnix int64) (string, error) {
	if taskID == "" {
		return "", fmt.Errorf("taskID is required")
	}
	// Keep taskID safe for the filename. Same charset as the regex.
	if !taskIDValidRE.MatchString(taskID) {
		return "", fmt.Errorf("invalid taskID")
	}
	ext, ok := mimeToExt[strings.ToLower(mimeType)]
	if !ok {
		return "", fmt.Errorf("unsupported mime type %q (allowed: image/png, image/jpeg, image/webp)", mimeType)
	}
	if len(data) == 0 {
		return "", fmt.Errorf("empty image data")
	}
	if len(data) > TaskScreenshotMaxBytes {
		return "", fmt.Errorf("image too large: %d bytes (max %d)", len(data), TaskScreenshotMaxBytes)
	}

	dir := TaskScreenshotsDir()
	if dir == "" {
		return "", fmt.Errorf("no home directory")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir screenshots: %w", err)
	}
	filename := fmt.Sprintf("%s-%d.%s", taskID, nowUnix, ext)
	full := filepath.Join(dir, filename)
	tmp := full + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return "", fmt.Errorf("write tmp: %w", err)
	}
	if err := renameFile(tmp, full); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("rename: %w", err)
	}

	if err := pruneOldScreenshots(dir, TaskScreenshotMaxKeep); err != nil {
		slog.Warn("prune old screenshots failed", "err", err)
	}
	return filename, nil
}

// LoadTaskScreenshot returns the bytes of a stored screenshot by
// filename. Callers must pre-validate with ValidateTaskScreenshotFilename;
// this function re-checks defensively (same pattern as R33 reports).
func LoadTaskScreenshot(filename string) ([]byte, error) {
	if !ValidateTaskScreenshotFilename(filename) {
		return nil, fmt.Errorf("invalid screenshot filename")
	}
	dir := TaskScreenshotsDir()
	if dir == "" {
		return nil, fmt.Errorf("no home directory")
	}
	full := filepath.Join(dir, filename)
	// Resolve symlinks on both sides so macOS /var -> /private/var
	// doesn't trip the containment check.
	resolved, err := evalSymlinks(full)
	if err != nil {
		return nil, err
	}
	resolvedDir, err := evalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(resolved, resolvedDir+string(filepath.Separator)) {
		return nil, fmt.Errorf("screenshot path escaped screenshots dir")
	}
	return os.ReadFile(resolved)
}

// ListTaskScreenshots returns every filename belonging to the given
// taskID, newest-first. Used by the task detail modal + the
// soft-gate "attachment present?" check.
func ListTaskScreenshots(taskID string) ([]string, error) {
	dir := TaskScreenshotsDir()
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	prefix := taskID + "-"
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !ValidateTaskScreenshotFilename(e.Name()) {
			continue
		}
		if !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		out = append(out, e.Name())
	}
	// Newest first -- timestamps grow monotonically so reverse-sort
	// by filename.
	sort.Sort(sort.Reverse(sort.StringSlice(out)))
	return out, nil
}

// HasTaskScreenshot reports whether any screenshot is attached to
// the given taskID. Used by the R18 soft-gate to suppress the
// "UI task missing verification" warning when concrete evidence
// has been uploaded.
func HasTaskScreenshot(taskID string) bool {
	list, err := ListTaskScreenshots(taskID)
	if err != nil {
		return false
	}
	return len(list) > 0
}

// ---------------------------------------------------------------------------
// Internals
// ---------------------------------------------------------------------------

var taskIDValidRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,127}$`)

func pruneOldScreenshots(dir string, keep int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	valid := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if ValidateTaskScreenshotFilename(e.Name()) {
			valid = append(valid, e.Name())
		}
	}
	if len(valid) <= keep {
		return nil
	}
	// Names end in <unix>.<ext>; lexical sort ~ chronological for
	// same-digit-length timestamps. Safer to parse-then-sort but
	// unix seconds pad to 10 digits through year 2286.
	sort.Strings(valid)
	drop := valid[:len(valid)-keep]
	for _, name := range drop {
		_ = os.Remove(filepath.Join(dir, name))
	}
	return nil
}
