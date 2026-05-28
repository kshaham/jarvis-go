package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/kshaham/jarvis/internal/logscrub"
	"github.com/kshaham/jarvis/internal/web/request"
)

// R4.2 · OfficeSpawnRemote shells out to `claude --remote "<task>"` so
// the mobile app can offload a task to a claude.ai web session. The
// CLI prints the resulting session URL on stdout (and a structured
// JSON line when --json is set); we parse both shapes so the same
// handler keeps working if the operator's `claude` install is a
// version that hasn't grown JSON output yet.
//
// Why shell-out (vs. the SDK): --remote is a CLI orchestration
// command that creates a web session on Anthropic's hosted runtime.
// The SDK has no programmatic equivalent — the only contract is the
// CLI flag, so shelling out is the only path.
//
// POST /api/office/spawn-remote
//
//	{"task": "Refactor the auth module", "title": "auth refactor"}
//
// Response:
//
//	{"url": "https://claude.ai/chat/<id>", "sessionId": "<id>"}
//
// Errors: 400 for empty task, 502 when the CLI exits non-zero or
// emits no parseable URL, 504 when the call exceeds the timeout.

// spawnRemoteBinary is the CLI invoked. Variable (not const) so tests
// can swap in a stub. Same pattern as oneshot.go's oneShotBinary.
var spawnRemoteBinary = "claude"

// maxSpawnRemoteTaskLen caps the post-trim task length. The CLI accepts
// the task as a positional arg, so it ends up on the spawn's argv —
// excessively large strings here either fail the OS exec limit or
// signal client abuse (whitespace-stuffed body to evade the 16 KB
// JSON cap). 4 KB is generous for a one-shot task description.
const maxSpawnRemoteTaskLen = 4096

// maxSpawnRemoteTitleLen mirrors the task cap for the optional --title
// flag value. P7.3.11 — parallel hardening so neither argv slot can
// smuggle an outsized payload past the JSON body cap.
const maxSpawnRemoteTitleLen = 256

// spawnRemoteTimeout caps the shell-out. The CLI uploads + handshakes
// with claude.ai, so allow more headroom than a local oneshot but
// less than a long-running session.
var spawnRemoteTimeout = 60 * time.Second

// claudeAiURLRe captures the first https://claude.ai/... URL the CLI
// prints. Used as a fallback when the response isn't structured JSON.
var claudeAiURLRe = regexp.MustCompile(`https://claude\.ai/[A-Za-z0-9/_\-]+`)

type spawnRemoteRequest struct {
	Task  string `json:"task"`
	Title string `json:"title,omitempty"`
}

type spawnRemoteResponse struct {
	URL       string `json:"url"`
	SessionID string `json:"sessionId,omitempty"`
}

func (ctx *Context) OfficeSpawnRemote(w http.ResponseWriter, r *http.Request) {
	var req spawnRemoteRequest
	if !request.DecodeAndValidateMax(w, r, &req, 1<<14) {
		return
	}
	task := strings.TrimSpace(req.Task)
	if task == "" {
		request.WriteError(w, http.StatusBadRequest, "task is required")
		return
	}
	// Cap post-trim task length so a body padded with whitespace can't
	// smuggle a huge command line through DecodeAndValidateMax's 16 KB
	// body limit. 4 KB is generous for a one-shot task description and
	// keeps the resulting argv far below typical OS exec limits.
	if len(task) > maxSpawnRemoteTaskLen {
		request.WriteError(w, http.StatusBadRequest, "task too long (max 4096 chars)")
		return
	}
	title := strings.TrimSpace(req.Title)
	if len(title) > maxSpawnRemoteTitleLen {
		request.WriteError(w, http.StatusBadRequest, "title too long (max 256 chars)")
		return
	}

	url, sessionID, err := runSpawnRemote(r.Context(), task, title)
	if err != nil {
		if errors.Is(err, errSpawnRemoteTimeout) {
			request.WriteError(w, http.StatusGatewayTimeout, "claude --remote timed out")
			return
		}
		// IMPORTANT: do NOT echo err.Error() to the HTTP body. The CLI
		// stderr captured inside spawnRemoteCLIError can contain
		// `Authorization: Bearer ...` headers from Claude's auth
		// debugging output. Log the full error server-side; hand the
		// client a generic message that doesn't disclose credentials
		// or stack frames.
		slog.Warn("spawn-remote failed", "err", err)
		request.WriteError(w, http.StatusBadGateway, "remote session creation failed; see server logs")
		return
	}

	if ctx.SSEBroker != nil {
		ctx.SSEBroker.Broadcast("office:remoteSessionCreated", map[string]interface{}{
			"url":       url,
			"sessionId": sessionID,
			"task":      task,
		})
	}
	slog.Info("remote claude session created", "url", url, "sessionId", sessionID)
	request.WriteJSON(w, http.StatusOK, spawnRemoteResponse{
		URL:       url,
		SessionID: sessionID,
	})
}

var errSpawnRemoteTimeout = errors.New("spawn-remote: timed out")

func runSpawnRemote(ctx context.Context, task, title string) (string, string, error) {
	callCtx, cancel := context.WithTimeout(ctx, spawnRemoteTimeout)
	defer cancel()

	args := []string{"--remote", "--json"}
	if title != "" {
		args = append(args, "--title", title)
	}
	args = append(args, task)

	cmd := exec.CommandContext(callCtx, spawnRemoteBinary, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if callCtx.Err() == context.DeadlineExceeded {
			return "", "", errSpawnRemoteTimeout
		}
		return "", "", &spawnRemoteCLIError{Err: err, Stderr: logscrub.All(truncateLog(stderr.String()))}
	}

	out := stdout.Bytes()
	// Prefer the structured response when present — newer CLI builds
	// emit a single JSON object on stdout when --json is set.
	var structured struct {
		URL       string `json:"url"`
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out), &structured); err == nil && structured.URL != "" {
		return structured.URL, structured.SessionID, nil
	}

	// Fall back to regex over the raw stdout. Best-effort: the CLI
	// frequently prints a banner / hint lines before the URL.
	match := claudeAiURLRe.FindString(string(out))
	if match == "" {
		return "", "", errors.New("spawn-remote: no claude.ai URL in CLI output")
	}
	// Derive a best-guess session ID from the trailing path segment so
	// the caller has something to key on.
	parts := strings.Split(match, "/")
	sid := ""
	if len(parts) > 0 {
		sid = parts[len(parts)-1]
	}
	return match, sid, nil
}

type spawnRemoteCLIError struct {
	Err    error
	Stderr string
}

func (e *spawnRemoteCLIError) Error() string {
	if e.Stderr != "" {
		return "spawn-remote CLI failed: " + e.Err.Error() + "; stderr=" + e.Stderr
	}
	return "spawn-remote CLI failed: " + e.Err.Error()
}

// Unwrap exposes the underlying exec.Command error so callers can
// errors.Is against context.DeadlineExceeded or other wrapped
// sentinels without parsing the string. Lets us keep richer context
// in the wrapper without losing standard error-handling semantics.
func (e *spawnRemoteCLIError) Unwrap() error { return e.Err }

func truncateLog(s string) string {
	const max = 2048
	if len(s) <= max {
		return s
	}
	return s[:max] + "…(truncated)"
}
