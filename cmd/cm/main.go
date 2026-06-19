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
	"unicode"
)

var (
	apiURL    string
	sessionID string
	client    = &http.Client{Timeout: 15 * time.Second}
)

// osExit is the process-exit hook. Production code exits via os.Exit; tests
// override it (panic + recover) so they can exercise fatal/exit branches
// without terminating the test binary.
var osExit = os.Exit

func init() {
	apiURL = os.Getenv("JARVIS_API_URL")
	sessionID = os.Getenv("JARVIS_SESSION_ID")
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		osExit(0)
		return
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

	// Ask -- structured Q&A with the team lead (and fallback teammate).
	// See ~/.claude/skills/office-ask-for-help.
	case "ask":
		handleAsk(args)

	// Skill
	case "skill":
		handleSkill(args)

	// Memory
	case "memory":
		handleMemory(args)

	// Team dev instance (shared browser verification URL)
	case "dev":
		handleDev(args)

	// Sticky notes on the shared whiteboard (also fed into Reports).
	case "whiteboard", "note":
		handleWhiteboard(args)

	// Unified team knowledge search (R17)
	case "knowledge", "know":
		handleKnowledge(args)

	// R31 · Onboarding reference -- print a topic's full content on
	// demand so the kickstart doesn't have to ship it. Topics:
	// protocols, browser, dev-instance, dev-lead (strategic only),
	// context-budget, skills, memory.
	case "onboard", "onboarding":
		handleOnboard(args)

	// Project management (invariants, etc.)
	case "project":
		handleProject(args)

	// Agent recovery (Guardian protocol): inspect peer health and ask the
	// server to respawn a stuck or exited peer by name.
	case "agent":
		handleAgent(args)

	// Help
	case "help", "--help", "-h":
		printUsage()

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		printUsage()
		osExit(1)
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
		fatal("Usage: cm standup respond <content> | --json '<schema body>'\n" +
			"  JSON shape:\n" + indent2(SchemaExampleFor("standup")))
	}
	rest := args[1:]
	body := map[string]interface{}{"sessionId": sessionID}
	if jsonStr, ok := extractFlag(&rest, "--json"); ok {
		report, rendered, err := DecodeStandupJSON(jsonStr)
		if err != nil {
			fatal(fmt.Sprintf("--json validation failed: %v\nExpected shape:\n%s", err, indent2(SchemaExampleFor("standup"))))
		}
		body["content"] = rendered
		body["structured"] = report
	} else {
		body["content"] = strings.Join(rest, " ")
	}
	post("/api/office/agent/standup/response", body)
	fmt.Println("Standup response submitted.")
}

func handleBrainstorm(args []string) {
	if len(args) < 2 || args[0] != "respond" {
		fatal("Usage: cm brainstorm respond <proposals> | --json '<schema body>'\n" +
			"  JSON shape:\n" + indent2(SchemaExampleFor("brainstorm")))
	}
	rest := args[1:]
	body := map[string]interface{}{"sessionId": sessionID}
	if jsonStr, ok := extractFlag(&rest, "--json"); ok {
		resp, rendered, err := DecodeBrainstormJSON(jsonStr)
		if err != nil {
			fatal(fmt.Sprintf("--json validation failed: %v\nExpected shape:\n%s", err, indent2(SchemaExampleFor("brainstorm"))))
		}
		body["proposals"] = rendered
		body["structured"] = resp
	} else {
		body["proposals"] = strings.Join(rest, " ")
	}
	post("/api/office/agent/brainstorm/response", body)
	fmt.Println("Brainstorm proposals submitted.")
}

func handleLeaderTasks(args []string) {
	if len(args) < 2 || args[0] != "respond" {
		fatal("Usage: cm leader-tasks respond <content> | --json '<schema body>'\n" +
			"  JSON shape:\n" + indent2(SchemaExampleFor("leader-tasks")))
	}
	rest := args[1:]
	body := map[string]interface{}{"sessionId": sessionID}
	if jsonStr, ok := extractFlag(&rest, "--json"); ok {
		lt, rendered, err := DecodeLeaderTasksJSON(jsonStr)
		if err != nil {
			fatal(fmt.Sprintf("--json validation failed: %v\nExpected shape:\n%s", err, indent2(SchemaExampleFor("leader-tasks"))))
		}
		body["content"] = rendered
		body["structured"] = lt
	} else {
		body["content"] = strings.Join(rest, " ")
	}
	post("/api/office/agent/leader-tasks/response", body)
	fmt.Println("Leader task assignments submitted.")
}

// extractFlag removes the first `--flagName <value>` occurrence from
// args (in place) and returns the value. Used by the standup /
// brainstorm / leader-tasks handlers to pull out --json without
// disturbing the positional rest. Returns ("", false) when the flag
// is absent.
func extractFlag(args *[]string, flagName string) (string, bool) {
	for i, a := range *args {
		if a != flagName {
			continue
		}
		if i+1 >= len(*args) {
			return "", true // flag without value -- treat as empty
		}
		value := (*args)[i+1]
		// Remove flag + value from the slice.
		*args = append((*args)[:i], (*args)[i+2:]...)
		return value, true
	}
	return "", false
}

// indent2 prefixes every line of s with two spaces so multi-line
// usage examples render cleanly under "Usage: ..." headers.
func indent2(s string) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = "  " + l
	}
	return strings.Join(lines, "\n")
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
	// --findings is multi-value: it consumes every following non-flag token (and
	// also splits comma-separated tokens, so `--findings a b c` and `--findings
	// a,b,c` both work). The previous code routed it through parseArgsAndFlags,
	// which captured only ONE token (dropping the rest into the summary) and
	// then split on comma -- so the documented space-separated form silently
	// lost all findings but the first. An absent flag yields an EMPTY slice, not
	// the phantom [""] that strings.Split("", ",") produces.
	var summaryParts, findings []string
	committed := false
	selfAssigned := ""
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--committed":
			committed = true
		case args[i] == "--findings":
			for i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				for _, f := range strings.Split(args[i+1], ",") {
					if f = strings.TrimSpace(f); f != "" {
						findings = append(findings, f)
					}
				}
				i++
			}
		case args[i] == "--self-assigned":
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				selfAssigned = args[i+1]
				i++
			}
		case strings.HasPrefix(args[i], "--"):
			// Unknown flag: skip it (and its value if one follows).
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				i++
			}
		default:
			summaryParts = append(summaryParts, args[i])
		}
	}
	if findings == nil {
		findings = []string{}
	}
	body := map[string]interface{}{
		"sessionId":   sessionID,
		"summary":     strings.Join(summaryParts, " "),
		"discoveries": findings,
		"ledToCommit": committed,
	}
	if selfAssigned != "" {
		body["selfAssignedTask"] = selfAssigned
	}
	post("/api/office/agent/curiosity/discovery", body)
	fmt.Println("Discovery reported.")
}

func handleTask(args []string) {
	if len(args) < 1 {
		fatal("Usage: cm task <start|complete|ack|progress|blocked|list|self-add|triage|handoff|handoff-accept|handoff-reject|handoff-cancel|label|criteria> ...")
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
		// Also accept repeated --criterion flags (mirror of --label).
		var labels []string
		var criteria []string
		filtered := make([]string, 0, len(subargs))
		for i := 0; i < len(subargs); i++ {
			if subargs[i] == "--label" && i+1 < len(subargs) {
				labels = append(labels, subargs[i+1])
				i++
				continue
			}
			if subargs[i] == "--criterion" && i+1 < len(subargs) {
				criteria = append(criteria, subargs[i+1])
				i++
				continue
			}
			filtered = append(filtered, subargs[i])
		}
		positional, _ := parseArgsAndFlags(filtered)
		fields := strings.Fields(positional)
		if len(fields) < 2 {
			fatal("cm task self-add <title> <description> [priority] [--label <name>]... [--criterion \"<c>\"]...")
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
		if len(criteria) > 0 {
			body["acceptanceCriteria"] = criteria
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

	// cm task criteria <taskId> set "<c>"... | add "<c>"... | remove <index>
	// Edits acceptance criteria on a task. op="remove" takes a SINGLE 0-based index.
	case "criteria":
		if len(subargs) < 3 {
			fatal("Usage: cm task criteria <taskId> <set|add|remove> <criterion|index>...")
		}
		taskID := subargs[0]
		op := subargs[1]
		values := subargs[2:]
		switch op {
		case "set", "add", "remove":
			// valid
		default:
			fatal("Usage: cm task criteria <taskId> <set|add|remove> <criterion|index>...")
		}
		post("/api/office/agent/task/criteria", map[string]interface{}{
			"sessionId": sessionID,
			"taskId":    taskID,
			"op":        op,
			"criteria":  values,
		})
		fmt.Printf("Acceptance criteria %sd on task %s.\n", op, taskID)

	// cm task phase <taskId> <define|plan|build|verify|review|simplify|ship>
	// Records the current 7-step engineering phase on a task (advisory).
	case "phase":
		requireArgs(subargs, 2, "cm task phase <taskId> <define|plan|build|verify|review|simplify|ship>")
		post("/api/office/agent/task/phase", map[string]interface{}{
			"sessionId": sessionID,
			"taskId":    subargs[0],
			"phase":     subargs[1],
		})
		fmt.Printf("Task %s phase set to %s.\n", subargs[0], subargs[1])

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

// handleWhiteboard dispatches `cm whiteboard <content...>` (alias
// `cm note`). Posting a note appends a sticky note to the shared team
// whiteboard feed -- it renders as a card on the Whiteboard screen AND
// is merged into the Reports library. `cm whiteboard list` prints the
// recent feed. Optional `--color <hex>` overrides the per-author color.
func handleWhiteboard(args []string) {
	if len(args) >= 1 && args[0] == "list" {
		result := get("/api/office/sticky-notes")
		printStickyNotes(result)
		return
	}
	rest := append([]string{}, args...)
	color, _ := extractFlag(&rest, "--color")
	// Guard against accidental command fragments: a leftover token that still
	// looks like a flag (e.g. an agent ran `cm whiteboard --tail 8`) is almost
	// never a real note, so reject it with a quote-your-note hint instead of
	// posting the fragment as a sticky note.
	for _, tok := range rest {
		if strings.HasPrefix(tok, "-") && len(tok) > 1 {
			fatal(fmt.Sprintf("cm whiteboard: unexpected option %q -- write a plain note and quote it, e.g.\n  cm whiteboard \"Decided to use SSR for the landing page\"\n(only --color <hex> is supported)", tok))
		}
	}
	content := strings.TrimSpace(strings.Join(rest, " "))
	if !looksLikeRealNote(content) {
		fatal("cm whiteboard: note is empty or too short.\nUsage: cm whiteboard \"<your note>\" [--color <hex>]  |  cm whiteboard list")
	}
	body := map[string]interface{}{"sessionId": sessionID, "content": content}
	if color != "" {
		body["color"] = color
	}
	post("/api/office/agent/sticky-note", body)
	fmt.Println("Sticky note posted.")
}

// looksLikeRealNote returns true when s is a plausible sticky note: at least
// 3 non-space chars and containing at least one letter, so stray fragments
// (bare punctuation, leftover shell tokens) don't become notes.
func looksLikeRealNote(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 3 {
		return false
	}
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

// printStickyNotes renders the sticky-note feed compactly: one block per
// note with author, relative age, and content.
func printStickyNotes(result interface{}) {
	m, ok := result.(map[string]interface{})
	if !ok {
		printJSON(result)
		return
	}
	notesAny, _ := m["notes"].([]interface{})
	if len(notesAny) == 0 {
		fmt.Println("(no sticky notes)")
		return
	}
	now := time.Now()
	for _, n := range notesAny {
		nm, ok := n.(map[string]interface{})
		if !ok {
			continue
		}
		author, _ := nm["author"].(string)
		content, _ := nm["content"].(string)
		tsStr, _ := nm["createdAt"].(string)
		age := ""
		if tsStr != "" {
			if t, err := time.Parse(time.RFC3339Nano, tsStr); err == nil {
				age = formatAge(now.Sub(t))
			}
		}
		if age != "" {
			fmt.Printf("[%s] %s\n  %s\n", age, author, content)
		} else {
			fmt.Printf("%s\n  %s\n", author, content)
		}
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

// handleAsk implements `cm ask`. Three forms:
//
//	cm ask "<question>" [--about <taskId>] [--wait 5m]
//	cm ask answer <questionId> "<answer>"
//	cm ask list
//
// The blocking form has its own HTTP client (longer timeout than the
// 15s default) because it intentionally sits on the connection while
// the office rotates through recipients.
func handleAsk(args []string) {
	if len(args) < 1 {
		fatal("Usage: cm ask <question> [--about <taskId>] [--wait 5m]\n  cm ask answer <questionId> <answer>\n  cm ask list")
	}
	switch args[0] {
	case "answer":
		if len(args) < 3 {
			fatal("Usage: cm ask answer <questionId> <answer>")
		}
		post("/api/office/agent/ask/answer", map[string]interface{}{
			"sessionId":     sessionID,
			"fromSessionId": sessionID,
			"questionId":    args[1],
			"answer":        strings.Join(args[2:], " "),
		})
		fmt.Println("Answer submitted.")
		return
	case "list":
		printJSON(get(fmt.Sprintf("/api/office/agent/ask/list?sessionId=%s", url.QueryEscape(sessionID))))
		return
	}

	// Blocking ask form. Parse positional question text + flags.
	question, flags := parseArgsAndFlags(args)
	if question == "" {
		fatal("Usage: cm ask <question> [--about <taskId>] [--wait 5m]")
	}
	wait := 5 * time.Minute
	// Treat an empty value (e.g. `--wait` with no argument) as absent and use
	// the default, rather than feeding "" to ParseDuration (which errors and
	// would fatal). parseArgsAndFlags now records a value-less flag as present
	// with an empty string, so check the value, not just presence.
	if w := flags["wait"]; w != "" {
		parsed, err := time.ParseDuration(w)
		if err != nil {
			fatal(fmt.Sprintf("invalid --wait duration %q: %v", w, err))
		}
		wait = parsed
	}

	// Long-poll client: sit on the request as long as the server might
	// hold it, plus a small slack for the round trip. Independent of the
	// package-level `client` so other CLI calls keep their tight 15s
	// timeout.
	askClient := &http.Client{Timeout: wait + 30*time.Second}
	body, _ := json.Marshal(map[string]interface{}{
		"sessionId":     sessionID,
		"fromSessionId": sessionID,
		"content":       question,
		"aboutTaskId":   flags["about"],
		"waitSeconds":   int(wait.Seconds()),
	})
	requireEnv()
	req, err := http.NewRequest("POST", apiURL+"/api/office/agent/ask", bytes.NewReader(body))
	if err != nil {
		fatal("HTTP error: " + err.Error())
	}
	req.Header.Set("Content-Type", "application/json")
	addAgentAuth(req)
	resp, err := askClient.Do(req)
	if err != nil {
		fatal("HTTP error: " + err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		text, _ := io.ReadAll(resp.Body)
		fatal(fmt.Sprintf("Error %d: %s", resp.StatusCode, string(text)))
	}
	var out struct {
		Success    bool     `json:"success"`
		QuestionID string   `json:"questionId"`
		Status     string   `json:"status"`
		Answer     string   `json:"answer"`
		AnsweredBy string   `json:"answeredBy"`
		Tried      []string `json:"tried"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		fatal("decode error: " + err.Error())
	}
	switch out.Status {
	case "answered":
		fmt.Printf("status: answered (by %s, qid: %s)\n%s\n", out.AnsweredBy, out.QuestionID, out.Answer)
	case "timeout":
		fmt.Printf("status: timeout (qid: %s, asked: %s)\nNo one answered in time. Run `cm task blocked <taskId>` to escalate.\n",
			out.QuestionID, strings.Join(out.Tried, ", "))
		osExit(2)
	case "no-recipient":
		fmt.Printf("status: no-recipient (qid: %s)\nNobody on the team is available right now. Run `cm task blocked <taskId>` to escalate.\n",
			out.QuestionID)
		osExit(2)
	default:
		fmt.Printf("status: %s (qid: %s)\n", out.Status, out.QuestionID)
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

// handleProject dispatches project-level subcommands.
func handleProject(args []string) {
	if len(args) < 1 {
		fatal("Usage: cm project <invariants> [subcommand]")
	}
	switch args[0] {
	case "invariants":
		handleProjectInvariants(args[1:])
	default:
		fatal("Unknown project subcommand: " + args[0] + " (expected: invariants)")
	}
}

// handleProjectInvariants manages the project-level invariants list.
//
//	cm project invariants              -- list all invariants
//	cm project invariants list         -- list all invariants
//	cm project invariants add "<rule>" -- append a rule
//	cm project invariants remove <n>   -- remove rule at 0-based index
//	cm project invariants set "<r1>" "<r2>" ...  -- replace full list
func handleProjectInvariants(args []string) {
	subCmd := "list"
	if len(args) > 0 {
		subCmd = args[0]
		args = args[1:]
	}
	switch subCmd {
	case "list", "ls", "":
		result := get("/api/office/project/invariants")
		printInvariantsList(result)
	case "add":
		if len(args) < 1 {
			fatal("Usage: cm project invariants add \"<rule>\"")
		}
		text := strings.Join(args, " ")
		result := postBody("/api/office/project/invariants", map[string]interface{}{"text": text})
		printInvariantsList(result)
	case "remove", "rm", "del", "delete":
		if len(args) < 1 {
			fatal("Usage: cm project invariants remove <index>")
		}
		idx, err := strconv.Atoi(args[0])
		if err != nil || idx < 0 {
			fatal("index must be a non-negative integer (0-based)")
		}
		delResult := delBody(fmt.Sprintf("/api/office/project/invariants/%d", idx))
		printInvariantsList(delResult)
	case "set":
		if len(args) < 1 {
			fatal("Usage: cm project invariants set \"<r1>\" \"<r2>\" ...")
		}
		result := put("/api/office/project/invariants", map[string]interface{}{"invariants": args})
		printInvariantsList(result)
	default:
		fatal("Unknown invariants subcommand: " + subCmd + " (expected: list|add|remove|set)")
	}
}

// decodeJSONResponseBody decodes a 2xx response body into out. An empty body
// (io.EOF) is expected for some endpoints and ignored. Any OTHER error means the
// server returned malformed JSON; surface it to stderr (callers tolerate a nil
// result, so this stays non-fatal) instead of swallowing it silently.
func decodeJSONResponseBody(body io.Reader, out interface{}) {
	if err := json.NewDecoder(body).Decode(out); err != nil && err != io.EOF {
		fmt.Fprintln(os.Stderr, "warning: failed to decode server response: "+err.Error())
	}
}

// delBody issues an HTTP DELETE and returns the decoded JSON response body.
// del() exists for fire-and-forget deletes; this variant captures the body.
func delBody(path string) interface{} {
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
	var out interface{}
	decodeJSONResponseBody(resp.Body, &out)
	return out
}

// printInvariantsList pretty-prints the invariants array from a server response.
func printInvariantsList(result interface{}) {
	m, ok := result.(map[string]interface{})
	if !ok {
		printJSON(result)
		return
	}
	raw, ok := m["invariants"]
	if !ok {
		printJSON(result)
		return
	}
	arr, ok := raw.([]interface{})
	if !ok || len(arr) == 0 {
		fmt.Println("(no invariants)")
		return
	}
	for i, v := range arr {
		fmt.Printf("%d. %v\n", i, v)
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
	decodeJSONResponseBody(resp.Body, &out)
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

// put issues an HTTP PUT request with a JSON body against the given path.
// Returns the decoded JSON response. Fatal-exits on non-2xx.
func put(path string, body map[string]interface{}) interface{} {
	requireEnv()
	data, _ := json.Marshal(body)
	req, err := http.NewRequest("PUT", apiURL+path, bytes.NewReader(data))
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
	decodeJSONResponseBody(resp.Body, &out)
	return out
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
	osExit(1)
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
			} else {
				// No usable value (last token, or immediately followed by
				// another --flag). Record the flag as present-but-empty rather
				// than dropping it entirely, so its presence is observable
				// instead of vanishing silently.
				flags[key] = ""
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
  task self-add <title> <desc> [--label <name>]... [--criterion "<c>"]...
                                      Add self-generated task with optional labels and acceptance criteria
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
  ask <question> [--about <taskId>] [--wait 5m]
                                      Ask the lead (fallback: non-busy teammate);
                                      blocks until answered or timeout. Status
                                      "answered" prints the reply; "timeout" or
                                      "no-recipient" exit non-zero so scripts can
                                      escalate via cm task blocked. See
                                      cm skill read office-ask-for-help.
  ask answer <questionId> <answer>    Reply to a question addressed to you
  ask list                            List pending questions for you
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
  whiteboard "<note>" [--color <hex>]
                                      Post a sticky note to the shared whiteboard
                                      (decisions, blockers, milestones, hand-offs,
                                      findings). QUOTE the note as one argument, e.g.
                                      cm whiteboard "Switched gauge to canvas for perf".
                                      Shows on the Whiteboard screen and in the
                                      Reports library. Alias: cm note
  whiteboard list                     Print the recent sticky-note feed
  project invariants                  List project invariants (non-negotiable rules)
  project invariants add "<rule>"     Append a rule to the invariants list
  project invariants remove <index>   Remove rule at given 0-based index
  project invariants set "<r1>" ...   Replace full invariants list
  agent health                        Guardian: print each peer's name, status, last activity
  agent restart <name>                Guardian: ask the server to respawn a stuck/exited peer
  help                                Show this help

Environment:
  JARVIS_API_URL      Server URL (e.g., http://localhost:3555)
  JARVIS_SESSION_ID   Current session ID`)
}
