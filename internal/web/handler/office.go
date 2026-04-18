package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/claudeman/jarvis-go/internal/office"
	"github.com/claudeman/jarvis-go/internal/pty"
	"github.com/claudeman/jarvis-go/internal/teamtemplates"
	"github.com/claudeman/jarvis-go/internal/web/request"
)

// safeName validates that a name contains only safe characters for use in file paths.
// Rejects path traversal attempts (../, /, \) and non-alphanumeric chars except - and _.
var safeNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

func safeName(name string) bool {
	return len(name) > 0 && len(name) <= 128 && safeNameRe.MatchString(name)
}

// ---------------------------------------------------------------------------
// Request types
// ---------------------------------------------------------------------------

type memberOverride struct {
	ID           string   `json:"id"`
	Provider     string   `json:"provider"`
	Model        string   `json:"model"`
	Name         string   `json:"name,omitempty"`
	Role         string   `json:"role,omitempty"`
	Tier         string   `json:"tier,omitempty"`
	Color        string   `json:"color,omitempty"`
	Emoji        string   `json:"emoji,omitempty"`
	Personality  string   `json:"personality,omitempty"`
	SystemPrompt string   `json:"systemPrompt,omitempty"`
	Skills       []string `json:"skills,omitempty"`
	WorkingDir   string   `json:"workingDir,omitempty"`
}

type spawnTeamRequest struct {
	WorkingDir          string           `json:"workingDir"`
	TemplateID          string           `json:"templateId"`
	Mission             string           `json:"mission"`
	MemberOverrides     []memberOverride `json:"memberOverrides"`
	PerpetualMode       *bool            `json:"perpetualMode"` // nil = default true
	TelegramEnabled     bool             `json:"telegramEnabled"`
	TelegramIntervalMin int              `json:"telegramIntervalMin"`
}

type autoStandupRequest struct {
	Enabled  bool `json:"enabled"`
	Interval int  `json:"interval"`
}

type whiteboardRequest struct {
	SessionID string `json:"sessionId"`
	Text      string `json:"text"`
}

type bugTaskRequest struct {
	ReportedBy  string `json:"reportedBy"`
	AssignedTo  string `json:"assignedTo"`
	File        string `json:"file"`
	Line        int    `json:"line,omitempty"`
	Description string `json:"description"`
	Priority    string `json:"priority"`
	Category    string `json:"category"`
}

type bugTaskUpdateRequest struct {
	Status string `json:"status"`
}

type messageRequest struct {
	FromSessionID string `json:"fromSessionId"`
	ToSessionID   string `json:"toSessionId"`
	Content       string `json:"content"`
}

type standupUserMessageRequest struct {
	Content string `json:"content"`
	Type    string `json:"type,omitempty"`
}

type agentStandupResponseRequest struct {
	SessionID string `json:"sessionId"`
	Content   string `json:"content"`
}

type agentMessageRequest struct {
	SessionID     string `json:"sessionId"`
	FromSessionID string `json:"fromSessionId"`
	ToSessionID   string `json:"toSessionId"`
	Content       string `json:"content"`
}

type agentTaskStartRequest struct {
	SessionID string `json:"sessionId"`
	TaskID    string `json:"taskId"`
}

type agentTaskCompleteRequest struct {
	SessionID string `json:"sessionId"`
	TaskID    string `json:"taskId"`
	Result    string `json:"result"`
}

type agentTaskAckRequest struct {
	SessionID string `json:"sessionId"`
	TaskID    string `json:"taskId"`
}

type agentTaskProgressRequest struct {
	SessionID          string `json:"sessionId"`
	TaskID             string `json:"taskId"`
	Progress           string `json:"progress"`
	IsBlocked          bool   `json:"isBlocked,omitempty"`
	BlockerDescription string `json:"blockerDescription,omitempty"`
}

type agentTaskSelfAddRequest struct {
	SessionID   string `json:"sessionId"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Priority    string `json:"priority"`
}

type agentTaskTriageRequest struct {
	SessionID   string `json:"sessionId"`
	TaskID      string `json:"taskId"`
	ToSessionID string `json:"toSessionId"`
	Reason      string `json:"reason"`
}

type agentBrainstormResponseRequest struct {
	SessionID string `json:"sessionId"`
	Proposals string `json:"proposals"`
}

type agentLeaderTasksResponseRequest struct {
	SessionID string `json:"sessionId"`
	Content   string `json:"content"`
}

type agentReflectionResponseRequest struct {
	SessionID string `json:"sessionId"`
	TaskID    string `json:"taskId,omitempty"`
	TaskTitle string `json:"taskTitle,omitempty"`
	Learning  string `json:"learning"`
	Category  string `json:"category"`
}

type agentCuriosityDiscoveryRequest struct {
	SessionID        string   `json:"sessionId"`
	Summary          string   `json:"summary"`
	Discoveries      []string `json:"discoveries"`
	SelfAssignedTask string   `json:"selfAssignedTask,omitempty"`
	LedToCommit      bool     `json:"ledToCommit,omitempty"`
}

type agentFileLockRequest struct {
	SessionID string `json:"sessionId"`
	FilePath  string `json:"filePath"`
	Action    string `json:"action"`
}

type agentSkillCreateRequest struct {
	SkillName string `json:"skillName"`
	Content   string `json:"content"`
}

type agentMemoryWriteRequest struct {
	SessionID string `json:"sessionId"`
	Content   string `json:"content"`
}

type agentMemoryAppendRequest struct {
	SessionID string `json:"sessionId"`
	Entry     string `json:"entry"`
}

type agentMemorySectionRequest struct {
	SessionID string `json:"sessionId"`
	Name      string `json:"name"`
	Content   string `json:"content"`
	Append    bool   `json:"append,omitempty"`
}

type brainstormRequest struct {
	ParticipantIDs []string `json:"participantIds"`
}

type personalityRequest struct {
	Personality  string `json:"personality"`
	SystemPrompt string `json:"systemPrompt"`
}

type codeGraphImpactRequest struct {
	Files []string `json:"files"`
}

// ---------------------------------------------------------------------------
// Helper: read JSON body with size limit
// ---------------------------------------------------------------------------

func decodeBody(w http.ResponseWriter, r *http.Request, dst interface{}) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func ok(w http.ResponseWriter, extra map[string]interface{}) {
	resp := map[string]interface{}{"success": true}
	for k, v := range extra {
		resp[k] = v
	}
	request.WriteJSON(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------------------
// Template data (static for now)
// ---------------------------------------------------------------------------

// allTeamTemplates returns built-in templates merged with custom templates from the store.
func (ctx *Context) allTeamTemplates() []teamtemplates.TeamTemplate {
	builtIn := teamtemplates.BuiltInTemplates()
	custom := ctx.TeamTemplateStore.List()
	result := make([]teamtemplates.TeamTemplate, 0, len(builtIn)+len(custom))
	result = append(result, builtIn...)
	for _, t := range custom {
		result = append(result, *t)
	}
	return result
}

// findTeamTemplate looks up a template by ID from both built-in and custom templates.
func (ctx *Context) findTeamTemplate(id string) *teamtemplates.TeamTemplate {
	for _, tpl := range teamtemplates.BuiltInTemplates() {
		if tpl.ID == id {
			return &tpl
		}
	}
	return ctx.TeamTemplateStore.Get(id)
}

// ---------------------------------------------------------------------------
// Core status handlers
// ---------------------------------------------------------------------------

// OfficeStatus returns the current office state.
func (ctx *Context) OfficeStatus(w http.ResponseWriter, r *http.Request) {
	state := ctx.OfficeManager.GetOfficeState()
	ok(w, state)
}

// OfficeTemplates returns available team templates (built-in + custom).
func (ctx *Context) OfficeTemplates(w http.ResponseWriter, r *http.Request) {
	all := ctx.allTeamTemplates()
	// Add memberCount for frontend compatibility
	type tplJSON struct {
		teamtemplates.TeamTemplate
		MemberCount int `json:"memberCount"`
	}
	result := make([]tplJSON, len(all))
	for i, t := range all {
		result[i] = tplJSON{TeamTemplate: t, MemberCount: len(t.Members)}
	}
	ok(w, map[string]interface{}{
		"templates": result,
	})
}

// OfficeCreateTeamTemplate creates a new custom team template.
func (ctx *Context) OfficeCreateTeamTemplate(w http.ResponseWriter, r *http.Request) {
	var tpl teamtemplates.TeamTemplate
	if !decodeBody(w, r, &tpl) {
		return
	}
	if tpl.Name == "" {
		request.WriteError(w, http.StatusBadRequest, "name is required")
		return
	}
	if len(tpl.Members) == 0 {
		request.WriteError(w, http.StatusBadRequest, "at least one member is required")
		return
	}
	created := ctx.TeamTemplateStore.Create(tpl)
	request.WriteJSON(w, http.StatusOK, created)
}

// OfficeUpdateTeamTemplate updates a custom team template.
func (ctx *Context) OfficeUpdateTeamTemplate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	// Reject updates to built-in templates
	for _, b := range teamtemplates.BuiltInTemplates() {
		if b.ID == id {
			request.WriteError(w, http.StatusForbidden, "cannot edit built-in templates")
			return
		}
	}
	var tpl teamtemplates.TeamTemplate
	if !decodeBody(w, r, &tpl) {
		return
	}
	updated := ctx.TeamTemplateStore.Update(id, tpl)
	if updated == nil {
		request.WriteError(w, http.StatusNotFound, "template not found")
		return
	}
	request.WriteJSON(w, http.StatusOK, updated)
}

// OfficeDeleteTeamTemplate deletes a custom team template.
func (ctx *Context) OfficeDeleteTeamTemplate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	// Reject deletion of built-in templates
	for _, b := range teamtemplates.BuiltInTemplates() {
		if b.ID == id {
			request.WriteError(w, http.StatusForbidden, "cannot delete built-in templates")
			return
		}
	}
	if !ctx.TeamTemplateStore.Delete(id) {
		request.WriteError(w, http.StatusNotFound, "template not found")
		return
	}
	ok(w, map[string]interface{}{"deleted": true})
}

// OfficeDuplicateTeamTemplate duplicates any template (built-in or custom) as a new custom template.
func (ctx *Context) OfficeDuplicateTeamTemplate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	// Check built-in first for the source
	var builtIn *teamtemplates.TeamTemplate
	for _, b := range teamtemplates.BuiltInTemplates() {
		if b.ID == id {
			builtIn = &b
			break
		}
	}
	dup := ctx.TeamTemplateStore.Duplicate(id, builtIn)
	if dup == nil {
		request.WriteError(w, http.StatusNotFound, "template not found")
		return
	}
	request.WriteJSON(w, http.StatusOK, dup)
}

// OfficeTemplateFromTeam saves the currently running team as a new custom template.
func (ctx *Context) OfficeTemplateFromTeam(w http.ResponseWriter, r *http.Request) {
	if !ctx.OfficeManager.IsTeamSpawned() {
		request.WriteError(w, http.StatusBadRequest, "no team is currently spawned")
		return
	}
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Category    string `json:"category"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if req.Name == "" {
		request.WriteError(w, http.StatusBadRequest, "name is required")
		return
	}

	roster := ctx.OfficeManager.GetRoster()
	members := make([]teamtemplates.TeamTemplateMember, 0, len(roster))
	for _, agent := range roster {
		cfg := agent.Config
		m := teamtemplates.TeamTemplateMember{
			ID:   cfg.ID,
			Name: cfg.Name,
			Role: cfg.Role,
			Tier: cfg.Tier,
			Color: cfg.Color,
		}
		if po := agent.GetPersonalityOverride(); po != nil {
			m.Personality = po.Personality
			m.SystemPrompt = po.SystemPrompt
		} else {
			m.Personality = cfg.Personality
			m.SystemPrompt = cfg.SystemPrompt
		}
		m.Skills = cfg.Skills
		members = append(members, m)
	}

	saved := ctx.TeamTemplateStore.Create(teamtemplates.TeamTemplate{
		Name:        req.Name,
		Description: req.Description,
		Category:    req.Category,
		Members:     members,
	})
	request.WriteJSON(w, http.StatusOK, saved)
}

// OfficeHealth returns office health info.
func (ctx *Context) OfficeHealth(w http.ResponseWriter, r *http.Request) {
	mgr := ctx.OfficeManager
	settings := mgr.GetOfficeSettings()
	teamSpawnedAt, _ := settings["teamSpawnedAt"].(int64)
	var uptime int64
	if teamSpawnedAt > 0 {
		uptime = time.Now().UnixMilli() - teamSpawnedAt
	}
	ok(w, map[string]interface{}{
		"status":             "healthy",
		"teamSpawned":        settings["teamSpawned"],
		"memberCount":        settings["memberCount"],
		"activeMeetingCount": settings["activeMeetingCount"],
		"uptime":             uptime,
		"metrics":            settings["metrics"],
		"timestamp":          time.Now().UnixMilli(),
	})
}

// OfficeSpawnTeam spawns a team from a template.
// Creates a Claude session for each team member, registers them with the
// office manager, and broadcasts session:created events.
// spawnAgentSession creates either an SDK or PTY session for a team member.
// Returns the sessionID and mode used, or empty string on failure.
func spawnAgentSession(ctx *Context, member teamtemplates.TeamTemplateMember, req spawnTeamRequest, envVars map[string]string, teamConfigs []*office.TeamMemberConfig, perpetual bool, trySDK bool) (string, string) {
	if trySDK {
		agentMemory := office.LoadAgentMemory(member.ID, req.WorkingDir)
		kickstart := office.BuildKickstartPrompt(
			&office.TeamMemberConfig{ID: member.ID, Name: member.Name, Role: member.Role, Tier: member.Tier, Color: member.Color},
			req.WorkingDir, teamConfigs, req.Mission, agentMemory, perpetual,
		)
		// Pre-generate session ID so we can inject it into MCP env and subprocess env.
		sessionID := uuid.New().String()
		// Agent token authorizes /api/office/agent/* calls from `cm` and jarvis-mcp.
		// When per-member env comes from getProviderEnvVarsFor (Z.AI rate-limit
		// allocation) the token is dropped, so we re-inject it here. Without
		// this every `cm skill read`, `cm task list`, etc. gets 401.
		agentToken := ""
		if ctx.AgentTokenMgr != nil {
			agentToken = ctx.AgentTokenMgr.Token()
		}
		mcpJarvisEnv := map[string]string{
			"JARVIS_API_URL":    "http://localhost:3555",
			"JARVIS_SESSION_ID": sessionID,
		}
		if agentToken != "" {
			mcpJarvisEnv["JARVIS_AGENT_TOKEN"] = agentToken
		}
		mcpServers := map[string]any{
			"jarvis": map[string]any{
				"type":    "stdio",
				"command": "jarvis-mcp",
				"env":     mcpJarvisEnv,
			},
			"codegraphcontext": map[string]any{
				"type":    "stdio",
				"command": "cgc",
				"args":    []string{"mcp", "start"},
			},
		}
		// Also inject Jarvis env into subprocess so `cm` bash commands work.
		agentEnv := make(map[string]string, len(envVars)+3)
		for k, v := range envVars {
			agentEnv[k] = v
		}
		agentEnv["JARVIS_API_URL"] = "http://localhost:3555"
		agentEnv["JARVIS_SESSION_ID"] = sessionID
		if agentToken != "" {
			agentEnv["JARVIS_AGENT_TOKEN"] = agentToken
		}
		sdkSession, err := ctx.SessionManager.CreateSDKSession(
			context.Background(),
			pty.SessionConfig{
				ID:           sessionID,
				WorkingDir:   req.WorkingDir,
				Mode:         pty.ModeClaudeSDK,
				Name:         member.Name,
				ProviderEnv:  agentEnv,
				SystemPrompt: kickstart,
				McpServers:   mcpServers,
			},
		)
		if err != nil {
			slog.Error("Failed to create SDK session, falling back to PTY", "name", member.Name, "error", err)
		} else {
			// Start event forwarding before broadcast to avoid losing early events
			go forwardSDKSessionEvents(sdkSession, ctx)
			ctx.SSEBroker.Broadcast("session:created", map[string]interface{}{
				"id": sdkSession.ID, "name": member.Name, "workingDir": req.WorkingDir,
				"mode": "claude-sdk", "status": "idle", "createdAt": sdkSession.CreatedAt.UnixMilli(),
			})
			return sdkSession.ID, "sdk"
		}
	}

	// PTY fallback
	// Ensure agent token is present so `cm` calls from the shell can
	// authenticate. See the SDK path for the full reasoning.
	ptyEnv := make(map[string]string, len(envVars)+1)
	for k, v := range envVars {
		ptyEnv[k] = v
	}
	if ctx.AgentTokenMgr != nil {
		if tok := ctx.AgentTokenMgr.Token(); tok != "" {
			ptyEnv["JARVIS_AGENT_TOKEN"] = tok
		}
	}
	session, err := ctx.SessionManager.CreateSession(
		context.Background(),
		pty.SessionConfig{
			WorkingDir:  req.WorkingDir,
			Mode:        pty.ModeClaude,
			Name:        member.Name,
			ProviderEnv: ptyEnv,
		},
	)
	if err != nil {
		slog.Error("Failed to create PTY session", "name", member.Name, "error", err)
		return "", ""
	}
	go forwardSessionEvents(session, ctx)
	ctx.SSEBroker.Broadcast("session:created", sessionToJSON(session))
	return session.ID, "pty"
}

func (ctx *Context) OfficeSpawnTeam(w http.ResponseWriter, r *http.Request) {
	var req spawnTeamRequest
	if !decodeBody(w, r, &req) {
		return
	}
	ctx.spawnTeamFromRequest(w, req)
}

// spawnTeamFromRequest performs the validated spawn flow given a decoded
// spawnTeamRequest. Extracted from OfficeSpawnTeam so ConciergeSpawnTeam can
// call it directly without re-marshalling and rewriting r.Body.
func (ctx *Context) spawnTeamFromRequest(w http.ResponseWriter, req spawnTeamRequest) {
	// Refuse new team spawns when today's spending cap is reached.
	if ctx.IsSpendingFrozen() {
		request.WriteCodedError(w, http.StatusTooManyRequests, request.ErrCodeSpendingFrozen,
			"daily spending cap reached; new team spawns are frozen until the cap is raised or the day rolls over")
		return
	}
	if req.WorkingDir == "" {
		request.WriteError(w, http.StatusBadRequest, "workingDir is required")
		return
	}
	resolvedDir, err := ctx.ResolveWorkingDir(req.WorkingDir)
	if err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid working directory: "+err.Error())
		return
	}
	req.WorkingDir = resolvedDir
	// Atomically claim the spawn slot. A plain IsTeamSpawned check is
	// racy: two rapid POSTs would both pass, both run the spawn loop,
	// and RegisterAgent would overwrite configIndex entries, orphaning
	// the first-spawned session. Rollback happens below if zero agents
	// successfully register.
	if !ctx.OfficeManager.TrySetTeamSpawning() {
		request.WriteError(w, http.StatusConflict, "Team has already been spawned")
		return
	}

	// Resolve template members from built-in or custom templates
	var members []teamtemplates.TeamTemplateMember
	templateUsed := "office-7"
	if req.TemplateID != "" {
		if tpl := ctx.findTeamTemplate(req.TemplateID); tpl != nil {
			members = tpl.Members
			templateUsed = tpl.ID
		}
	}
	if members == nil {
		// Default to office-7
		if tpl := ctx.findTeamTemplate("office-7"); tpl != nil {
			members = tpl.Members
		} else {
			members = teamtemplates.BuiltInTemplates()[0].Members
		}
	}

	// Build default provider env vars
	defaultProviderEnv := ctx.getProviderEnvVars()

	// Check if using Z.AI -- allocate models across agents to respect rate limits.
	settings := ctx.StateStore.GetSettings()
	provider, _ := settings["provider"].(string)
	isZAI := provider == "zai-glm"

	// Collect requested models per member for rate-limit-aware allocation
	memberIDs := make([]string, 0, len(members))
	requestedModels := make(map[string]string)
	memberProviders := make(map[string]string)
	for _, member := range members {
		memberIDs = append(memberIDs, member.ID)
	}
	for _, ov := range req.MemberOverrides {
		requestedModels[ov.ID] = ov.Model
		memberProviders[ov.ID] = ov.Provider
	}

	// Allocate Sonnet models across agents to distribute rate limits
	var sonnetAlloc map[string]string
	if isZAI {
		sonnetAlloc = AllocateZAIModels(requestedModels, memberIDs)
		slog.Info("Z.AI model allocation", "allocation", sonnetAlloc)
	}

	// Build per-member provider override map with allocated Sonnet models
	overrideMap := make(map[string]map[string]string)
	for _, ov := range req.MemberOverrides {
		sonnet := ""
		if sonnetAlloc != nil {
			sonnet = sonnetAlloc[ov.ID]
		}
		overrideMap[ov.ID] = ctx.getProviderEnvVarsFor(ov.Provider, ov.Model, sonnet)
	}

	spawned := make([]string, 0, len(members))
	errors := make([]string, 0)

	// Build member override lookup for name/role/personality/etc overrides
	memberOverrideMap := make(map[string]memberOverride)
	for _, ov := range req.MemberOverrides {
		memberOverrideMap[ov.ID] = ov
	}

	// Apply name/role/tier/color/emoji overrides to template members before spawning
	for i, m := range members {
		if ov, has := memberOverrideMap[m.ID]; has {
			if ov.Name != "" {
				members[i].Name = ov.Name
			}
			if ov.Role != "" {
				members[i].Role = ov.Role
			}
			if ov.Tier != "" {
				members[i].Tier = ov.Tier
			}
			if ov.Color != "" {
				members[i].Color = ov.Color
			}
			if ov.Emoji != "" {
				members[i].Emoji = ov.Emoji
			}
			// Personality and skills are set on the template member for kickstart use
			if ov.Personality != "" {
				members[i].Personality = ov.Personality
			}
			if ov.SystemPrompt != "" {
				members[i].SystemPrompt = ov.SystemPrompt
			}
			if len(ov.Skills) > 0 {
				members[i].Skills = ov.Skills
			}
		}
	}

	slog.Info("Spawning office team", "template", templateUsed, "members", len(members), "workingDir", req.WorkingDir)

	// Build kickstart prompts ahead of time for SDK mode
	teamConfigs := make([]*office.TeamMemberConfig, 0, len(members))
	for _, m := range members {
		teamConfigs = append(teamConfigs, &office.TeamMemberConfig{
			ID: m.ID, Name: m.Name, Role: m.Role, Tier: m.Tier, Color: m.Color,
			Personality: m.Personality, SystemPrompt: m.SystemPrompt, Skills: m.Skills,
		})
	}

	// Set perpetual mode (default: true)
	perpetual := true
	if req.PerpetualMode != nil {
		perpetual = *req.PerpetualMode
	}

	// Index the project with CodeGraphContext for optimized context.
	go func() {
		cgcCtx, cgcCancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cgcCancel()
		cmd := exec.CommandContext(cgcCtx, "cgc", "index", req.WorkingDir)
		cmd.Dir = req.WorkingDir
		if out, err := cmd.CombinedOutput(); err != nil {
			outStr := string(out)
			if len(outStr) > 200 {
				outStr = outStr[:200]
			}
			slog.Warn("CGC index failed (non-fatal)", "error", err, "output", outStr)
		} else {
			slog.Info("CGC index complete", "workingDir", req.WorkingDir)
		}
	}()

	// Determine session mode: SDK (default for agents) or PTY (fallback)
	useSDK := true // Use SDK mode by default for office agents

	for _, member := range members {
		// Use member-specific provider env if overridden, else apply default with allocated sonnet
		envVars := defaultProviderEnv
		if ov, hasOverride := overrideMap[member.ID]; hasOverride {
			envVars = ov
		} else if isZAI && sonnetAlloc != nil {
			envVars = ctx.getProviderEnvVarsFor(provider, "", sonnetAlloc[member.ID])
		}

		// Support per-agent working directory override
		spawnReq := req
		if ov, has := memberOverrideMap[member.ID]; has && ov.WorkingDir != "" {
			spawnReq.WorkingDir = ov.WorkingDir
		}

		sessionID, usedMode := spawnAgentSession(ctx, member, spawnReq, envVars, teamConfigs, perpetual, useSDK)
		if sessionID == "" {
			errors = append(errors, fmt.Sprintf("Failed to create session for %s", member.Name))
			continue
		}

		// Register agent with full config including personality and skills
		ctx.OfficeManager.RegisterAgent(sessionID, member.ID, &office.TeamMemberConfig{
			ID:           member.ID,
			Name:         member.Name,
			Role:         member.Role,
			Tier:         member.Tier,
			Color:        member.Color,
			Personality:  member.Personality,
			SystemPrompt: member.SystemPrompt,
			Skills:       member.Skills,
		})

		// Record the resolved provider + model so BuildMemberPayload can
		// surface them to the UI (Active Now cards, current-team banner).
		// Priority (highest first):
		//   1. per-spawn memberOverride from the modal
		//   2. per-template member.Provider / member.Model (custom
		//      templates can pin a specific LLM per member)
		//   3. team-wide default provider, plus Z.AI sonnet allocation
		resolvedProvider := provider
		resolvedModel := ""
		if member.Provider != "" {
			resolvedProvider = member.Provider
		}
		if member.Model != "" {
			resolvedModel = member.Model
		}
		if ov, has := memberOverrideMap[member.ID]; has {
			if ov.Provider != "" {
				resolvedProvider = ov.Provider
			}
			if ov.Model != "" {
				resolvedModel = ov.Model
			}
		}
		if resolvedModel == "" && isZAI && sonnetAlloc != nil {
			if m, ok := sonnetAlloc[member.ID]; ok {
				resolvedModel = m
			}
		}
		if agent := ctx.OfficeManager.GetAgent(sessionID); agent != nil {
			agent.SetProvider(resolvedProvider, resolvedModel)
		}

		if ctx.TelegramHandler != nil {
			ctx.TelegramHandler.NotifySessionStart(sessionID, member.Name, req.WorkingDir)
		}

		spawned = append(spawned, member.Name)
		slog.Info("Spawned team member", "name", member.Name, "sessionId", sessionID, "role", member.Role, "mode", usedMode)
	}

	// Release the atomic claim if nothing actually spawned -- otherwise
	// the team sits in "spawned with 0 agents" limbo and the operator
	// can't retry without restarting jarvis.
	if len(spawned) == 0 {
		ctx.OfficeManager.SetTeamSpawned(false)
		request.WriteError(w, http.StatusInternalServerError, "Failed to spawn any agents: "+strings.Join(errors, "; "))
		return
	}

	// Record the spawn context so the UI can show a "current team" banner
	// while the team is active. Must happen BEFORE SetTeamSpawned(true) so
	// any consumer listening on that signal already sees the context.
	ctx.OfficeManager.SetSpawnContext(templateUsed, req.WorkingDir)

	// Mark team as spawned (also triggers StartIdleMonitoring). The flag
	// itself is already true from TrySetTeamSpawning above; this call is
	// what starts the monitoring goroutines.
	ctx.OfficeManager.SetTeamSpawned(true)

	// Set team mission if provided
	if req.Mission != "" {
		ctx.OfficeManager.SetTeamMission(req.Mission)
	}

	ctx.OfficeManager.SetPerpetualMode(perpetual)

	// Inject kickstart prompts into PTY sessions (SDK sessions get kickstart via SystemPrompt).
	if !useSDK {
	teamMission := ctx.OfficeManager.GetTeamMission()
	roster := ctx.OfficeManager.GetRoster()
	for _, agent := range roster {
		go func(a *office.AgentState) {
			// A panic here (malformed memory file, nil deref in
			// BuildKickstartPrompt, PTY write on a closed fd, etc.)
			// would otherwise take down the whole jarvis process and
			// all running agents with it.
			defer func() {
				if r := recover(); r != nil {
					slog.Error("kickstart injection panic", "agent", a.ConfigID, "err", r)
				}
			}()
			agentMemory := office.LoadAgentMemory(a.ConfigID, req.WorkingDir)
			prompt := office.BuildKickstartPrompt(a.Config, req.WorkingDir, teamConfigs, teamMission, agentMemory, perpetual)
			if prompt == "" {
				return
			}

			s, ok := ctx.SessionManager.GetSession(a.SessionID)
			if !ok {
				return
			}

			// Use output observer to detect when Claude's prompt is ready.
			// Claude shows ">" or "❯" when its TextInput is ready for input.
			ready := make(chan struct{}, 1)
			unsubscribe := s.AddOutputObserver(func(data string) {
				// Look for Claude's prompt indicator in terminal output
				if strings.Contains(data, "❯") || strings.Contains(data, "> ") ||
					strings.Contains(data, "bypass permissions") {
					select {
					case ready <- struct{}{}:
					default:
					}
				}
			})

			// Wait up to 60s for Claude to be ready
			select {
			case <-ready:
				slog.Info("Claude prompt detected, injecting kickstart", "name", a.Config.Name)
			case <-time.After(60 * time.Second):
				slog.Warn("Timed out waiting for Claude prompt, injecting anyway", "name", a.Config.Name)
			}
			unsubscribe()

			// Extra delay after prompt appears to ensure Ink's TextInput is fully initialized.
			// 3s gives enough time for all 6+ agents on server hardware.
			time.Sleep(3 * time.Second)

			// Send kickstart and verify it was accepted (retry up to 3 times).
			maxRetries := 3
			for attempt := 0; attempt < maxRetries; attempt++ {
				s.WriteInkPrompt(prompt)
				slog.Info("Kickstart attempt", "name", a.Config.Name, "attempt", attempt+1, "session", a.SessionID)

				// Watch for output activity (Claude responding = prompt accepted)
				activity := make(chan struct{}, 1)
				unsub := s.AddOutputObserver(func(data string) {
					if len(data) > 20 {
						select {
						case activity <- struct{}{}:
						default:
						}
					}
				})

				select {
				case <-activity:
					unsub()
					slog.Info("Kickstart confirmed -- agent is working", "name", a.Config.Name)
					return // Success
				case <-time.After(30 * time.Second):
					unsub()
					slog.Warn("No activity after kickstart, retrying", "name", a.Config.Name, "attempt", attempt+1)
					// Try sending just Enter in case text was pasted but not submitted
					s.WriteToPTY("\r")
					time.Sleep(5 * time.Second)
				}
			}
			slog.Error("Kickstart failed after all retries", "name", a.Config.Name, "session", a.SessionID)
		}(agent)
	}
	} // end if !useSDK

	// Configure Telegram updates if requested
	if req.TelegramEnabled {
		interval := 60
		if req.TelegramIntervalMin > 0 {
			interval = req.TelegramIntervalMin
		}
		ctx.OfficeManager.SetTelegramConfig(true, interval)
		ctx.OfficeManager.NotifyOfficeEvent("Team Spawned", fmt.Sprintf("%d members from template %s", len(spawned), templateUsed))
	}

	ok(w, map[string]interface{}{
		"spawned":      spawned,
		"errors":       errors,
		"templateUsed": templateUsed,
	})
}

// OfficeShutdownTeam shuts down the team.
func (ctx *Context) OfficeShutdownTeam(w http.ResponseWriter, r *http.Request) {
	ctx.OfficeManager.NotifyOfficeEvent("Team Shutting Down", "")

	// Record experiment before shutdown so metrics are captured.
	spawnedAt := ctx.OfficeManager.GetTeamSpawnedAt()
	record := ctx.OfficeManager.BuildExperimentRecord(spawnedAt)
	if err := office.RecordExperiment(record); err != nil {
		slog.Warn("Failed to record experiment", "err", err)
	}

	// Stop idle monitoring and auto-standup timers.
	ctx.OfficeManager.StopIdleMonitoring()

	// Send shutdown message to each agent and stop their sessions.
	shutdownMsg := "SHUTDOWN: The team is shutting down. Your session will be terminated."
	roster := ctx.OfficeManager.GetRoster()
	for _, agent := range roster {
		// Try PTY session first, then SDK session
		if s, found := ctx.SessionManager.GetSession(agent.SessionID); found {
			s.WriteViaScreen(shutdownMsg + "\r")
			_ = ctx.SessionManager.StopSession(context.Background(), agent.SessionID)
		} else if sdk, found := ctx.SessionManager.GetSDKSession(agent.SessionID); found {
			sdk.WriteInkPrompt(shutdownMsg)
			_ = ctx.SessionManager.StopSDKSession(agent.SessionID)
		}
		ctx.cleanupSessionResources(agent.SessionID)
		ctx.OfficeManager.UnregisterAgent(agent.SessionID)
	}

	ctx.OfficeManager.SetTeamSpawned(false)
	ok(w, nil)
}

// ---------------------------------------------------------------------------
// Concierge handlers (Gemini Live voice assistant)
// ---------------------------------------------------------------------------

// ConciergeConfig returns the Gemini API key + model + voice to the authenticated
// browser so it can connect directly to the Gemini Live WebSocket API.
//
// SECURITY NOTE: returning the raw key to the browser is the path-of-least-
// resistance for the Gemini Live handshake but means a stolen session cookie
// or XSS yields the key. The safer fix is a server-side WebSocket proxy so
// the key never leaves the Jarvis process. Tracked as a follow-up.
func (ctx *Context) ConciergeConfig(w http.ResponseWriter, r *http.Request) {
	settings := ctx.StateStore.GetSettings()
	apiKey, _ := settings["geminiApiKey"].(string)
	if apiKey == "" {
		request.WriteError(w, http.StatusPreconditionFailed, "Gemini API key not configured -- set it in Settings")
		return
	}
	workingDir, _ := settings["defaultWorkingDir"].(string)
	if workingDir == "" {
		workingDir = "/app/workspace"
	}
	voice, _ := settings["geminiVoice"].(string)
	if voice == "" {
		voice = "Charon" // Jarvis-style butler voice default
	}
	// Two models:
	//   voiceModel: native-audio Live model for voice (AUDIO-only responses)
	//   textModel:  general Live model for text chat (supports TEXT responses)
	// Note the Developer API model IDs differ from Vertex AI names; on
	// generativelanguage.googleapis.com we use the dated preview IDs.
	ok(w, map[string]interface{}{
		"apiKey":     apiKey,
		"voiceModel": "gemini-2.5-flash-native-audio-preview-12-2025",
		"textModel":  "gemini-2.0-flash-live-001",
		"voice":      voice,
		"workingDir": workingDir,
	})
}

// ConciergeTemplates returns the template list formatted for Gemini function calling.
func (ctx *Context) ConciergeTemplates(w http.ResponseWriter, r *http.Request) {
	ok(w, map[string]interface{}{"templates": ctx.allTeamTemplates()})
}

// conciergeSpawnRequest is the simplified shape the Gemini tool call sends.
type conciergeSpawnRequest struct {
	TemplateID string `json:"templateId"`
	Mission    string `json:"mission"`
	WorkingDir string `json:"workingDir"`
	Perpetual  *bool  `json:"perpetual"`
}

// ConciergeSpawnTeam is called by the Gemini concierge's function-call handler.
// It validates the request, falls back to the default working directory, and
// forwards to the shared spawnTeamFromRequest helper.
func (ctx *Context) ConciergeSpawnTeam(w http.ResponseWriter, r *http.Request) {
	var req conciergeSpawnRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if req.TemplateID == "" {
		request.WriteError(w, http.StatusBadRequest, "templateId is required")
		return
	}
	if req.Mission == "" {
		request.WriteError(w, http.StatusBadRequest, "mission is required")
		return
	}
	// Validate template ID against known templates (built-in + custom).
	if ctx.findTeamTemplate(req.TemplateID) == nil {
		request.WriteError(w, http.StatusBadRequest, "unknown templateId")
		return
	}

	// Fall back to default working directory from settings if not provided.
	if req.WorkingDir == "" {
		settings := ctx.StateStore.GetSettings()
		wd, _ := settings["defaultWorkingDir"].(string)
		if wd == "" {
			wd = "/app/workspace"
		}
		req.WorkingDir = wd
	}

	// Build the spawnTeamRequest and call the shared helper directly.
	// (No r.Body rewriting -- avoids double MaxBytesReader wrap and
	// header/content-length mismatch.) The IsTeamSpawned check is
	// authoritative inside spawnTeamFromRequest, so no redundant check here.
	spawnReq := spawnTeamRequest{
		WorkingDir:    req.WorkingDir,
		TemplateID:    req.TemplateID,
		Mission:       req.Mission,
		PerpetualMode: req.Perpetual,
	}
	ctx.spawnTeamFromRequest(w, spawnReq)
}

// ---------------------------------------------------------------------------
// Settings & config handlers
// ---------------------------------------------------------------------------

// OfficeGetSettings returns office settings.
func (ctx *Context) OfficeGetSettings(w http.ResponseWriter, r *http.Request) {
	settings := ctx.OfficeManager.GetOfficeSettings()
	ok(w, settings)
}

// OfficePutSettings updates office settings.
func (ctx *Context) OfficePutSettings(w http.ResponseWriter, r *http.Request) {
	var body map[string]interface{}
	if !decodeBody(w, r, &body) {
		return
	}
	mgr := ctx.OfficeManager

	// Handle autoStandup sub-object
	if autoStandup, isMap := body["autoStandup"].(map[string]interface{}); isMap {
		if enabled, isBool := autoStandup["enabled"].(bool); isBool {
			mgr.SetAutoStandupEnabled(enabled)
		}
		if intervalMs, isFloat := autoStandup["intervalMs"].(float64); isFloat && intervalMs >= 60000 {
			cfg := mgr.GetConfig()
			updated := *cfg
			updated.AutoStandupInterval = time.Duration(intervalMs) * time.Millisecond
			mgr.SetConfig(&updated)
		}
	}

	// Apply general settings with validation
	mgr.UpdateOfficeSettings(body)

	// Return full updated settings
	settings := mgr.GetOfficeSettings()
	ok(w, settings)
}

// OfficeGetAutoStandup returns auto-standup configuration.
func (ctx *Context) OfficeGetAutoStandup(w http.ResponseWriter, r *http.Request) {
	cfg := ctx.OfficeManager.GetConfig()
	ok(w, map[string]interface{}{
		"enabled":    ctx.OfficeManager.IsAutoStandupEnabled(),
		"intervalMs": int(cfg.AutoStandupInterval.Milliseconds()),
	})
}

// OfficePutAutoStandup updates auto-standup configuration.
func (ctx *Context) OfficePutAutoStandup(w http.ResponseWriter, r *http.Request) {
	var req autoStandupRequest
	if !decodeBody(w, r, &req) {
		return
	}

	ctx.OfficeManager.SetAutoStandupEnabled(req.Enabled)

	cfg := ctx.OfficeManager.GetConfig()
	updated := *cfg
	if req.Interval > 0 {
		// Interval is in milliseconds from the frontend
		updated.AutoStandupInterval = time.Duration(req.Interval) * time.Millisecond
	}
	ctx.OfficeManager.SetConfig(&updated)

	ctx.SSEBroker.Broadcast("office:autoStandupScheduled", map[string]interface{}{
		"enabled":    req.Enabled,
		"intervalMs": int(updated.AutoStandupInterval.Milliseconds()),
	})

	ok(w, map[string]interface{}{
		"enabled":    req.Enabled,
		"intervalMs": int(updated.AutoStandupInterval.Milliseconds()),
	})
}

// ---------------------------------------------------------------------------
// Telegram config endpoints
// ---------------------------------------------------------------------------

type officeTelegramConfigRequest struct {
	Enabled     bool `json:"enabled"`
	IntervalMin int  `json:"intervalMin"`
}

// OfficeGetTelegramConfig returns the current Telegram update configuration.
func (ctx *Context) OfficeGetTelegramConfig(w http.ResponseWriter, r *http.Request) {
	enabled, intervalMin, lastUpdate := ctx.OfficeManager.GetTelegramConfig()
	telegramConnected := ctx.TelegramHandler != nil && ctx.TelegramHandler.IsConnected()
	var lastUpdateMs int64
	if !lastUpdate.IsZero() {
		lastUpdateMs = lastUpdate.UnixMilli()
	}
	ok(w, map[string]interface{}{
		"enabled":           enabled,
		"intervalMin":       intervalMin,
		"lastUpdateAt":      lastUpdateMs,
		"telegramConnected": telegramConnected,
	})
}

// OfficePutTelegramConfig updates the Telegram update configuration.
func (ctx *Context) OfficePutTelegramConfig(w http.ResponseWriter, r *http.Request) {
	var req officeTelegramConfigRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if req.Enabled && (ctx.TelegramHandler == nil || !ctx.TelegramHandler.IsConnected()) {
		request.WriteError(w, http.StatusBadRequest, "Telegram is not connected")
		return
	}
	intervalMin := req.IntervalMin
	if intervalMin < 10 {
		intervalMin = 10
	}
	ctx.OfficeManager.SetTelegramConfig(req.Enabled, intervalMin)
	ok(w, map[string]interface{}{
		"enabled":     req.Enabled,
		"intervalMin": intervalMin,
	})
}

// ---------------------------------------------------------------------------
// Data endpoints
// ---------------------------------------------------------------------------

// OfficeGetWhiteboard returns all whiteboard notes.
func (ctx *Context) OfficeGetWhiteboard(w http.ResponseWriter, r *http.Request) {
	notes := ctx.OfficeManager.GetWhiteboard()
	ok(w, map[string]interface{}{"notes": notes})
}

// OfficePostWhiteboard adds a whiteboard note.
func (ctx *Context) OfficePostWhiteboard(w http.ResponseWriter, r *http.Request) {
	var req whiteboardRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if req.SessionID == "" || req.Text == "" {
		request.WriteError(w, http.StatusBadRequest, "sessionId and text are required")
		return
	}
	if len(req.Text) > 200 {
		request.WriteError(w, http.StatusBadRequest, "Note too long (max 200 chars)")
		return
	}
	ctx.OfficeManager.SetWhiteboardNote(req.SessionID, req.Text)
	ctx.SSEBroker.Broadcast("office:whiteboardUpdated", map[string]interface{}{"sessionId": req.SessionID})
	ok(w, nil)
}

// OfficeGetTaskQueues returns task queues for all agents.
func (ctx *Context) OfficeGetTaskQueues(w http.ResponseWriter, r *http.Request) {
	roster := ctx.OfficeManager.GetRoster()
	queues := make([]map[string]interface{}, 0, len(roster))
	for _, a := range roster {
		if a.Config == nil {
			continue
		}
		queues = append(queues, map[string]interface{}{
			"sessionId": a.SessionID,
			"agentName": a.Config.Name,
			"tasks":     a.GetTasks(),
		})
	}
	ok(w, map[string]interface{}{"queues": queues})
}

// OfficeGetTaskQueueBySession returns tasks for a specific session.
func (ctx *Context) OfficeGetTaskQueueBySession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionId")
	agent := ctx.OfficeManager.GetAgent(sessionID)
	if agent == nil || agent.Config == nil {
		request.WriteError(w, http.StatusNotFound, "agent not found")
		return
	}
	ok(w, map[string]interface{}{"queue": map[string]interface{}{
		"sessionId": agent.SessionID,
		"agentName": agent.Config.Name,
		"tasks":     agent.GetTasks(),
	}})
}

// OfficeGetBugTasks returns all bug tasks.
func (ctx *Context) OfficeGetBugTasks(w http.ResponseWriter, r *http.Request) {
	tasks := ctx.OfficeManager.GetBugTasks()
	ok(w, map[string]interface{}{"tasks": tasks})
}

// OfficePostBugTask creates a new bug task.
func (ctx *Context) OfficePostBugTask(w http.ResponseWriter, r *http.Request) {
	var req bugTaskRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if req.ReportedBy == "" || req.AssignedTo == "" || req.File == "" || req.Description == "" || req.Priority == "" || req.Category == "" {
		request.WriteError(w, http.StatusBadRequest, "reportedBy, assignedTo, file, description, priority, and category are required")
		return
	}
	task := &office.BugTask{
		ID:          uuid.New().String(),
		ReportedBy:  req.ReportedBy,
		AssignedTo:  req.AssignedTo,
		File:        req.File,
		Line:        req.Line,
		Description: req.Description,
		Priority:    req.Priority,
		Category:    req.Category,
		Status:      "open",
		CreatedAt:   time.Now(),
	}
	ctx.OfficeManager.AddBugTask(task)
	ctx.SSEBroker.Broadcast("office:bugTaskCreated", map[string]interface{}{"task": task})
	ok(w, map[string]interface{}{"task": task})
}

// OfficePutBugTask updates a bug task.
func (ctx *Context) OfficePutBugTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req bugTaskUpdateRequest
	if !decodeBody(w, r, &req) {
		return
	}
	ctx.OfficeManager.UpdateBugTask(id, req.Status)
	ctx.SSEBroker.Broadcast("office:bugTaskUpdated", map[string]interface{}{"id": id, "status": req.Status})
	if req.Status == "resolved" {
		ctx.SSEBroker.Broadcast("office:bugTaskResolved", map[string]interface{}{"id": id, "status": req.Status})
	}
	ok(w, nil)
}

// OfficeGetFileLocks returns all active file locks.
func (ctx *Context) OfficeGetFileLocks(w http.ResponseWriter, r *http.Request) {
	locks := ctx.OfficeManager.GetLocks()
	ok(w, map[string]interface{}{"locks": locks})
}

// OfficeDeleteFileLock releases a file lock by path.
func (ctx *Context) OfficeDeleteFileLock(w http.ResponseWriter, r *http.Request) {
	filePath := chi.URLParam(r, "*")
	ctx.OfficeManager.ForceReleaseLock(filePath)
	ctx.SSEBroker.Broadcast("office:fileLockChanged", map[string]interface{}{
		"action": "unlocked",
		"lock": map[string]interface{}{
			"filePath":  filePath,
			"agentName": "admin",
		},
	})
	ok(w, nil)
}

// OfficeGetMessages returns office messages.
func (ctx *Context) OfficeGetMessages(w http.ResponseWriter, r *http.Request) {
	msgs := ctx.OfficeManager.GetMessages()
	ok(w, map[string]interface{}{"messages": msgs})
}

// OfficePostMessage sends an office message.
func (ctx *Context) OfficePostMessage(w http.ResponseWriter, r *http.Request) {
	var req messageRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if req.FromSessionID == "" || req.ToSessionID == "" || req.Content == "" {
		request.WriteError(w, http.StatusBadRequest, "fromSessionId, toSessionId, and content are required")
		return
	}
	if len(req.Content) > 500 {
		request.WriteError(w, http.StatusBadRequest, "Message too long (max 500 chars)")
		return
	}
	ctx.OfficeManager.SendMessage(req.FromSessionID, req.ToSessionID, req.Content)
	ok(w, nil)
}

// OfficeGetMetrics returns office metrics.
func (ctx *Context) OfficeGetMetrics(w http.ResponseWriter, r *http.Request) {
	snap := ctx.OfficeManager.GetMetricsSnapshot()
	ok(w, map[string]interface{}{"metrics": snap})
}

// OfficeGetExperiments returns the experiment history log.
func (ctx *Context) OfficeGetExperiments(w http.ResponseWriter, r *http.Request) {
	records := office.LoadExperimentHistory()
	ok(w, map[string]interface{}{"experiments": records})
}

// OfficeGetScores returns current agent performance scores.
func (ctx *Context) OfficeGetScores(w http.ResponseWriter, r *http.Request) {
	scores := ctx.OfficeManager.CalculateTeamScores()
	ok(w, map[string]interface{}{"scores": scores})
}

// OfficeGetAuditLog returns audit log entries.
func (ctx *Context) OfficeGetAuditLog(w http.ResponseWriter, r *http.Request) {
	entries := ctx.OfficeManager.GetAuditEntries()
	ok(w, map[string]interface{}{"entries": entries})
}

// OfficeGetMeetings returns all meetings.
func (ctx *Context) OfficeGetMeetings(w http.ResponseWriter, r *http.Request) {
	meetings := ctx.OfficeManager.GetMeetings()
	ok(w, map[string]interface{}{"meetings": meetings})
}

// OfficeGetLearnings returns all learnings.
func (ctx *Context) OfficeGetLearnings(w http.ResponseWriter, r *http.Request) {
	homeDir, _ := os.UserHomeDir()
	learningsDir := filepath.Join(homeDir, ".jarvis", "learnings")
	entries, err := os.ReadDir(learningsDir)
	if err != nil {
		ok(w, map[string]interface{}{"learnings": []interface{}{}})
		return
	}
	var learnings []map[string]interface{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		info, infoErr := e.Info()
		var modTime string
		if infoErr == nil {
			modTime = info.ModTime().Format(time.RFC3339)
		}
		configID := strings.TrimSuffix(e.Name(), ".json")
		learnings = append(learnings, map[string]interface{}{
			"configId": configID,
			"file":     e.Name(),
			"modified": modTime,
		})
	}
	if learnings == nil {
		learnings = make([]map[string]interface{}, 0)
	}
	ok(w, map[string]interface{}{"learnings": learnings})
}

// OfficeGetLearningsByConfig returns learnings for a specific config ID.
// The frontend (office.js _renderDetailLearnings) expects data.learnings
// to be a flat array of learning entries. Previously the handler
// returned the ENTIRE parsed file -- a struct with {configId, name,
// learnings:[...]} -- nested under learnings, so data.learnings ended
// up being an object rather than an array. items.slice(0,30) threw and
// the modal showed "Failed to load learnings." for every agent.
func (ctx *Context) OfficeGetLearningsByConfig(w http.ResponseWriter, r *http.Request) {
	configID := chi.URLParam(r, "configId")
	if !safeName(configID) {
		request.WriteError(w, http.StatusBadRequest, "invalid config ID")
		return
	}
	homeDir, _ := os.UserHomeDir()
	learningPath := filepath.Join(homeDir, ".jarvis", "learnings", configID+".json")
	data, err := os.ReadFile(learningPath)
	if err != nil {
		if os.IsNotExist(err) {
			ok(w, map[string]interface{}{"learnings": []interface{}{}, "configId": configID})
			return
		}
		request.WriteError(w, http.StatusInternalServerError, "failed to read learnings file")
		return
	}

	// Decode the file as the canonical AgentLearningData shape and hand
	// the UI the entries array directly.
	var parsed struct {
		ConfigID  string      `json:"configId"`
		Name      string      `json:"name"`
		Learnings interface{} `json:"learnings"`
	}
	if jsonErr := json.Unmarshal(data, &parsed); jsonErr != nil {
		request.WriteError(w, http.StatusInternalServerError, "failed to parse learnings file: "+jsonErr.Error())
		return
	}
	out := parsed.Learnings
	if out == nil {
		out = []interface{}{}
	}
	ok(w, map[string]interface{}{
		"learnings": out,
		"configId":  configID,
		"name":      parsed.Name,
	})
}

// OfficeGetCodeGraph returns the code graph by walking the team's working directory.
func (ctx *Context) OfficeGetCodeGraph(w http.ResponseWriter, r *http.Request) {
	graph := ctx.buildCodeGraph()
	ok(w, map[string]interface{}{
		"graph":     graph,
		"available": graph != nil,
	})
}

// OfficePostCodeGraphBuild triggers a code graph build (same as get).
func (ctx *Context) OfficePostCodeGraphBuild(w http.ResponseWriter, r *http.Request) {
	graph := ctx.buildCodeGraph()
	ok(w, map[string]interface{}{
		"graph":     graph,
		"available": graph != nil,
	})
}

// buildCodeGraph walks the working directory tree (max 3 levels, 500 entries).
func (ctx *Context) buildCodeGraph() interface{} {
	// Determine root directory from the first session's working directory.
	sessions := ctx.SessionManager.GetAllSessions()
	if len(sessions) == 0 {
		return nil
	}
	rootDir := sessions[0].WorkingDir
	if rootDir == "" {
		return nil
	}

	type dirEntry struct {
		Path      string        `json:"path"`
		Name      string        `json:"name"`
		IsDir     bool          `json:"isDir"`
		FileCount int           `json:"fileCount,omitempty"`
		Children  []interface{} `json:"children,omitempty"`
	}

	const maxEntries = 500
	const maxDepth = 3
	count := 0

	var entries []interface{}
	filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if count >= maxEntries {
			return filepath.SkipAll
		}

		rel, relErr := filepath.Rel(rootDir, path)
		if relErr != nil {
			return nil
		}
		if rel == "." {
			return nil
		}

		// Check depth
		depth := len(strings.Split(rel, string(filepath.Separator)))
		if depth > maxDepth {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip hidden directories and common non-essential directories
		name := info.Name()
		if strings.HasPrefix(name, ".") && info.IsDir() {
			return filepath.SkipDir
		}
		if info.IsDir() && (name == "node_modules" || name == "vendor" || name == "__pycache__") {
			return filepath.SkipDir
		}

		entry := map[string]interface{}{
			"path":  rel,
			"name":  name,
			"isDir": info.IsDir(),
		}
		if !info.IsDir() {
			entry["size"] = info.Size()
		}
		entries = append(entries, entry)
		count++
		return nil
	})

	if entries == nil {
		entries = make([]interface{}, 0)
	}

	return map[string]interface{}{
		"root":    rootDir,
		"entries": entries,
		"total":   count,
	}
}

// OfficePostCodeGraphImpact returns impact analysis for files.
func (ctx *Context) OfficePostCodeGraphImpact(w http.ResponseWriter, r *http.Request) {
	var req codeGraphImpactRequest
	if !decodeBody(w, r, &req) {
		return
	}

	// Determine root directory from the first session's working directory.
	rootDir := ""
	sessions := ctx.SessionManager.GetAllSessions()
	if len(sessions) > 0 {
		rootDir = sessions[0].WorkingDir
	}

	type fileInfo struct {
		Path   string `json:"path"`
		Exists bool   `json:"exists"`
		Size   int64  `json:"size,omitempty"`
	}

	files := make([]fileInfo, 0, len(req.Files))
	for _, f := range req.Files {
		fullPath := f
		if rootDir != "" && !filepath.IsAbs(f) {
			fullPath = filepath.Join(rootDir, f)
		}
		info, err := os.Stat(fullPath)
		if err != nil {
			files = append(files, fileInfo{Path: f, Exists: false})
		} else {
			files = append(files, fileInfo{Path: f, Exists: true, Size: info.Size()})
		}
	}

	ok(w, map[string]interface{}{
		"files": files,
	})
}

// OfficeGetWrapUp returns wrap-up state.
func (ctx *Context) OfficeGetWrapUp(w http.ResponseWriter, r *http.Request) {
	status := ctx.OfficeManager.GetWrapUpStatus()
	ok(w, map[string]interface{}{
		"active":      status.Active,
		"startedAt":   status.StartedAt,
		"agentStatus": status.AgentStatus,
	})
}

// OfficePostWrapUp initiates a wrap-up.
func (ctx *Context) OfficePostWrapUp(w http.ResponseWriter, r *http.Request) {
	mgr := ctx.OfficeManager
	if !mgr.IsTeamSpawned() {
		request.WriteError(w, http.StatusBadRequest, "No team is spawned")
		return
	}
	if mgr.IsWrappingUp() {
		status := mgr.GetWrapUpStatus()
		ok(w, map[string]interface{}{
			"alreadyActive": true,
			"active":        status.Active,
			"startedAt":     status.StartedAt,
			"agentStatus":   status.AgentStatus,
		})
		return
	}
	mgr.SetWrappingUp(true)
	ctx.SSEBroker.Broadcast("office:wrapUpInitiated", map[string]interface{}{"active": true})
	mgr.NotifyOfficeEvent("Wrap-up Initiated", "")

	// Stop idle monitoring so no new brainstorms/replenishment triggers
	mgr.StopIdleMonitoring()

	// Send wrap-up notification to all team sessions with clear instructions.
	wrapUpPrompt := `[WRAP-UP INITIATED] The team is wrapping up. Follow these steps:

1. FINISH your current task -- complete what you're working on right now
2. COMMIT all changes with a clear commit message
3. SAVE your work context: cm memory write "<summary of what you did, current state, and any unfinished work>"
4. DO NOT start any new tasks from your queue
5. DO NOT pick up any new work
6. Report your final status: cm task complete <taskId>
7. Once you've committed, saved memory, and reported status, stop working

This is a graceful shutdown. Take the time to finish cleanly, but do not begin anything new.`

	// Fan out the wrap-up prompt AND cancel queued work that hasn't
	// started yet. The prompt tells agents "DO NOT start new tasks" but
	// marking pending/acknowledged tasks as failed is the backstop: an
	// agent that ignores the prompt still can't pull a fresh task
	// because there are none left in TaskPending state.
	roster := mgr.GetRoster()
	notifiedSessionIDs := make([]string, 0, len(roster))
	missedAgents := make([]string, 0)
	cancelledTasks := 0

	for _, agent := range roster {
		delivered := false
		if s, found := ctx.SessionManager.GetSession(agent.SessionID); found {
			if s.WriteInkPrompt(wrapUpPrompt) {
				delivered = true
			}
		} else if sdk, found := ctx.SessionManager.GetSDKSession(agent.SessionID); found {
			if sdk.WriteInkPrompt(wrapUpPrompt) {
				delivered = true
			}
		}

		if delivered {
			notifiedSessionIDs = append(notifiedSessionIDs, agent.SessionID)
		} else {
			name := agent.SessionID
			if agent.Config != nil {
				name = agent.Config.Name
			}
			missedAgents = append(missedAgents, name)
		}

		// Cancel all queued (but not yet started) tasks. TaskInProgress
		// is deliberately left alone so agents can FINISH current work
		// -- that's step 1 of the wrap-up prompt.
		for _, t := range agent.GetTasks() {
			if t.Status == office.TaskPending || t.Status == office.TaskAcknowledged {
				if agent.UpdateTaskStatus(t.ID, office.TaskFailed) {
					cancelledTasks++
				}
			}
		}
	}

	if len(missedAgents) > 0 {
		slog.Warn("Wrap-up prompt undelivered for some agents",
			"missed", missedAgents, "notified", len(notifiedSessionIDs))
	}
	slog.Info("Wrap-up initiated",
		"notified", len(notifiedSessionIDs),
		"missed", len(missedAgents),
		"tasksCancelled", cancelledTasks)

	status := mgr.GetWrapUpStatus()
	ok(w, map[string]interface{}{
		"active":         status.Active,
		"startedAt":      status.StartedAt,
		"agentStatus":    status.AgentStatus,
		"sessionIds":     notifiedSessionIDs,
		"cancelledTasks": cancelledTasks,
	})
}

// OfficePostWrapUpCancel cancels a wrap-up.
func (ctx *Context) OfficePostWrapUpCancel(w http.ResponseWriter, r *http.Request) {
	ctx.OfficeManager.SetWrappingUp(false)
	ctx.SSEBroker.Broadcast("office:wrapUpCancelled", map[string]interface{}{"active": false})
	ok(w, nil)
}

// ---------------------------------------------------------------------------
// Standup handlers
// ---------------------------------------------------------------------------

// OfficeGetStandup returns the current standup state.
func (ctx *Context) OfficeGetStandup(w http.ResponseWriter, r *http.Request) {
	su := ctx.OfficeManager.GetStandup()
	if su == nil {
		ok(w, map[string]interface{}{"standup": nil})
		return
	}
	ok(w, map[string]interface{}{
		"standup": map[string]interface{}{
			"active":       su.Active,
			"participants": su.Participants,
			"responses":    su.Responses,
			"startedAt":    su.StartedAt.Format(time.RFC3339),
			"currentIndex": su.CurrentIndex,
		},
	})
}

// OfficePostStandup starts a new standup.
func (ctx *Context) OfficePostStandup(w http.ResponseWriter, r *http.Request) {
	// Decode optional body for action-based control (start/stop)
	var body map[string]interface{}
	_ = json.NewDecoder(r.Body).Decode(&body) // ignore error for empty body

	action, _ := body["action"].(string)
	if action == "stop" {
		ctx.OfficeManager.StopStandup()
		ok(w, map[string]interface{}{"standup": ctx.OfficeManager.GetStandup()})
		return
	}

	// Start standup -- this sends prompts to agents, collects responses, persists
	participantIDs, _ := body["participantIds"].([]interface{})
	var ids []string
	for _, id := range participantIDs {
		if s, ok := id.(string); ok {
			ids = append(ids, s)
		}
	}

	standup := ctx.OfficeManager.StartStandup(ids)
	ok(w, map[string]interface{}{"standup": standup})
}

// OfficePostStandupUserMessage sends a user message during standup.
func (ctx *Context) OfficePostStandupUserMessage(w http.ResponseWriter, r *http.Request) {
	var req standupUserMessageRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if req.Content == "" {
		request.WriteError(w, http.StatusBadRequest, "content is required")
		return
	}
	su := ctx.OfficeManager.GetStandup()
	if su == nil || !su.Active {
		request.WriteError(w, http.StatusBadRequest, "No active standup")
		return
	}
	// Broadcast user message to all clients via SSE.
	ctx.SSEBroker.Broadcast("office:standup:user-message", map[string]interface{}{
		"message": req.Content,
	})
	ok(w, nil)
}

// OfficeGetStandupHistory returns standup history.
func (ctx *Context) OfficeGetStandupHistory(w http.ResponseWriter, r *http.Request) {
	homeDir, _ := os.UserHomeDir()
	standupsDir := filepath.Join(homeDir, ".jarvis", "office-memory", "standups")
	entries, err := os.ReadDir(standupsDir)
	if err != nil {
		ok(w, map[string]interface{}{"standups": []interface{}{}})
		return
	}
	var standups []map[string]interface{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		info, infoErr := e.Info()
		var modTime string
		if infoErr == nil {
			modTime = info.ModTime().Format(time.RFC3339)
		}
		standupID := strings.TrimSuffix(e.Name(), ".json")
		standups = append(standups, map[string]interface{}{
			"id":       standupID,
			"file":     e.Name(),
			"modified": modTime,
		})
	}
	if standups == nil {
		standups = make([]map[string]interface{}, 0)
	}
	ok(w, map[string]interface{}{"standups": standups})
}

// OfficeGetStandupByID returns a specific historical standup.
func (ctx *Context) OfficeGetStandupByID(w http.ResponseWriter, r *http.Request) {
	standupID := chi.URLParam(r, "standupId")
	if !safeName(standupID) {
		request.WriteError(w, http.StatusBadRequest, "invalid standup ID")
		return
	}
	homeDir, _ := os.UserHomeDir()
	standupPath := filepath.Join(homeDir, ".jarvis", "office-memory", "standups", standupID+".json")
	data, err := os.ReadFile(standupPath)
	if err != nil {
		if os.IsNotExist(err) {
			request.WriteError(w, http.StatusNotFound, "standup not found")
			return
		}
		request.WriteError(w, http.StatusInternalServerError, "failed to read standup file")
		return
	}

	var parsed interface{}
	if jsonErr := json.Unmarshal(data, &parsed); jsonErr != nil {
		parsed = string(data)
	}
	ok(w, map[string]interface{}{"standup": parsed, "id": standupID})
}

// ---------------------------------------------------------------------------
// Agent callback handlers
// ---------------------------------------------------------------------------

// OfficeAgentGetTelegramConfig returns Telegram config relevant to agents.
// GET /api/office/agent/telegram-config
func (ctx *Context) OfficeAgentGetTelegramConfig(w http.ResponseWriter, r *http.Request) {
	enabled, intervalMin, _ := ctx.OfficeManager.GetTelegramConfig()
	ok(w, map[string]interface{}{
		"enabled":     enabled,
		"intervalMin": intervalMin,
	})
}

// OfficeAgentPostTelegramConfig updates Telegram config from an agent.
// POST /api/office/agent/telegram-config
func (ctx *Context) OfficeAgentPostTelegramConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled     bool `json:"enabled"`
		IntervalMin int  `json:"intervalMin"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	intervalMin := req.IntervalMin
	if intervalMin < 1 {
		intervalMin = 10
	}
	ctx.OfficeManager.SetTelegramConfig(req.Enabled, intervalMin)
	ok(w, nil)
}

// OfficeAgentStandupResponse handles an agent's standup response.
func (ctx *Context) OfficeAgentStandupResponse(w http.ResponseWriter, r *http.Request) {
	var req agentStandupResponseRequest
	if !decodeBody(w, r, &req) {
		return
	}
	ctx.OfficeManager.SubmitStandupResponse(req.SessionID, req.Content)
	ok(w, nil)
}

// OfficeAgentTeamRoster returns the team roster for agents.
func (ctx *Context) OfficeAgentTeamRoster(w http.ResponseWriter, r *http.Request) {
	roster := ctx.OfficeManager.GetRoster()
	result := make([]map[string]interface{}, 0, len(roster))
	for _, a := range roster {
		if a.Config == nil {
			continue
		}
		result = append(result, map[string]interface{}{
			"sessionId": a.SessionID,
			"configId":  a.ConfigID,
			"name":      a.Config.Name,
			"role":      a.Config.Role,
			"tier":      a.Config.Tier,
		})
	}
	ok(w, map[string]interface{}{"roster": result})
}

// OfficeAgentMessage handles an inter-agent message.
func (ctx *Context) OfficeAgentMessage(w http.ResponseWriter, r *http.Request) {
	var req agentMessageRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if req.FromSessionID == "" || req.ToSessionID == "" || req.Content == "" {
		request.WriteError(w, http.StatusBadRequest, "fromSessionId, toSessionId, and content are required")
		return
	}
	if len(req.Content) > 8000 {
		request.WriteError(w, http.StatusBadRequest, "Message too long (max 8000 chars)")
		return
	}
	ctx.OfficeManager.SendMessage(req.FromSessionID, req.ToSessionID, req.Content)
	ok(w, nil)
}

// OfficeAgentGetTasks returns tasks for the requesting agent.
func (ctx *Context) OfficeAgentGetTasks(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")
	agent := ctx.OfficeManager.GetAgentAndTouch(sessionID)
	if agent == nil {
		ok(w, map[string]interface{}{"tasks": []interface{}{}})
		return
	}
	ok(w, map[string]interface{}{"tasks": agent.GetTasks()})
}

// OfficeAgentTaskStart marks a task as in-progress.
func (ctx *Context) OfficeAgentTaskStart(w http.ResponseWriter, r *http.Request) {
	if ctx.OfficeManager.IsWrappingUp() {
		request.WriteError(w, http.StatusConflict, "Team is wrapping up, no new tasks accepted")
		return
	}
	var req agentTaskStartRequest
	if !decodeBody(w, r, &req) {
		return
	}
	agent := ctx.OfficeManager.GetAgentAndTouch(req.SessionID)
	if agent != nil {
		agent.UpdateTaskStatus(req.TaskID, office.TaskInProgress)
		ctx.SSEBroker.Broadcast("office:taskUpdated", map[string]interface{}{
			"sessionId": req.SessionID,
			"taskId":    req.TaskID,
			"status":    string(office.TaskInProgress),
		})
		ctx.SSEBroker.Broadcast("office:taskQueueUpdated", map[string]interface{}{
			"sessionId": req.SessionID,
		})
		if payload := ctx.OfficeManager.BuildMemberPayload(agent); payload != nil {
			ctx.SSEBroker.Broadcast("office:memberUpdated", payload)
		}
	}
	ok(w, nil)
}

// OfficeAgentTaskComplete marks a task as completed.
func (ctx *Context) OfficeAgentTaskComplete(w http.ResponseWriter, r *http.Request) {
	var req agentTaskCompleteRequest
	if !decodeBody(w, r, &req) {
		return
	}
	agent := ctx.OfficeManager.GetAgentAndTouch(req.SessionID)
	if agent != nil {
		agent.UpdateTaskStatus(req.TaskID, office.TaskCompleted)
		ctx.SSEBroker.Broadcast("office:taskUpdated", map[string]interface{}{
			"sessionId": req.SessionID,
			"taskId":    req.TaskID,
			"status":    string(office.TaskCompleted),
			"result":    req.Result,
		})
		ctx.SSEBroker.Broadcast("office:taskQueueUpdated", map[string]interface{}{
			"sessionId": req.SessionID,
		})

		// Broadcast full member payload with updated status
		if payload := ctx.OfficeManager.BuildMemberPayload(agent); payload != nil {
			ctx.SSEBroker.Broadcast("office:memberUpdated", payload)
		}

		// Immediate re-tasking (async to avoid blocking the HTTP response).
		nextTask := agent.NextPendingTask()
		if nextTask != nil {
			sid := agent.SessionID
			title := nextTask.Title
			taskID := nextTask.ID
			go func() {
				// Background prompt injection -- a panic here (closed
				// PTY fd, SDK writer race on session teardown) must not
				// take down the whole jarvis process. Same pattern as
				// the round-3 kickstart goroutine at office.go:798.
				defer func() {
					if r := recover(); r != nil {
						slog.Error("next-task prompt injection panic", "session", sid, "taskId", taskID, "err", r)
					}
				}()
				sessions := ctx.OfficeManager.Sessions()
				if sessions != nil {
					prompt := fmt.Sprintf("[NEXT TASK] %s\nAcknowledge and start: cm task ack %s && cm task start %s", title, taskID, taskID)
					sessions.WriteInkPrompt(sid, prompt)
				}
			}()
		} else {
			go ctx.OfficeManager.CheckForIdleAgents()
		}
	}
	ok(w, nil)
}

// OfficeAgentTaskAck acknowledges a task.
func (ctx *Context) OfficeAgentTaskAck(w http.ResponseWriter, r *http.Request) {
	var req agentTaskAckRequest
	if !decodeBody(w, r, &req) {
		return
	}
	agent := ctx.OfficeManager.GetAgentAndTouch(req.SessionID)
	if agent != nil {
		agent.UpdateTaskStatus(req.TaskID, office.TaskAcknowledged)
		ctx.SSEBroker.Broadcast("office:taskUpdated", map[string]interface{}{
			"sessionId": req.SessionID,
			"taskId":    req.TaskID,
			"status":    string(office.TaskAcknowledged),
		})
		if payload := ctx.OfficeManager.BuildMemberPayload(agent); payload != nil {
			ctx.SSEBroker.Broadcast("office:memberUpdated", payload)
		}
	}
	ok(w, nil)
}

// OfficeAgentTaskProgress updates task progress.
func (ctx *Context) OfficeAgentTaskProgress(w http.ResponseWriter, r *http.Request) {
	var req agentTaskProgressRequest
	if !decodeBody(w, r, &req) {
		return
	}
	agent := ctx.OfficeManager.GetAgentAndTouch(req.SessionID)
	if agent != nil {
		progress := req.Progress
		if req.IsBlocked {
			agent.UpdateTaskStatus(req.TaskID, office.TaskBlocked)
			if req.BlockerDescription != "" {
				progress = req.BlockerDescription
			}
		}
		agent.SetTaskProgress(req.TaskID, progress)
		ctx.SSEBroker.Broadcast("office:taskUpdated", map[string]interface{}{
			"sessionId": req.SessionID,
			"taskId":    req.TaskID,
			"progress":  progress,
			"isBlocked": req.IsBlocked,
		})
		if payload := ctx.OfficeManager.BuildMemberPayload(agent); payload != nil {
			ctx.SSEBroker.Broadcast("office:memberUpdated", payload)
		}
	}
	ok(w, nil)
}

// OfficeAgentTaskSelfAdd allows an agent to add a task to their own queue.
func (ctx *Context) OfficeAgentTaskSelfAdd(w http.ResponseWriter, r *http.Request) {
	if ctx.OfficeManager.IsWrappingUp() {
		request.WriteError(w, http.StatusConflict, "Team is wrapping up, no new tasks accepted")
		return
	}
	var req agentTaskSelfAddRequest
	if !decodeBody(w, r, &req) {
		return
	}
	agent := ctx.OfficeManager.GetAgentAndTouch(req.SessionID)
	if agent != nil {
		task := &office.Task{
			ID:          uuid.New().String(),
			Title:       req.Title,
			Description: req.Description,
			Priority:    req.Priority,
			Status:      office.TaskPending,
			AssignedAt:  time.Now(),
			Source:      "self",
		}
		agent.AddTask(task)

		ctx.SSEBroker.Broadcast("office:taskUpdated", map[string]interface{}{
			"sessionId": req.SessionID,
			"taskId":    task.ID,
			"status":    string(office.TaskPending),
			"title":     req.Title,
		})
		ctx.SSEBroker.Broadcast("office:taskQueueUpdated", map[string]interface{}{
			"sessionId": req.SessionID,
		})
		if payload := ctx.OfficeManager.BuildMemberPayload(agent); payload != nil {
			ctx.SSEBroker.Broadcast("office:memberUpdated", payload)
		}
	}
	ok(w, nil)
}

// OfficeAgentTaskTriage handles task triage by a leader agent.
func (ctx *Context) OfficeAgentTaskTriage(w http.ResponseWriter, r *http.Request) {
	var req agentTaskTriageRequest
	if !decodeBody(w, r, &req) {
		return
	}

	source := ctx.OfficeManager.GetAgentAndTouch(req.SessionID)
	if source == nil {
		request.WriteError(w, http.StatusNotFound, "source agent not found")
		return
	}
	target := ctx.OfficeManager.GetAgent(req.ToSessionID)
	if target == nil {
		request.WriteError(w, http.StatusNotFound, "target agent not found")
		return
	}

	task := source.RemoveTask(req.TaskID)
	if task == nil {
		request.WriteError(w, http.StatusNotFound, "task not found")
		return
	}

	task.Status = office.TaskPending
	task.AssignedAt = time.Now()
	task.Source = "triage"
	target.AddTask(task)

	ctx.SSEBroker.Broadcast("office:taskQueueUpdated", map[string]interface{}{"sessionId": req.SessionID})
	ctx.SSEBroker.Broadcast("office:taskQueueUpdated", map[string]interface{}{"sessionId": req.ToSessionID})
	if payload := ctx.OfficeManager.BuildMemberPayload(source); payload != nil {
		ctx.SSEBroker.Broadcast("office:memberUpdated", payload)
	}
	if payload := ctx.OfficeManager.BuildMemberPayload(target); payload != nil {
		ctx.SSEBroker.Broadcast("office:memberUpdated", payload)
	}
	ok(w, nil)
}

// OfficeAgentBrainstormResponse handles an agent's brainstorm response.
func (ctx *Context) OfficeAgentBrainstormResponse(w http.ResponseWriter, r *http.Request) {
	var req agentBrainstormResponseRequest
	if !decodeBody(w, r, &req) {
		return
	}
	ctx.OfficeManager.SubmitBrainstormResponse(req.SessionID, req.Proposals)
	ok(w, nil)
}

// OfficeAgentLeaderTasksResponse handles a leader tasks response.
func (ctx *Context) OfficeAgentLeaderTasksResponse(w http.ResponseWriter, r *http.Request) {
	var req agentLeaderTasksResponseRequest
	if !decodeBody(w, r, &req) {
		return
	}
	ctx.OfficeManager.SubmitLeaderTasksResponse(req.SessionID, req.Content)
	ok(w, nil)
}

// OfficeAgentReflectionResponse handles an agent's reflection response.
func (ctx *Context) OfficeAgentReflectionResponse(w http.ResponseWriter, r *http.Request) {
	var req agentReflectionResponseRequest
	if !decodeBody(w, r, &req) {
		return
	}
	ctx.OfficeManager.SubmitReflectionResponse(req.SessionID, req.Learning, req.Category, req.TaskID, req.TaskTitle)
	ok(w, nil)
}

// OfficeAgentCuriosityDiscovery handles an agent's curiosity discovery.
func (ctx *Context) OfficeAgentCuriosityDiscovery(w http.ResponseWriter, r *http.Request) {
	var req agentCuriosityDiscoveryRequest
	if !decodeBody(w, r, &req) {
		return
	}
	ctx.OfficeManager.SubmitCuriosityDiscovery(req.SessionID, req.Summary)
	ok(w, nil)
}

// OfficeAgentFileLock acquires or releases a file lock for an agent.
func (ctx *Context) OfficeAgentFileLock(w http.ResponseWriter, r *http.Request) {
	var req agentFileLockRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if req.SessionID == "" || req.FilePath == "" || req.Action == "" {
		request.WriteError(w, http.StatusBadRequest, "sessionId, filePath, and action are required")
		return
	}
	if agent := ctx.OfficeManager.GetAgent(req.SessionID); agent != nil {
		agent.TouchAgentAPI()
	}
	// Validate file path: no path traversal, no absolute paths
	if strings.Contains(req.FilePath, "..") || strings.HasPrefix(req.FilePath, "/") || strings.Contains(req.FilePath, "\x00") {
		request.WriteError(w, http.StatusBadRequest, "Invalid file path")
		return
	}
	switch req.Action {
	case "lock":
		lock, err := ctx.OfficeManager.AcquireLock(req.FilePath, req.SessionID, "")
		if err != nil {
			request.WriteError(w, http.StatusConflict, err.Error())
			return
		}
		ok(w, map[string]interface{}{"lock": lock})
	case "unlock":
		ctx.OfficeManager.ForceReleaseLock(req.FilePath)
		ok(w, nil)
	default:
		request.WriteError(w, http.StatusBadRequest, "action must be \"lock\" or \"unlock\"")
	}
}

// OfficeAgentGetFileLocks returns file locks visible to agents.
func (ctx *Context) OfficeAgentGetFileLocks(w http.ResponseWriter, r *http.Request) {
	locks := ctx.OfficeManager.GetLocks()
	ok(w, map[string]interface{}{"locks": locks})
}

// OfficeAgentGetSkills returns skills available to agents.
func (ctx *Context) OfficeAgentGetSkills(w http.ResponseWriter, r *http.Request) {
	homeDir, _ := os.UserHomeDir()
	skillsDir := filepath.Join(homeDir, ".claude", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		ok(w, map[string]interface{}{"skills": []interface{}{}})
		return
	}
	var skills []map[string]string
	for _, e := range entries {
		if e.IsDir() {
			skills = append(skills, map[string]string{
				"name": e.Name(),
				"path": filepath.Join(skillsDir, e.Name()),
			})
		}
	}
	if skills == nil {
		skills = make([]map[string]string, 0)
	}
	ok(w, map[string]interface{}{"skills": skills})
}

// OfficeAgentSearchSkills searches for skills.
func (ctx *Context) OfficeAgentSearchSkills(w http.ResponseWriter, r *http.Request) {
	query := strings.ToLower(r.URL.Query().Get("q"))
	homeDir, _ := os.UserHomeDir()
	skillsDir := filepath.Join(homeDir, ".claude", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		ok(w, map[string]interface{}{"skills": []interface{}{}})
		return
	}
	var skills []map[string]string
	for _, e := range entries {
		if e.IsDir() && (query == "" || strings.Contains(strings.ToLower(e.Name()), query)) {
			skills = append(skills, map[string]string{
				"name": e.Name(),
				"path": filepath.Join(skillsDir, e.Name()),
			})
		}
	}
	if skills == nil {
		skills = make([]map[string]string, 0)
	}
	ok(w, map[string]interface{}{"skills": skills})
}

// OfficeAgentGetSkillByName returns a specific skill.
func (ctx *Context) OfficeAgentGetSkillByName(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if !safeName(name) {
		request.WriteError(w, http.StatusBadRequest, "invalid skill name")
		return
	}
	homeDir, _ := os.UserHomeDir()
	skillPath := filepath.Join(homeDir, ".claude", "skills", name, "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		request.WriteError(w, http.StatusNotFound, "skill not found")
		return
	}
	ok(w, map[string]interface{}{
		"name":    name,
		"path":    filepath.Join(homeDir, ".claude", "skills", name),
		"content": string(data),
	})
}

// OfficeAgentSkillCreate creates a new skill.
func (ctx *Context) OfficeAgentSkillCreate(w http.ResponseWriter, r *http.Request) {
	var req agentSkillCreateRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if req.SkillName == "" || req.Content == "" {
		request.WriteError(w, http.StatusBadRequest, "skillName and content are required")
		return
	}
	if !safeName(req.SkillName) {
		request.WriteError(w, http.StatusBadRequest, "invalid skill name")
		return
	}
	homeDir, _ := os.UserHomeDir()
	skillDir := filepath.Join(homeDir, ".claude", "skills", req.SkillName)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		request.WriteError(w, http.StatusInternalServerError, "failed to create skill directory")
		return
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	isUpdate := false
	if _, err := os.Stat(skillPath); err == nil {
		isUpdate = true
	}
	if err := os.WriteFile(skillPath, []byte(req.Content), 0o644); err != nil {
		request.WriteError(w, http.StatusInternalServerError, "failed to write skill file")
		return
	}
	action := "created"
	if isUpdate {
		action = "updated"
	}
	ok(w, map[string]interface{}{
		"action":    action,
		"skillName": req.SkillName,
		"path":      skillPath,
	})
}

// OfficeAgentDeleteSkill deletes a shared skill by name.
func (ctx *Context) OfficeAgentDeleteSkill(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if !safeName(name) {
		request.WriteError(w, http.StatusBadRequest, "invalid skill name")
		return
	}
	homeDir, _ := os.UserHomeDir()
	skillPath := filepath.Join(homeDir, ".claude", "skills", name)

	absSkillPath, _ := filepath.Abs(skillPath)
	absSkillsBase, _ := filepath.Abs(filepath.Join(homeDir, ".claude", "skills"))
	if !strings.HasPrefix(absSkillPath, absSkillsBase+string(filepath.Separator)) {
		request.WriteError(w, http.StatusBadRequest, "invalid skill name")
		return
	}

	if err := os.RemoveAll(skillPath); err != nil {
		request.WriteError(w, http.StatusInternalServerError, "failed to delete skill")
		return
	}
	ok(w, map[string]interface{}{"deleted": name})
}

// getSessionWorkingDir returns the working directory for a session (PTY or SDK).
func (ctx *Context) getSessionWorkingDir(sessionID string) string {
	if s, found := ctx.SessionManager.GetSession(sessionID); found {
		return s.WorkingDir
	}
	if sdk, found := ctx.SessionManager.GetSDKSession(sessionID); found {
		return sdk.WorkingDir
	}
	return ""
}

// OfficeAgentGetMemory returns memory for an agent.
func (ctx *Context) OfficeAgentGetMemory(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")
	agent := ctx.OfficeManager.GetAgent(sessionID)
	if agent == nil {
		ok(w, map[string]interface{}{"memory": ""})
		return
	}
	workingDir := ctx.getSessionWorkingDir(sessionID)
	memory := office.LoadAgentMemory(agent.ConfigID, workingDir)
	ok(w, map[string]interface{}{"memory": memory})
}

// OfficeAgentPostMemory writes agent memory.
func (ctx *Context) OfficeAgentPostMemory(w http.ResponseWriter, r *http.Request) {
	var req agentMemoryWriteRequest
	if !decodeBody(w, r, &req) {
		return
	}
	agent := ctx.OfficeManager.GetAgentAndTouch(req.SessionID)
	if agent == nil {
		request.WriteError(w, http.StatusNotFound, "agent not found")
		return
	}
	workingDir := ctx.getSessionWorkingDir(req.SessionID)
	if err := office.SaveAgentMemory(agent.ConfigID, workingDir, req.Content); err != nil {
		request.WriteError(w, http.StatusInternalServerError, "failed to write memory")
		return
	}
	ok(w, nil)
}

// OfficeAgentPostMemoryAppend appends to agent memory.
func (ctx *Context) OfficeAgentPostMemoryAppend(w http.ResponseWriter, r *http.Request) {
	var req agentMemoryAppendRequest
	if !decodeBody(w, r, &req) {
		return
	}
	agent := ctx.OfficeManager.GetAgentAndTouch(req.SessionID)
	if agent == nil {
		request.WriteError(w, http.StatusNotFound, "agent not found")
		return
	}
	workingDir := ctx.getSessionWorkingDir(req.SessionID)
	if err := office.AppendAgentMemory(agent.ConfigID, workingDir, req.Entry); err != nil {
		request.WriteError(w, http.StatusInternalServerError, "failed to append memory")
		return
	}
	ok(w, nil)
}

// OfficeAgentPostMemorySection writes (or appends to) a named memory
// section. POST body: {sessionId, name, content, append?}.
// When append=false (default) the section is overwritten; when true the
// content is appended to the existing section.
func (ctx *Context) OfficeAgentPostMemorySection(w http.ResponseWriter, r *http.Request) {
	var req agentMemorySectionRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if !office.IsValidMemorySectionName(req.Name) {
		request.WriteError(w, http.StatusBadRequest, "invalid section name")
		return
	}
	agent := ctx.OfficeManager.GetAgentAndTouch(req.SessionID)
	if agent == nil {
		request.WriteError(w, http.StatusNotFound, "agent not found")
		return
	}
	workingDir := ctx.getSessionWorkingDir(req.SessionID)
	var err error
	if req.Append {
		err = office.MemoryAppendSection(agent.ConfigID, workingDir, req.Name, req.Content)
	} else {
		err = office.MemoryWriteSection(agent.ConfigID, workingDir, req.Name, req.Content)
	}
	if err != nil {
		request.WriteError(w, http.StatusInternalServerError, "failed to write section: "+err.Error())
		return
	}
	ok(w, map[string]interface{}{"section": req.Name, "append": req.Append})
}

// OfficeAgentGetMemorySection returns a named section's content.
// GET /api/office/agent/memory/section?sessionId=<id>&name=<section>
func (ctx *Context) OfficeAgentGetMemorySection(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")
	name := r.URL.Query().Get("name")
	if !office.IsValidMemorySectionName(name) {
		request.WriteError(w, http.StatusBadRequest, "invalid section name")
		return
	}
	agent := ctx.OfficeManager.GetAgent(sessionID)
	if agent == nil {
		ok(w, map[string]interface{}{"section": name, "content": ""})
		return
	}
	workingDir := ctx.getSessionWorkingDir(sessionID)
	content, err := office.MemoryReadSection(agent.ConfigID, workingDir, name)
	if err != nil {
		request.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]interface{}{"section": name, "content": content})
}

// OfficeAgentGetMemoryQuery searches the agent's memory file for a
// case-insensitive substring and returns up to 20 line-level hits.
// GET /api/office/agent/memory/query?sessionId=<id>&q=<keyword>
func (ctx *Context) OfficeAgentGetMemoryQuery(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")
	q := r.URL.Query().Get("q")
	if q == "" {
		request.WriteError(w, http.StatusBadRequest, "missing q parameter")
		return
	}
	agent := ctx.OfficeManager.GetAgent(sessionID)
	if agent == nil {
		ok(w, map[string]interface{}{"hits": []interface{}{}})
		return
	}
	workingDir := ctx.getSessionWorkingDir(sessionID)
	hits, err := office.MemoryQuery(agent.ConfigID, workingDir, q)
	if err != nil {
		request.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if hits == nil {
		hits = []office.MemoryHit{}
	}
	ok(w, map[string]interface{}{"hits": hits})
}

// OfficeBrainstorm initiates a brainstorm session.
func (ctx *Context) OfficeBrainstorm(w http.ResponseWriter, r *http.Request) {
	var req brainstormRequest
	if !decodeBody(w, r, &req) {
		return
	}
	meeting := ctx.OfficeManager.StartBrainstorm(req.ParticipantIDs)
	if meeting == nil {
		request.WriteError(w, http.StatusBadRequest, "Could not start brainstorm (cooldown, wrap-up, or insufficient participants)")
		return
	}
	ok(w, map[string]interface{}{"meeting": meeting})
}

// ---------------------------------------------------------------------------
// Member-specific handlers
// ---------------------------------------------------------------------------

// OfficeGetMemberMemory returns memory for a specific member.
func (ctx *Context) OfficeGetMemberMemory(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionId")
	agent := ctx.OfficeManager.GetAgent(sessionID)
	if agent == nil {
		ok(w, map[string]interface{}{"memory": ""})
		return
	}
	workingDir := ctx.getSessionWorkingDir(sessionID)
	memory := office.LoadAgentMemory(agent.ConfigID, workingDir)
	ok(w, map[string]interface{}{"memory": memory})
}

// OfficeGetMemberPersonality returns personality for a specific member.
func (ctx *Context) OfficeGetMemberPersonality(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionId")
	agent := ctx.OfficeManager.GetAgent(sessionID)
	if agent == nil {
		ok(w, map[string]interface{}{
			"personality":  "",
			"systemPrompt": "",
		})
		return
	}
	personality := agent.Config.Personality
	systemPrompt := agent.Config.SystemPrompt
	if override := agent.GetPersonalityOverride(); override != nil {
		if override.Personality != "" {
			personality = override.Personality
		}
		if override.SystemPrompt != "" {
			systemPrompt = override.SystemPrompt
		}
	}
	ok(w, map[string]interface{}{
		"personality":  personality,
		"systemPrompt": systemPrompt,
	})
}

// OfficePutMemberPersonality updates personality for a specific member.
func (ctx *Context) OfficePutMemberPersonality(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionId")
	var req personalityRequest
	if !decodeBody(w, r, &req) {
		return
	}
	agent := ctx.OfficeManager.GetAgent(sessionID)
	if agent != nil {
		agent.SetPersonalityOverride(&office.PersonalityOverride{
			Personality:  req.Personality,
			SystemPrompt: req.SystemPrompt,
		})
		// Include the fields the Personality modal binds to so an open
		// modal on another client reflects the update instead of
		// blanking the inputs (undefined -> '') and logging
		// "Personality updated for undefined" in the activity feed.
		ctx.SSEBroker.Broadcast("office:personalityUpdated", map[string]interface{}{
			"sessionId":    sessionID,
			"configId":     agent.ConfigID,
			"personality":  req.Personality,
			"systemPrompt": req.SystemPrompt,
		})
	}
	ok(w, nil)
}
