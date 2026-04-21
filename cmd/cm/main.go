// cm is the Jarvis agent CLI for server communication.
// Agents in office sessions use this to interact with the Jarvis server
// (submit standup responses, report task progress, send messages, etc.)
//
// Environment variables:
//
//	JARVIS_API_URL    - Server URL (e.g., http://localhost:3555)
//	JARVIS_SESSION_ID - Current session ID
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

var (
	apiURL    string
	sessionID string
	client    = &http.Client{Timeout: 15 * time.Second}
)

func init() {
	apiURL = os.Getenv("JARVIS_API_URL")
	sessionID = os.Getenv("JARVIS_SESSION_ID")
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(0)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	// Standup
	case "standup":
		handleStandup(args)

	// Brainstorm
	case "brainstorm":
		handleBrainstorm(args)

	// Leader tasks
	case "leader-tasks":
		handleLeaderTasks(args)

	// Reflect
	case "reflect":
		handleReflect(args)

	// Discover
	case "discover":
		handleDiscover(args)

	// Task management
	case "task":
		handleTask(args)

	// File locks
	case "lock":
		requireArgs(args, 1, "cm lock <filePath>")
		post("/api/office/agent/file-lock", map[string]interface{}{
			"sessionId": sessionID, "filePath": args[0], "action": "lock",
		})
		fmt.Printf("Locked: %s\n", args[0])

	case "unlock":
		requireArgs(args, 1, "cm unlock <filePath>")
		post("/api/office/agent/file-lock", map[string]interface{}{
			"sessionId": sessionID, "filePath": args[0], "action": "unlock",
		})
		fmt.Printf("Unlocked: %s\n", args[0])

	case "locks":
		result := get("/api/office/agent/file-locks")
		printJSON(result)

	// Roster
	case "roster":
		result := get("/api/office/agent/team-roster")
		printJSON(result)

	// Message
	case "message":
		requireArgs(args, 2, "cm message <toSessionId> <content>")
		post("/api/office/agent/message", map[string]interface{}{
			"fromSessionId": sessionID, "toSessionId": args[0], "content": strings.Join(args[1:], " "),
		})
		fmt.Println("Message sent.")

	// Skill
	case "skill":
		handleSkill(args)

	// Memory
	case "memory":
		handleMemory(args)

	// Team dev instance (shared browser verification URL)
	case "dev":
		handleDev(args)

	// Unified team knowledge search (R17)
	case "knowledge", "know":
		handleKnowledge(args)

	// R31 · Onboarding reference -- print a topic's full content on
	// demand so the kickstart doesn't have to ship it. Topics:
	// protocols, browser, dev-instance, dev-lead (strategic only),
	// context-budget, skills, memory.
	case "onboard", "onboarding":
		handleOnboard(args)

	// Help
	case "help", "--help", "-h":
		printUsage()

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

// uploadTaskScreenshot reads a PNG / JPEG / WebP from disk,
// base64-encodes it, and POSTs to the task-screenshot endpoint.
// R34 · Evidence attachment for `cm task complete --screenshot`.
func uploadTaskScreenshot(taskID, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	// Very small mime sniff based on the file signature -- avoids
	// requiring the caller to pass the mime type.
	mime := "image/png"
	if len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		mime = "image/jpeg"
	} else if len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		mime = "image/webp"
	}
	post("/api/office/agent/task/screenshot", map[string]interface{}{
		"sessionId":  sessionID,
		"taskId":     taskID,
		"mimeType":   mime,
		"dataBase64": base64.StdEncoding.EncodeToString(data),
	})
	return nil
}

func handleOnboard(args []string) {
	if len(args) < 1 {
		fatal("Usage: cm onboard <topic>\nTopics: protocols, browser, dev-instance, dev-lead, context-budget, skills, memory")
	}
	topic := args[0]
	// Plain-text endpoint -- bypass the JSON-expecting `get` helper.
	requireEnv()
	req, err := http.NewRequest("GET",
		fmt.Sprintf("%s/api/office/agent/onboarding?sessionId=%s&topic=%s",
			apiURL, url.QueryEscape(sessionID), url.QueryEscape(topic)),
		nil)
	if err != nil {
		fatal("HTTP error: " + err.Error())
	}
	addAgentAuth(req)
	resp, err := client.Do(req)
	if err != nil {
		fatal("HTTP error: " + err.Error())
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		fatal(fmt.Sprintf("Error %d: %s", resp.StatusCode, string(body)))
	}
	fmt.Println(string(body))
}

func handleStandup(args []string) {
	if len(args) < 2 || args[0] != "respond" {
		fatal("Usage: cm standup respond <content>")
	}
	content := strings.Join(args[1:], " ")
	post("/api/office/agent/standup/response", map[string]interface{}{
		"sessionId": sessionID, "content": content,
	})
	fmt.Println("Standup response submitted.")
}

func handleBrainstorm(args []string) {
	if len(args) < 2 || args[0] != "respond" {
		fatal("Usage: cm brainstorm respond <proposals>")
	}
	proposals := strings.Join(args[1:], " ")
	post("/api/office/agent/brainstorm/response", map[string]interface{}{
		"sessionId": sessionID, "proposals": proposals,
	})
	fmt.Println("Brainstorm proposals submitted.")
}

func handleLeaderTasks(args []string) {
	if len(args) < 2 || args[0] != "respond" {
		fatal("Usage: cm leader-tasks respond <content>")
	}
	content := strings.Join(args[1:], " ")
	post("/api/office/agent/leader-tasks/response", map[string]interface{}{
		"sessionId": sessionID, "content": content,
	})
	fmt.Println("Leader task assignments submitted.")
}

func handleReflect(args []string) {
	if len(args) < 1 {
		fatal("Usage: cm reflect <learning> --category <cat> [--task-id <id>] [--task-title <title>]")
	}
	learning, opts := parseArgsAndFlags(args)
	category := opts["category"]
	if category == "" {
		fatal("--category is required")
	}
	body := map[string]interface{}{
		"sessionId": sessionID, "learning": learning, "category": category,
	}
	if v := opts["task-id"]; v != "" {
		body["taskId"] = v
	}
	if v := opts["task-title"]; v != "" {
		body["taskTitle"] = v
	}
	post("/api/office/agent/reflection/response", body)
	fmt.Println("Reflection submitted.")
}

func handleDiscover(args []string) {
	if len(args) < 1 {
		fatal("Usage: cm discover <summary> --findings <item1> <item2> ... [--self-assigned <task>] [--committed]")
	}
	summary, opts := parseArgsAndFlags(args)
	findings := strings.Split(opts["findings"], ",")
	body := map[string]interface{}{
		"sessionId":   sessionID,
		"summary":     summary,
		"discoveries": findings,
		"ledToCommit": opts["committed"] == "true",
	}
	if v := opts["self-assigned"]; v != "" {
		body["selfAssignedTask"] = v
	}
	post("/api/office/agent/curiosity/discovery", body)
	fmt.Println("Discovery reported.")
}

func handleTask(args []string) {
	if len(args) < 1 {
		fatal("Usage: cm task <start|complete|ack|progress|blocked|list|self-add|triage|handoff|handoff-accept|handoff-reject|handoff-cancel|label> ...")
	}
	subcmd := args[0]
	subargs := args[1:]

	switch subcmd {
	case "start":
		requireArgs(subargs, 1, "cm task start <taskId>")
		post("/api/office/agent/task-start", map[string]interface{}{"sessionId": sessionID, "taskId": subargs[0]})
		fmt.Printf("Task %s marked as in-progress.\n", subargs[0])

	case "complete":
		requireArgs(subargs, 1, "cm task complete <taskId> [--summary \"<2-3 sentence handoff>\"]")
		// Parse optional --summary flag. Positional args are joined; the first
		// positional token is the task ID, anything else is appended to the
		// summary if --summary wasn't supplied.
		positional, flags := parseArgsAndFlags(subargs)
		fields := strings.Fields(positional)
		if len(fields) == 0 {
			fatal("cm task complete <taskId> [--summary \"...\"]")
		}
		taskID := fields[0]
		summary := flags["summary"]
		if summary == "" && len(fields) > 1 {
			summary = strings.Join(fields[1:], " ")
		}
		// R34 · Optional --screenshot <path> uploads a PNG/JPEG/WebP
		// to the server as concrete visual evidence. The server's
		// soft-gate suppresses the "UI task missing verification"
		// warning when an attachment is present. Upload BEFORE the
		// complete call so the gate sees the attachment.
		if sPath := flags["screenshot"]; sPath != "" {
			if err := uploadTaskScreenshot(taskID, sPath); err != nil {
				fmt.Fprintf(os.Stderr, "warning: --screenshot upload failed (%v); continuing with complete\n", err)
			} else {
				fmt.Fprintf(os.Stderr, "attached screenshot %s to task %s\n", sPath, taskID)
			}
		}
		// Append the summary to memory section task_history BEFORE marking
		// the task complete, so a failure on the complete call still leaves
		// the handoff durable.
		if summary != "" {
			ts := time.Now().UTC().Format("2006-01-02T15:04Z")
			line := fmt.Sprintf("- [%s] task %s: %s", ts, taskID, summary)
			post("/api/office/agent/memory/section", map[string]interface{}{
				"sessionId": sessionID,
				"name":      "task_history",
				"content":   line,
				"append":    true,
			})
		} else {
			fmt.Fprintln(os.Stderr, "warning: cm task complete called without --summary; future-you will not know what you did. See `cm skill read office-task-handoff`.")
		}
		// Send the summary as `result` so the server's soft screenshot gate
		// can scan it for browser-verification evidence. Without this the
		// gate scans an empty string and EVERY UI-keyword task-complete
		// fires the warning regardless of whether the agent cited a
		// screenshot in their summary.
		completeResp := postBody("/api/office/agent/task-complete", map[string]interface{}{
			"sessionId": sessionID,
			"taskId":    taskID,
			"result":    summary,
		})
		fmt.Printf("Task %s marked as complete.\n", taskID)
		// Surface the server's soft-gate warning (e.g. UI-keyword task
		// completed without visible browser verification). Previously the
		// CLI discarded the response body, so agents had no feedback loop
		// and kept violating the rule.
		if m, ok := completeResp.(map[string]interface{}); ok {
			if warn, ok := m["warning"].(string); ok && warn != "" {
				fmt.Fprintln(os.Stderr, "warning: "+warn)
			}
			if already, ok := m["alreadyComplete"].(bool); ok && already {
				fmt.Fprintln(os.Stderr, "note: task was already complete; re-call was a no-op.")
			}
		}

	case "ack":
		requireArgs(subargs, 1, "cm task ack <taskId>")
		post("/api/office/agent/task-ack", map[string]interface{}{"sessionId": sessionID, "taskId": subargs[0]})
		fmt.Printf("Task %s acknowledged.\n", subargs[0])

	case "progress":
		requireArgs(subargs, 2, "cm task progress <taskId> <note>")
		post("/api/office/agent/task-progress", map[string]interface{}{
			"sessionId": sessionID, "taskId": subargs[0], "progress": strings.Join(subargs[1:], " "),
		})
		fmt.Printf("Progress reported for task %s.\n", subargs[0])

	case "blocked":
		requireArgs(subargs, 2, "cm task blocked <taskId> <reason>")
		post("/api/office/agent/task-progress", map[string]interface{}{
			"sessionId": sessionID, "taskId": subargs[0],
			"progress": strings.Join(subargs[1:], " "), "isBlocked": true,
			"blockerDescription": strings.Join(subargs[1:], " "),
		})
		fmt.Printf("Task %s marked as blocked.\n", subargs[0])

	case "list":
		result := get(fmt.Sprintf("/api/office/agent/tasks?sessionId=%s", url.QueryEscape(sessionID)))
		printJSON(result)

	case "self-add":
		// R36 · Accept repeated --label flags. parseArgsAndFlags
		// overwrites on repeat, so scan manually for --label pairs
		// while letting it handle the rest (positional + priority).
		var labels []string
		filtered := make([]string, 0, len(subargs))
		for i := 0; i < len(subargs); i++ {
			if subargs[i] == "--label" && i+1 < len(subargs) {
				labels = append(labels, subargs[i+1])
				i++
				continue
			}
			filtered = append(filtered, subargs[i])
		}
		positional, _ := parseArgsAndFlags(filtered)
		fields := strings.Fields(positional)
		if len(fields) < 2 {
			fatal("cm task self-add <title> <description> [priority] [--label <name>]...")
		}
		priority := "medium"
		if len(fields) > 2 {
			priority = fields[2]
		}
		body := map[string]interface{}{
			"sessionId": sessionID, "title": fields[0], "description": fields[1], "priority": priority,
		}
		if len(labels) > 0 {
			body["labels"] = labels
		}
		post("/api/office/agent/task/self-add", body)
		fmt.Printf("Self-generated task added: %s\n", fields[0])

	// R36 · cm task label add/remove <taskId> <label>
	case "label":
		if len(subargs) < 3 {
			fatal("Usage: cm task label <add|remove> <taskId> <label>")
		}
		action := subargs[0]
		taskID := subargs[1]
		label := subargs[2]
		body := map[string]interface{}{
			"sessionId": sessionID,
			"taskId":    taskID,
		}
		switch action {
		case "add":
			body["add"] = []string{label}
		case "remove":
			body["remove"] = []string{label}
		default:
			fatal("Usage: cm task label <add|remove> <taskId> <label>")
		}
		post("/api/office/agent/task/labels", body)
		fmt.Printf("Label %q %sed on task %s.\n", label, action, taskID)

	case "triage":
		requireArgs(subargs, 3, "cm task triage <taskId> <toSessionId> <reason>")
		post("/api/office/agent/task/triage", map[string]interface{}{
			"taskId": subargs[0], "sessionId": sessionID,
			"toSessionId": subargs[1], "reason": strings.Join(subargs[2:], " "),
		})
		fmt.Printf("Task %s triaged to %s.\n", subargs[0], subargs[1])

	// R29 · Structured agent-to-agent handoff.
	case "handoff":
		requireArgs(subargs, 3, "cm task handoff <taskId> <toConfigId> <context...>")
		post("/api/office/agent/task/handoff", map[string]interface{}{
			"sessionId":  sessionID,
			"taskId":     subargs[0],
			"toConfigId": subargs[1],
			"context":    strings.Join(subargs[2:], " "),
		})
		fmt.Printf("Handoff of task %s to %s initiated. They accept with `cm task handoff-accept %s` or reject with `cm task handoff-reject %s \"<reason>\"`.\n",
			subargs[0], subargs[1], subargs[0], subargs[0])

	case "handoff-accept":
		requireArgs(subargs, 1, "cm task handoff-accept <taskId>")
		post("/api/office/agent/task/handoff/accept", map[string]interface{}{
			"sessionId": sessionID,
			"taskId":    subargs[0],
		})
		fmt.Printf("Handoff accepted. Task %s is now yours.\n", subargs[0])

	case "handoff-reject":
		requireArgs(subargs, 1, "cm task handoff-reject <taskId> [<reason>]")
		reason := ""
		if len(subargs) > 1 {
			reason = strings.Join(subargs[1:], " ")
		}
		post("/api/office/agent/task/handoff/reject", map[string]interface{}{
			"sessionId": sessionID,
			"taskId":    subargs[0],
			"reason":    reason,
		})
		fmt.Printf("Handoff of task %s rejected.\n", subargs[0])

	// R32 · sender-initiated cancel. Retract a pending handoff before
	// the receiver accepts or rejects. Only the original sender can
	// cancel (server returns 403 otherwise).
	case "handoff-cancel":
		requireArgs(subargs, 1, "cm task handoff-cancel <taskId>")
		post("/api/office/agent/task/handoff/cancel", map[string]interface{}{
			"sessionId": sessionID,
			"taskId":    subargs[0],
		})
		fmt.Printf("Handoff of task %s cancelled; task is back on your queue.\n", subargs[0])

	default:
		fatal("Unknown task subcommand: " + subcmd)
	}
}

// handleKnowledge dispatches `cm knowledge <search|tail>`.
// Aliases: `cm know search ...` for typing convenience.
func handleKnowledge(args []string) {
	if len(args) < 1 {
		fatal("Usage: cm knowledge <search|tail> [args]")
	}
	switch args[0] {
	case "search":
		query, limit, allProjects := parseKnowledgeArgs(args[1:])
		if query == "" {
			fatal("Usage: cm knowledge search <query> [--limit N] [--all]")
		}
		path := fmt.Sprintf("/api/office/agent/knowledge/search?q=%s&limit=%d",
			url.QueryEscape(query), limit)
		if allProjects {
			path += "&all=true"
		}
		result := get(path)
		printKnowledgeHits(result)
	case "tail":
		_, limit, allProjects := parseKnowledgeArgs(args[1:])
		path := fmt.Sprintf("/api/office/agent/knowledge/tail?limit=%d", limit)
		if allProjects {
			path += "&all=true"
		}
		result := get(path)
		printKnowledgeHits(result)
	default:
		fatal("Unknown knowledge subcommand: " + args[0] + " (expected: search|tail)")
	}
}

// parseKnowledgeArgs pulls --limit N + --all from mixed positional args.
// Remaining positional tokens join into the free-text query.
func parseKnowledgeArgs(args []string) (query string, limit int, allProjects bool) {
	limit = 20
	var words []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "--limit", "-n":
			if i+1 >= len(args) {
				fatal(a + " requires a value")
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil || n <= 0 {
				fatal(a + " must be a positive integer")
			}
			limit = n
			i++
		case "--all", "--all-projects":
			allProjects = true
		default:
			if strings.HasPrefix(a, "--") {
				fatal("unknown flag: " + a)
			}
			words = append(words, a)
		}
	}
	query = strings.Join(words, " ")
	return
}

// printKnowledgeHits renders the server's hits array in a compact,
// tty-friendly form. One line per hit: age, source/agent, excerpt.
func printKnowledgeHits(result interface{}) {
	m, ok := result.(map[string]interface{})
	if !ok {
		printJSON(result)
		return
	}
	hitsAny, _ := m["hits"].([]interface{})
	if len(hitsAny) == 0 {
		fmt.Println("(no matches)")
		return
	}
	now := time.Now()
	for _, h := range hitsAny {
		hm, ok := h.(map[string]interface{})
		if !ok {
			continue
		}
		source, _ := hm["source"].(string)
		agent, _ := hm["agentName"].(string)
		section, _ := hm["section"].(string)
		excerpt, _ := hm["excerpt"].(string)
		tsStr, _ := hm["timestamp"].(string)
		age := ""
		if tsStr != "" {
			if t, err := time.Parse(time.RFC3339Nano, tsStr); err == nil {
				age = formatAge(now.Sub(t))
			}
		}
		srcTag := source
		if agent != "" {
			srcTag = source + "/" + agent
		}
		if section != "" && section != "notes" {
			srcTag += " (" + section + ")"
		}
		if age != "" {
			fmt.Printf("[%s] %s\n  %s\n", age, srcTag, excerpt)
		} else {
			fmt.Printf("%s\n  %s\n", srcTag, excerpt)
		}
	}
}

// formatAge renders a duration compactly (e.g. "12s", "3m", "2h", "1d").
func formatAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// handleDev dispatches `cm dev <announce|url|clear|list|creds>`. Wraps
// /api/office/agent/dev-instance + /dev-credentials so leads can announce
// the dev server URL + login creds for one or more slots ("app" by
// default; fullstack teams can use --name for "frontend"/"api"/etc.).
func handleDev(args []string) {
	if len(args) < 1 {
		fatal("Usage: cm dev <announce|url|clear|list|creds> [args]")
	}
	switch args[0] {
	case "url":
		// Positional slot name, optional — defaults to "app".
		// --probe flag triggers a client-side HEAD request against
		// the returned URL so the agent sees live reachability
		// instead of relying on the 30s health-tick cache.
		slot := "app"
		probe := false
		for _, a := range args[1:] {
			if a == "--probe" {
				probe = true
				continue
			}
			if !strings.HasPrefix(a, "--") {
				slot = a
			}
		}
		result := get("/api/office/agent/dev-instance?name=" + url.QueryEscape(slot))
		var devURL string
		if m, ok := result.(map[string]interface{}); ok {
			devURL, _ = m["url"].(string)
		}
		if devURL == "" {
			fmt.Println("")
			return
		}
		fmt.Println(devURL)
		if probe {
			probeClient := &http.Client{Timeout: 5 * time.Second}
			req, err := http.NewRequest(http.MethodHead, devURL, nil)
			if err != nil {
				fmt.Fprintf(os.Stderr, "probe: build request: %v\n", err)
				return
			}
			resp, err := probeClient.Do(req)
			if err != nil {
				fmt.Fprintf(os.Stderr, "probe: %v (dev server may be down; ask the lead to respawn)\n", err)
				return
			}
			_ = resp.Body.Close()
			if resp.StatusCode >= 500 {
				fmt.Fprintf(os.Stderr, "probe: HTTP %d -- dev server reachable but erroring\n", resp.StatusCode)
				return
			}
			fmt.Fprintf(os.Stderr, "probe: HTTP %d -- ready to screenshot\n", resp.StatusCode)
		}
	case "list":
		result := get("/api/office/agent/dev-instance/all")
		printJSON(result)
	case "announce":
		slot, u, pid, force := parseAnnounceArgs(args[1:])
		if u == "" {
			fatal("Usage: cm dev announce <url> [--name <slot>] [--pid <n>] [--force]  (e.g. cm dev announce http://jarvis:3001 --pid 1234)")
		}
		if !force && (strings.Contains(u, "localhost") || strings.Contains(u, "127.0.0.1")) {
			fatal(fmt.Sprintf("%q is localhost-only; headless-shell agents can't reach it. Rebind your dev server to 0.0.0.0 and use http://jarvis:<port>, or pass --force to override.", u))
		}
		body := map[string]interface{}{"name": slot, "url": u, "pid": pid}
		path := "/api/office/agent/dev-instance"
		if force {
			path += "?force=true"
		}
		post(path, body)
		if force {
			fmt.Fprintf(os.Stderr, "warning: announced %q with --force; teammates may get connection refused.\n", u)
		}
		slotLabel := ""
		if slot != "" && slot != "app" {
			slotLabel = fmt.Sprintf(" slot=%q", slot)
		}
		if pid > 0 {
			fmt.Printf("Dev URL announced: %s%s (pid=%d, Jarvis will clean up on wrap-up / lead death)\n", u, slotLabel, pid)
		} else {
			fmt.Printf("Dev URL announced: %s%s (no PID tracked; remember to `cm dev clear` + kill the process yourself on wrap-up)\n", u, slotLabel)
		}
	case "clear":
		// --all wipes every slot; otherwise a positional name (default "app").
		if len(args) > 1 && args[1] == "--all" {
			del("/api/office/agent/dev-instance?all=true")
			fmt.Println("All dev URLs cleared.")
			return
		}
		slot := "app"
		if len(args) > 1 && !strings.HasPrefix(args[1], "--") {
			slot = args[1]
		}
		del("/api/office/agent/dev-instance?name=" + url.QueryEscape(slot))
		fmt.Printf("Dev URL cleared (slot=%q).\n", slot)
	case "creds":
		handleDevCreds(args[1:])
	default:
		fatal("Unknown dev subcommand: " + args[0])
	}
}

// parseAnnounceArgs extracts URL + slot + optional --pid / --force flags
// from `cm dev announce` args. Slot defaults to "app"; flags may appear
// in any order.
// parseAnnounceArgs is the thin wrapper around parseAnnounceArgsE that
// escalates validation errors to `fatal` (the CLI's terminate-with-usage
// helper). Keeping the error-returning variant separate lets main_test.go
// exercise every edge case without intercepting os.Exit.
func parseAnnounceArgs(args []string) (slot, url string, pid int, force bool) {
	slot, url, pid, force, err := parseAnnounceArgsE(args)
	if err != nil {
		fatal(err.Error())
	}
	return
}

// parseAnnounceArgsE is the pure, testable implementation. Returns an
// error for any validation failure instead of calling fatal. Slot
// defaults to "app". Flags may appear in any order relative to the
// positional URL.
func parseAnnounceArgsE(args []string) (slot, url string, pid int, force bool, err error) {
	slot = "app"
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "--force":
			force = true
		case "--pid":
			if i+1 >= len(args) {
				return "", "", 0, false, fmt.Errorf("--pid requires a value")
			}
			v, aerr := strconv.Atoi(args[i+1])
			if aerr != nil || v < 0 {
				return "", "", 0, false, fmt.Errorf("--pid must be a non-negative integer, got %q", args[i+1])
			}
			pid = v
			i++
		case "--name":
			if i+1 >= len(args) {
				return "", "", 0, false, fmt.Errorf("--name requires a value")
			}
			slot = args[i+1]
			i++
		default:
			if strings.HasPrefix(a, "--") {
				return "", "", 0, false, fmt.Errorf("unknown flag: %s", a)
			}
			if url == "" {
				url = a
			}
		}
	}
	return slot, url, pid, force, nil
}

// handleDevCreds dispatches `cm dev creds [set <text...>|clear] [--name <slot>]`.
func handleDevCreds(args []string) {
	// Strip out --name flag so the remaining positional args form the
	// subcommand + text blob. Default slot is "app".
	slot := "app"
	cleaned := []string{}
	for i := 0; i < len(args); i++ {
		if args[i] == "--name" {
			if i+1 >= len(args) {
				fatal("--name requires a value")
			}
			slot = args[i+1]
			i++
			continue
		}
		cleaned = append(cleaned, args[i])
	}

	if len(cleaned) == 0 {
		// Read
		result := get("/api/office/agent/dev-credentials?name=" + url.QueryEscape(slot))
		if m, ok := result.(map[string]interface{}); ok {
			if c, ok := m["credentials"].(string); ok {
				fmt.Println(c)
				return
			}
		}
		fmt.Println("")
		return
	}
	switch cleaned[0] {
	case "set":
		if len(cleaned) < 2 {
			fatal("Usage: cm dev creds set <text...>  (e.g. cm dev creds set \"admin@dev.local / password123\")")
		}
		blob := strings.Join(cleaned[1:], " ")
		post("/api/office/agent/dev-credentials",
			map[string]interface{}{"name": slot, "credentials": blob})
		fmt.Printf("Dev credentials set (slot=%q).\n", slot)
	case "clear":
		post("/api/office/agent/dev-credentials",
			map[string]interface{}{"name": slot, "credentials": ""})
		fmt.Printf("Dev credentials cleared (slot=%q).\n", slot)
	default:
		fatal("Unknown creds subcommand: " + cleaned[0] + " (expected: set|clear, or no args to read)")
	}
}

func handleSkill(args []string) {
	if len(args) < 1 {
		fatal("Usage: cm skill <list|read|create|search> ...")
	}
	switch args[0] {
	case "list":
		printJSON(get("/api/office/agent/skills"))
	case "read":
		requireArgs(args[1:], 1, "cm skill read <name>")
		printJSON(get(fmt.Sprintf("/api/office/agent/skills/%s", url.PathEscape(args[1]))))
	case "create":
		requireArgs(args[1:], 2, "cm skill create <name> <content>")
		post("/api/office/agent/skill-create", map[string]interface{}{
			"skillName": args[1], "content": strings.Join(args[2:], " "),
		})
		fmt.Printf("Skill %q created.\n", args[1])
	case "search":
		requireArgs(args[1:], 1, "cm skill search <query>")
		printJSON(get(fmt.Sprintf("/api/office/agent/skills/search?q=%s", url.QueryEscape(args[1]))))
	default:
		fatal("Unknown skill subcommand: " + args[0])
	}
}

func handleMemory(args []string) {
	if len(args) < 1 {
		fatal("Usage: cm memory <read|write|append|section|query> ...")
	}
	switch args[0] {
	case "read":
		printJSON(get(fmt.Sprintf("/api/office/agent/memory?sessionId=%s", url.QueryEscape(sessionID))))
	case "write":
		requireArgs(args[1:], 1, "cm memory write <content>")
		post("/api/office/agent/memory", map[string]interface{}{
			"sessionId": sessionID, "content": strings.Join(args[1:], " "),
		})
		fmt.Println("Memory written.")
	case "append":
		requireArgs(args[1:], 1, "cm memory append <entry>")
		post("/api/office/agent/memory/append", map[string]interface{}{
			"sessionId": sessionID, "entry": strings.Join(args[1:], " "),
		})
		fmt.Println("Memory appended.")
	case "section":
		// cm memory section <name>                  -> read the section
		// cm memory section <name> <content>        -> overwrite the section
		// cm memory section --append <name> <entry> -> append to the section
		rest := args[1:]
		appendMode := false
		if len(rest) > 0 && rest[0] == "--append" {
			appendMode = true
			rest = rest[1:]
		}
		if len(rest) == 0 {
			fatal("cm memory section <name> [content] | cm memory section --append <name> <entry>")
		}
		name := rest[0]
		if len(rest) == 1 && !appendMode {
			// Read mode.
			printJSON(get(fmt.Sprintf("/api/office/agent/memory/section?sessionId=%s&name=%s",
				url.QueryEscape(sessionID), url.QueryEscape(name))))
			return
		}
		content := strings.Join(rest[1:], " ")
		post("/api/office/agent/memory/section", map[string]interface{}{
			"sessionId": sessionID,
			"name":      name,
			"content":   content,
			"append":    appendMode,
		})
		if appendMode {
			fmt.Printf("Memory section %q appended.\n", name)
		} else {
			fmt.Printf("Memory section %q written.\n", name)
		}
	case "query":
		requireArgs(args[1:], 1, "cm memory query <keyword>")
		printJSON(get(fmt.Sprintf("/api/office/agent/memory/query?sessionId=%s&q=%s",
			url.QueryEscape(sessionID), url.QueryEscape(strings.Join(args[1:], " ")))))
	default:
		fatal("Unknown memory subcommand: " + args[0])
	}
}

// --- Helpers ---

// addAgentAuth attaches the agent token header (when set via env) so the
// request passes the AgentTokenAuth middleware on /api/office/agent/*.
func addAgentAuth(req *http.Request) {
	if tok := os.Getenv("JARVIS_AGENT_TOKEN"); tok != "" {
		req.Header.Set("X-Jarvis-Agent-Token", tok)
	}
}

func post(path string, body map[string]interface{}) {
	_ = postBody(path, body)
}

// postBody is the same as post but returns the decoded JSON response.
// Useful when the server signals semantic info in the body (e.g. the
// soft screenshot gate returns {"warning": "..."} on task-complete).
// Callers that don't need the body should stick to post() so the
// majority of call sites stay terse.
func postBody(path string, body map[string]interface{}) interface{} {
	requireEnv()
	data, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", apiURL+path, bytes.NewReader(data))
	if err != nil {
		fatal("HTTP error: " + err.Error())
	}
	req.Header.Set("Content-Type", "application/json")
	addAgentAuth(req)
	resp, err := client.Do(req)
	if err != nil {
		fatal("HTTP error: " + err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		text, _ := io.ReadAll(resp.Body)
		fatal(fmt.Sprintf("Error %d: %s", resp.StatusCode, string(text)))
	}
	var out interface{}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out
}

// del issues an HTTP DELETE against the given API path with agent auth.
// Fatal-exits on non-2xx so CLI scripts fail loudly instead of silently
// skipping over a delete that didn't happen.
func del(path string) {
	requireEnv()
	req, err := http.NewRequest("DELETE", apiURL+path, nil)
	if err != nil {
		fatal("HTTP error: " + err.Error())
	}
	addAgentAuth(req)
	resp, err := client.Do(req)
	if err != nil {
		fatal("HTTP error: " + err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		text, _ := io.ReadAll(resp.Body)
		fatal(fmt.Sprintf("Error %d: %s", resp.StatusCode, string(text)))
	}
}

func get(path string) interface{} {
	requireEnv()
	req, err := http.NewRequest("GET", apiURL+path, nil)
	if err != nil {
		fatal("HTTP error: " + err.Error())
	}
	addAgentAuth(req)
	resp, err := client.Do(req)
	if err != nil {
		fatal("HTTP error: " + err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		text, _ := io.ReadAll(resp.Body)
		fatal(fmt.Sprintf("Error %d: %s", resp.StatusCode, string(text)))
	}
	var result interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	return result
}

func printJSON(v interface{}) {
	data, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(data))
}

func requireEnv() {
	if apiURL == "" {
		fatal("JARVIS_API_URL not set")
	}
	if sessionID == "" {
		fatal("JARVIS_SESSION_ID not set")
	}
}

func requireArgs(args []string, n int, usage string) {
	if len(args) < n {
		fatal("Usage: " + usage)
	}
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "Error:", msg)
	os.Exit(1)
}

func parseArgsAndFlags(args []string) (string, map[string]string) {
	var positional []string
	flags := make(map[string]string)
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "--") {
			key := strings.TrimPrefix(args[i], "--")
			if key == "committed" {
				flags[key] = "true"
				continue
			}
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				flags[key] = args[i+1]
				i++
			}
		} else {
			positional = append(positional, args[i])
		}
	}
	return strings.Join(positional, " "), flags
}

func printUsage() {
	fmt.Println(`cm - Jarvis agent CLI

Commands:
  standup respond <content>           Submit standup status update
  brainstorm respond <proposals>      Submit brainstorm task proposals
  leader-tasks respond <content>      Submit leader task assignments
  reflect <learning> --category <cat> Submit post-task reflection
  discover <summary> --findings ...   Report curiosity findings
  task start|ack <taskId>             Manage task lifecycle
  task complete <taskId> --summary "<2-3 sentence handoff>" [--screenshot <path>]
                                      Mark complete (summary appended to memory.task_history)
  task progress <taskId> <note>       Report task progress
  task blocked <taskId> <reason>      Report blocker
  task list                           List your task queue
  task self-add <title> <desc> [--label <name>]...
                                      Add self-generated task with optional labels
  task label add|remove <taskId> <label>
                                      Edit labels on an existing task
  task triage <taskId> <to> <reason>  Triage to another agent
  task handoff <taskId> <toConfigId> <context>   Hand work over mid-task
  task handoff-accept <taskId>        Accept an incoming handoff
  task handoff-reject <taskId> <reason>          Decline an incoming handoff
  task handoff-cancel <taskId>        Retract your own pending handoff
  lock <filePath>                     Lock a file before editing
  unlock <filePath>                   Unlock after committing
  locks                               List active file locks
  roster                              Get team roster
  message <toSessionId> <content>     Send message to teammate
  skill list|read|create|search       Manage shared skills
  memory read|write|append            Manage agent memory (flat)
  memory section <name> [content]     Read or overwrite a memory section
  memory section --append <name> <entry>
                                      Append to a memory section
  memory query <keyword>              Search across memory sections
  knowledge search <query> [--limit N] [--all]
                                      Team-wide search: all memory + learnings
                                      + standups + whiteboard, recency-ranked
  knowledge tail [--limit N]          Rolling tail of recent team knowledge
  onboard <topic>                     Lazy-load reference: protocols, browser,
                                      dev-instance, dev-lead (lead only),
                                      context-budget, skills, memory
  dev announce <url> [--name <s>] [--pid <n>]
                                      Lead: announce dev URL for slot s (default "app")
  dev announce <url> --force          Allow localhost URL (headless-shell won't reach it)
  dev url [<slot>] [--probe]          Print URL for slot (default "app"; empty if none). --probe HEADs the URL and prints live status on stderr.
  dev list                            JSON dump of every dev slot (url, pid, health, creds)
  dev clear [<slot>]                  Lead: clear one slot (SIGTERMs PID tree)
  dev clear --all                     Lead: wipe every dev slot (e.g. before wrap-up)
  dev creds [--name <s>]              Print dev login blob for slot s (empty if none)
  dev creds set <text...> [--name <s>]
                                      Lead: set dev login blob for slot s
  dev creds clear [--name <s>]        Lead: clear dev credentials for slot s
  help                                Show this help

Environment:
  JARVIS_API_URL      Server URL (e.g., http://localhost:3555)
  JARVIS_SESSION_ID   Current session ID`)
}
