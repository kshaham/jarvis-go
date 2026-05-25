package office

// memify_tick.go — bridges the office package to the periodic
// memify + auto-promotion job in internal/library.
//
// The idle-monitor tick in recovery.go calls (*Manager).maybeRunMemify
// once per tick. The helper here gates execution by elapsed wall
// time (memifyInterval) so the costly disk walk + workflow writes
// only run a few times an hour even when the idle ticker is short.
//
// Promotion uses office.WriteSharedSkill as the SkillWriter
// callback. Keeps library decoupled from office (no import cycle —
// library never sees office; office passes a function pointer in).

import (
	"log/slog"
	"time"

	"github.com/claudeman/jarvis-go/internal/library"
	"github.com/claudeman/jarvis-go/internal/safego"
)

// memifyInterval is the minimum wall-clock spacing between memify
// runs. 6h matches the original plan target. Smaller values waste
// I/O without surfacing meaningfully fresher patterns; larger
// values delay self-improvement after a busy day.
const memifyInterval = 6 * time.Hour

// maybeRunMemify runs a memify + promotion pass when the gate
// timer has expired. Safe to call every idle tick — the gate is
// the cheap path and pays the disk + write cost only when due.
//
// Wraps the actual work in safego so a panic inside memify or
// promotion can't crash the idle-monitor goroutine. The recovery
// tick already has its own panic recover, but library writes are
// I/O-heavy and the extra layer is essentially free.
//
// Returns immediately if the library directory is unavailable
// (graceful degradation matching the rest of the integration).
func (m *Manager) maybeRunMemify() {
	if library.Dir() == "" {
		return
	}
	m.mu.Lock()
	due := m.memifyLastRunAt.IsZero() || time.Since(m.memifyLastRunAt) >= memifyInterval
	if !due {
		m.mu.Unlock()
		return
	}
	m.memifyLastRunAt = time.Now()
	m.mu.Unlock()

	safego.Go("office.memifyTick", m.runMemifyTick)
}

// runMemifyTick is the actual work — memify pass + promotion pass +
// per-promotion SSE broadcast. Separated from maybeRunMemify so
// tests can call it directly without poking the gate timer.
func (m *Manager) runMemifyTick() {
	// Memify: consolidate raw experiences into workflow markdowns.
	mres, err := library.RunMemify(library.MemifyOptions{})
	if err != nil {
		slog.Warn("memify pass failed", "err", err)
		return
	}
	if mres.WorkflowsWritten > 0 {
		slog.Info("memify wrote workflows",
			"count", mres.WorkflowsWritten,
			"slugs", mres.WrittenSlugs,
			"experiences_scanned", mres.ExperiencesScanned)
	}

	// Promotion: graduate workflows whose ledger stats clear the
	// threshold to ~/.claude/skills/.
	ledger, err := library.OpenLedger()
	if err != nil {
		slog.Warn("memify: open ledger failed", "err", err)
		return
	}
	if ledger == nil {
		return
	}
	pres, err := library.RunPromotion(library.PromoteOptions{}, ledger, m.promoteSkillWriter)
	if err != nil {
		slog.Warn("promotion pass failed", "err", err)
		return
	}
	if len(pres.Promoted) > 0 {
		slog.Info("knowledge auto-promotion", "summary", pres.Summary(), "promoted", pres.Promoted)
		for _, p := range pres.Promoted {
			m.broadcast("knowledge:autoPromoted", map[string]interface{}{
				"path":      p,
				"timestamp": time.Now().UnixMilli(),
			})
		}
	}
	if len(pres.Failed) > 0 {
		slog.Warn("knowledge promotion had failures", "failed", pres.Failed)
	}
}

// promoteSkillWriter is the SkillWriter callback library invokes
// when a workflow graduates. Just delegates to the existing
// WriteSharedSkill helper — keeps the on-disk skill layout
// identical to what the shared-skills handler already produces.
//
// Method on Manager (not a free function) so future variants can
// inspect manager state (e.g. team-level opt-out flag).
func (m *Manager) promoteSkillWriter(name, body string) error {
	_, _, err := WriteSharedSkill(name, body)
	return err
}
