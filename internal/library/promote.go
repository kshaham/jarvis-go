package library

// Auto-promotion of high-reuse workflows into the shared skills
// library. When a workflow's ledger entry shows it's been cited
// often AND those citations resulted in good outcomes, jarvis
// graduates the workflow doc to `~/.claude/skills/<slug>/SKILL.md`
// where Claude CLI auto-discovers it. From that point on every
// agent (including ones in fresh projects) benefits from the
// pattern.
//
// Operator override: any promotion can be revoked via
// Ledger.MarkRevoked. Memify's next pass deletes the on-disk skill
// file (best-effort — operator can also `rm` it directly). A 24h
// cool-down prevents re-promotion of revoked entries.
//
// The promotion job takes a writeSkill callback so library stays
// decoupled from internal/office's WriteSharedSkill. The office
// caller wires the two together.

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// PromoteOptions controls a promotion pass.
type PromoteOptions struct {
	// MinReuseCount: ledger entry's ReuseCount must be ≥ this
	// before promotion. Default 5.
	MinReuseCount int

	// MinAvgOutcome: ledger entry's AvgOutcome must be ≥ this.
	// Default 0.75.
	MinAvgOutcome float64

	// RevokeCoolDown: a revoked entry can't be re-promoted until
	// this duration has passed since the revocation. Default 24h.
	RevokeCoolDown time.Duration

	// NowFunc lets tests inject a deterministic clock. nil →
	// time.Now.
	NowFunc func() time.Time
}

// PromoteResult bundles what changed in a promotion pass. Paths
// are relative to the library root.
type PromoteResult struct {
	Promoted []string // paths that graduated to ~/.claude/skills/
	Skipped  []string // paths considered but blocked by a threshold
	Failed   []string // paths whose write attempt failed
}

func (o *PromoteOptions) applyDefaults() {
	if o.MinReuseCount <= 0 {
		o.MinReuseCount = 5
	}
	if o.MinAvgOutcome <= 0 {
		o.MinAvgOutcome = 0.75
	}
	if o.RevokeCoolDown <= 0 {
		o.RevokeCoolDown = 24 * time.Hour
	}
	if o.NowFunc == nil {
		o.NowFunc = time.Now
	}
}

// SkillWriter is the callback the promotion job invokes when a
// workflow graduates. Wraps office.WriteSharedSkill at the call
// site so library doesn't import office (cycle prevention).
type SkillWriter func(name, body string) error

// RunPromotion scans the ledger for entries that meet the
// promotion thresholds and graduates them via writeSkill.
//
// writeSkill nil is treated as a dry-run: the ledger isn't updated
// and no on-disk skill file is created, but the result still
// reports which entries WOULD have been promoted. Useful for
// telemetry + the operator's "what's about to be promoted?" view.
func RunPromotion(opts PromoteOptions, ledger *Ledger, writeSkill SkillWriter) (PromoteResult, error) {
	opts.applyDefaults()
	root := Dir()
	if root == "" {
		return PromoteResult{}, nil
	}
	if ledger == nil {
		return PromoteResult{}, nil
	}

	entries, err := ledger.List()
	if err != nil {
		return PromoteResult{}, err
	}

	now := opts.NowFunc()
	result := PromoteResult{}

	for _, e := range entries {
		// Skip already-promoted entries (memify can re-promote if
		// revoked + cool-down expired).
		if !e.PromotedAt.IsZero() {
			continue
		}

		// Threshold check.
		if e.ReuseCount < opts.MinReuseCount {
			result.Skipped = append(result.Skipped, e.Path)
			continue
		}
		if e.AvgOutcome < opts.MinAvgOutcome {
			result.Skipped = append(result.Skipped, e.Path)
			continue
		}

		// Revocation cool-down.
		if !e.RevokedAt.IsZero() {
			coolEnd := e.RevokedAt.Add(opts.RevokeCoolDown)
			if now.Before(coolEnd) {
				result.Skipped = append(result.Skipped, e.Path)
				continue
			}
		}

		// Read the underlying workflow file. Only workflows graduate
		// — bare experiences aren't suitable as a SKILL.md (they're
		// task-specific, not pattern-level). Skip experience paths
		// silently.
		if !isWorkflowPath(e.Path) {
			result.Skipped = append(result.Skipped, e.Path)
			continue
		}
		fullPath := filepath.Join(root, e.Path)
		body, rerr := os.ReadFile(fullPath)
		if rerr != nil {
			result.Failed = append(result.Failed, e.Path)
			continue
		}

		// Slug = filename without .md, sanitized.
		slug := sanitize(workflowSlugFromPath(e.Path))

		// Dry-run mode (writeSkill nil) records the intended
		// promotion without performing any side effects.
		if writeSkill == nil {
			result.Promoted = append(result.Promoted, e.Path)
			continue
		}

		if werr := writeSkill(slug, string(body)); werr != nil {
			result.Failed = append(result.Failed, e.Path)
			continue
		}

		if merr := ledger.MarkPromoted(e.Path); merr != nil {
			// Skill is on disk but the ledger update failed; treat
			// as a successful promotion (operator can revoke
			// manually if needed). Log via the result instead of
			// returning so other promotions still get processed.
			result.Failed = append(result.Failed, e.Path+" (skill written, ledger update failed)")
			continue
		}

		result.Promoted = append(result.Promoted, e.Path)
	}

	return result, nil
}

// isWorkflowPath reports whether the ledger path points at a
// workflow doc (eligible for promotion). Experience paths and
// future categories are filtered out — only workflows should
// graduate to shared skills.
func isWorkflowPath(p string) bool {
	return len(p) > len("workflows/") && p[:len("workflows/")] == "workflows/"
}

// workflowSlugFromPath strips the directory + ".md" suffix from a
// workflows/<slug>.md ledger path.
func workflowSlugFromPath(p string) string {
	base := filepath.Base(p)
	const ext = ".md"
	if len(base) > len(ext) && base[len(base)-len(ext):] == ext {
		base = base[:len(base)-len(ext)]
	}
	return base
}

// String makes PromoteResult log-friendly. Not in fmt.Stringer to
// keep the type a plain struct for JSON encoding.
func (r PromoteResult) Summary() string {
	return fmt.Sprintf("promoted=%d skipped=%d failed=%d",
		len(r.Promoted), len(r.Skipped), len(r.Failed))
}
