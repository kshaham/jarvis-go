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
		requireArgs(subargs, 1, "cm task complete <taskId>")
		post("/api/office/agent/task-complete", map[string]interface{}{"sessionId": sessionID, "taskId": subargs[0]})
		fmt.Printf("Task %s marked as complete.\n", subargs[0])

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
		fatal("Usage: cm memory <read|write|append> ...")
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
	default:
		fatal("Unknown memory subcommand: " + args[0])
	}
}

// --- Helpers ---

func post(path string, body map[string]interface{}) {
	requireEnv()
	data, _ := json.Marshal(body)
	resp, err := client.Post(apiURL+path, "application/json", bytes.NewReader(data))
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
	resp, err := client.Get(apiURL + path)
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
  task start|complete|ack <taskId>    Manage task lifecycle
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
  memory read|write|append            Manage agent memory
  help                                Show this help

Environment:
  JARVIS_API_URL      Server URL (e.g., http://localhost:3555)
  JARVIS_SESSION_ID   Current session ID`)
}
