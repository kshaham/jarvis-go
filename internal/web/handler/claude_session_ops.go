package handler

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/kshaham/jarvis/internal/logscrub"
	"github.com/kshaham/jarvis/internal/strs"
	"github.com/kshaham/jarvis/internal/web/request"
)

// R4.4 · Operator-facing wrappers for `claude logs` / `claude stop`
// against background-session IDs we already track. `claude attach`
// stays client-side (the dashboard renders an xterm.js terminal and
// runs the command in-browser via the existing PTY bridge) — server
// side just hands back the command string operators / the frontend
// can launch.
//
// All three operate on the Claude-side session ID (returned by
// SDKSession.ClaudeSessionID()), NOT jarvis's own session ID. We map
// jarvis -> claude up-front so callers can pass either.

// claudeSessionOpsBinary is the CLI invoked. Variable so tests can
// swap in a stub.
var claudeSessionOpsBinary = "claude"

// claudeSessionOpsTimeout caps individual operator commands. Logs may
// be large so the timeout is generous; the call itself just reads
// from claude's local store.
var claudeSessionOpsTimeout = 30 * time.Second

// OfficeClaudeLogs runs `claude logs <claudeSessionId>` and returns the
// raw output as text/plain. The browser typically renders this in a
// modal viewer.
//
// GET /api/office/claude-sessions/{sessionId}/logs
func (ctx *Context) OfficeClaudeLogs(w http.ResponseWriter, r *http.Request) {
	cid, err := ctx.resolveClaudeSessionID(chi.URLParam(r, "sessionId"))
	if err != nil {
		request.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	out, runErr := runClaudeSessionCmd(r.Context(), "logs", cid)
	if runErr != nil {
		slog.Warn("claude logs failed", "sessionId", cid, "err", runErr)
		request.WriteError(w, http.StatusBadGateway, runErr.Error())
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(out)
}

// OfficeClaudeStop runs `claude stop <claudeSessionId>`. Returns
// {success:true} when the CLI exits 0. The local jarvis session may
// still be considered alive after this — operators run /api/sessions/
// {id} DELETE to fully tear down jarvis-side state.
//
// POST /api/office/claude-sessions/{sessionId}/stop
func (ctx *Context) OfficeClaudeStop(w http.ResponseWriter, r *http.Request) {
	cid, err := ctx.resolveClaudeSessionID(chi.URLParam(r, "sessionId"))
	if err != nil {
		request.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	_, runErr := runClaudeSessionCmd(r.Context(), "stop", cid)
	if runErr != nil {
		slog.Warn("claude stop failed", "sessionId", cid, "err", runErr)
		request.WriteError(w, http.StatusBadGateway, runErr.Error())
		return
	}
	ctx.emit("office:claudeSessionStopped", map[string]interface{}{
		"claudeSessionId": cid,
	})
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":         true,
		"claudeSessionId": cid,
	})
}

// OfficeClaudeAttachCommand returns the shell command the operator (or
// the dashboard's xterm.js embed) should run to attach to the
// session. Stays server-side so future changes to argv shape (e.g.
// adding --terminal-mode) flow without a UI deploy.
//
// GET /api/office/claude-sessions/{sessionId}/attach-command
func (ctx *Context) OfficeClaudeAttachCommand(w http.ResponseWriter, r *http.Request) {
	cid, err := ctx.resolveClaudeSessionID(chi.URLParam(r, "sessionId"))
	if err != nil {
		request.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"claudeSessionId": cid,
		"command":         claudeSessionOpsBinary,
		"args":            []string{"attach", cid},
	})
}

// sessionIDRe is the lenient format every valid jarvis sessionId or
// Claude session ID conforms to today: UUIDs, short alphanumeric
// slugs, hyphen-separated. The 8-char floor rules out empty/placeholder
// inputs and forces a minimum entropy threshold; the 128-char ceiling
// keeps argv length sane on every OS we support. Anything outside
// this shape is almost certainly a typo or an injection attempt
// (`..`, `/etc/passwd`, etc.) and never reaches the exec layer.
// sessionIDRe is retained as an alias for strs.SessionIDPattern so
// existing call sites stay unchanged after the cross-package extract.
var sessionIDRe = strs.SessionIDPattern

// resolveClaudeSessionID accepts either a jarvis sessionId or a raw
// Claude session ID. When the input matches a live jarvis SDK
// session, the captured ClaudeSessionID is returned; otherwise the
// input is returned verbatim (the operator may know the Claude ID
// directly). Format-validates against sessionIDRe BEFORE any lookup
// so a malformed string can't reach exec.Command via the pass-through
// branch.
func (ctx *Context) resolveClaudeSessionID(input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", errors.New("sessionId is required")
	}
	if !sessionIDRe.MatchString(input) {
		return "", errors.New("sessionId must match [A-Za-z0-9_-]{8,128}")
	}
	if ctx.SessionManager != nil {
		if sdk, ok := ctx.SessionManager.GetSDKSession(input); ok {
			if cid := sdk.ClaudeSessionID(); cid != "" {
				return cid, nil
			}
			return "", errors.New("session has not produced a Claude session ID yet")
		}
	}
	return input, nil
}

func runClaudeSessionCmd(ctx context.Context, sub, sessionID string) ([]byte, error) {
	callCtx, cancel := context.WithTimeout(ctx, claudeSessionOpsTimeout)
	defer cancel()
	cmd := exec.CommandContext(callCtx, claudeSessionOpsBinary, sub, sessionID)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if callCtx.Err() == context.DeadlineExceeded {
			return nil, errors.New("claude " + sub + " timed out")
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, errors.New("claude " + sub + " failed: " + logscrub.All(msg))
	}
	return stdout.Bytes(), nil
}
