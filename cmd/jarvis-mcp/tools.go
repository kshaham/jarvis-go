package main

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Dynamic tools stored in memory
var (
	dynamicTools   = make(map[string]DynamicTool)
	dynamicToolsMu sync.RWMutex
)

type DynamicTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Prompt      string `json:"prompt"`
	WorkingDir  string `json:"workingDir"`
}

// All static tool definitions (29 tools)
var allToolDefs = []ToolDefinition{
	// --- Session Tools (6) ---
	{
		Name:        "create_session",
		Description: "Create a new Claude Code session in the specified working directory",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"workingDir": {Type: "string", Description: "Absolute path to the working directory for the session"},
				"name":       {Type: "string", Description: "Optional human-readable name for the session"},
				"mode":       {Type: "string", Description: "Session mode", Default: "claude", Enum: []string{"claude", "office"}},
			},
			Required: []string{"workingDir"},
		},
	},
	{
		Name:        "list_sessions",
		Description: "List all active Jarvis sessions",
		InputSchema: InputSchema{Type: "object"},
	},
	{
		Name:        "get_session",
		Description: "Get detailed information about a specific session",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"sessionId": {Type: "string", Description: "The session ID to look up"},
			},
			Required: []string{"sessionId"},
		},
	},
	{
		Name:        "stop_session",
		Description: "Stop and remove a running session",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"sessionId": {Type: "string", Description: "The session ID to stop"},
			},
			Required: []string{"sessionId"},
		},
	},
	{
		Name:        "send_to_session",
		Description: "Send text input to a running session's stdin",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"sessionId": {Type: "string", Description: "The session ID to send input to"},
				"input":     {Type: "string", Description: "The text to send to the session"},
			},
			Required: []string{"sessionId", "input"},
		},
	},
	{
		Name:        "get_session_output",
		Description: "Get the recent output from a session",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"sessionId": {Type: "string", Description: "The session ID to read output from"},
			},
			Required: []string{"sessionId"},
		},
	},

	// --- Agent Tools (3) ---
	{
		Name:        "list_agents",
		Description: "List all active sub-agents across all sessions",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"workingDir": {Type: "string", Description: "Optional: filter agents by working directory"},
			},
		},
	},
	{
		Name:        "get_agent_status",
		Description: "Get detailed status of a specific sub-agent",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"agentId": {Type: "string", Description: "The agent ID to look up"},
			},
			Required: []string{"agentId"},
		},
	},
	{
		Name:        "get_agent_transcript",
		Description: "Get the conversation transcript of a sub-agent",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"agentId": {Type: "string", Description: "The agent ID to get transcript for"},
				"limit":   {Type: "string", Description: "Maximum number of transcript entries to return"},
			},
			Required: []string{"agentId"},
		},
	},

	// --- Task Tools (4) ---
	{
		Name:        "add_task",
		Description: "Add a new task to the scheduled task queue",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"prompt":     {Type: "string", Description: "The task prompt/description"},
				"workingDir": {Type: "string", Description: "Working directory for the task"},
				"priority":   {Type: "string", Description: "Task priority", Enum: []string{"low", "medium", "high"}},
			},
			Required: []string{"prompt"},
		},
	},
	{
		Name:        "list_tasks",
		Description: "List all scheduled tasks",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"status": {Type: "string", Description: "Filter by status", Enum: []string{"pending", "running", "completed", "failed"}},
			},
		},
	},
	{
		Name:        "get_task",
		Description: "Get details of a specific scheduled task",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"taskId": {Type: "string", Description: "The task ID to look up"},
			},
			Required: []string{"taskId"},
		},
	},
	{
		Name:        "remove_task",
		Description: "Remove a scheduled task",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"taskId": {Type: "string", Description: "The task ID to remove"},
			},
			Required: []string{"taskId"},
		},
	},

	// --- Office Agent Task Tools (7) ---
	{
		Name:        "office_task_list",
		Description: "List your office task queue (assigned and self-generated tasks)",
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{}},
	},
	{
		Name:        "office_task_self_add",
		Description: "Add a self-generated task to your office task queue",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"title":       {Type: "string", Description: "Task title"},
				"description": {Type: "string", Description: "Task description"},
				"priority":    {Type: "string", Description: "Task priority", Enum: []string{"low", "medium", "high"}},
			},
			Required: []string{"title", "description"},
		},
	},
	{
		Name:        "office_task_ack",
		Description: "Acknowledge receipt of an assigned task",
		InputSchema: InputSchema{
			Type:       "object",
			Properties: map[string]Property{"taskId": {Type: "string", Description: "Task ID to acknowledge"}},
			Required:   []string{"taskId"},
		},
	},
	{
		Name:        "office_task_start",
		Description: "Mark a task as in-progress",
		InputSchema: InputSchema{
			Type:       "object",
			Properties: map[string]Property{"taskId": {Type: "string", Description: "Task ID to start"}},
			Required:   []string{"taskId"},
		},
	},
	{
		Name:        "office_task_complete",
		Description: "Mark a task as completed",
		InputSchema: InputSchema{
			Type:       "object",
			Properties: map[string]Property{"taskId": {Type: "string", Description: "Task ID to complete"}},
			Required:   []string{"taskId"},
		},
	},
	{
		Name:        "office_task_progress",
		Description: "Report progress on a task",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"taskId": {Type: "string", Description: "Task ID"},
				"note":   {Type: "string", Description: "Progress update note"},
			},
			Required: []string{"taskId", "note"},
		},
	},
	{
		Name:        "office_task_blocked",
		Description: "Report that a task is blocked",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"taskId": {Type: "string", Description: "Task ID"},
				"reason": {Type: "string", Description: "What is blocking the task"},
			},
			Required: []string{"taskId", "reason"},
		},
	},
	{
		Name:        "office_standup_respond",
		Description: "Submit your standup status update",
		InputSchema: InputSchema{
			Type:       "object",
			Properties: map[string]Property{"content": {Type: "string", Description: "Standup response content"}},
			Required:   []string{"content"},
		},
	},
	{
		Name:        "office_reflect",
		Description: "Submit a post-task reflection/learning",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"learning": {Type: "string", Description: "What you learned"},
				"category": {Type: "string", Description: "Category", Enum: []string{"technique", "pattern", "tool", "insight", "mistake"}},
			},
			Required: []string{"learning", "category"},
		},
	},
	{
		Name:        "office_team_roster",
		Description: "Get the team roster (names, roles, session IDs of all agents)",
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{}},
	},
	{
		Name:        "office_send_message",
		Description: "Send a message to a teammate",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"toSessionId": {Type: "string", Description: "Session ID of the recipient"},
				"content":     {Type: "string", Description: "Message content"},
			},
			Required: []string{"toSessionId", "content"},
		},
	},

	// --- Skill Tools (5) ---
	{
		Name:        "list_skills",
		Description: "List all available shared skills",
		InputSchema: InputSchema{Type: "object"},
	},
	{
		Name:        "get_skill",
		Description: "Get the content of a specific skill",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"skillName": {Type: "string", Description: "Name of the skill to retrieve"},
				"file":      {Type: "string", Description: "Optional specific file within the skill"},
			},
			Required: []string{"skillName"},
		},
	},
	{
		Name:        "create_skill",
		Description: "Create or update a shared skill",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"skillName": {Type: "string", Description: "Name of the skill to create"},
				"content":   {Type: "string", Description: "Content of the skill"},
				"file":      {Type: "string", Description: "Optional filename within the skill directory"},
			},
			Required: []string{"skillName", "content"},
		},
	},
	{
		Name:        "search_skills",
		Description: "Search skills by keyword",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"query": {Type: "string", Description: "Search query string"},
			},
			Required: []string{"query"},
		},
	},
	{
		Name:        "delete_skill",
		Description: "Delete a skill from the filesystem",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"skillName": {Type: "string", Description: "Name of the skill to delete"},
				"file":      {Type: "string", Description: "Optional specific file to delete within the skill"},
			},
			Required: []string{"skillName"},
		},
	},

	// --- Monitoring Tools (4) ---
	{
		Name:        "get_git_status",
		Description: "Get git status for a working directory (finds the session for that directory)",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"workingDir": {Type: "string", Description: "Working directory to check git status for"},
			},
			Required: []string{"workingDir"},
		},
	},
	{
		Name:        "get_build_status",
		Description: "Get build status for a working directory",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"workingDir": {Type: "string", Description: "Working directory to check build status for"},
			},
			Required: []string{"workingDir"},
		},
	},
	{
		Name:        "get_test_status",
		Description: "Get test results for a working directory",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"workingDir": {Type: "string", Description: "Working directory to check test status for"},
			},
			Required: []string{"workingDir"},
		},
	},
	{
		Name:        "get_integration_state",
		Description: "Get integration state for a working directory",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"workingDir": {Type: "string", Description: "Working directory to check integration state for"},
			},
			Required: []string{"workingDir"},
		},
	},

	// --- State Tools (3) ---
	{
		Name:        "get_app_state",
		Description: "Get the overall Jarvis application state and status",
		InputSchema: InputSchema{Type: "object"},
	},
	{
		Name:        "get_stats",
		Description: "Get Jarvis usage statistics",
		InputSchema: InputSchema{Type: "object"},
	},
	{
		Name:        "get_config",
		Description: "Get Jarvis configuration",
		InputSchema: InputSchema{Type: "object"},
	},

	// --- Planning Tools (1) ---
	{
		Name:        "generate_plan",
		Description: "Generate a detailed implementation plan for a task",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"taskDescription": {Type: "string", Description: "Description of the task to plan"},
				"workingDir":      {Type: "string", Description: "Working directory for context"},
			},
			Required: []string{"taskDescription"},
		},
	},

	// --- Dynamic Tools (4) ---
	{
		Name:        "register_tool",
		Description: "Register a dynamic tool that runs a prompt in a session",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"name":        {Type: "string", Description: "Unique name for the tool"},
				"description": {Type: "string", Description: "Human-readable description"},
				"prompt":      {Type: "string", Description: "The prompt template to execute"},
				"workingDir":  {Type: "string", Description: "Working directory for execution"},
			},
			Required: []string{"name", "description", "prompt", "workingDir"},
		},
	},
	{
		Name:        "unregister_tool",
		Description: "Remove a previously registered dynamic tool",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"name": {Type: "string", Description: "Name of the tool to remove"},
			},
			Required: []string{"name"},
		},
	},
	{
		Name:        "list_dynamic_tools",
		Description: "List all registered dynamic tools",
		InputSchema: InputSchema{Type: "object"},
	},
	{
		Name:        "run_prompt",
		Description: "Run an arbitrary prompt in a Claude Code session",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"prompt":     {Type: "string", Description: "The prompt to execute"},
				"workingDir": {Type: "string", Description: "Working directory for execution"},
				"model":      {Type: "string", Description: "Optional model override"},
			},
			Required: []string{"prompt", "workingDir"},
		},
	},
}

// allTools maps tool names to their handler functions.
var allTools = map[string]ToolHandler{
	// Session Tools
	"create_session":    handleCreateSession,
	"list_sessions":     handleListSessions,
	"get_session":       handleGetSession,
	"stop_session":      handleStopSession,
	"send_to_session":   handleSendToSession,
	"get_session_output": handleGetSessionOutput,

	// Agent Tools
	"list_agents":          handleListAgents,
	"get_agent_status":     handleGetAgentStatus,
	"get_agent_transcript": handleGetAgentTranscript,

	// Task Tools
	"add_task":    handleAddTask,
	"list_tasks":  handleListTasks,
	"get_task":    handleGetTask,
	"remove_task": handleRemoveTask,

	// Skill Tools
	"list_skills":   handleListSkills,
	"get_skill":     handleGetSkill,
	"create_skill":  handleCreateSkill,
	"search_skills": handleSearchSkills,
	"delete_skill":  handleDeleteSkill,

	// Monitoring Tools
	"get_git_status":       handleGetGitStatus,
	"get_build_status":     handleGetBuildStatus,
	"get_test_status":      handleGetTestStatus,
	"get_integration_state": handleGetIntegrationState,

	// State Tools
	"get_app_state": handleGetAppState,
	"get_stats":     handleGetStats,
	"get_config":    handleGetConfig,

	// Planning Tools
	"generate_plan": handleGeneratePlan,

	// Dynamic Tools
	"register_tool":     handleRegisterTool,
	"unregister_tool":   handleUnregisterTool,
	"list_dynamic_tools": handleListDynamicTools,
	"run_prompt":        handleRunPrompt,

	// Office Agent Tools
	"office_task_list":       handleOfficeTaskList,
	"office_task_self_add":   handleOfficeTaskSelfAdd,
	"office_task_ack":        handleOfficeTaskAck,
	"office_task_start":      handleOfficeTaskStart,
	"office_task_complete":   handleOfficeTaskComplete,
	"office_task_progress":   handleOfficeTaskProgress,
	"office_task_blocked":    handleOfficeTaskBlocked,
	"office_standup_respond": handleOfficeStandupRespond,
	"office_reflect":         handleOfficeReflect,
	"office_team_roster":     handleOfficeTeamRoster,
	"office_send_message":    handleOfficeSendMessage,
}

// --- Session Tool Handlers ---

func handleCreateSession(args map[string]interface{}) ToolResult {
	workingDir, ok := requireStringArg(args, "workingDir")
	if !ok {
		return toolError("workingDir is required")
	}
	body := map[string]interface{}{
		"workingDir": workingDir,
		"mode":       getStringArg(args, "mode", "claude"),
	}
	if name := getStringArg(args, "name", ""); name != "" {
		body["name"] = name
	}
	result, err := apiPost("/api/sessions", body)
	if err != nil {
		return toolError(err.Error())
	}
	return toolSuccess(result)
}

func handleListSessions(_ map[string]interface{}) ToolResult {
	result, err := apiGet("/api/sessions")
	if err != nil {
		return toolError(err.Error())
	}
	return toolSuccess(result)
}

func handleGetSession(args map[string]interface{}) ToolResult {
	id, ok := requireStringArg(args, "sessionId")
	if !ok {
		return toolError("sessionId is required")
	}
	result, err := apiGet("/api/sessions/" + id)
	if err != nil {
		return toolError(err.Error())
	}
	return toolSuccess(result)
}

func handleStopSession(args map[string]interface{}) ToolResult {
	id, ok := requireStringArg(args, "sessionId")
	if !ok {
		return toolError("sessionId is required")
	}
	result, err := apiDelete("/api/sessions/" + id)
	if err != nil {
		return toolError(err.Error())
	}
	return toolSuccess(result)
}

func handleSendToSession(args map[string]interface{}) ToolResult {
	id, ok := requireStringArg(args, "sessionId")
	if !ok {
		return toolError("sessionId is required")
	}
	input, ok := requireStringArg(args, "input")
	if !ok {
		return toolError("input is required")
	}
	result, err := apiPost("/api/sessions/"+id+"/input", map[string]interface{}{
		"input": input,
	})
	if err != nil {
		return toolError(err.Error())
	}
	return toolSuccess(result)
}

func handleGetSessionOutput(args map[string]interface{}) ToolResult {
	id, ok := requireStringArg(args, "sessionId")
	if !ok {
		return toolError("sessionId is required")
	}
	result, err := apiGet("/api/sessions/" + id + "/output")
	if err != nil {
		return toolError(err.Error())
	}
	return toolSuccess(result)
}

// --- Agent Tool Handlers ---

func handleListAgents(args map[string]interface{}) ToolResult {
	path := "/api/subagents"
	if workingDir := getStringArg(args, "workingDir", ""); workingDir != "" {
		path += "?workingDir=" + url.QueryEscape(workingDir)
	}
	result, err := apiGet(path)
	if err != nil {
		return toolError(err.Error())
	}
	return toolSuccess(result)
}

func handleGetAgentStatus(args map[string]interface{}) ToolResult {
	id, ok := requireStringArg(args, "agentId")
	if !ok {
		return toolError("agentId is required")
	}
	result, err := apiGet("/api/subagents/" + id)
	if err != nil {
		return toolError(err.Error())
	}
	return toolSuccess(result)
}

func handleGetAgentTranscript(args map[string]interface{}) ToolResult {
	id, ok := requireStringArg(args, "agentId")
	if !ok {
		return toolError("agentId is required")
	}
	path := "/api/subagents/" + id + "/transcript"
	if limit := getStringArg(args, "limit", ""); limit != "" {
		path += "?limit=" + url.QueryEscape(limit)
	}
	result, err := apiGet(path)
	if err != nil {
		return toolError(err.Error())
	}
	return toolSuccess(result)
}

// --- Task Tool Handlers ---

func handleAddTask(args map[string]interface{}) ToolResult {
	prompt, ok := requireStringArg(args, "prompt")
	if !ok {
		return toolError("prompt is required")
	}
	body := map[string]interface{}{
		"prompt": prompt,
	}
	if workingDir := getStringArg(args, "workingDir", ""); workingDir != "" {
		body["workingDir"] = workingDir
	}
	if priority := getStringArg(args, "priority", ""); priority != "" {
		body["priority"] = priority
	}
	result, err := apiPost("/api/scheduled", body)
	if err != nil {
		return toolError(err.Error())
	}
	return toolSuccess(result)
}

func handleListTasks(args map[string]interface{}) ToolResult {
	path := "/api/scheduled"
	if status := getStringArg(args, "status", ""); status != "" {
		path += "?status=" + url.QueryEscape(status)
	}
	result, err := apiGet(path)
	if err != nil {
		return toolError(err.Error())
	}
	return toolSuccess(result)
}

func handleGetTask(args map[string]interface{}) ToolResult {
	id, ok := requireStringArg(args, "taskId")
	if !ok {
		return toolError("taskId is required")
	}
	result, err := apiGet("/api/scheduled/" + id)
	if err != nil {
		return toolError(err.Error())
	}
	return toolSuccess(result)
}

func handleRemoveTask(args map[string]interface{}) ToolResult {
	id, ok := requireStringArg(args, "taskId")
	if !ok {
		return toolError("taskId is required")
	}
	result, err := apiDelete("/api/scheduled/" + id)
	if err != nil {
		return toolError(err.Error())
	}
	return toolSuccess(result)
}

// --- Skill Tool Handlers ---

func handleListSkills(_ map[string]interface{}) ToolResult {
	result, err := apiGet("/api/office/agent/skills")
	if err != nil {
		return toolError(err.Error())
	}
	return toolSuccess(result)
}

func handleGetSkill(args map[string]interface{}) ToolResult {
	name, ok := requireStringArg(args, "skillName")
	if !ok {
		return toolError("skillName is required")
	}
	path := "/api/office/agent/skills/" + url.PathEscape(name)
	if file := getStringArg(args, "file", ""); file != "" {
		path += "?file=" + url.QueryEscape(file)
	}
	result, err := apiGet(path)
	if err != nil {
		return toolError(err.Error())
	}
	return toolSuccess(result)
}

func handleCreateSkill(args map[string]interface{}) ToolResult {
	name, ok := requireStringArg(args, "skillName")
	if !ok {
		return toolError("skillName is required")
	}
	content, ok := requireStringArg(args, "content")
	if !ok {
		return toolError("content is required")
	}
	body := map[string]interface{}{
		"skillName": name,
		"content":   content,
	}
	if file := getStringArg(args, "file", ""); file != "" {
		body["file"] = file
	}
	result, err := apiPost("/api/office/agent/skill-create", body)
	if err != nil {
		return toolError(err.Error())
	}
	return toolSuccess(result)
}

func handleSearchSkills(args map[string]interface{}) ToolResult {
	query, ok := requireStringArg(args, "query")
	if !ok {
		return toolError("query is required")
	}
	result, err := apiGet("/api/office/agent/skills/search?q=" + url.QueryEscape(query))
	if err != nil {
		return toolError(err.Error())
	}
	return toolSuccess(result)
}

func handleDeleteSkill(args map[string]interface{}) ToolResult {
	name, ok := requireStringArg(args, "skillName")
	if !ok {
		return toolError("skillName is required")
	}

	// delete_skill operates on the local filesystem
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return toolError("failed to get home directory: " + err.Error())
	}

	// Validate name to prevent path traversal
	if strings.ContainsAny(name, "/\\..") || strings.Contains(name, "..") {
		return toolError("invalid skill name")
	}

	skillsBase := filepath.Join(homeDir, ".claude", "skills")
	skillPath := filepath.Join(skillsBase, name)

	// Verify resolved path is still under skills directory
	absSkillPath, _ := filepath.Abs(skillPath)
	absSkillsBase, _ := filepath.Abs(skillsBase)
	if !strings.HasPrefix(absSkillPath, absSkillsBase+string(filepath.Separator)) {
		return toolError("invalid skill name")
	}

	file := getStringArg(args, "file", "")

	if file != "" {
		// Validate file name too
		if strings.ContainsAny(file, "/\\") || strings.Contains(file, "..") {
			return toolError("invalid file name")
		}
		targetPath := filepath.Join(skillPath, file)
		if err := os.Remove(targetPath); err != nil {
			return toolError("failed to delete skill file: " + err.Error())
		}
		return toolSuccessText(fmt.Sprintf("Deleted skill file: %s/%s", name, file))
	}

	// Delete the entire skill directory
	if err := os.RemoveAll(skillPath); err != nil {
		return toolError("failed to delete skill: " + err.Error())
	}
	return toolSuccessText(fmt.Sprintf("Deleted skill: %s", name))
}

// --- Monitoring Tool Handlers ---
// These tools find the session for a workingDir, then query that session's endpoint.

func findSessionByWorkingDir(workingDir string) (string, error) {
	data, err := apiGet("/api/sessions")
	if err != nil {
		return "", err
	}

	// The response should be a list of sessions
	sessions, ok := data.([]interface{})
	if !ok {
		// Try map with "sessions" key
		if m, ok := data.(map[string]interface{}); ok {
			if s, ok := m["sessions"]; ok {
				sessions, _ = s.([]interface{})
			}
		}
	}

	for _, s := range sessions {
		session, ok := s.(map[string]interface{})
		if !ok {
			continue
		}
		if wd, ok := session["workingDir"].(string); ok && wd == workingDir {
			if id, ok := session["id"].(string); ok {
				return id, nil
			}
		}
	}
	return "", fmt.Errorf("no session found for workingDir: %s", workingDir)
}

func handleGetGitStatus(args map[string]interface{}) ToolResult {
	workingDir, ok := requireStringArg(args, "workingDir")
	if !ok {
		return toolError("workingDir is required")
	}
	sessionID, err := findSessionByWorkingDir(workingDir)
	if err != nil {
		return toolError(err.Error())
	}
	result, err := apiGet("/api/sessions/" + sessionID + "/git-status")
	if err != nil {
		return toolError(err.Error())
	}
	return toolSuccess(result)
}

func handleGetBuildStatus(args map[string]interface{}) ToolResult {
	workingDir, ok := requireStringArg(args, "workingDir")
	if !ok {
		return toolError("workingDir is required")
	}
	sessionID, err := findSessionByWorkingDir(workingDir)
	if err != nil {
		return toolError(err.Error())
	}
	result, err := apiGet("/api/sessions/" + sessionID + "/build-status")
	if err != nil {
		return toolError(err.Error())
	}
	return toolSuccess(result)
}

func handleGetTestStatus(args map[string]interface{}) ToolResult {
	workingDir, ok := requireStringArg(args, "workingDir")
	if !ok {
		return toolError("workingDir is required")
	}
	sessionID, err := findSessionByWorkingDir(workingDir)
	if err != nil {
		return toolError(err.Error())
	}
	result, err := apiGet("/api/sessions/" + sessionID + "/test-status")
	if err != nil {
		return toolError(err.Error())
	}
	return toolSuccess(result)
}

func handleGetIntegrationState(args map[string]interface{}) ToolResult {
	workingDir, ok := requireStringArg(args, "workingDir")
	if !ok {
		return toolError("workingDir is required")
	}
	sessionID, err := findSessionByWorkingDir(workingDir)
	if err != nil {
		return toolError(err.Error())
	}
	result, err := apiGet("/api/sessions/" + sessionID + "/integrations")
	if err != nil {
		return toolError(err.Error())
	}
	return toolSuccess(result)
}

// --- State Tool Handlers ---

func handleGetAppState(_ map[string]interface{}) ToolResult {
	result, err := apiGet("/api/status")
	if err != nil {
		return toolError(err.Error())
	}
	return toolSuccess(result)
}

func handleGetStats(_ map[string]interface{}) ToolResult {
	result, err := apiGet("/api/stats")
	if err != nil {
		return toolError(err.Error())
	}
	return toolSuccess(result)
}

func handleGetConfig(_ map[string]interface{}) ToolResult {
	result, err := apiGet("/api/config")
	if err != nil {
		return toolError(err.Error())
	}
	return toolSuccess(result)
}

// --- Planning Tool Handlers ---

func handleGeneratePlan(args map[string]interface{}) ToolResult {
	desc, ok := requireStringArg(args, "taskDescription")
	if !ok {
		return toolError("taskDescription is required")
	}
	body := map[string]interface{}{
		"taskDescription": desc,
	}
	if workingDir := getStringArg(args, "workingDir", ""); workingDir != "" {
		body["workingDir"] = workingDir
	}
	result, err := apiPost("/api/generate-plan-detailed", body)
	if err != nil {
		return toolError(err.Error())
	}
	return toolSuccess(result)
}

// --- Dynamic Tool Handlers ---

func handleRegisterTool(args map[string]interface{}) ToolResult {
	name, ok := requireStringArg(args, "name")
	if !ok {
		return toolError("name is required")
	}
	description, ok := requireStringArg(args, "description")
	if !ok {
		return toolError("description is required")
	}
	prompt, ok := requireStringArg(args, "prompt")
	if !ok {
		return toolError("prompt is required")
	}
	workingDir, ok := requireStringArg(args, "workingDir")
	if !ok {
		return toolError("workingDir is required")
	}

	dynamicToolsMu.Lock()
	dynamicTools[name] = DynamicTool{
		Name:        name,
		Description: description,
		Prompt:      prompt,
		WorkingDir:  workingDir,
	}
	dynamicToolsMu.Unlock()

	return toolSuccessText(fmt.Sprintf("Registered dynamic tool: %s", name))
}

func handleUnregisterTool(args map[string]interface{}) ToolResult {
	name, ok := requireStringArg(args, "name")
	if !ok {
		return toolError("name is required")
	}

	dynamicToolsMu.Lock()
	if _, exists := dynamicTools[name]; !exists {
		dynamicToolsMu.Unlock()
		return toolError(fmt.Sprintf("dynamic tool %q not found", name))
	}
	delete(dynamicTools, name)
	dynamicToolsMu.Unlock()

	return toolSuccessText(fmt.Sprintf("Unregistered dynamic tool: %s", name))
}

func handleListDynamicTools(_ map[string]interface{}) ToolResult {
	dynamicToolsMu.RLock()
	tools := make([]DynamicTool, 0, len(dynamicTools))
	for _, t := range dynamicTools {
		tools = append(tools, t)
	}
	dynamicToolsMu.RUnlock()

	return toolSuccess(tools)
}

func handleRunPrompt(args map[string]interface{}) ToolResult {
	prompt, ok := requireStringArg(args, "prompt")
	if !ok {
		return toolError("prompt is required")
	}
	workingDir, ok := requireStringArg(args, "workingDir")
	if !ok {
		return toolError("workingDir is required")
	}

	body := map[string]interface{}{
		"command":    prompt,
		"workingDir": workingDir,
	}
	if model := getStringArg(args, "model", ""); model != "" {
		body["model"] = model
	}

	result, err := apiPost("/api/run", body)
	if err != nil {
		return toolError(err.Error())
	}
	return toolSuccess(result)
}

// dispatchToolCall handles a tools/call request by looking up the handler.
func dispatchToolCall(params ToolCallParams) ToolResult {
	// Check static tools first
	handler, ok := allTools[params.Name]
	if ok {
		return handler(params.Arguments)
	}

	// Check dynamic tools
	dynamicToolsMu.RLock()
	dynTool, found := dynamicTools[params.Name]
	dynamicToolsMu.RUnlock()

	if found {
		// Execute dynamic tool by running its prompt
		return handleRunPrompt(map[string]interface{}{
			"prompt":     dynTool.Prompt,
			"workingDir": dynTool.WorkingDir,
		})
	}

	return toolError(fmt.Sprintf("unknown tool: %s", params.Name))
}

// getAllToolDefs returns static tool defs plus any registered dynamic tools.
func getAllToolDefs() []ToolDefinition {
	dynamicToolsMu.RLock()
	defer dynamicToolsMu.RUnlock()

	defs := make([]ToolDefinition, len(allToolDefs))
	copy(defs, allToolDefs)

	for _, dt := range dynamicTools {
		defs = append(defs, ToolDefinition{
			Name:        dt.Name,
			Description: dt.Description,
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"input": {Type: "string", Description: "Input for the dynamic tool"},
				},
			},
		})
	}

	return defs
}

// --- Office Agent Tool Handlers ---

// officeSessionID returns the JARVIS_SESSION_ID env var, required for office agent tools.
func officeSessionID() (string, error) {
	sid := os.Getenv("JARVIS_SESSION_ID")
	if sid == "" {
		return "", fmt.Errorf("JARVIS_SESSION_ID not set -- office tools require a session context")
	}
	return sid, nil
}

func handleOfficeTaskList(_ map[string]interface{}) ToolResult {
	sid, err := officeSessionID()
	if err != nil {
		return toolError(err.Error())
	}
	result, err := apiGet("/api/office/agent/tasks?sessionId=" + sid)
	if err != nil {
		return toolError(err.Error())
	}
	return toolSuccess(result)
}

func handleOfficeTaskSelfAdd(args map[string]interface{}) ToolResult {
	sid, err := officeSessionID()
	if err != nil {
		return toolError(err.Error())
	}
	title, ok := requireStringArg(args, "title")
	if !ok {
		return toolError("title is required")
	}
	desc, _ := requireStringArg(args, "description")
	priority := getStringArg(args, "priority", "medium")
	result, err := apiPost("/api/office/agent/task/self-add", map[string]interface{}{
		"sessionId":   sid,
		"title":       title,
		"description": desc,
		"priority":    priority,
	})
	if err != nil {
		return toolError(err.Error())
	}
	return toolSuccess(result)
}

func handleOfficeTaskAck(args map[string]interface{}) ToolResult {
	sid, err := officeSessionID()
	if err != nil {
		return toolError(err.Error())
	}
	taskID, ok := requireStringArg(args, "taskId")
	if !ok {
		return toolError("taskId is required")
	}
	result, err := apiPost("/api/office/agent/task-ack", map[string]interface{}{"sessionId": sid, "taskId": taskID})
	if err != nil {
		return toolError(err.Error())
	}
	return toolSuccess(result)
}

func handleOfficeTaskStart(args map[string]interface{}) ToolResult {
	sid, err := officeSessionID()
	if err != nil {
		return toolError(err.Error())
	}
	taskID, ok := requireStringArg(args, "taskId")
	if !ok {
		return toolError("taskId is required")
	}
	result, err := apiPost("/api/office/agent/task-start", map[string]interface{}{"sessionId": sid, "taskId": taskID})
	if err != nil {
		return toolError(err.Error())
	}
	return toolSuccess(result)
}

func handleOfficeTaskComplete(args map[string]interface{}) ToolResult {
	sid, err := officeSessionID()
	if err != nil {
		return toolError(err.Error())
	}
	taskID, ok := requireStringArg(args, "taskId")
	if !ok {
		return toolError("taskId is required")
	}
	result, err := apiPost("/api/office/agent/task-complete", map[string]interface{}{"sessionId": sid, "taskId": taskID})
	if err != nil {
		return toolError(err.Error())
	}
	return toolSuccess(result)
}

func handleOfficeTaskProgress(args map[string]interface{}) ToolResult {
	sid, err := officeSessionID()
	if err != nil {
		return toolError(err.Error())
	}
	taskID, ok := requireStringArg(args, "taskId")
	if !ok {
		return toolError("taskId is required")
	}
	note, _ := requireStringArg(args, "note")
	result, err := apiPost("/api/office/agent/task-progress", map[string]interface{}{
		"sessionId": sid, "taskId": taskID, "note": note,
	})
	if err != nil {
		return toolError(err.Error())
	}
	return toolSuccess(result)
}

func handleOfficeTaskBlocked(args map[string]interface{}) ToolResult {
	sid, err := officeSessionID()
	if err != nil {
		return toolError(err.Error())
	}
	taskID, ok := requireStringArg(args, "taskId")
	if !ok {
		return toolError("taskId is required")
	}
	reason, _ := requireStringArg(args, "reason")
	result, err := apiPost("/api/office/agent/task-progress", map[string]interface{}{
		"sessionId": sid, "taskId": taskID, "note": reason, "status": "blocked",
	})
	if err != nil {
		return toolError(err.Error())
	}
	return toolSuccess(result)
}

func handleOfficeStandupRespond(args map[string]interface{}) ToolResult {
	sid, err := officeSessionID()
	if err != nil {
		return toolError(err.Error())
	}
	content, ok := requireStringArg(args, "content")
	if !ok {
		return toolError("content is required")
	}
	result, err := apiPost("/api/office/agent/standup/response", map[string]interface{}{
		"sessionId": sid, "content": content,
	})
	if err != nil {
		return toolError(err.Error())
	}
	return toolSuccess(result)
}

func handleOfficeReflect(args map[string]interface{}) ToolResult {
	sid, err := officeSessionID()
	if err != nil {
		return toolError(err.Error())
	}
	learning, ok := requireStringArg(args, "learning")
	if !ok {
		return toolError("learning is required")
	}
	category := getStringArg(args, "category", "insight")
	result, err := apiPost("/api/office/agent/reflection/response", map[string]interface{}{
		"sessionId": sid, "learning": learning, "category": category,
	})
	if err != nil {
		return toolError(err.Error())
	}
	return toolSuccess(result)
}

func handleOfficeTeamRoster(_ map[string]interface{}) ToolResult {
	sid, err := officeSessionID()
	if err != nil {
		return toolError(err.Error())
	}
	result, err := apiGet("/api/office/agent/team-roster?sessionId=" + sid)
	if err != nil {
		return toolError(err.Error())
	}
	return toolSuccess(result)
}

func handleOfficeSendMessage(args map[string]interface{}) ToolResult {
	sid, err := officeSessionID()
	if err != nil {
		return toolError(err.Error())
	}
	toSID, ok := requireStringArg(args, "toSessionId")
	if !ok {
		return toolError("toSessionId is required")
	}
	content, ok := requireStringArg(args, "content")
	if !ok {
		return toolError("content is required")
	}
	result, err := apiPost("/api/office/agent/message", map[string]interface{}{
		"sessionId": sid, "toSessionId": toSID, "content": content,
	})
	if err != nil {
		return toolError(err.Error())
	}
	return toolSuccess(result)
}

