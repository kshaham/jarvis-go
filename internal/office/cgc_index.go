package office

import (
	"context"
	"log/slog"
	"os/exec"
	"time"

	"github.com/kshaham/jarvis/internal/safego"
)

// CGC (CodeGraphContext, the Python MCP server) and codegraph (the
// Rust MCP server) are two SEPARATE tools wired into every spawned
// agent's MCP map. Each indexes the workspace for its own server.
// EnsureCodegraphIndex (codegraph.go) is the codegraph one; this file
// is the cgc counterpart.
//
// Both are fire-and-forget at spawn time: the agent may attempt
// queries against the MCP server before the index is ready, in which
// case the server returns a "not initialised" error and the agent
// retries on subsequent turns. Indexing in the background avoids
// stalling the spawn flow on disk-heavy work.

// cgcIndexTimeout caps the index run. 5 minutes is generous for
// repositories under ~100k files; longer indexes log a warning but
// don't block jarvis itself.
const cgcIndexTimeout = 5 * time.Minute

// cgcIndexLogTruncate is the byte cap for cgc subprocess output we
// keep in the warn log. Avoids dumping multi-MB stack traces from a
// crash into the daemon's slog stream.
const cgcIndexLogTruncate = 200

// RunCGCIndex runs `cgc index <workingDir>` in a detached goroutine.
// Pure background work — no Context state, no SSE broadcast. Failure
// is non-fatal: the spawn proceeds and the MCP server reports its
// own readiness on subsequent agent queries.
//
// If the cgc binary isn't on PATH, the command's err is logged at
// warn level and the function returns silently. Operators who don't
// use codegraphcontext can ignore the warning or remove the binary
// from their image entirely.
func RunCGCIndex(workingDir string) {
	// safego: cgc subprocess can hang or panic on filesystem races;
	// the wrapper keeps a panic local to this goroutine instead of
	// taking down the daemon.
	safego.Go("office.cgc.index", func() {
		cgcCtx, cancel := context.WithTimeout(context.Background(), cgcIndexTimeout)
		defer cancel()
		cmd := exec.CommandContext(cgcCtx, "cgc", "index", workingDir)
		cmd.Dir = workingDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			outStr := string(out)
			if len(outStr) > cgcIndexLogTruncate {
				outStr = outStr[:cgcIndexLogTruncate]
			}
			slog.Warn("CGC index failed (non-fatal)", "error", err, "output", outStr, "workingDir", workingDir)
			return
		}
		slog.Info("CGC index complete", "workingDir", workingDir)
	})
}
