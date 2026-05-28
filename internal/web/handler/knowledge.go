package handler

// Knowledge-library operator surface.
//
// Backs the new "Knowledge" tab in the office dashboard with four
// read/operate endpoints over the markdown library + promotion
// ledger:
//
//   GET  /api/knowledge/stats       — counts + PageIndex health
//   GET  /api/knowledge/promotions  — promotion ledger view
//   GET  /api/knowledge/library     — read-only file browse (?path=)
//   POST /api/knowledge/revoke      — operator override
//
// Design rules followed:
//   - All write paths go through library.Ledger.MarkRevoked + the
//     existing office.DeleteSharedSkill, never touch the on-disk
//     skill files directly. That keeps the security audit surface
//     in one place (shared_skills.go's path-traversal guard).
//   - Read paths cap the response size so a corrupted library
//     can't OOM the dashboard.
//   - secpath.ResolveUnderRoot is the only gate between the
//     ?path= query param and a real os.ReadFile call.

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kshaham/jarvis/internal/library"
	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/secpath"
	"github.com/kshaham/jarvis/internal/web/request"
)

// knowledgeMaxFileBytes caps the size of any single library file
// the operator can pull through /api/knowledge/library. Larger
// hits are truncated with a marker so the dashboard stays
// responsive on a runaway-growth incident.
const knowledgeMaxFileBytes = 1 << 20 // 1 MB

// knowledgeMaxBodyBytes caps the POST /revoke request body.
const knowledgeMaxBodyBytes = 4 << 10 // 4 KB

// GetKnowledgeStats returns counts + health for the dashboard tab.
// Cheap (directory listings only); safe to call frequently.
//
// GET /api/knowledge/stats
func (ctx *Context) GetKnowledgeStats(w http.ResponseWriter, r *http.Request) {
	root := library.Dir()
	out := map[string]interface{}{
		"libraryDir":        root,
		"experiences":       countMarkdownFiles(filepath.Join(root, "experiences")),
		"workflows":         countMarkdownFiles(filepath.Join(root, "workflows")),
		"standups":          countMarkdownFiles(filepath.Join(root, "standups")),
		"experiments":       countMarkdownFiles(filepath.Join(root, "experiments")),
		"pageindexAvailable": office.PageIndexAvailable(),
	}

	// Promotion-ledger summary: total entries + how many are
	// currently promoted vs revoked. Avoids the operator having to
	// hit /promotions just to know "is anything graduated yet?".
	ledger, _ := library.OpenLedger()
	if ledger != nil {
		entries, err := ledger.List()
		if err == nil {
			promoted := 0
			revoked := 0
			for _, e := range entries {
				if !e.PromotedAt.IsZero() {
					promoted++
				}
				if !e.RevokedAt.IsZero() {
					revoked++
				}
			}
			out["ledgerEntries"] = len(entries)
			out["ledgerPromoted"] = promoted
			out["ledgerRevoked"] = revoked
		}
	}

	request.WriteJSON(w, http.StatusOK, out)
}

// GetKnowledgePromotions returns the full promotion ledger sorted
// by reuse count descending. Used by the dashboard's promotion view
// + the one-click revoke action.
//
// GET /api/knowledge/promotions
func (ctx *Context) GetKnowledgePromotions(w http.ResponseWriter, r *http.Request) {
	ledger, err := library.OpenLedger()
	if err != nil {
		request.WriteError(w, http.StatusInternalServerError, "open ledger: "+err.Error())
		return
	}
	if ledger == nil {
		request.WriteJSON(w, http.StatusOK, map[string]interface{}{"entries": []library.LedgerEntry{}})
		return
	}
	entries, err := ledger.List()
	if err != nil {
		request.WriteError(w, http.StatusInternalServerError, "list ledger: "+err.Error())
		return
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{"entries": entries})
}

// GetKnowledgeLibrary reads a single file under the library root
// and returns the content + metadata. Path is treated as relative
// to library.Dir(); secpath.ResolveUnderRoot is the security gate.
//
// GET /api/knowledge/library?path=experiences/vex/task-001.md
func (ctx *Context) GetKnowledgeLibrary(w http.ResponseWriter, r *http.Request) {
	root := library.Dir()
	if root == "" {
		request.WriteError(w, http.StatusServiceUnavailable, "library directory unavailable")
		return
	}
	relPath := r.URL.Query().Get("path")
	if relPath == "" {
		request.WriteError(w, http.StatusBadRequest, "missing path query parameter")
		return
	}
	absPath, err := secpath.ResolveUnderRoot(root, relPath)
	if err != nil {
		// Map secpath errors to HTTP codes. The error message
		// itself is generic enough to surface to the operator
		// (no internal-path leakage).
		switch {
		case isPathRefusedErr(err):
			request.WriteError(w, http.StatusForbidden, err.Error())
		default:
			request.WriteError(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			request.WriteError(w, http.StatusNotFound, "file not found")
			return
		}
		request.WriteError(w, http.StatusInternalServerError, "read failed: "+err.Error())
		return
	}
	truncated := false
	if len(data) > knowledgeMaxFileBytes {
		data = data[:knowledgeMaxFileBytes]
		truncated = true
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"path":      relPath,
		"size":      len(data),
		"truncated": truncated,
		"content":   string(data),
	})
}

// isPathRefusedErr is true for any secpath error that should map
// to HTTP 403 (vs 400 for malformed input). Wraps the existing
// sentinels so the handler doesn't repeat the switch in each call.
func isPathRefusedErr(err error) bool {
	return strings.Contains(err.Error(), "absolute paths not allowed") ||
		strings.Contains(err.Error(), "path traversal not allowed")
}

// knowledgeRevokeRequest is the POST /revoke body shape.
type knowledgeRevokeRequest struct {
	// Path is relative to the library root, e.g.
	// "workflows/auth.md". Required.
	Path string `json:"path"`
	// Reason is operator-supplied free text. Stored on the ledger
	// entry for future audit. Optional but encouraged.
	Reason string `json:"reason"`
}

// PostKnowledgeRevoke records an operator revocation in the
// promotion ledger AND removes the on-disk SKILL.md if one exists
// for the workflow. 24h cool-down before re-promotion is enforced
// in library.RunPromotion — this handler just stamps the ledger.
//
// POST /api/knowledge/revoke
func (ctx *Context) PostKnowledgeRevoke(w http.ResponseWriter, r *http.Request) {
	var req knowledgeRevokeRequest
	if !request.DecodeAndValidateMax(w, r, &req, knowledgeMaxBodyBytes) {
		return
	}
	if req.Path == "" {
		request.WriteError(w, http.StatusBadRequest, "path is required")
		return
	}

	ledger, err := library.OpenLedger()
	if err != nil {
		request.WriteError(w, http.StatusInternalServerError, "open ledger: "+err.Error())
		return
	}
	if ledger == nil {
		request.WriteError(w, http.StatusServiceUnavailable, "library unavailable")
		return
	}

	reason := req.Reason
	if reason == "" {
		reason = "operator revoke"
	}
	if err := ledger.MarkRevoked(req.Path, reason); err != nil {
		request.WriteError(w, http.StatusInternalServerError, "mark revoked: "+err.Error())
		return
	}

	// If the revoked workflow was promoted, the SKILL.md needs to
	// come down too — otherwise the agent still sees it via the
	// Claude CLI's auto-discovery and the revoke is meaningless.
	// Best-effort: a failed delete logs but doesn't fail the call.
	skillRemoved := false
	if isWorkflowPathLedger(req.Path) {
		slug := workflowSlugFromLedgerPath(req.Path)
		// office.DeleteSharedSkill is idempotent on missing dirs
		// (os.RemoveAll returns nil for non-existent paths) so
		// we don't need a pre-check.
		if derr := office.DeleteSharedSkill(slug); derr == nil {
			skillRemoved = true
		}
	}

	ctx.SSEBroker.Broadcast("knowledge:revoked", map[string]interface{}{
		"path":         req.Path,
		"reason":       reason,
		"skillRemoved": skillRemoved,
		"timestamp":    time.Now().UnixMilli(),
	})

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"revoked":      req.Path,
		"skillRemoved": skillRemoved,
	})
}

// isWorkflowPathLedger mirrors library.isWorkflowPath without
// importing the unexported helper — duplicated here because cross-
// package access via reflection or a public re-export would muddy
// the library API for one boolean check.
func isWorkflowPathLedger(p string) bool {
	const prefix = "workflows/"
	return strings.HasPrefix(p, prefix) && len(p) > len(prefix)
}

// workflowSlugFromLedgerPath extracts the slug from a workflows/<slug>.md
// path. Mirrors library.workflowSlugFromPath; same dup-vs-export
// trade-off as above.
func workflowSlugFromLedgerPath(p string) string {
	base := filepath.Base(p)
	const ext = ".md"
	if len(base) > len(ext) && strings.HasSuffix(base, ext) {
		return base[:len(base)-len(ext)]
	}
	return base
}

// countMarkdownFiles returns the count of .md files under dir
// recursively. Returns 0 on any I/O error (graceful degradation —
// stats endpoint stays usable even when one subdir is missing).
func countMarkdownFiles(dir string) int {
	count := 0
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".md") {
			count++
		}
		return nil
	})
	return count
}

