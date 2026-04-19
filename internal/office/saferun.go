package office

// R19 · Panic-recovering wrapper for long-running goroutines.
//
// Office flows spawn several long-lived goroutines (standup round
// robin, brainstorm flow, auto-standup timer) that used to propagate
// panics all the way out of process silently — the main server kept
// running but the subsystem stayed permanently dead until restart.
//
// safeRun wraps a function in defer/recover, emits an ERROR log with
// a full stack trace, and broadcasts office:panicRecovered so the
// dashboard shows a toast. The goroutine still ends after the panic;
// the caller is responsible for any restart logic (none of the three
// sites currently restart, matching today's behavior).

import (
	"fmt"
	"log/slog"
	"runtime/debug"
)

// safeRun executes fn with panic recovery. The name is included in
// the error log and the broadcast payload so operators can tell which
// subsystem died.
//
// broadcast may be nil for tests; when present, a panic triggers an
// office:panicRecovered SSE event.
func safeRun(name string, broadcast BroadcastFunc, fn func()) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		stack := string(debug.Stack())
		slog.Error("office goroutine panic",
			"where", name,
			"recover", fmt.Sprint(r),
			"stack", stack)
		if broadcast != nil {
			broadcast("office:panicRecovered", map[string]interface{}{
				"where": name,
				"error": fmt.Sprint(r),
			})
		}
	}()
	fn()
}
