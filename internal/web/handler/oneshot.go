package handler

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/claudeman/jarvis-go/internal/pty"
	"github.com/claudeman/jarvis-go/internal/web/request"
)

// One-shot session timeouts.
const (
	oneShotDefaultTimeout = 2 * time.Minute
	oneShotMaxTimeout     = 10 * time.Minute
)

// oneShotRequest is the body shape for POST /api/sessions/one-shot.
type oneShotRequest struct {
	Prompt         string `json:"prompt"`
	WorkingDir     string `json:"workingDir"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
	Mode           string `json:"mode"` // "shell" (default) or "claude"
}

// OneShotSession runs a single prompt in an ephemeral session, waits for the
// session to go idle (or the timeout elapses), captures the output, stops the
// session, and returns the captured output. Enables scripted use:
//
//	curl -X POST /api/sessions/one-shot \
//	     -H 'Content-Type: application/json' \
//	     -d '{"prompt":"ls -la","workingDir":"/tmp"}' | jq .output
//
// POST /api/sessions/one-shot
func (ctx *Context) OneShotSession(w http.ResponseWriter, r *http.Request) {
	if ctx.IsSpendingFrozen() {
		request.WriteCodedError(w, http.StatusTooManyRequests, request.ErrCodeSpendingFrozen,
			"daily spending cap reached; one-shot refused until the cap is raised or the day rolls over")
		return
	}

	var req oneShotRequest
	if !request.DecodeAndValidate(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		request.WriteError(w, http.StatusBadRequest, "prompt is required")
		return
	}

	// Default working directory from settings, same pattern as CreateSession.
	workingDir := req.WorkingDir
	if workingDir == "" {
		settings := ctx.StateStore.GetSettings()
		if v, _ := settings["defaultWorkingDir"].(string); v != "" {
			workingDir = v
		}
	}
	if workingDir == "" {
		request.WriteError(w, http.StatusBadRequest, "workingDir is required (or set defaultWorkingDir in settings)")
		return
	}
	resolved, err := ctx.ResolveWorkingDir(workingDir)
	if err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid working directory: "+err.Error())
		return
	}
	workingDir = resolved

	timeout := oneShotDefaultTimeout
	if req.TimeoutSeconds > 0 {
		d := time.Duration(req.TimeoutSeconds) * time.Second
		if d > oneShotMaxTimeout {
			d = oneShotMaxTimeout
		}
		timeout = d
	}

	mode := pty.ModeShell
	if req.Mode == "claude" {
		mode = pty.ModeClaude
	}

	providerEnv := ctx.getProviderEnvVars()
	session, err := ctx.SessionManager.CreateSession(
		context.Background(),
		pty.SessionConfig{
			WorkingDir:  workingDir,
			Mode:        mode,
			Name:        "one-shot",
			ProviderEnv: providerEnv,
		},
	)
	if err != nil {
		request.WriteError(w, http.StatusInternalServerError, "failed to create session: "+err.Error())
		return
	}

	// Unconditional cleanup: the session must be stopped even if the caller
	// disconnects or we time out.
	defer func() {
		if err := ctx.SessionManager.StopSession(context.Background(), session.ID); err != nil {
			slog.Warn("one-shot cleanup failed to stop session", "session", session.ID, "error", err)
		}
		ctx.cleanupSessionResources(session.ID)
	}()

	// Small grace period for the session to boot before we send the prompt.
	time.Sleep(200 * time.Millisecond)

	// Capture the starting offset so we only return output generated *after*
	// the prompt was submitted.
	startOffset := len(session.TerminalBuffer())

	if !session.WriteViaScreen(req.Prompt + "\r") {
		request.WriteError(w, http.StatusInternalServerError, "failed to send prompt to session")
		return
	}

	started := time.Now()
	deadline := started.Add(timeout)

	// Wait for the session to become idle. Polls the session status with a
	// short interval; falls back to the deadline.
	for {
		if session.Status() == pty.StatusIdle && time.Since(started) > 500*time.Millisecond {
			break
		}
		if time.Now().After(deadline) {
			buf := session.TerminalBuffer()
			output := ""
			if startOffset < len(buf) {
				output = buf[startOffset:]
			}
			request.WriteJSON(w, http.StatusGatewayTimeout, map[string]interface{}{
				"timeout":    true,
				"output":     output,
				"durationMs": time.Since(started).Milliseconds(),
				"message":    "one-shot exceeded timeout; partial output returned",
			})
			return
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Session is idle. Capture the slice of terminal buffer that arrived
	// since the prompt went out.
	buf := session.TerminalBuffer()
	output := ""
	if startOffset < len(buf) {
		output = buf[startOffset:]
	}

	inputTokens, outputTokens, totalTokens := session.Tokens()
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"output":     output,
		"durationMs": time.Since(started).Milliseconds(),
		"timeout":    false,
		"tokens": map[string]int64{
			"input":  inputTokens,
			"output": outputTokens,
			"total":  totalTokens,
		},
	})
}
