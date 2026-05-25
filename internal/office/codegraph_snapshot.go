package office

import (
	"os"
	"path/filepath"
	"strings"
)

// Code-graph directory snapshot.
//
// Quick directory walk used by the office dashboard's "current
// project tree" surface — NOT the codegraph MCP server, which is a
// separate concept (codegraph.go). This walk produces a flat list of
// entries up to a depth + count cap; the dashboard JS renders it as
// a collapsible tree.
//
// Extracted from handler.buildCodeGraph so the walk logic + caps are
// testable in isolation and a future caller (mobile API, CLI) can
// reuse the same shape.

// codeGraphMaxEntries caps how many entries the walk emits. The
// dashboard renders all of them in one go; a cap protects both the
// JSON payload size and the browser's DOM cost on very large
// repos. Adjust upward if operators complain about cut-off trees.
const codeGraphMaxEntries = 500

// codeGraphMaxDepth caps directory-tree depth. Avoids recursion into
// node_modules-shaped paths a user might check in by accident.
const codeGraphMaxDepth = 3

// codeGraphSkipDirs is the set of directory names the walk descends
// into never. node_modules / vendor / __pycache__ dwarf the rest of
// any repo by file count and serve no dashboard purpose. Hidden dirs
// (anything starting with ".") are also skipped.
var codeGraphSkipDirs = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	"__pycache__": true,
}

// CodeGraphFileInfo is one row in an AnalyzeCodeGraphImpact result.
// Reports whether the file exists on disk + its size (zero when
// missing or on stat error).
type CodeGraphFileInfo struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	Size   int64  `json:"size,omitempty"`
}

// AnalyzeCodeGraphImpact stat()s each entry in files relative to
// rootDir (or as-is when the path is absolute) and returns a row
// per input describing whether the file exists + its size. Used by
// the dashboard's "files touched in this PR" surface to mark which
// referenced files are still on disk.
//
// Pure given the rootDir + files: just file-system reads. Empty
// rootDir leaves relative paths to fail the stat call (they
// resolve relative to the process CWD), so callers should either
// pass a real rootDir or normalise the input to absolute paths
// first.
//
// Returns an empty slice (not nil) for an empty files list so the
// JSON encoder produces `[]` rather than `null`.
func AnalyzeCodeGraphImpact(rootDir string, files []string) []CodeGraphFileInfo {
	out := make([]CodeGraphFileInfo, 0, len(files))
	for _, f := range files {
		fullPath := f
		if rootDir != "" && !filepath.IsAbs(f) {
			fullPath = filepath.Join(rootDir, f)
		}
		info, err := os.Stat(fullPath)
		if err != nil {
			out = append(out, CodeGraphFileInfo{Path: f, Exists: false})
			continue
		}
		out = append(out, CodeGraphFileInfo{Path: f, Exists: true, Size: info.Size()})
	}
	return out
}

// BuildCodeGraphSnapshot walks rootDir up to codeGraphMaxDepth /
// codeGraphMaxEntries and returns a snapshot suitable for the
// dashboard's directory-tree surface. The returned shape is
// intentionally `interface{}` (always a `map[string]interface{}`)
// to match the handler's prior signature and the JSON encoder's
// expectations.
//
// Returns nil when rootDir is empty or doesn't exist.
//
// Pure given rootDir — no Context, no SessionManager. The handler
// supplies rootDir by querying the live session manager.
func BuildCodeGraphSnapshot(rootDir string) interface{} {
	if rootDir == "" {
		return nil
	}
	if _, err := os.Stat(rootDir); err != nil {
		return nil
	}

	count := 0
	var entries []interface{}
	_ = filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors (permission denied, file gone mid-walk)
		}
		if count >= codeGraphMaxEntries {
			return filepath.SkipAll
		}
		rel, relErr := filepath.Rel(rootDir, path)
		if relErr != nil {
			return nil
		}
		if rel == "." {
			return nil
		}
		// Depth filter: skip dir contents once we're past max-depth.
		depth := len(strings.Split(rel, string(filepath.Separator)))
		if depth > codeGraphMaxDepth {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		// Skip hidden dirs and known noise directories.
		name := info.Name()
		if info.IsDir() {
			if strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			if codeGraphSkipDirs[name] {
				return filepath.SkipDir
			}
		}
		entry := map[string]interface{}{
			"path":  rel,
			"name":  name,
			"isDir": info.IsDir(),
		}
		if !info.IsDir() {
			entry["size"] = info.Size()
		}
		entries = append(entries, entry)
		count++
		return nil
	})

	if entries == nil {
		entries = make([]interface{}, 0)
	}
	return map[string]interface{}{
		"root":    rootDir,
		"entries": entries,
		"total":   count,
	}
}
