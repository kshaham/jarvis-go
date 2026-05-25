// Package safego provides a tiny helper for launching goroutines
// that survive panics. A bare `go fn()` that panics takes down the
// whole Go process — for a long-lived daemon like jarvis, a single
// panic anywhere in any background loop is an unattended-operation
// killer.
//
// Use Go(name, fn) for any goroutine that:
//   - runs longer than a single HTTP request, OR
//   - touches code that could plausibly panic (nil deref, map race,
//     external library), OR
//   - lives in a long-running tick / watcher / forwarder loop.
//
// One-line spawns of pure-Go small helpers that you have audited end
// to end are fine to keep as bare `go ...`. When in doubt, use Go —
// the cost is one allocation.
package safego

import (
	"log/slog"
	"runtime/debug"
)

// Go launches fn in a new goroutine wrapped in panic recovery. A
// panic is logged at error level with the supplied name + the
// captured panic value + a stack trace, but does NOT propagate up
// and crash the daemon.
//
// Returns nothing — fire-and-forget. For goroutines whose lifecycle
// the caller needs to track, the caller should still own its own
// sync.WaitGroup or channel; Go just guards the panic surface.
//
// name should identify the spawn site uniquely enough for an
// operator to find it in logs: e.g., "office.codegraph.init",
// "respawn.stuckDetector", "office.telegram.periodicUpdates".
func Go(name string, fn func()) {
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("safego: goroutine panicked",
					"name", name,
					"panic", rec,
					"stack", string(debug.Stack()))
			}
		}()
		fn()
	}()
}
