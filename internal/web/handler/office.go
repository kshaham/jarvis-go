package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/claudeman/jarvis-go/internal/office"
	"github.com/claudeman/jarvis-go/internal/pty"
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
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

type spawnTeamRequest struct {
	WorkingDir          string           `json:"workingDir"`
	TemplateID          string           `json:"templateId"`
	Mission             string           `json:"mission"`
	MemberOverrides     []memberOverride `json:"memberOverrides"`
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
	SessionID string `json:"sessionId"`
	TaskID    string `json:"taskId"`
	Progress  string `json:"progress"`
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

type templateMember struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Role  string `json:"role"`
	Tier  string `json:"tier"`
	Color string `json:"color"`
	Emoji string `json:"emoji"`
}

type officeTemplate struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Category    string           `json:"category"`
	MemberCount int              `json:"memberCount"`
	Members     []templateMember `json:"members"`
}

var office7Members = []templateMember{
	{ID: "max", Name: "Max", Role: "Engineering Manager", Tier: "strategic", Color: "blue", Emoji: "\U0001F9E0"},
	{ID: "sage", Name: "Sage", Role: "Senior Architect", Tier: "strategic", Color: "purple", Emoji: "\U0001F989"},
	{ID: "nova", Name: "Nova", Role: "UI/UX Designer", Tier: "worker", Color: "pink", Emoji: "\U0001F3A8"},
	{ID: "vex", Name: "Vex", Role: "Frontend Engineer", Tier: "worker", Color: "green", Emoji: "\u26A1"},
	{ID: "bolt", Name: "Bolt", Role: "Backend Engineer", Tier: "worker", Color: "orange", Emoji: "\u2699\uFE0F"},
	{ID: "forge", Name: "Forge", Role: "DevOps Engineer", Tier: "worker", Color: "yellow", Emoji: "\U0001F528"},
	{ID: "lint", Name: "Lint", Role: "QA Engineer", Tier: "worker", Color: "red", Emoji: "\U0001F50D"},
}

var backendAIMembers = []templateMember{
	{ID: "aria", Name: "Aria", Role: "AI Engineering Lead", Tier: "strategic", Color: "purple", Emoji: "\U0001F9E0"},
	{ID: "tensor", Name: "Tensor", Role: "ML Engineer", Tier: "worker", Color: "blue", Emoji: "\U0001F916"},
	{ID: "prompt", Name: "Prompt", Role: "Prompt Engineer", Tier: "worker", Color: "green", Emoji: "\u270D\uFE0F"},
	{ID: "dataflow", Name: "Dataflow", Role: "Data Engineer", Tier: "worker", Color: "orange", Emoji: "\U0001F4CA"},
	{ID: "nexus", Name: "Nexus", Role: "API Engineer", Tier: "worker", Color: "yellow", Emoji: "\U0001F310"},
	{ID: "guardian", Name: "Guardian", Role: "AI QA / Evaluator", Tier: "worker", Color: "red", Emoji: "\U0001F6E1\uFE0F"},
}

var frontendStudioMembers = []templateMember{
	{ID: "pixel", Name: "Pixel", Role: "Design Lead", Tier: "strategic", Color: "pink", Emoji: "\U0001F3A8"},
	{ID: "react", Name: "React", Role: "Frontend Architect", Tier: "strategic", Color: "blue", Emoji: "\u269B\uFE0F"},
	{ID: "craft", Name: "Craft", Role: "UI Engineer", Tier: "worker", Color: "green", Emoji: "\U0001F528"},
	{ID: "flow", Name: "Flow", Role: "Interaction Engineer", Tier: "worker", Color: "orange", Emoji: "\U0001F3AE"},
	{ID: "access", Name: "Access", Role: "Accessibility & Testing", Tier: "worker", Color: "purple", Emoji: "\u267F"},
	{ID: "perf", Name: "Perf", Role: "Performance Engineer", Tier: "worker", Color: "yellow", Emoji: "\u26A1"},
}

var fullstackCompactMembers = []templateMember{
	{ID: "captain", Name: "Captain", Role: "Tech Lead", Tier: "strategic", Color: "blue", Emoji: "\U0001F9ED"},
	{ID: "dash", Name: "Dash", Role: "Frontend Developer", Tier: "worker", Color: "green", Emoji: "\U0001F3A8"},
	{ID: "stack", Name: "Stack", Role: "Backend Developer", Tier: "worker", Color: "orange", Emoji: "\u2699\uFE0F"},
	{ID: "shield", Name: "Shield", Role: "QA & Security", Tier: "worker", Color: "red", Emoji: "\U0001F6E1\uFE0F"},
	{ID: "ops", Name: "Ops", Role: "DevOps", Tier: "worker", Color: "yellow", Emoji: "\U0001F680"},
}

var researchLabMembers = []templateMember{
	{ID: "atlas", Name: "Atlas", Role: "Research Director", Tier: "strategic", Color: "blue", Emoji: "\U0001F9ED"},
	{ID: "cipher", Name: "Cipher", Role: "Deep Researcher", Tier: "worker", Color: "purple", Emoji: "\U0001F4DA"},
	{ID: "prism", Name: "Prism", Role: "Technical Analyst", Tier: "worker", Color: "green", Emoji: "\U0001F52C"},
	{ID: "proto", Name: "Proto", Role: "POC Developer", Tier: "worker", Color: "orange", Emoji: "\U0001F6E0\uFE0F"},
	{ID: "slide", Name: "Slide", Role: "Presentation Specialist", Tier: "worker", Color: "pink", Emoji: "\U0001F4CA"},
	{ID: "muse", Name: "Muse", Role: "Brand Strategist", Tier: "worker", Color: "yellow", Emoji: "\u2728"},
	{ID: "quill", Name: "Quill", Role: "Documentation Writer", Tier: "worker", Color: "red", Emoji: "\u270D\uFE0F"},
}

var innovationSprintMembers = []templateMember{
	{ID: "spark", Name: "Spark", Role: "Innovation Lead", Tier: "strategic", Color: "blue", Emoji: "\U0001F4A1"},
	{ID: "scout", Name: "Scout", Role: "Market Researcher", Tier: "worker", Color: "purple", Emoji: "\U0001F50E"},
	{ID: "flux", Name: "Flux", Role: "Prototype Engineer", Tier: "worker", Color: "orange", Emoji: "\u26A1"},
	{ID: "pitch", Name: "Pitch", Role: "Visual Storyteller", Tier: "worker", Color: "pink", Emoji: "\U0001F3AC"},
	{ID: "brief", Name: "Brief", Role: "Technical Writer", Tier: "worker", Color: "green", Emoji: "\U0001F4DD"},
	{ID: "verify", Name: "Verify", Role: "Validation Analyst", Tier: "worker", Color: "red", Emoji: "\u2705"},
}

var defaultTemplates = []officeTemplate{
	{ID: "office-7", Name: "Office (Default)", Description: "The original 7-member dev team: manager, architect, designer, frontend, backend, devops, and QA.", Category: "fullstack", MemberCount: 7, Members: office7Members},
	{ID: "backend-ai-6", Name: "Backend / AI", Description: "AI-focused team with ML engineers, prompt engineer, data engineer, API engineer, and AI QA.", Category: "backend", MemberCount: 6, Members: backendAIMembers},
	{ID: "frontend-studio-6", Name: "Frontend Studio", Description: "Frontend-focused team with design lead, architect, UI engineer, interaction engineer, accessibility, and performance.", Category: "frontend", MemberCount: 6, Members: frontendStudioMembers},
	{ID: "fullstack-compact-5", Name: "Full-stack Compact", Description: "Lean 5-person team: tech lead, frontend dev, backend dev, QA/security, and devops.", Category: "fullstack", MemberCount: 5, Members: fullstackCompactMembers},
	{ID: "research-lab-7", Name: "Research Lab", Description: "Deep research team: director, researcher, analyst, POC developer, presentation specialist, brand strategist, and documentation writer. Biweekly sprint reviews.", Category: "research", MemberCount: 7, Members: researchLabMembers},
	{ID: "innovation-sprint-6", Name: "Innovation Sprint", Description: "Lean innovation team: market research, rapid prototyping, visual storytelling, documentation, and validation. Built-in sprint review cadence.", Category: "research", MemberCount: 6, Members: innovationSprintMembers},
}

// ---------------------------------------------------------------------------
// Core status handlers
// ---------------------------------------------------------------------------

// OfficeStatus returns the current office state.
func (ctx *Context) OfficeStatus(w http.ResponseWriter, r *http.Request) {
	state := ctx.OfficeManager.GetOfficeState()
	ok(w, state)
}

// OfficeTemplates returns available team templates.
func (ctx *Context) OfficeTemplates(w http.ResponseWriter, r *http.Request) {
	ok(w, map[string]interface{}{
		"templates": defaultTemplates,
	})
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
func (ctx *Context) OfficeSpawnTeam(w http.ResponseWriter, r *http.Request) {
	var req spawnTeamRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if req.WorkingDir == "" {
		request.WriteError(w, http.StatusBadRequest, "workingDir is required")
		return
	}
	if ctx.OfficeManager.IsTeamSpawned() {
		request.WriteError(w, http.StatusConflict, "Team has already been spawned")
		return
	}

	// Resolve template members
	var members []templateMember
	templateUsed := "office-7"
	for _, tpl := range defaultTemplates {
		if tpl.ID == req.TemplateID || req.TemplateID == "" {
			members = tpl.Members
			templateUsed = tpl.ID
			break
		}
	}
	if members == nil {
		members = office7Members
	}

	// Build default provider env vars
	defaultProviderEnv := ctx.getProviderEnvVars()

	// Build per-member provider override map
	overrideMap := make(map[string]map[string]string)
	for _, ov := range req.MemberOverrides {
		overrideMap[ov.ID] = ctx.getProviderEnvVarsFor(ov.Provider, ov.Model)
	}

	spawned := make([]string, 0, len(members))
	errors := make([]string, 0)

	slog.Info("Spawning office team", "template", templateUsed, "members", len(members), "workingDir", req.WorkingDir)

	for _, member := range members {
		// Use member-specific provider env if overridden, else default
		envVars := defaultProviderEnv
		if ov, hasOverride := overrideMap[member.ID]; hasOverride {
			envVars = ov
		}

		// Create a Claude session for each team member
		session, err := ctx.SessionManager.CreateSession(
			context.Background(),
			pty.SessionConfig{
				WorkingDir:  req.WorkingDir,
				Mode:        pty.ModeClaude,
				Name:        member.Name,
				ProviderEnv: envVars,
			},
		)
		if err != nil {
			slog.Error("Failed to create session for team member", "name", member.Name, "error", err)
			errors = append(errors, fmt.Sprintf("Failed to create session for %s: %s", member.Name, err.Error()))
			continue
		}

		// Register agent with office manager
		ctx.OfficeManager.RegisterAgent(session.ID, member.ID, &office.TeamMemberConfig{
			ID:    member.ID,
			Name:  member.Name,
			Role:  member.Role,
			Tier:  member.Tier,
			Color: member.Color,
		})

		// Broadcast session created
		ctx.SSEBroker.Broadcast("session:created", sessionToJSON(session))

		// Start event forwarding for the session
		go forwardSessionEvents(session, ctx)

		// Notify Telegram
		if ctx.TelegramHandler != nil {
			ctx.TelegramHandler.NotifySessionStart(session.ID, member.Name, req.WorkingDir)
		}

		spawned = append(spawned, member.Name)
		slog.Info("Spawned team member", "name", member.Name, "sessionId", session.ID, "role", member.Role)
	}

	// Mark team as spawned
	ctx.OfficeManager.SetTeamSpawned(true)

	// Set team mission if provided
	if req.Mission != "" {
		ctx.OfficeManager.SetTeamMission(req.Mission)
	}

	// Build teammate list for kickstart context
	teamConfigs := make([]*office.TeamMemberConfig, 0, len(members))
	for _, m := range members {
		teamConfigs = append(teamConfigs, &office.TeamMemberConfig{
			ID: m.ID, Name: m.Name, Role: m.Role, Tier: m.Tier, Color: m.Color,
		})
	}

	// Inject kickstart prompts into each session.
	// Wait for Claude's input prompt to appear before injecting.
	teamMission := ctx.OfficeManager.GetTeamMission()
	roster := ctx.OfficeManager.GetRoster()
	for _, agent := range roster {
		go func(a *office.AgentState) {
			agentMemory := office.LoadAgentMemory(a.ConfigID, req.WorkingDir)
			prompt := office.BuildKickstartPrompt(a.Config, req.WorkingDir, teamConfigs, teamMission, agentMemory)
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
	roster := ctx.OfficeManager.GetRoster()
	for _, agent := range roster {
		s, found := ctx.SessionManager.GetSession(agent.SessionID)
		if found {
			s.WriteViaScreen("SHUTDOWN: The team is shutting down. Your session will be terminated.\r")
		}
		_ = ctx.SessionManager.StopSession(context.Background(), agent.SessionID)
		ctx.cleanupSessionResources(agent.SessionID)
		ctx.OfficeManager.UnregisterAgent(agent.SessionID)
	}

	ctx.OfficeManager.SetTeamSpawned(false)
	ok(w, nil)
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
	if agent == nil {
		ok(w, map[string]interface{}{"queue": nil})
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
	ctx.SSEBroker.Broadcast("office:fileLockChanged", map[string]interface{}{"released": filePath})
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

	// Attempt to parse as JSON; fall back to raw string if invalid.
	var parsed interface{}
	if jsonErr := json.Unmarshal(data, &parsed); jsonErr != nil {
		parsed = string(data)
	}
	ok(w, map[string]interface{}{"learnings": parsed, "configId": configID})
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
3. DO NOT start any new tasks from your queue
4. DO NOT pick up any new work
5. Report your final status: cm task complete <taskId>
6. Once you've committed and reported status, stop working and exit

This is a graceful shutdown. Take the time to finish cleanly, but do not begin anything new.`

	roster := mgr.GetRoster()
	for _, agent := range roster {
		s, found := ctx.SessionManager.GetSession(agent.SessionID)
		if found {
			s.WriteInkPrompt(wrapUpPrompt)
		}
	}

	status := mgr.GetWrapUpStatus()
	ok(w, map[string]interface{}{
		"active":      status.Active,
		"startedAt":   status.StartedAt,
		"agentStatus": status.AgentStatus,
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
	agent := ctx.OfficeManager.GetAgent(sessionID)
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
	agent := ctx.OfficeManager.GetAgent(req.SessionID)
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
		// Update member status to working when a task starts
		ctx.SSEBroker.Broadcast("office:memberUpdated", map[string]interface{}{
			"sessionId": req.SessionID,
			"status":    "working",
		})
	}
	ok(w, nil)
}

// OfficeAgentTaskComplete marks a task as completed.
func (ctx *Context) OfficeAgentTaskComplete(w http.ResponseWriter, r *http.Request) {
	var req agentTaskCompleteRequest
	if !decodeBody(w, r, &req) {
		return
	}
	agent := ctx.OfficeManager.GetAgent(req.SessionID)
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

		// Update member status based on remaining active tasks
		activeTask := agent.ActiveTask()
		if activeTask != nil {
			ctx.SSEBroker.Broadcast("office:memberUpdated", map[string]interface{}{
				"sessionId": req.SessionID,
				"status":    "working",
			})
		} else {
			ctx.SSEBroker.Broadcast("office:memberUpdated", map[string]interface{}{
				"sessionId": req.SessionID,
				"status":    "idle",
			})
		}

		// Check if agent's queue is now empty after a grace period,
		// then trigger work-finding so the agent does not sit idle.
		mgr := ctx.OfficeManager
		go func() {
			time.Sleep(30 * time.Second)
			mgr.CheckForIdleAgents()
		}()
	}
	ok(w, nil)
}

// OfficeAgentTaskAck acknowledges a task.
func (ctx *Context) OfficeAgentTaskAck(w http.ResponseWriter, r *http.Request) {
	var req agentTaskAckRequest
	if !decodeBody(w, r, &req) {
		return
	}
	agent := ctx.OfficeManager.GetAgent(req.SessionID)
	if agent != nil {
		agent.UpdateTaskStatus(req.TaskID, office.TaskAcknowledged)
		ctx.SSEBroker.Broadcast("office:taskUpdated", map[string]interface{}{
			"sessionId": req.SessionID,
			"taskId":    req.TaskID,
			"status":    string(office.TaskAcknowledged),
		})
	}
	ok(w, nil)
}

// OfficeAgentTaskProgress updates task progress.
func (ctx *Context) OfficeAgentTaskProgress(w http.ResponseWriter, r *http.Request) {
	var req agentTaskProgressRequest
	if !decodeBody(w, r, &req) {
		return
	}
	agent := ctx.OfficeManager.GetAgent(req.SessionID)
	if agent != nil {
		agent.SetTaskProgress(req.TaskID, req.Progress)
		ctx.SSEBroker.Broadcast("office:taskUpdated", map[string]interface{}{
			"sessionId": req.SessionID,
			"taskId":    req.TaskID,
			"progress":  req.Progress,
		})
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
	agent := ctx.OfficeManager.GetAgent(req.SessionID)
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
	}
	ok(w, nil)
}

// OfficeAgentTaskTriage handles task triage by a leader agent.
func (ctx *Context) OfficeAgentTaskTriage(w http.ResponseWriter, r *http.Request) {
	var req agentTaskTriageRequest
	if !decodeBody(w, r, &req) {
		return
	}

	source := ctx.OfficeManager.GetAgent(req.SessionID)
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

// OfficeAgentGetMemory returns memory for an agent.
func (ctx *Context) OfficeAgentGetMemory(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")
	agent := ctx.OfficeManager.GetAgent(sessionID)
	if agent == nil {
		ok(w, map[string]interface{}{"memory": ""})
		return
	}
	workingDir := ""
	if s, found := ctx.SessionManager.GetSession(sessionID); found {
		workingDir = s.WorkingDir
	}
	memory := office.LoadAgentMemory(agent.ConfigID, workingDir)
	ok(w, map[string]interface{}{"memory": memory})
}

// OfficeAgentPostMemory writes agent memory.
func (ctx *Context) OfficeAgentPostMemory(w http.ResponseWriter, r *http.Request) {
	var req agentMemoryWriteRequest
	if !decodeBody(w, r, &req) {
		return
	}
	agent := ctx.OfficeManager.GetAgent(req.SessionID)
	if agent == nil {
		request.WriteError(w, http.StatusNotFound, "agent not found")
		return
	}
	workingDir := ""
	if s, found := ctx.SessionManager.GetSession(req.SessionID); found {
		workingDir = s.WorkingDir
	}
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
	agent := ctx.OfficeManager.GetAgent(req.SessionID)
	if agent == nil {
		request.WriteError(w, http.StatusNotFound, "agent not found")
		return
	}
	workingDir := ""
	if s, found := ctx.SessionManager.GetSession(req.SessionID); found {
		workingDir = s.WorkingDir
	}
	if err := office.AppendAgentMemory(agent.ConfigID, workingDir, req.Entry); err != nil {
		request.WriteError(w, http.StatusInternalServerError, "failed to append memory")
		return
	}
	ok(w, nil)
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
	workingDir := ""
	if s, found := ctx.SessionManager.GetSession(sessionID); found {
		workingDir = s.WorkingDir
	}
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
	if agent.PersonalityOverride != nil {
		if agent.PersonalityOverride.Personality != "" {
			personality = agent.PersonalityOverride.Personality
		}
		if agent.PersonalityOverride.SystemPrompt != "" {
			systemPrompt = agent.PersonalityOverride.SystemPrompt
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
		agent.PersonalityOverride = &office.PersonalityOverride{
			Personality:  req.Personality,
			SystemPrompt: req.SystemPrompt,
		}
		ctx.SSEBroker.Broadcast("office:personalityUpdated", map[string]interface{}{"sessionId": sessionID})
	}
	ok(w, nil)
}
