// cm is the Jarvis agent CLI for server communication.
// Agents in office sessions use this to interact with the Jarvis server
// (submit standup responses, report task progress, send messages, etc.)
//
// Environment variables:
//   JARVIS_API_URL    - Server URL (e.g., http://localhost:3555)
//   JARVIS_SESSION_ID - Current session ID
package main

import (
	"bytes"
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

	// Help
	case "help", "--help", "-h":
		printUsage()

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
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
		fatal("Usage: cm task <start|complete|ack|progress|blocked|list|self-add|triage> ...")
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
		post("/api/office/agent/task-complete", map[string]interface{}{"sessionId": sessionID, "taskId": taskID})
		fmt.Printf("Task %s marked as complete.\n", taskID)

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
		requireArgs(subargs, 2, "cm task self-add <title> <description> [priority]")
		priority := "medium"
		if len(subargs) > 2 {
			priority = subargs[2]
		}
		post("/api/office/agent/task/self-add", map[string]interface{}{
			"sessionId": sessionID, "title": subargs[0], "description": subargs[1], "priority": priority,
		})
		fmt.Printf("Self-generated task added: %s\n", subargs[0])

	case "triage":
		requireArgs(subargs, 3, "cm task triage <taskId> <toSessionId> <reason>")
		post("/api/office/agent/task/triage", map[string]interface{}{
			"taskId": subargs[0], "sessionId": sessionID,
			"toSessionId": subargs[1], "reason": strings.Join(subargs[2:], " "),
		})
		fmt.Printf("Task %s triaged to %s.\n", subargs[0], subargs[1])

	default:
		fatal("Unknown task subcommand: " + subcmd)
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
		slot := "app"
		if len(args) > 1 && !strings.HasPrefix(args[1], "--") {
			slot = args[1]
		}
		result := get("/api/office/agent/dev-instance?name=" + url.QueryEscape(slot))
		if m, ok := result.(map[string]interface{}); ok {
			if u, ok := m["url"].(string); ok {
				fmt.Println(u)
				return
			}
		}
		fmt.Println("")
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
func parseAnnounceArgs(args []string) (slot, url string, pid int, force bool) {
	slot = "app"
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "--force":
			force = true
		case "--pid":
			if i+1 >= len(args) {
				fatal("--pid requires a value")
			}
			v, err := strconv.Atoi(args[i+1])
			if err != nil || v < 0 {
				fatal(fmt.Sprintf("--pid must be a non-negative integer, got %q", args[i+1]))
			}
			pid = v
			i++
		case "--name":
			if i+1 >= len(args) {
				fatal("--name requires a value")
			}
			slot = args[i+1]
			i++
		default:
			if strings.HasPrefix(a, "--") {
				fatal("unknown flag: " + a)
			}
			if url == "" {
				url = a
			}
		}
	}
	return
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
  task complete <taskId> --summary "<2-3 sentence handoff>"
                                      Mark complete (summary appended to memory.task_history)
  task progress <taskId> <note>       Report task progress
  task blocked <taskId> <reason>      Report blocker
  task list                           List your task queue
  task self-add <title> <desc>        Add self-generated task
  task triage <taskId> <to> <reason>  Triage to another agent
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
  dev announce <url> [--name <s>] [--pid <n>]
                                      Lead: announce dev URL for slot s (default "app")
  dev announce <url> --force          Allow localhost URL (headless-shell won't reach it)
  dev url [<slot>]                    Print URL for slot (default "app"; empty if none)
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
