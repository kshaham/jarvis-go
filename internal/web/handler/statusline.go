package handler

import (
	"fmt"
	"net/http"
	"strings"
)

// R2.3 · GET /api/office/agent/statusline?sessionId=<id>
// Returns a single-line plain-text statusline for the requesting agent.
// Format (subject to UX iteration):
//   <name> · <status> · task <taskTitle> · <blockerCount> blocker[s] team-wide
//
// Designed for the statusline.sh script jarvis writes into every agent
// worktree at spawn time (see internal/office/statusline.go). The
// response is intentionally one short line — Claude's statusline
// renders in a single row at the bottom of the prompt.

func (ctx *Context) OfficeAgentStatusLine(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimSpace(r.URL.Query().Get("sessionId"))
	if sessionID == "" {
		// Don't 400 — the operator would see an ugly red line in their
		// terminal. Return a placeholder so the script renders cleanly.
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, "[office] (no session)")
		return
	}
	if ctx.OfficeManager == nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, "[office] (manager offline)")
		return
	}

	// O(1) lookup — GetAgent reads the agents map under RLock.
	// Previously this walked the full roster to find the requesting agent.
	a := ctx.OfficeManager.GetAgent(sessionID)
	var name, status, role, tier, currentTaskTitle string
	if a != nil {
		cfg := a.SnapshotConfig()
		name = cfg.Name
		role = cfg.Role
		tier = cfg.Tier
		if a.GetSDKWorking() {
			status = "working"
		} else {
			status = "idle"
		}
		if active := a.ActiveTaskCopy(); active != nil {
			currentTaskTitle = active.Title
		}
	}
	if name == "" {
		// Agent not registered yet. Statusline still renders something
		// usable rather than a blank line.
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, "[office] (agent not registered)")
		return
	}

	// O(1) read — maintained atomically by applyTaskStatusLocked /
	// RemoveTask whenever a task transitions into or out of TaskBlocked.
	// Previously this walked all agents * tasks on every shell prompt.
	blockerCount := int(ctx.OfficeManager.BlockedTaskCount())
	paused := ctx.OfficeManager.IsPaused()

	parts := []string{
		fmt.Sprintf("[office] %s", name),
	}
	if role != "" {
		parts = append(parts, role)
	}
	if tier == "strategic" {
		parts = append(parts, "lead")
	}
	if status != "" {
		parts = append(parts, status)
	}
	if currentTaskTitle != "" {
		parts = append(parts, fmt.Sprintf("on \"%s\"", truncateRune(currentTaskTitle, 40)))
	}
	if blockerCount > 0 {
		noun := "blocker"
		if blockerCount > 1 {
			noun = "blockers"
		}
		parts = append(parts, fmt.Sprintf("%d %s team-wide", blockerCount, noun))
	}
	if paused {
		parts = append(parts, "⏸ paused")
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintln(w, strings.Join(parts, " · "))
}

// truncateRune caps a string at n runes so multi-byte characters
// aren't sliced mid-codepoint. Keeps the statusline visually stable
// regardless of task title length.
func truncateRune(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}
