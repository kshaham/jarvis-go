package library

// Purge — disk-growth cap for the experience library.
//
// Memify (memify.go) marks experiences past DecayCutoffDays with a
// `valid_until` frontmatter field but never deletes them. Over
// multi-month operation that means experiences/<configID>/
// accumulates thousands of files; PageIndex's tree builder slows
// linearly with corpus size and disk usage grows unbounded.
//
// PurgeExpiredExperiences walks experiences/ and deletes any file
// whose valid_until + PurgeGracePeriod is in the past. Workflows,
// anti-patterns, and standups are NEVER purged here — they're
// consolidated artefacts, low cardinality, and operators expect
// them to stick around.
//
// Safety:
//   - Per-tick cap (MaxPurgePerTick) bounds the slam on disk if a
//     sudden glut of files expires together.
//   - Grace period gives operators a window to inspect a doc before
//     it disappears (default 30 days).
//   - Best-effort: any per-file failure is logged + skipped, not
//     propagated. Next tick retries.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// PurgeOptions controls one purge pass.
type PurgeOptions struct {
	// GracePeriod is the time between an experience's valid_until
	// stamp and its actual deletion. Default 30 days. Lets the
	// operator notice + recover if memify decayed something they
	// still wanted.
	GracePeriod time.Duration

	// MaxPurgePerTick caps how many files one purge pass deletes.
	// Default 100 — keeps the disk slam bounded when many
	// experiences expire on the same tick.
	MaxPurgePerTick int

	// NowFunc lets tests inject a deterministic clock. nil →
	// time.Now.
	NowFunc func() time.Time
}

// PurgeResult bundles what changed in a purge pass.
type PurgeResult struct {
	Scanned int
	Deleted int
	Skipped int // hit the per-tick cap before deleting
	Failed  int // per-file delete error
}

func (o *PurgeOptions) applyDefaults() {
	if o.GracePeriod <= 0 {
		o.GracePeriod = 30 * 24 * time.Hour
	}
	if o.MaxPurgePerTick <= 0 {
		o.MaxPurgePerTick = 100
	}
	if o.NowFunc == nil {
		o.NowFunc = time.Now
	}
}

// PurgeExpiredExperiences walks the experiences directory and
// deletes any file whose valid_until + opts.GracePeriod is in the
// past. Idempotent: re-running with no newly-expired files is a
// no-op.
//
// Returns counters for the memify tick's structured log line.
// Errors during the directory walk return non-nil error; per-file
// errors land in result.Failed and don't fail the call.
func PurgeExpiredExperiences(opts PurgeOptions) (PurgeResult, error) {
	opts.applyDefaults()
	root := Dir()
	if root == "" {
		return PurgeResult{}, nil
	}

	expRoot := filepath.Join(root, "experiences")
	configDirs, err := os.ReadDir(expRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return PurgeResult{}, nil
		}
		return PurgeResult{}, fmt.Errorf("read experiences root: %w", err)
	}

	cutoff := opts.NowFunc()
	result := PurgeResult{}

	for _, cd := range configDirs {
		if !cd.IsDir() {
			continue
		}
		dir := filepath.Join(expRoot, cd.Name())
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
				continue
			}
			if result.Deleted >= opts.MaxPurgePerTick {
				result.Skipped++
				continue
			}
			full := filepath.Join(dir, f.Name())
			result.Scanned++

			validUntil, ok := readValidUntil(full)
			if !ok {
				// No valid_until stamp → memify hasn't decayed
				// this file yet. Leave it alone.
				continue
			}
			deleteAfter := validUntil.Add(opts.GracePeriod)
			if cutoff.Before(deleteAfter) {
				// Past valid_until but inside the grace window.
				continue
			}
			if rerr := os.Remove(full); rerr != nil {
				result.Failed++
				continue
			}
			result.Deleted++
		}
	}
	return result, nil
}

// readValidUntil opens the experience file and pulls the
// `valid_until` frontmatter field. Returns (zero, false) when the
// file can't be opened, lacks frontmatter, or has no
// valid_until line.
func readValidUntil(path string) (time.Time, bool) {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}, false
	}
	defer f.Close()
	fm, _, ok := splitFrontmatter(f)
	if !ok {
		return time.Time{}, false
	}
	raw, present := fm["valid_until"]
	if !present || raw == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}
