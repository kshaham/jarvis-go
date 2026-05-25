package claudehooks

// Per-event handlers. Each one mirrors a fragment of the SDK event
// forwarder in internal/pty/sdk_session.go and translates the hook
// payload into AgentState mutations + dashboard broadcasts.
//
// All handlers MUST be fast (no I/O beyond the transcript tail-read
// in onStop) because HTTP hooks are blocking on the spawned claude.

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
)

// onSessionStart fires once per session at startup. We just log and
// rely on Jarvis's existing kickstart prompt being delivered through
// the normal stdin path -- we don't want to double-inject the
// kickstart via additionalContext (would duplicate tokens).
//
// Future: if Jarvis's kickstart was ever lost (e.g. a respawn after
// a wedged session), we could detect that here and re-emit it via
// HookSpecificOutput.AdditionalContext. Out of scope for v1.
func (d *Dispatcher) onSessionStart(sessionID string, ev *SessionStartEvent) *HookResponse {
	slog.Info("claudehooks SessionStart",
		"session", sessionID,
		"source", ev.Source,
		"model", ev.Model,
	)
	d.registry.Broadcast("office:claudeSessionStarted", map[string]interface{}{
		"sessionId": sessionID,
		"source":    ev.Source,
		"model":     ev.Model,
	})
	return nil
}

// onUserPromptSubmit marks the agent as working and clears the
// per-turn text buffer (in preparation for fresh assistant output).
// Mirrors the SDK forwarder's EventWorking handler.
func (d *Dispatcher) onUserPromptSubmit(sessionID string, ev *UserPromptSubmitEvent) *HookResponse {
	d.registry.MutateAgent(sessionID, func(a MutableAgent) {
		a.SetSDKWorking(true)
		_ = a.FlushTurnText() // discard any stale buffer from a prior turn
	})
	d.registry.Broadcast("office:agentTurnStarted", map[string]interface{}{
		"sessionId":     sessionID,
		"promptPreview": truncate(ev.Prompt, 200),
	})
	return nil
}

// onPreToolUse does two things:
//  1. Sets ActiveTool so the dashboard reflects what the agent is
//     doing right now (mirrors the SDK's tool_use begin event).
//  2. Consults the active ToolPolicy (if any) and returns a
//     permissionDecision="deny" response when the call matches a
//     denyRule. The deny reason is surfaced back to Claude so the
//     next turn knows why the tool was blocked.
func (d *Dispatcher) onPreToolUse(sessionID string, ev *PreToolUseEvent) *HookResponse {
	d.registry.MutateAgent(sessionID, func(a MutableAgent) {
		a.SetActiveTool(ev.ToolName)
	})
	if d.toolPolicy != nil {
		if denied, reason := d.toolPolicy.Evaluate(ev.ToolName, ev.ToolInput); denied {
			slog.Warn("claudehooks PreToolUse: tool-policy deny",
				"session", sessionID,
				"tool", ev.ToolName,
				"reason", reason,
			)
			d.registry.Broadcast("office:toolDenied", map[string]interface{}{
				"sessionId": sessionID,
				"tool":      ev.ToolName,
				"toolUseId": ev.ToolUseID,
				"reason":    reason,
			})
			return &HookResponse{
				HookSpecificOutput: &HookSpecificOutput{
					HookEventName:            "PreToolUse",
					PermissionDecision:       "deny",
					PermissionDecisionReason: reason,
				},
			}
		}
	}
	return nil
}

// onPostToolUse clears ActiveTool and feeds the tool-loop detector.
// failure flag distinguishes PostToolUse vs PostToolUseFailure -- both
// flow through here because the office only differentiates by tool
// name + result hash, not by success status.
func (d *Dispatcher) onPostToolUse(sessionID string, ev *PostToolUseEvent, failure bool) *HookResponse {
	d.registry.MutateAgent(sessionID, func(a MutableAgent) {
		a.SetActiveTool("")
	})
	if d.observeToolInvocation != nil {
		// resultHash mirrors the SDK forwarder's hashing convention
		// (sha256 of the tool result content). The tool-loop detector
		// keys on (toolName, resultHash) tuples, so this hash just
		// needs to be stable -- any byte representation works.
		hash := sha256.Sum256(ev.ToolResult)
		d.observeToolInvocation(sessionID, ev.ToolName, hex.EncodeToString(hash[:]))
	}
	if failure {
		d.registry.Broadcast("office:agentToolFailure", map[string]interface{}{
			"sessionId": sessionID,
			"tool":      ev.ToolName,
		})
	}
	return nil
}

// onStop is the heaviest handler. Three responsibilities:
//   1. Flip SDKWorking false so the office's recovery layer sees
//      the agent as available.
//   2. Tail-read the transcript JSONL to extract token usage and
//      record it (the SDK's EventTokens equivalent).
//   3. Flush the per-turn text buffer (ignored by hooks today, but
//      kept reset so the buffer never grows unbounded).
//
// Transcript read is synchronous because RecordTurnUsage needs the
// data NOW for the cost rollup. ~64 KB tail read is microsecond-fast
// on any sensible filesystem; we accept the latency because keeping
// the cost accounting accurate is the whole point of hooks parity.
func (d *Dispatcher) onStop(sessionID string, ev *StopEvent) *HookResponse {
	d.registry.MutateAgent(sessionID, func(a MutableAgent) {
		a.SetSDKWorking(false)
		_ = a.FlushTurnText()
	})

	usage, err := ReadLatestUsage(ev.TranscriptPath)
	if err != nil {
		// ErrNoUsage is expected when the turn produced no assistant
		// output (tool-only turns, very rare). Log at debug so it's
		// available without flooding the log.
		if err == ErrNoUsage {
			slog.Debug("claudehooks Stop: no usage in transcript", "session", sessionID)
		} else {
			slog.Warn("claudehooks Stop: transcript read failed",
				"session", sessionID,
				"path", ev.TranscriptPath,
				"err", err,
			)
		}
		return nil
	}

	d.registry.MutateAgent(sessionID, func(a MutableAgent) {
		a.RecordTurnUsage(usage.InputTokens, usage.OutputTokens, usage.CostUSD)
	})
	d.registry.Broadcast("office:tokens", map[string]interface{}{
		"sessionId":         sessionID,
		"inputTokens":       usage.InputTokens,
		"outputTokens":      usage.OutputTokens,
		"cacheCreatedTokens": usage.CacheCreationInputTokens,
		"cacheReadTokens":   usage.CacheReadInputTokens,
		"costUsd":           usage.CostUSD,
		"model":             usage.Model,
	})
	return nil
}

// onSessionEnd routes through the office's existing agent-death
// path (task reassignment, dev-instance cleanup, broadcasts). The
// underlying claude process exit detection still fires too -- this
// is belt-and-suspenders for the case where the process exits
// cleanly but the supervisor hasn't noticed yet.
func (d *Dispatcher) onSessionEnd(sessionID string, ev *SessionEndEvent) *HookResponse {
	slog.Info("claudehooks SessionEnd", "session", sessionID, "reason", ev.Reason)
	d.registry.HandleAgentDeath(sessionID)
	return nil
}

// onNotification surfaces Claude Code's notifications (permission
// requests, "needs your input", etc) to the dashboard without
// mutating AgentState. The dashboard can then display a banner and
// the operator decides what to do.
func (d *Dispatcher) onNotification(sessionID string, ev *NotificationEvent) *HookResponse {
	d.registry.Broadcast("office:claudeNotification", map[string]interface{}{
		"sessionId": sessionID,
		"message":   ev.Message,
	})
	return nil
}

// onCompact broadcasts the compaction event so operators see a
// deterministic context-fill marker rather than guessing from raw
// output bytes. event is "PreCompact" or "PostCompact".
func (d *Dispatcher) onCompact(sessionID, event string, ev *CompactEvent) *HookResponse {
	d.registry.Broadcast("office:claudeCompact", map[string]interface{}{
		"sessionId": sessionID,
		"event":     event, // "PreCompact" | "PostCompact"
		"trigger":   ev.Trigger,
	})
	return nil
}

// onPostToolBatch fires after a batch of parallel tool calls
// resolves. Used as a finer-grained cost-tracking pivot than Stop:
// every batch produces a broadcast the dashboard can render as a
// per-batch token spend rollup, even when an agent runs dozens of
// batches inside a single Stop window. No AgentState mutation -- the
// authoritative token totals still come from onStop's transcript
// tail-read.
func (d *Dispatcher) onPostToolBatch(sessionID string, ev *PostToolBatchEvent) *HookResponse {
	d.registry.Broadcast("office:agentBatchCompleted", map[string]interface{}{
		"sessionId": sessionID,
		"batchSize": len(ev.ToolCalls),
		"tools":     summariseBatchTools(ev.ToolCalls),
	})
	return nil
}

// onStopFailure surfaces API-error turn terminations to the dashboard.
// ErrorType is the documented matcher value (rate_limit, billing_error,
// oauth_org_not_allowed, model_not_found, server_error,
// max_output_tokens, invalid_request, authentication_failed, unknown).
//
// We do NOT auto-reassign tasks here. SessionEnd fires too on a fatal
// failure and handles that path; keeping the two events separate
// avoids double-handling.
func (d *Dispatcher) onStopFailure(sessionID string, ev *StopFailureEvent) *HookResponse {
	slog.Warn("claudehooks StopFailure",
		"session", sessionID,
		"errorType", ev.ErrorType,
		"errorMessage", ev.ErrorMessage,
	)
	d.registry.MutateAgent(sessionID, func(a MutableAgent) {
		a.SetSDKWorking(false)
	})
	d.registry.Broadcast("office:agentTurnError", map[string]interface{}{
		"sessionId":    sessionID,
		"errorType":    ev.ErrorType,
		"errorMessage": ev.ErrorMessage,
	})
	return nil
}

// onPermissionRequest is a tripwire for jarvis's bypass-mode agents.
// Seeing a permission dialog means bypass mode is unexpectedly off
// (e.g. settings change at runtime). Log + broadcast so the operator
// can investigate. We do NOT return a permissionDecision -- letting
// the dialog reach the operator is the safe default; if the operator
// disabled bypass deliberately, jarvis shouldn't silently re-enable it.
func (d *Dispatcher) onPermissionRequest(sessionID string, ev *PermissionRequestEvent) *HookResponse {
	slog.Warn("claudehooks PermissionRequest",
		"session", sessionID,
		"tool", ev.ToolName,
	)
	d.registry.Broadcast("office:agentPermissionPrompt", map[string]interface{}{
		"sessionId": sessionID,
		"tool":      ev.ToolName,
		"toolUseId": ev.ToolUseID,
	})
	return nil
}

// onPermissionDenied surfaces auto-mode classifier denials. Like
// onPermissionRequest, this is anomalous for jarvis agents (which run
// in bypass mode). When the classifier denies a tool we set retry=true
// so the model can try again with a different approach rather than
// hard-failing the turn.
func (d *Dispatcher) onPermissionDenied(sessionID string, ev *PermissionDeniedEvent) *HookResponse {
	slog.Warn("claudehooks PermissionDenied",
		"session", sessionID,
		"tool", ev.ToolName,
	)
	d.registry.Broadcast("office:agentPermissionDenied", map[string]interface{}{
		"sessionId": sessionID,
		"tool":      ev.ToolName,
		"toolUseId": ev.ToolUseID,
	})
	return &HookResponse{
		HookSpecificOutput: &HookSpecificOutput{
			HookEventName: "PermissionDenied",
			Retry:         true,
		},
	}
}

// onTaskCreated mirrors Claude's internal subagent task lists into the
// office activity stream so operators see what the agent is grinding
// through without opening the transcript. These are NOT office tasks
// -- they're per-turn TaskCreate tool calls that help the model track
// its own multi-step work.
func (d *Dispatcher) onTaskCreated(sessionID string, ev *TaskCreatedEvent) *HookResponse {
	d.registry.Broadcast("office:agentInternalTaskCreated", map[string]interface{}{
		"sessionId":   sessionID,
		"taskName":    ev.TaskName,
		"description": ev.TaskDescription,
	})
	return nil
}

func (d *Dispatcher) onTaskCompleted(sessionID string, ev *TaskCompletedEvent) *HookResponse {
	d.registry.Broadcast("office:agentInternalTaskCompleted", map[string]interface{}{
		"sessionId": sessionID,
		"taskName":  ev.TaskName,
	})
	return nil
}

// onSubagentStart / onSubagentStop drive the subagent lifecycle hooks.
// R3.2 wires these into internal/subagent/watcher.go via the optional
// subagentStart / subagentStop callbacks so the dashboard sees the
// lifecycle as soon as the hook fires (instead of waiting for the
// next 10-second filesystem-scan tick). Filesystem polling stays as
// the fallback for SDK-mode sessions without hooks.
func (d *Dispatcher) onSubagentStart(sessionID string, ev *SubagentStartEvent) *HookResponse {
	if d.subagentStart != nil {
		d.subagentStart(sessionID, ev.AgentType, ev.AgentID)
	}
	d.registry.Broadcast("subagent:discovered", map[string]interface{}{
		"sessionId": sessionID,
		"agentType": ev.AgentType,
		"agentId":   ev.AgentID,
	})
	return nil
}

func (d *Dispatcher) onSubagentStop(sessionID string, ev *SubagentStopEvent) *HookResponse {
	if d.subagentStop != nil {
		d.subagentStop(sessionID, ev.AgentID)
	}
	d.registry.Broadcast("subagent:completed", map[string]interface{}{
		"sessionId": sessionID,
		"agentType": ev.AgentType,
		"agentId":   ev.AgentID,
	})
	return nil
}

// onCwdChanged keeps the dashboard in sync with where the agent
// actually is, not just where it spawned.
func (d *Dispatcher) onCwdChanged(sessionID string, ev *CwdChangedEvent) *HookResponse {
	d.registry.Broadcast("office:agentCwdChanged", map[string]interface{}{
		"sessionId": sessionID,
		"previous":  ev.PreviousCwd,
		"new":       ev.NewCwd,
	})
	return nil
}

// onInstructionsLoaded surfaces which instructions (CLAUDE.md,
// .claude/rules/*.md) are in context per agent. Useful for "why did
// the agent ignore X?" debugging. We don't store the list anywhere
// today; broadcast + log only.
func (d *Dispatcher) onInstructionsLoaded(sessionID string, ev *InstructionsLoadedEvent) *HookResponse {
	d.registry.Broadcast("office:agentInstructionsLoaded", map[string]interface{}{
		"sessionId":  sessionID,
		"filePath":   ev.FilePath,
		"memoryType": ev.MemoryType,
		"loadReason": ev.LoadReason,
	})
	return nil
}

// onConfigChange logs settings-file changes during a session. Audit
// only -- we don't restore overridden settings. Useful for "did the
// operator change the model mid-task?" forensics.
func (d *Dispatcher) onConfigChange(sessionID string, ev *ConfigChangeEvent) *HookResponse {
	slog.Info("claudehooks ConfigChange",
		"session", sessionID,
		"source", ev.ConfigSource,
		"changes", ev.Changes,
	)
	d.registry.Broadcast("office:agentConfigChanged", map[string]interface{}{
		"sessionId": sessionID,
		"source":    ev.ConfigSource,
		"changes":   ev.Changes,
	})
	return nil
}

// onElicitation surfaces MCP server input requests. Future work
// (R3.x): pattern-match the elicitation form against `cm dev creds`
// stored credentials and auto-fill matching fields. For now operators
// see the prompt in the dashboard and intervene manually.
func (d *Dispatcher) onElicitation(sessionID string, ev *ElicitationEvent) *HookResponse {
	d.registry.Broadcast("office:elicitation", map[string]interface{}{
		"sessionId":     sessionID,
		"serverName":    ev.ServerName,
		"tool":          ev.ToolName,
		"elicitationId": ev.ElicitationID,
	})
	return nil
}

// onElicitationResult is audit-only.
func (d *Dispatcher) onElicitationResult(sessionID string, ev *ElicitationResultEvent) *HookResponse {
	d.registry.Broadcast("office:elicitationResult", map[string]interface{}{
		"sessionId":     sessionID,
		"serverName":    ev.ServerName,
		"elicitationId": ev.ElicitationID,
		"action":        ev.Action,
	})
	return nil
}

// onWorktreeCreate fires when Claude itself creates a worktree (via
// `--worktree` if an operator opts in manually, or via a Task-tool
// spawn that requests isolation). R3.1 keeps jarvis's own worktree
// creation in place -- the settings-injection race makes a full
// migration to `--worktree` impractical -- but observes Claude-created
// worktrees here so AgentState.WorktreePath stays current. Useful for
// Phase R4 (`claude attach`/`logs`/`stop`) which keys off the path.
func (d *Dispatcher) onWorktreeCreate(sessionID string, ev *WorktreeCreateEvent) *HookResponse {
	previous := ""
	d.registry.MutateAgent(sessionID, func(a MutableAgent) {
		previous = a.SetWorktreePath(ev.WorktreePath)
	})
	if previous != ev.WorktreePath {
		slog.Info("claudehooks WorktreeCreate",
			"session", sessionID,
			"previousPath", previous,
			"newPath", ev.WorktreePath,
			"branch", ev.WorktreeBranch,
		)
	}
	d.registry.Broadcast("office:agentWorktreeCreated", map[string]interface{}{
		"sessionId": sessionID,
		"path":      ev.WorktreePath,
		"branch":    ev.WorktreeBranch,
	})
	return nil
}

// onWorktreeRemove fires when Claude tears down a worktree it created.
// We don't clear AgentState.WorktreePath here -- jarvis owns the
// authoritative path and the session might keep using it; a Task-tool
// inner worktree removal shouldn't bash the outer cwd.
func (d *Dispatcher) onWorktreeRemove(sessionID string, ev *WorktreeRemoveEvent) *HookResponse {
	slog.Info("claudehooks WorktreeRemove",
		"session", sessionID,
		"path", ev.WorktreePath,
		"reason", ev.RemoveReason,
	)
	d.registry.Broadcast("office:agentWorktreeRemoved", map[string]interface{}{
		"sessionId": sessionID,
		"path":      ev.WorktreePath,
		"reason":    ev.RemoveReason,
	})
	return nil
}

// summariseBatchTools collapses a tool-call slice into a short slice
// of unique tool names so SSE event payloads stay small.
func summariseBatchTools(calls []ToolCallSummary) []string {
	seen := make(map[string]struct{}, len(calls))
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		if _, ok := seen[c.ToolName]; ok {
			continue
		}
		seen[c.ToolName] = struct{}{}
		out = append(out, c.ToolName)
	}
	return out
}

// truncate caps a string at n runes for safe logging/broadcasting
// without sending the operator a megabyte of prompt text on every
// turn.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
