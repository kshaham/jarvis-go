package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/kshaham/jarvis/internal/library"
	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/office/spawnpayload"
	"github.com/kshaham/jarvis/internal/projects"
	"github.com/kshaham/jarvis/internal/safego"
	"github.com/kshaham/jarvis/internal/strs"
	"github.com/kshaham/jarvis/internal/teamtemplates"
	"github.com/kshaham/jarvis/internal/tts"
	"github.com/kshaham/jarvis/internal/web/request"
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

// Type aliases keep existing handler code working with minimal churn
// while the canonical definitions live in internal/office/spawnpayload.
// The longer-term plan is for handler files to import spawnpayload
// directly and drop these aliases; see spawnpayload package doc.
type (
	memberOverride   = spawnpayload.MemberOverride
	spawnTeamRequest = spawnpayload.SpawnTeamRequest
)

// Function-pointer aliases for the helpers that moved to spawnpayload.
// var (not const) so they're addressable; the indirection cost is
// negligible compared to the spawn flow itself.
var (
	codegraphOptOut      = spawnpayload.CodegraphOptOut
	eccOptOut            = spawnpayload.EccOptOut
	resolveSessionTuning = spawnpayload.ResolveSessionTuning
)

const (
	defaultEffortWorker    = spawnpayload.DefaultEffortWorker
	defaultEffortStrategic = spawnpayload.DefaultEffortStrategic
)

// projectNameRe + newProjectRoot were moved to internal/office (see
// office.ProjectNameRe and office.NewProjectRoot) when the
// new-project flow was extracted to office.PrepareNewProject.

// defaultMaxTurnsPerSession is the safety-net ceiling applied to every
// freshly-spawned SDK session. R18 · An agent hit 155 turns ($57.87) on
// one task window before anyone noticed; this cap ensures future
// runaway loops fail fast. Legitimate long migrations can override via
// the member spawn payload.
const defaultMaxTurnsPerSession = 100

// firstNonEmpty thin-wraps strs.FirstNonEmpty so the existing call
// sites in this file don't need to grow an import each. P11.3.11
// dedupe: the shared helper lives in internal/strs/ alongside other
// zero-dep string utilities (was duplicated against ecc.go's copy
// pre-Phase 11).
var firstNonEmpty = strs.FirstNonEmpty

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

type agentAskRequest struct {
	SessionID     string `json:"sessionId"`
	FromSessionID string `json:"fromSessionId"`
	Content       string `json:"content"`
	AboutTaskID   string `json:"aboutTaskId,omitempty"`
	WaitSeconds   int    `json:"waitSeconds,omitempty"`
}

type agentAskAnswerRequest struct {
	SessionID     string `json:"sessionId"`
	FromSessionID string `json:"fromSessionId"`
	QuestionID    string `json:"questionId"`
	Answer        string `json:"answer"`
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
	// R36 · Optional labels applied on task creation. Each entry
	// normalised + validated; invalid entries return 400 so the
	// operator / agent learns about the typo immediately.
	Labels []string `json:"labels,omitempty"`
	// AcceptanceCriteria holds the DoD conditions supplied at creation.
	// Each entry is validated; invalid entries return 400.
	AcceptanceCriteria []string `json:"acceptanceCriteria,omitempty"`
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

// agentDevInstanceRequest is the body for POST /office/agent/dev-instance.
// URL is the http://jarvis:<port> address of the dev server the lead has
// just started. Empty URL clears the slot (equivalent to DELETE). PID is
// optional — pass the dev server's process ID so Jarvis can SIGTERM its
// process tree on wrap-up or lead death. 0 = no PID tracked (orphan risk
// accepted). Name is the slot key (defaults to "app" when blank) so
// fullstack teams can track multiple dev servers under distinct names.
type agentDevInstanceRequest struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	PID  int    `json:"pid"`
}

// agentDevCredentialsRequest is the body for POST /office/agent/dev-credentials.
// Credentials is a free-text blob (e.g. "admin@dev.local / password123"),
// whatever format makes sense for the dev app. Empty clears the slot.
// Name defaults to "app" and selects which slot the creds belong to.
type agentDevCredentialsRequest struct {
	Name        string `json:"name"`
	Credentials string `json:"credentials"`
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

type memberVoiceRequest struct {
	Voice string `json:"voice"`
}

type codeGraphImpactRequest struct {
	Files []string `json:"files"`
}

// agentTaskCriteriaRequest is the body for POST /office/agent/task/criteria.
// Op is "set" | "add" | "remove". For op="remove", Criteria holds a single
// 0-based index string; pass only one index per call (removal shifts positions).
type agentTaskCriteriaRequest struct {
	SessionID string   `json:"sessionId"`
	TaskID    string   `json:"taskId"`
	Op        string   `json:"op"`
	Criteria  []string `json:"criteria"`
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

	members := ctx.OfficeManager.SnapshotRosterAsTemplateMembers()

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
// spawnAgentSpec carries the per-agent fields the spawn handler
// needs to honour officeSessionMode = "hooks". perAgentCwd is the
// pre-resolved working directory (a worktree path); hooksEnabled
// flips the new ModeClaude+hooks code path; preassignedSessionID
// (when non-empty) lets the caller bake the same sessionID into
// .claude/settings.local.json BEFORE the session starts so the
// hook URLs and the agent registration agree.

// resolveOfficeSessionMode reads the operator's preferred office spawn
// mode from settings. Default is "sdk" for back-compat with installs
// from before hooks-mode shipped. Valid values:
//   - "sdk"    : ModeClaudeSDK via claude-agent-sdk-go (programmatic
//     billing pool starting 2026-06-15).
//   - "hooks"  : ModeClaude + HTTP hooks; runs interactive `claude`
//     in a PTY, billed on the interactive subscription pool.
//   - "legacy" : ModeClaude without hooks (raw PTY, no structured
//     events; escape hatch).
//
// Unknown values fall back to "sdk".
func (ctx *Context) resolveOfficeSessionMode() string {
	settings := ctx.StateStore.GetSettings()
	mode, _ := settings["officeSessionMode"].(string)
	switch mode {
	case "sdk", "hooks", "legacy":
		return mode
	}
	return "sdk"
}

func (ctx *Context) OfficeSpawnTeam(w http.ResponseWriter, r *http.Request) {
	var req spawnTeamRequest
	if !decodeBody(w, r, &req) {
		return
	}
	ctx.spawnTeamFromRequest(w, req)
}

// demoMaxTurns caps each demo agent's turn count so the one-click demo
// stays cheap and bounded. Combined with PerpetualMode=false and
// DisableWorkflows=true, it keeps a demo run to a few minutes of work.
const demoMaxTurns = 10

// demoMission is the mission handed to the built-in "Demo Jarvis" team.
// It frames a tiny self-contained sample app and reinforces the ritual
// sequence the lead's system prompt already describes, so every office
// screen fills with genuine activity within a few minutes.
const demoMission = `This is a guided DEMONSTRATION of the Virtual Office. Build a tiny sample to-do web app called "TaskPal" -- but the real point is to show the office working, not to ship a big product.

Keep every step small and do it for real:
- Run a standup so each member gives a one-line update.
- Post a short plan to the whiteboard.
- Create a small task queue (about four tasks) with one dependency, and file one bug.
- Coordinate over team messages; hold one short brainstorm meeting.
- Capture at least one knowledge/learning entry.
- Attach a mockup and a short deliverable summary.
- Finish with a wrap-up summary.

Breadth over depth: touch every office ritual once rather than polishing any single one.`

// resolveDemoWorkspaceDir picks a writable, allowlisted sandbox
// directory for the demo team. Preference order:
//  1. the container workspace (/app/workspace) when it exists — the
//     server / Docker deploy, already allowlisted;
//  2. the operator's configured defaultWorkingDir;
//  3. $HOME (always part of the default working-dir allowlist).
//
// In every case the demo project lives at <base>/jarvis-demo, so the
// standard ResolveWorkingDir validation inside spawnTeamFromRequest
// accepts it. The dir is reused idempotently across demo runs.
func (ctx *Context) resolveDemoWorkspaceDir() string {
	const demoSlug = "jarvis-demo"
	if info, err := os.Stat(office.NewProjectRoot); err == nil && info.IsDir() {
		return filepath.Join(office.NewProjectRoot, demoSlug)
	}
	if settings := ctx.StateStore.GetSettings(); settings != nil {
		if dir, _ := settings["defaultWorkingDir"].(string); dir != "" {
			return filepath.Join(dir, demoSlug)
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, demoSlug)
	}
	return demoSlug
}

// OfficeDemoSpawn launches the built-in "Demo Jarvis" team: a tiny,
// cost-bounded, REAL team spawned into a self-contained sandbox
// project. It reuses the standard spawn pipeline (and therefore its
// atomic single-team guard), so attempting a demo while any team is
// already live returns 409 Conflict — the demo can never clobber a
// real team's work. The team is tagged via templateId "demo-jarvis"
// (recorded in office status as teamTemplateId) so the UI can tell a
// demo team apart from a real one and offer a one-click "Stop demo".
func (ctx *Context) OfficeDemoSpawn(w http.ResponseWriter, r *http.Request) {
	dir := ctx.resolveDemoWorkspaceDir()

	// Scaffold + seed the sandbox project. SeedNewProjectCLAUDEMd does
	// MkdirAll + an atomic, idempotent CLAUDE.md write (never overwrites
	// an existing one), so re-running the demo reuses the same dir
	// harmlessly. A seed failure is non-fatal — the team can still spawn.
	if err := office.SeedNewProjectCLAUDEMd(dir, "jarvis-demo", demoMission); err != nil {
		slog.Warn("demo spawn: sandbox seed failed", "dir", dir, "err", err)
	}
	if ctx.SSEBroker != nil {
		ctx.SSEBroker.Broadcast("office:projectCreated", map[string]interface{}{
			"name": "jarvis-demo",
			"path": dir,
		})
	}

	// One-shot, workflows off, low effort, capped turns. Provider/Model
	// are intentionally left empty on every override so the demo inherits
	// the operator's configured provider + default model — provider
	// agnostic (Anthropic / Codex / Z.AI), no brittle hardcoded names.
	perpetual := false
	req := spawnTeamRequest{
		TemplateID:       "demo-jarvis",
		Mission:          demoMission,
		WorkingDir:       dir,
		PerpetualMode:    &perpetual,
		DisableWorkflows: true,
		MemberOverrides: []memberOverride{
			{ID: "atlas", MaxTurns: demoMaxTurns, Effort: "medium"},
			{ID: "pixel", MaxTurns: demoMaxTurns, Effort: "low"},
			{ID: "byte", MaxTurns: demoMaxTurns, Effort: "low"},
		},
	}
	ctx.spawnTeamFromRequest(w, req)
}

// spawnTeamFromRequest performs the validated spawn flow given a decoded
// spawnTeamRequest. Extracted from OfficeSpawnTeam so ConciergeSpawnTeam can
// call it directly without re-marshalling and rewriting r.Body.
func (ctx *Context) spawnTeamFromRequest(w http.ResponseWriter, req spawnTeamRequest) {

	// New-project flow: validate the slug, create /app/workspace/<name>
	// if it doesn't exist, seed a CLAUDE.md with design defaults, and
	// hand the resulting path to the normal working-dir resolver. The
	// operator's pre-filled WorkingDir is ignored when NewProject is
	// true. The actual work lives in office.PrepareNewProject; here we
	// just translate its errors into HTTP responses.
	if req.NewProject {
		target, err := office.PrepareNewProject(req.ProjectName, req.Mission, ctx.SSEBroker.Broadcast)
		if err != nil {
			if errors.Is(err, office.ErrInvalidProjectName) {
				request.WriteError(w, http.StatusBadRequest, err.Error())
			} else {
				request.WriteError(w, http.StatusInternalServerError, err.Error())
			}
			return
		}
		req.WorkingDir = target
	}

	// GitHub-repo flow: clone (or refresh) the repo into the workspace and
	// use the checkout as the working dir. Mutually exclusive with
	// NewProject; ignored when NewProject already set WorkingDir.
	if !req.NewProject && req.GithubRepo != "" {
		dir, err := ctx.resolveGithubRepoDir(req)
		if err != nil {
			if errors.Is(err, office.ErrInvalidRepo) {
				request.WriteError(w, http.StatusBadRequest, err.Error())
			} else {
				request.WriteError(w, http.StatusInternalServerError, err.Error())
			}
			return
		}
		req.WorkingDir = dir
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

	// Resolve template members + per-member overrides into the final
	// team composition. The pure helper lives in the office package
	// so a future Orchestrator implementation can call it directly
	// without going through Context.
	members, templateUsed := office.ResolveMemberList(req, ctx.findTeamTemplate)

	// Build default provider env vars
	defaultProviderEnv := ctx.getProviderEnvVars()

	// Check if using Z.AI -- allocate models across agents to respect rate limits.
	settings := ctx.StateStore.GetSettings()
	provider, _ := settings["provider"].(string)
	isZAI := provider == "zai-glm"

	// Collect requested models per member for rate-limit-aware
	// allocation. `memberProviders` collection was removed during the
	// orchestrator extraction — it was assigned but never read.
	memberIDs, requestedModels := office.CollectModelRequests(members, req.MemberOverrides)

	// Allocate Sonnet models across agents to distribute rate limits.
	var sonnetAlloc map[string]string
	if isZAI {
		sonnetAlloc = office.AllocateZAIModels(requestedModels, memberIDs)
		slog.Info("Z.AI model allocation", "allocation", sonnetAlloc)
	}

	// Build per-member provider override map with allocated Sonnet
	// models. ctx.getProviderEnvVarsFor stays handler-side (it reads
	// operator-configured account settings); the loop itself lives in
	// the office package, with the builder passed as a callback.
	overrideMap := office.BuildPerMemberEnvOverrides(
		req.MemberOverrides, sonnetAlloc, ctx.getProviderEnvVarsFor,
	)

	spawned := make([]string, 0, len(members))
	errors := make([]string, 0)

	// Build member override lookup for name/role/personality/etc
	// overrides — used by the spawn loop further down for per-member
	// working dir + tuning resolution.
	memberOverrideMap := office.IndexMemberOverrides(req.MemberOverrides)

	// Apply name/role/tier/color/emoji overrides to template members
	// in place.
	office.ApplyMemberOverrides(members, req.MemberOverrides)

	slog.Info("Spawning office team", "template", templateUsed, "members", len(members), "workingDir", req.WorkingDir)

	// Build kickstart prompts ahead of time for SDK mode.
	teamConfigs := office.BuildTeamMemberConfigs(members)

	// Perpetual mode (default true; explicit false → one-shot team).
	perpetual := office.ResolvePerpetualMode(req)

	// Index the project with CodeGraphContext for optimized context.
	// Fire-and-forget background task; never blocks the spawn.
	office.RunCGCIndex(req.WorkingDir)

	// Determine session mode for office agents.
	// "sdk"    -> ModeClaudeSDK (existing default; uses programmatic
	//             billing pool after 2026-06-15).
	// "hooks"  -> ModeClaude + HTTP hooks (interactive billing).
	// "legacy" -> ModeClaude without hooks (raw PTY).
	officeMode := ctx.resolveOfficeSessionMode()
	useSDK := officeMode == "sdk"
	hooksMode := officeMode == "hooks"

	// In hooks mode, pre-assign sessionIDs so we can write the per-agent
	// .claude/settings.local.json BEFORE the claude process spawns. The
	// settings file bakes the sessionID into each hook URL; if Claude
	// reads the file before we've assigned the ID, the URLs are wrong
	// and every hook gets fail-open dropped.
	var preassignedIDs map[string]string
	var hookSpawnInfo []office.HookSpawnInfo
	if hooksMode {
		preassignedIDs, hookSpawnInfo = office.PreassignHookSessionIDs(members)
	} else {
		preassignedIDs = make(map[string]string)
	}

	// In hooks mode, set up per-agent worktrees + write the
	// settings.local.json before the spawn loop. Returns configID -> cwd
	// (worktree path or workingDir on fallback).
	var hooksAgentCwd map[string]string
	if hooksMode {
		daemonURL := ctx.OfficeManager.GetDaemonURL()
		hooksAgentCwd = office.SetupWorktreesWithHooks(office.WorktreeHooksConfig{
			WorkingDir:  req.WorkingDir,
			DaemonURL:   daemonURL,
			Members:     teamConfigs,
			SpawnInfo:   hookSpawnInfo,
			EccDisabled: eccOptOut(req),
		})
	}

	// R23's team-wide daily cap write was removed in R24 — operator
	// uses subscription plans where the $ cap is meaningless.

	// Validate every per-member working directory UP FRONT, before spawning any
	// agent. A workingDir failure discovered mid-loop would otherwise leave the
	// members already spawned in this request orphaned + unmonitored (and free
	// the spawn slot for a duplicate overlapping team). Failing here — before the
	// first spawn — keeps the validation phase all-or-nothing.
	for _, member := range members {
		var mov *memberOverride
		if ov, has := memberOverrideMap[member.ID]; has {
			ov := ov
			mov = &ov
		}
		if _, dirErr := office.ResolveMemberWorkingDir(req.WorkingDir, member.ID, mov, ctx.ResolveWorkingDir); dirErr != nil {
			ctx.OfficeManager.ReleaseTeamSpawnSlot()
			request.WriteError(w, http.StatusBadRequest, dirErr.Error())
			return
		}
	}

	for i, member := range members {
		// Use the member-specific provider env ONLY when the override actually
		// changed the provider. A snapshot restore (and model-only overrides)
		// emit MemberOverrides with Provider="", whose env was built via
		// getProviderEnvVarsFor("", ...) -> the empty-provider case sets NO auth
		// vars. Adopting it would strip the team's ANTHROPIC_AUTH_TOKEN/BASE_URL
		// (zai-glm) or OPENAI_API_KEY (codex), 401-ing every restored agent.
		// When the provider wasn't changed, keep the team-wide env (+ allocated
		// zai sonnet); the per-member MODEL still flows via resolvedModel below.
		envVars := defaultProviderEnv
		if mov, has := memberOverrideMap[member.ID]; has && mov.Provider != "" {
			if envOv, ok := overrideMap[member.ID]; ok {
				envVars = envOv
			}
		} else if isZAI && sonnetAlloc != nil {
			envVars = ctx.getProviderEnvVarsFor(provider, "", sonnetAlloc[member.ID])
		}

		// Per-agent working-directory resolution. Per-member overrides
		// must pass the same allowlist check the team-level WorkingDir
		// went through — see office.ResolveMemberWorkingDir for the
		// canonical implementation.
		spawnReq := req
		var memberOv *memberOverride
		if ov, has := memberOverrideMap[member.ID]; has {
			ov := ov // copy for &
			memberOv = &ov
		}
		resolvedDir, dirErr := office.ResolveMemberWorkingDir(req.WorkingDir, member.ID, memberOv, ctx.ResolveWorkingDir)
		if dirErr != nil {
			// All workingDirs were pre-validated above, so this is unexpected.
			// Don't release the slot + return here: members earlier in this loop
			// may already be spawned, and returning would orphan them. Treat it
			// as a per-member spawn failure (the post-loop len(spawned)==0
			// rollback releases the slot if nothing spawned).
			slog.Error("unexpected per-member workingDir error after pre-validation",
				"member", member.ID, "error", dirErr)
			errors = append(errors, fmt.Sprintf("workingDir error for %s: %s", member.Name, dirErr))
			continue
		}
		spawnReq.WorkingDir = resolvedDir

		// Resolve the per-member provider + model BEFORE spawning so the
		// spawn path can branch on the harness (Claude vs Codex). Same
		// precedence rules reused below for RegisterAgentWithProvider.
		zaiSonnet := ""
		if isZAI && sonnetAlloc != nil {
			zaiSonnet = sonnetAlloc[member.ID]
		}
		resolvedProvider, resolvedModel := office.ResolveMemberProviderModel(
			member, memberOv, provider, zaiSonnet,
		)
		// If this member resolves to the Codex harness but the env block
		// picked above doesn't carry an OpenAI key (e.g. a template pinned
		// openai-* on an otherwise-Claude team), rebuild it for the resolved
		// provider so OPENAI_API_KEY / CODEX_API_KEY are present.
		if office.HarnessFor(resolvedProvider) == "codex" && envVars["OPENAI_API_KEY"] == "" {
			envVars = ctx.getProviderEnvVarsFor(resolvedProvider, resolvedModel, "")
		}

		spec := spawnAgentSpec{}
		if hooksMode {
			spec.HooksEnabled = true
			spec.PreassignedSessionID = preassignedIDs[member.ID]
			spec.PerAgentCwd = hooksAgentCwd[member.ID]
		}
		sessionID, usedMode := spawnAgentSession(ctx, member, spawnReq, envVars, teamConfigs, perpetual, useSDK, memberOv, resolvedProvider, resolvedModel, spec)
		if sessionID == "" {
			errors = append(errors, fmt.Sprintf("Failed to create session for %s", member.Name))
			continue
		}

		// Per-member spawn delay (operator-set stagger between spawns).
		// Cap + skip-on-last + zero-skip logic lives in the helper.
		spawnDelayMs := 0
		if memberOv != nil {
			spawnDelayMs = memberOv.SpawnDelayMs
		}
		office.SpawnDelaySleep(member.Name, spawnDelayMs, i == len(members)-1)

		// One Manager call replaces the prior RegisterAgent + GetAgent
		// + SetProvider + SetSessionMode quartet. Internal "pty" →
		// "legacy" normalisation also moved inside.
		ctx.OfficeManager.RegisterAgentWithProvider(
			sessionID, teamConfigs[i], resolvedProvider, resolvedModel, usedMode,
		)

		if ctx.TelegramHandler != nil {
			ctx.TelegramHandler.NotifySessionStart(sessionID, member.Name, req.WorkingDir)
		}

		spawned = append(spawned, member.Name)
		slog.Info("Spawned team member", "name", member.Name, "sessionId", sessionID, "role", member.Role, "mode", usedMode)
	}

	// Release the atomic spawn-slot claim if nothing actually spawned
	// — otherwise the team sits in "spawned with 0 agents" limbo and
	// the operator can't retry. Paired with TrySetTeamSpawning above.
	if len(spawned) == 0 {
		ctx.OfficeManager.ReleaseTeamSpawnSlot()
		request.WriteError(w, http.StatusInternalServerError, "Failed to spawn any agents: "+strings.Join(errors, "; "))
		return
	}

	// Post-spawn state wiring: spawn-context banner, PR linkage,
	// codegraph index, SetTeamSpawned(true) (starts monitoring
	// goroutines), mission, perpetual mode. Order is documented
	// inside FinalizeTeamSpawn — don't replace this call with
	// inlined Manager calls.
	ctx.OfficeManager.FinalizeTeamSpawn(req, templateUsed, perpetual)

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
				prompt := office.BuildKickstartPrompt(a.Config, req.WorkingDir, teamConfigs, teamMission, agentMemory, perpetual, ctx.OfficeManager.GetProjectInvariants())
				if prompt == "" {
					return
				}

				s, ok := ctx.SessionManager.GetSession(a.SessionID())
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
					slog.Info("Kickstart attempt", "name", a.Config.Name, "attempt", attempt+1, "session", a.SessionID())

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
				slog.Error("Kickstart failed after all retries", "name", a.Config.Name, "session", a.SessionID())
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

	// Persist the working dir as a saved project so the spawn page's
	// dropdown remembers it. Non-fatal: a settings write failure must not
	// fail an otherwise-successful spawn (matches how SSE-seed writes are
	// treated). github projects record the repo URL; local/new record the
	// path.
	if len(spawned) > 0 && ctx.Projects != nil {
		now := time.Now().UTC().Format(time.RFC3339)
		p := projects.Project{Kind: projects.KindLocal, Path: req.WorkingDir, LastUsedAt: now}
		if req.GithubRepo != "" {
			if url, _, name, derr := office.NormalizeRepoInputExported(req.GithubRepo); derr == nil {
				p = projects.Project{Kind: projects.KindGitHub, RepoURL: url, Name: name, Path: req.WorkingDir, LastUsedAt: now}
			}
		}
		ctx.Projects.Upsert(p)
	}

	ok(w, map[string]interface{}{
		"spawned":      spawned,
		"errors":       errors,
		"templateUsed": templateUsed,
	})
}

// resolveGithubRepoDir clones (or refreshes) req.GithubRepo into the
// workspace and returns the checkout path. Token comes from settings (or
// the GITHUB_TOKEN env as a deploy-provided fallback). Returns
// office.ErrInvalidRepo for a bad identifier (caller maps to 400).
func (ctx *Context) resolveGithubRepoDir(req spawnTeamRequest) (string, error) {
	settings := ctx.StateStore.GetSettings()
	token, _ := settings["githubToken"].(string)
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	var broadcast office.SSEBroadcaster
	if ctx.SSEBroker != nil {
		broadcast = ctx.SSEBroker.Broadcast
	}
	dir, _, _, err := office.PrepareGitHubProject(req.GithubRepo, token, nil, broadcast)
	return dir, err
}

// OfficeShutdownTeam shuts down the team.
func (ctx *Context) OfficeShutdownTeam(w http.ResponseWriter, r *http.Request) {
	const shutdownMsg = "SHUTDOWN: The team is shutting down. Your session will be terminated."
	// All the shutdown bookkeeping (NotifyEvent, RecordExperiment,
	// StopIdleMonitoring, UnregisterAgent, SetTeamSpawned(false))
	// lives on Manager.TeardownTeam. The handler-side callback owns
	// the per-session stop work because office must not import pty.
	ctx.OfficeManager.TeardownTeam(func(sessionID string) {
		if s, found := ctx.SessionManager.GetSession(sessionID); found {
			s.WriteViaScreen(shutdownMsg + "\r")
			_ = ctx.SessionManager.StopSession(context.Background(), sessionID)
		} else if sdk, found := ctx.SessionManager.GetSDKSession(sessionID); found {
			sdk.WriteInkPrompt(shutdownMsg)
			_ = ctx.SessionManager.StopSDKSession(sessionID)
		} else if cx, found := ctx.SessionManager.GetCodexSession(sessionID); found {
			cx.WriteInkPrompt(shutdownMsg)
			_ = ctx.SessionManager.StopCodexSession(sessionID)
		}
		ctx.cleanupSessionResources(sessionID)
	})
	ok(w, nil)
}

// OfficePauseTeam freezes team automation — no auto-standup, no
// stuck-agent recovery nudges, no idle replenishment. In-flight
// turns still complete; pause means "no new input", not "suspend
// mid-step". Idempotent. R25.
func (ctx *Context) OfficePauseTeam(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reason string `json:"reason"`
	}
	if r.ContentLength > 0 {
		_ = request.DecodeAndValidateMax(w, r, &req, 1<<12)
	}
	wasPaused := ctx.OfficeManager.SetPaused(true, req.Reason)
	paused, since, reason := ctx.OfficeManager.PauseSnapshot()
	ctx.SSEBroker.Broadcast("office:teamPaused", map[string]interface{}{
		"paused":    paused,
		"since":     since.UnixMilli(),
		"reason":    reason,
		"wasPaused": wasPaused,
	})
	slog.Info("team paused", "reason", reason, "wasPaused", wasPaused)
	ok(w, map[string]interface{}{"paused": true, "reason": reason})
}

// OfficeListSnapshots returns summaries of every saved snapshot.
// GET /api/office/snapshots. R25.
func (ctx *Context) OfficeListSnapshots(w http.ResponseWriter, r *http.Request) {
	snaps, err := office.ListSnapshots()
	if err != nil {
		request.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if snaps == nil {
		snaps = []office.SnapshotSummary{}
	}
	ok(w, map[string]interface{}{"snapshots": snaps})
}

// OfficeCreateSnapshot captures the current team state to a named
// snapshot. POST /api/office/snapshots {name}. R25.
func (ctx *Context) OfficeCreateSnapshot(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !request.DecodeAndValidateMax(w, r, &req, 1<<12) {
		return
	}
	if err := office.ValidateSnapshotName(req.Name); err != nil {
		request.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Snapshot current workingDir from any live SDK session AND
	// harvest each member's Claude-side session ID so the snapshot
	// can later be restored as a fork via `--resume <id>`. The pure
	// helper lives in the office package; here we just adapt the
	// pty.SessionManager into the SDKSessionLookup shape.
	workingDir := ""
	claudeSessionIDs := map[string]string{}
	if ctx.SessionManager != nil {
		lookup := func(sid string) (office.SDKSessionData, bool) {
			sdk, ok := ctx.SessionManager.GetSDKSession(sid)
			if !ok {
				return office.SDKSessionData{}, false
			}
			return office.SDKSessionData{
				WorkingDir:      sdk.WorkingDir,
				ClaudeSessionID: sdk.ClaudeSessionID(),
			}, true
		}
		workingDir, claudeSessionIDs = office.HarvestSessionData(ctx.OfficeManager.GetRoster(), lookup)
	}

	snap, err := ctx.OfficeManager.CreateSnapshot(req.Name, workingDir, claudeSessionIDs)
	if err != nil {
		request.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx.SSEBroker.Broadcast("office:snapshotCreated", map[string]interface{}{
		"name":        snap.Name,
		"createdAt":   snap.CreatedAt.UnixMilli(),
		"memberCount": len(snap.Members),
	})
	slog.Info("team snapshot created", "name", snap.Name, "members", len(snap.Members))
	ok(w, map[string]interface{}{
		"name":        snap.Name,
		"createdAt":   snap.CreatedAt.UnixMilli(),
		"memberCount": len(snap.Members),
	})
}

// OfficeDeleteSnapshot removes a saved snapshot.
// DELETE /api/office/snapshots/{name}. R25.
func (ctx *Context) OfficeDeleteSnapshot(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := office.DeleteSnapshot(name); err != nil {
		request.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx.SSEBroker.Broadcast("office:snapshotDeleted", map[string]interface{}{"name": name})
	slog.Info("team snapshot deleted", "name", name)
	ok(w, map[string]interface{}{"deleted": name})
}

// OfficeRestoreSnapshot spawns a fresh team from a saved snapshot.
// The team must NOT already be spawned (operator shuts down first).
// Post-spawn, persisted task queues are seeded back onto the
// matching agents by configID. POST /api/office/snapshots/{name}/restore. R25.
func (ctx *Context) OfficeRestoreSnapshot(w http.ResponseWriter, r *http.Request) {
	if ctx.OfficeManager.IsTeamSpawned() {
		request.WriteError(w, http.StatusConflict,
			"team is already spawned; shut down the current team before restoring a snapshot")
		return
	}
	name := chi.URLParam(r, "name")
	snap, err := office.LoadSnapshot(name)
	if err != nil {
		request.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := office.ValidateSnapshotForRestore(snap); err != nil {
		request.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	// ?fork=<bool> asks restore to resume each member's prior Claude
	// session via --resume <id> --fork-session. Requires the snapshot
	// to carry per-member ClaudeSessionIDs (captured when the snapshot
	// was created against a live SDK team). Without the IDs we fall
	// back to a fresh transcript so the operator still gets a working
	// team. Strict parse — a typo returns 400 rather than silently
	// becoming a fresh team.
	forkRequested, err := office.ParseForkQuery(r.URL.Query().Get("fork"))
	if err != nil {
		request.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Rebuild a synthetic spawn request from the snapshot. We route
	// through spawnTeamFromRequest so all the existing spawn plumbing
	// (allowlist validation, template resolve, session creation)
	// runs unchanged.
	overrides, forkAppliedCount := buildRestoreOverrides(snap, forkRequested)
	perpetual := snap.PerpetualMode
	req := spawnTeamRequest{
		WorkingDir:      snap.WorkingDir,
		Mission:         snap.TeamMission,
		MemberOverrides: overrides,
		PerpetualMode:   &perpetual,
	}

	// spawnTeamFromRequest writes its own response — we can't read
	// its result directly, so use a buffering writer to see if it
	// succeeded before broadcasting + seeding queues.
	rec := &scheduledResponseWriter{}
	ctx.spawnTeamFromRequest(rec, req)
	if rec.status >= 400 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(rec.status)
		_, _ = w.Write(rec.body.Bytes())
		return
	}

	// Seed the task queues onto freshly-spawned agents by configID.
	// Memory + learnings are per-configID on disk so they rehydrate
	// automatically from BuildKickstartPrompt without our help.
	seeded := ctx.OfficeManager.ApplyPostSpawnTaskQueues(snap)

	// Restore pause state if the snapshot was paused.
	if snap.Paused {
		ctx.OfficeManager.SetPaused(true, snap.PauseReason)
		ctx.SSEBroker.Broadcast("office:teamPaused", map[string]interface{}{
			"paused": true, "reason": snap.PauseReason,
		})
	}

	ctx.SSEBroker.Broadcast("office:snapshotRestored", map[string]interface{}{
		"name":        snap.Name,
		"memberCount": len(snap.Members),
		"seededTasks": seeded,
		"forked":      forkApplied(forkRequested, forkAppliedCount),
		"forkCount":   forkAppliedCount,
	})
	slog.Info("team snapshot restored",
		"name", snap.Name, "members", len(snap.Members),
		"seededTasks", seeded, "forkRequested", forkRequested,
		"forkAppliedCount", forkAppliedCount)
	ok(w, map[string]interface{}{
		"restored":    snap.Name,
		"memberCount": len(snap.Members),
		"seededTasks": seeded,
		"forked":      forkApplied(forkRequested, forkAppliedCount),
		"forkCount":   forkAppliedCount,
	})
}

// buildRestoreOverrides + forkApplied were moved to the office
// package (office.BuildRestoreOverrides, office.ForkApplied). Thin
// local aliases retained so the existing snapshot tests in this
// package don't churn during the migration.
var (
	buildRestoreOverrides = office.BuildRestoreOverrides
	forkApplied           = office.ForkApplied
)

// OfficeResumeTeam lifts the pause flag. Idempotent. R25.
func (ctx *Context) OfficeResumeTeam(w http.ResponseWriter, r *http.Request) {
	wasPaused := ctx.OfficeManager.SetPaused(false, "")
	ctx.SSEBroker.Broadcast("office:teamResumed", map[string]interface{}{
		"wasPaused": wasPaused,
	})
	slog.Info("team resumed", "wasPaused", wasPaused)
	ok(w, map[string]interface{}{"paused": false})
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
			"sessionId": a.SessionID(),
			"agentName": a.Config.Name,
			"tasks":     a.GetTasksCopy(), // deep copy: marshaled outside a.mu
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
		"sessionId": agent.SessionID(),
		"agentName": agent.Config.Name,
		"tasks":     agent.GetTasksCopy(), // deep copy: marshaled outside a.mu
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

// OfficeGetLearnings returns all learning files (one entry per
// agent ConfigID). The walk + filter logic lives in office.
func (ctx *Context) OfficeGetLearnings(w http.ResponseWriter, r *http.Request) {
	ok(w, map[string]interface{}{"learnings": office.ListLearningFiles()})
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
	learnings, err := office.LoadLearningsForUI(configID)
	if err != nil {
		if errors.Is(err, office.ErrLearningsNotFound) {
			ok(w, map[string]interface{}{"learnings": []interface{}{}, "configId": configID})
			return
		}
		request.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Pull the friendly name from the full record (cheap re-load
	// because LoadLearningsForUI threw it away). Falls back to
	// configID when the file lacks one.
	full := office.LoadLearningData(configID)
	name := configID
	if full != nil && full.Name != "" {
		name = full.Name
	}
	ok(w, map[string]interface{}{
		"learnings": learnings,
		"configId":  configID,
		"name":      name,
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

// buildCodeGraph walks the working directory tree (max 3 levels,
// 500 entries). The walk + filter logic lives in
// office.BuildCodeGraphSnapshot — here we just resolve the rootDir
// from the live session manager.
func (ctx *Context) buildCodeGraph() interface{} {
	sessions := ctx.SessionManager.GetAllSessions()
	if len(sessions) == 0 {
		return nil
	}
	return office.BuildCodeGraphSnapshot(sessions[0].WorkingDir)
}

// OfficePostCodeGraphImpact returns impact analysis for files.
func (ctx *Context) OfficePostCodeGraphImpact(w http.ResponseWriter, r *http.Request) {
	var req codeGraphImpactRequest
	if !decodeBody(w, r, &req) {
		return
	}
	rootDir := ""
	sessions := ctx.SessionManager.GetAllSessions()
	if len(sessions) > 0 {
		rootDir = sessions[0].WorkingDir
	}
	ok(w, map[string]interface{}{
		"files": office.AnalyzeCodeGraphImpact(rootDir, req.Files),
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

	// Fan out the wrap-up prompt AND cancel queued work that hasn't
	// started yet. The prompt tells agents "DO NOT start new tasks";
	// marking pending/acknowledged tasks as failed is the backstop —
	// an agent that ignores the prompt still can't pull a fresh task
	// because there are none left in TaskPending state.
	notifiedSessionIDs, missedAgents, cancelledTasks := mgr.FanOutWrapUpPrompt(office.WrapUpPrompt, ctx.writeSessionPrompt)

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

	// Fire-and-forget structured wrap-up report. Generates a
	// markdown post-mortem into ~/.jarvis/office-memory/reports/ and
	// broadcasts office:reportGenerated when done. Failures are
	// logged but do not roll back the wrap-up response — operators
	// can always retry via POST /api/office/reports/generate.
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic in wrap-up report generator", "panic", rec)
			}
		}()
		office.GenerateAndBroadcastWrapUpReport(mgr, "wrap-up", "", ctx.SSEBroker.Broadcast)
	}()
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

// currentSpeakerID returns the session ID of the participant currently speaking,
// or empty string if there is none (nil state, inactive, or out-of-range index).
func currentSpeakerID(su *office.StandupState) string {
	if su == nil || !su.Active || su.CurrentIndex < 0 || su.CurrentIndex >= len(su.Participants) {
		return ""
	}
	return su.Participants[su.CurrentIndex]
}

// OfficeGetStandup returns the current standup state.
func (ctx *Context) OfficeGetStandup(w http.ResponseWriter, r *http.Request) {
	su := ctx.OfficeManager.GetStandup()
	if su == nil {
		ok(w, map[string]interface{}{"standup": nil})
		return
	}
	ok(w, map[string]interface{}{
		"standup": map[string]interface{}{
			"id":               su.ID,
			"active":           su.Active,
			"participants":     su.Participants,
			"responses":        su.Responses,
			"startedAt":        su.StartedAt.Format(time.RFC3339),
			"currentIndex":     su.CurrentIndex,
			"transcript":       su.Transcript,
			"currentSpeakerId": currentSpeakerID(su),
		},
	})
}

// OfficePostStandup starts a new standup.
func (ctx *Context) OfficePostStandup(w http.ResponseWriter, r *http.Request) {
	// Cap body so a malicious client can't stream gigabytes into the
	// decoder — the legitimate payload is a few hundred bytes of
	// action/participants JSON.
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
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

	// StartStandup launches the round-robin goroutine that concurrently mutates
	// the live *StandupState (its Responses map + Transcript). Respond with the
	// lock-protected deep copy from GetStandup(), NOT StartStandup's live
	// pointer -- marshaling the live map would be a fatal concurrent map
	// read/write that crashes the daemon.
	ctx.OfficeManager.StartStandup(ids)
	ok(w, map[string]interface{}{"standup": ctx.OfficeManager.GetStandup()})
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

// standupHistoryEntry is the wire shape sent to the frontend for each historical standup.
type standupHistoryEntry struct {
	ID           string   `json:"id"`
	StartedAt    string   `json:"startedAt"`
	EndedAt      string   `json:"endedAt,omitempty"`
	Participants []string `json:"participants,omitempty"`
	MessageCount int      `json:"messageCount"`
}

// standupHistoryEntryWithSort carries a sort key alongside the output entry.
type standupHistoryEntryWithSort struct {
	entry       standupHistoryEntry
	completedAt int64
}

// maxStandupHistoryEntries caps how many standup files buildStandupHistory
// parses per request. Entries beyond this cap (oldest by mtime) are silently
// omitted; in practice a UI showing 50 standups covers weeks of history.
const maxStandupHistoryEntries = 50

// buildStandupHistory loads persisted standups from dir, shapes them into
// standupHistoryEntry values, and returns them sorted newest-first by CompletedAt.
//
// To bound the per-request work, directory entries are sorted by mtime
// descending and only the most recent maxStandupHistoryEntries are parsed
// (Fix 5b). The final sort on CompletedAt is kept so the output order is
// correct even when mtime and CompletedAt diverge slightly.
func buildStandupHistory(dir string) []standupHistoryEntry {
	files, err := os.ReadDir(dir)
	if err != nil {
		return []standupHistoryEntry{}
	}

	// Collect .json entries with their mtimes so we can pick the N newest.
	type fileWithMtime struct {
		entry os.DirEntry
		mtime int64
	}
	var jsonFiles []fileWithMtime
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		info, infoErr := f.Info()
		if infoErr != nil {
			continue
		}
		jsonFiles = append(jsonFiles, fileWithMtime{entry: f, mtime: info.ModTime().UnixNano()})
	}
	// Sort newest-mtime first so we parse the most-recent standups.
	sort.Slice(jsonFiles, func(i, j int) bool {
		return jsonFiles[i].mtime > jsonFiles[j].mtime
	})
	// Cap the parse work.
	if len(jsonFiles) > maxStandupHistoryEntries {
		jsonFiles = jsonFiles[:maxStandupHistoryEntries]
	}

	var rows []standupHistoryEntryWithSort
	for _, jf := range jsonFiles {
		id := strings.TrimSuffix(jf.entry.Name(), ".json")
		ps, loadErr := office.LoadPersistedStandup(id)
		if loadErr != nil {
			continue
		}
		entry := standupHistoryEntry{
			ID:           ps.ID,
			StartedAt:    time.UnixMilli(ps.StartedAt).Format(time.RFC3339),
			Participants: ps.Participants,
			MessageCount: len(ps.Transcript),
		}
		if ps.CompletedAt > 0 {
			entry.EndedAt = time.UnixMilli(ps.CompletedAt).Format(time.RFC3339)
		}
		rows = append(rows, standupHistoryEntryWithSort{entry: entry, completedAt: ps.CompletedAt})
	}
	// Final sort by CompletedAt to guarantee correct order even if mtime
	// and CompletedAt diverge slightly (e.g. clock skew, restored backups).
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].completedAt > rows[j].completedAt
	})
	out := make([]standupHistoryEntry, len(rows))
	for i, r := range rows {
		out[i] = r.entry
	}
	return out
}

// OfficeGetStandupHistory returns standup history sorted newest-first.
func (ctx *Context) OfficeGetStandupHistory(w http.ResponseWriter, r *http.Request) {
	history := buildStandupHistory(office.StandupDir())
	ok(w, map[string]interface{}{"history": history})
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
			"sessionId": a.SessionID(),
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
	if len(req.Content) > office.MaxMessageContentLen {
		request.WriteError(w, http.StatusBadRequest, "Message too long (max 8000 chars)")
		return
	}
	ctx.OfficeManager.SendMessage(req.FromSessionID, req.ToSessionID, req.Content)
	ok(w, nil)
}

// OfficeAgentAsk implements the blocking `cm ask` primitive. The request
// blocks for up to WaitSeconds (capped server-side) until either a
// teammate answers, the rotation chain is exhausted, or no recipient is
// available. The response always carries a status field; non-200 only
// for input validation or unknown-asker errors.
func (ctx *Context) OfficeAgentAsk(w http.ResponseWriter, r *http.Request) {
	var req agentAskRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if req.FromSessionID == "" || req.Content == "" {
		request.WriteError(w, http.StatusBadRequest, "fromSessionId and content are required")
		return
	}
	wait := time.Duration(req.WaitSeconds) * time.Second
	if wait <= 0 {
		// Default if the CLI didn't pass --wait. 5m matches the skill's
		// recommended default and the office-ask-for-help skill.
		wait = 5 * time.Minute
	}
	q, err := ctx.OfficeManager.AskQuestion(req.FromSessionID, req.Content, req.AboutTaskID, wait)
	if err != nil {
		request.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	ok(w, map[string]interface{}{
		"questionId": q.ID,
		"status":     string(q.Status),
		"answer":     q.Answer,
		"answeredBy": q.ToName,
		"tried":      q.Tried,
	})
}

// OfficeAgentAskAnswer is the responder side of `cm ask`. The recipient
// identified by FromSessionID submits an answer for the named question.
// Validates that they are the current recipient (rotation-safe).
func (ctx *Context) OfficeAgentAskAnswer(w http.ResponseWriter, r *http.Request) {
	var req agentAskAnswerRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if req.FromSessionID == "" || req.QuestionID == "" || req.Answer == "" {
		request.WriteError(w, http.StatusBadRequest, "fromSessionId, questionId, and answer are required")
		return
	}
	if err := ctx.OfficeManager.AnswerQuestion(req.QuestionID, req.FromSessionID, req.Answer); err != nil {
		request.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	ok(w, nil)
}

// OfficeAgentListQuestions returns the questions currently addressed to
// the requesting session. Used by `cm ask list` so a teammate can see
// any pending questions they should answer.
func (ctx *Context) OfficeAgentListQuestions(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" {
		request.WriteError(w, http.StatusBadRequest, "sessionId is required")
		return
	}
	questions := ctx.OfficeManager.ListPendingQuestionsFor(sessionID)
	ok(w, map[string]interface{}{"questions": questions})
}

// OfficeAgentGetTasks returns tasks for the requesting agent, plus
// any incoming handoffs awaiting accept/reject. R29 adds
// `incomingHandoffs` to the payload so an agent polling its queue
// sees what peers are trying to hand them without a second request.
func (ctx *Context) OfficeAgentGetTasks(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")
	agent := ctx.OfficeManager.GetAgentAndTouch(sessionID)
	if agent == nil {
		ok(w, map[string]interface{}{"tasks": []interface{}{}, "incomingHandoffs": []interface{}{}})
		return
	}
	ok(w, map[string]interface{}{
		"tasks":            agent.GetTasksCopy(), // deep copy: marshaled outside a.mu
		"incomingHandoffs": ctx.OfficeManager.IncomingHandoffsFor(sessionID),
	})
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

// uiKeywordRe + verificationEvidenceRe were moved to the office
// package (office.UIKeywordRe, office.VerificationEvidenceRe).
// Aliases retained so the existing screenshot_gate_test.go in this
// package keeps compiling.
var (
	uiKeywordRe            = office.UIKeywordRe
	verificationEvidenceRe = office.VerificationEvidenceRe
)

// OfficeAgentTaskComplete marks a task as completed. Adds a `warning`
// field to the response + broadcasts office:uiTaskMissingScreenshot when
// the task title matches UI keywords but the summary lacks any mention
// of browser verification. Soft enforcement — never blocks completion.
func (ctx *Context) OfficeAgentTaskComplete(w http.ResponseWriter, r *http.Request) {
	var req agentTaskCompleteRequest
	if !decodeBody(w, r, &req) {
		return
	}
	agent := ctx.OfficeManager.GetAgentAndTouch(req.SessionID)
	var warning string
	if agent != nil {
		cwd := agent.GetWorktreePath()
		sid := req.SessionID

		// Criteria gate: read AcceptanceCriteria, labels, and CriteriaRejectCount
		// under the agent mutex via TaskGateInfo so the returned criteria slice is
		// a copy — no live task field is read after the lock is released.
		taskCriteria, taskIsGated, taskRejectCount, _ := agent.TaskGateInfo(req.TaskID)

		// Gated path: run evaluator synchronously before CompleteTask.
		if len(taskCriteria) > 0 && taskIsGated {
			v, _ := office.EvaluateAcceptanceCriteria(r.Context(), cwd, req.Result, taskCriteria)
			if v.EvaluatorError != "" {
				// Fail-open: broadcast the infra error and continue to completion.
				ctx.SSEBroker.Broadcast("office:taskCriteriaReviewed", map[string]interface{}{
					"sessionId":      sid,
					"taskId":         req.TaskID,
					"results":        v.Results,
					"allPass":        v.AllPass,
					"evaluatorError": v.EvaluatorError,
				})
				// Fall through to CompleteTask below.
			} else if !v.AllPass {
				// Real failure: reject or escalate.
				if taskRejectCount >= 2 {
					// Escalate: block the task, set blocker description, set progress, broadcast, respond.
					agent.UpdateTaskStatus(req.TaskID, office.TaskBlocked)
					var failingSummary strings.Builder
					for _, cr := range v.Results {
						if !cr.Pass {
							failingSummary.WriteString("- ")
							failingSummary.WriteString(cr.Criterion)
							failingSummary.WriteString(": ")
							failingSummary.WriteString(cr.Reason)
							failingSummary.WriteString("\n")
						}
					}
					blockerDesc := "Acceptance criteria not met after 2 rejections:\n" + failingSummary.String()
					agent.SetTaskBlockerDescription(req.TaskID, blockerDesc)
					agent.SetTaskProgress(req.TaskID, "escalated: criteria not met after 2 rejections\n"+failingSummary.String())
					ctx.SSEBroker.Broadcast("office:taskCompletionEscalated", map[string]interface{}{
						"sessionId": sid,
						"taskId":    req.TaskID,
						"failing":   v.Results,
					})
					request.WriteJSON(w, http.StatusAccepted, map[string]interface{}{
						"completed": false,
						"escalated": true,
						"failing":   v.Results,
					})
					return
				}
				// Reject: increment counter, deliver follow-up prompt, respond.
				n := agent.IncrementCriteriaReject(req.TaskID)
				var promptBuf strings.Builder
				promptBuf.WriteString("Your last task completion was rejected because the following acceptance criteria were not met:\n")
				for _, cr := range v.Results {
					if !cr.Pass {
						promptBuf.WriteString("- ")
						promptBuf.WriteString(cr.Criterion)
						promptBuf.WriteString(": ")
						promptBuf.WriteString(cr.Reason)
						promptBuf.WriteString("\n")
					}
				}
				promptBuf.WriteString("\nPlease address the above and call `cm task complete` again when the criteria are satisfied.")
				ctx.OfficeManager.Sessions().WriteInkPrompt(sid, promptBuf.String())
				ctx.SSEBroker.Broadcast("office:taskCompletionRejected", map[string]interface{}{
					"sessionId":   sid,
					"taskId":      req.TaskID,
					"failing":     v.Results,
					"rejectCount": n,
				})
				request.WriteJSON(w, http.StatusAccepted, map[string]interface{}{
					"completed":   false,
					"failing":     v.Results,
					"rejectCount": n,
				})
				return
			}
			// AllPass (or fail-open above) — fall through to CompleteTask.
		}

		// Single-lock lookup + mutation. Returns AlreadyComplete=true
		// for repeat calls so we short-circuit broadcasts (observed
		// 6× re-fire in prod before the idempotency guard).
		result := agent.CompleteTask(req.TaskID)
		if result.AlreadyComplete {
			ok(w, map[string]interface{}{"alreadyComplete": true})
			return
		}
		// Result.Found may be false when the task ID is unknown; the
		// pre-extraction code fell through here (UpdateTaskStatus
		// returned false, broadcasts still fired with the unknown
		// taskID, and downstream branches no-op'd on nil). Preserve
		// that — broadcasts for a nonexistent task are harmless.

		// Soft screenshot gate. The predicate (title hits UI keywords
		// AND summary lacks verification phrases AND no attached PNG)
		// lives in office.ShouldWarnMissingScreenshot; here we just
		// broadcast the SSE + log when it fires.
		if office.ShouldWarnMissingScreenshot(result.Title, req.Result, req.TaskID) {
			warning = "UI-tagged task completed without explicit visual verification evidence in the summary. Did you run browser-screenshot against the dev URL? Cite the path (e.g. '/tmp/after.png') in your --summary, or upload the PNG via `cm task complete <id> --screenshot <path>` so operators see the evidence in the dashboard."
			ctx.SSEBroker.Broadcast("office:uiTaskMissingScreenshot", map[string]interface{}{
				"sessionId": req.SessionID,
				"taskId":    req.TaskID,
				"title":     result.Title,
			})
			slog.Warn("UI task completed without visual verification",
				"sessionId", req.SessionID, "taskId", req.TaskID, "title", result.Title)
		}

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

		// R3.4 · Fire `claude ultrareview` against the agent's
		// worktree when the task carries the `needs-review` label.
		// Runs async — the operator gets a background SSE event with
		// findings; the agent's task-complete reply is unaffected
		// (and never blocks).
		if office.TaskHasNeedsReviewLabel(result.Task) {
			cwd := agent.GetWorktreePath()
			if cwd == "" {
				slog.Warn("ultrareview: task tagged needs-review but agent has no worktree",
					"sessionId", req.SessionID, "taskId", req.TaskID)
			} else {
				broker := ctx.SSEBroker
				sid := req.SessionID
				tid := req.TaskID
				configID := agent.ConfigID
				go func() {
					// Background-task panic guard: a panic inside the
					// review shell-out or SSE writer must not take down
					// jarvis. Same pattern as the kickstart goroutine.
					defer func() {
						if rec := recover(); rec != nil {
							slog.Error("ultrareview panic", "sessionId", sid, "taskId", tid, "rec", rec)
						}
					}()
					// Use the configID-aware variant so the findings
					// also append to the library experience markdown.
					office.RunUltrareviewAndBroadcastForAgent(context.Background(), cwd, configID, sid, tid, broker.Broadcast)
				}()
			}
		}

		// Immediate re-tasking (async to avoid blocking the HTTP
		// response). Falls back to idle-monitor if nothing's queued.
		nextTask := agent.NextPendingTask()
		if nextTask != nil {
			sid := agent.SessionID()
			title := nextTask.Title
			taskID := nextTask.ID
			go func() {
				// Background prompt injection — a panic here (closed
				// PTY fd, SDK writer race on session teardown) must
				// not take down the whole jarvis process.
				defer func() {
					if r := recover(); r != nil {
						slog.Error("next-task prompt injection panic", "session", sid, "taskId", taskID, "err", r)
					}
				}()
				ctx.OfficeManager.PromptNextTask(sid, title, taskID)
			}()
		} else {
			safego.Go("office.checkForIdleAgents", ctx.OfficeManager.CheckForIdleAgents)
		}

		// Advisory criteria review: when the task has AcceptanceCriteria but
		// is NOT gated, run the evaluator in the background so operators see
		// a non-blocking review event (office:taskCriteriaReviewed) without
		// delaying the HTTP response or gating completion.
		if len(taskCriteria) > 0 && !taskIsGated {
			criteriaSnap := taskCriteria
			resultSnap := req.Result
			cwdSnap := cwd
			tidSnap := req.TaskID
			broker := ctx.SSEBroker
			safego.Go("office.advisoryCriteria", func() {
				v, _ := office.EvaluateAcceptanceCriteria(context.Background(), cwdSnap, resultSnap, criteriaSnap)
				broker.Broadcast("office:taskCriteriaReviewed", map[string]interface{}{
					"sessionId":      sid,
					"taskId":         tidSnap,
					"results":        v.Results,
					"allPass":        v.AllPass,
					"evaluatorError": v.EvaluatorError,
				})
			})
		}

		// Knowledge-library write. Renders the just-completed task
		// into ~/.jarvis/office-memory/library/experiences/<id>/<task>.md
		// so PageIndex can index it for future agents. Wrapped in
		// safego so a library-write failure can't affect the
		// task-complete HTTP response.
		if result.Found && result.Task != nil {
			rec := buildLibraryExperience(agent, result.Task, req.Result)
			safego.Go("library.writeExperience", func() {
				if err := library.WriteExperience(rec); err != nil {
					slog.Warn("library write failed",
						"sessionId", req.SessionID,
						"taskId", req.TaskID,
						"err", err)
				}
			})
		}
	}
	if warning != "" {
		ok(w, map[string]interface{}{"warning": warning})
	} else {
		ok(w, nil)
	}
}

// buildLibraryExperience projects a completed task + agent state +
// the operator-supplied result string into a library.ExperienceRecord.
// Kept handler-side so the office package doesn't need to know about
// the library schema (clean dependency direction: handler imports
// both office + library).
//
// Outcome score is computed from completion status + ultrareview
// severity ("none" at this point because ultrareview runs async
// post-broadcast) + the agent's current quality proxy + a
// placeholder recovery attempts of 0 (TODO: thread the real count
// from recovery.go once it's tracked per-task).
func buildLibraryExperience(agent *office.AgentState, task *office.Task, completionNote string) library.ExperienceRecord {
	agentName := agent.ConfigID
	role := ""
	if agent.Config != nil {
		agentName = agent.Config.Name
		role = agent.Config.Role
	}
	durationMS := int64(0)
	if !task.StartedAt.IsZero() && !task.CompletedAt.IsZero() {
		durationMS = task.CompletedAt.Sub(task.StartedAt).Milliseconds()
	}
	quality := office.CalculateAgentScore(agent).QualityScore
	outcome := office.ComputeOutcomeScore(office.TaskCompleted, "none", quality, 0)
	approach := completionNote
	if approach == "" && len(task.ProgressLog) > 0 {
		// Fall back to the last progress entry as a one-line summary
		// when the operator didn't pass a --summary.
		approach = task.ProgressLog[len(task.ProgressLog)-1].Note
	}
	return library.ExperienceRecord{
		AgentConfigID:       agent.ConfigID,
		AgentName:           agentName,
		AgentRole:           role,
		TaskID:              task.ID,
		TaskTitle:           task.Title,
		TaskLabels:          task.Labels,
		WorkingDir:          agent.GetWorktreePath(),
		CompletionStatus:    "completed",
		OutcomeScore:        outcome,
		UltrareviewSeverity: "none",
		RecoveryAttempts:    0,
		StartedAt:           task.StartedAt,
		CompletedAt:         task.CompletedAt,
		DurationMS:          durationMS,
		CostUSD:             task.FinalCostUSD,
		ApproachSummary:     approach,
	}
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
	if agent == nil {
		ok(w, nil)
		return
	}
	// Validate labels BEFORE creating the task so invalid input
	// yields a clean 400 instead of a half-created task.
	normLabels, err := office.NormalizeAndValidateLabels(req.Labels)
	if err != nil {
		request.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Validate acceptance criteria before creating the task for the same reason.
	for _, c := range req.AcceptanceCriteria {
		if err := office.ValidateAcceptanceCriterion(c); err != nil {
			request.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	task := &office.Task{
		ID:                 uuid.New().String(),
		Title:              req.Title,
		Description:        req.Description,
		Priority:           req.Priority,
		Status:             office.TaskPending,
		AssignedAt:         time.Now(),
		Source:             "self",
		Labels:             normLabels,
		AcceptanceCriteria: req.AcceptanceCriteria,
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
	ok(w, map[string]interface{}{"taskId": task.ID})
}

// slotFromQuery returns the slot name for a dev-instance request. Empty
// query param defaults to "app" so R13/R14 callers keep working.
func slotFromQuery(r *http.Request) string {
	name := r.URL.Query().Get("name")
	if name == "" {
		return "app"
	}
	return name
}

// OfficeAgentGetDevInstance returns one slot's URL + PID + credentials-set
// flag. ?name=<slot> selects a specific slot (default "app"). Empty url/0
// pid when no dev server is running for that slot.
func (ctx *Context) OfficeAgentGetDevInstance(w http.ResponseWriter, r *http.Request) {
	slot := slotFromQuery(r)
	inst, exists := ctx.OfficeManager.GetDevInstance(slot)
	if !exists {
		ok(w, map[string]interface{}{"name": slot, "url": "", "pid": 0, "credsSet": false})
		return
	}
	ok(w, map[string]interface{}{
		"name":           slot,
		"url":            inst.URL,
		"pid":            inst.PID,
		"credsSet":       inst.Credentials != "",
		"forceAnnounced": inst.ForceAnnounced,
		"healthFailures": inst.HealthFailures,
	})
}

// OfficeAgentGetDevInstanceAll returns every slot. Used by jarvis-mcp
// running inside agent sessions (agent-token auth).
func (ctx *Context) OfficeAgentGetDevInstanceAll(w http.ResponseWriter, r *http.Request) {
	writeDevInstancesListing(w, ctx.OfficeManager)
}

// OfficeGetDevInstances is the browser-auth mirror of
// OfficeAgentGetDevInstanceAll. The R15 dashboard card fetches this from
// the office UI (which has a session cookie, not an agent token), so it
// needs a route under the browser-auth group rather than /office/agent.
// Value-free info: credsSet is a bool, never the plaintext credentials.
func (ctx *Context) OfficeGetDevInstances(w http.ResponseWriter, r *http.Request) {
	writeDevInstancesListing(w, ctx.OfficeManager)
}

// writeDevInstancesListing is the shared listing body so the agent-auth
// and browser-auth handlers return identical shapes.
func writeDevInstancesListing(w http.ResponseWriter, mgr *office.Manager) {
	all := mgr.ListDevInstances()
	out := make(map[string]interface{}, len(all))
	for name, inst := range all {
		out[name] = map[string]interface{}{
			"url":            inst.URL,
			"pid":            inst.PID,
			"credsSet":       inst.Credentials != "",
			"forceAnnounced": inst.ForceAnnounced,
			"healthFailures": inst.HealthFailures,
			"lastCheckAt":    inst.LastCheckAt,
		}
	}
	ok(w, map[string]interface{}{"slots": out})
}

// OfficeAgentPostDevInstance records a slot's URL + optional PID.
// Rejects localhost URLs (unreachable from headless-shell) with 400
// unless ?force=true, and rejects port conflicts with 409 unless
// ?force=true overrides. Slot name comes from the body (req.Name) or
// defaults to "app".
func (ctx *Context) OfficeAgentPostDevInstance(w http.ResponseWriter, r *http.Request) {
	var req agentDevInstanceRequest
	if !decodeBody(w, r, &req) {
		return
	}
	forced := r.URL.Query().Get("force") == "true"
	if !forced && (strings.Contains(req.URL, "localhost") || strings.Contains(req.URL, "127.0.0.1")) {
		request.WriteError(w, http.StatusBadRequest,
			"localhost URLs aren't reachable from headless-shell; use http://jarvis:<port> (or pass ?force=true to override)")
		return
	}
	slot := req.Name
	if slot == "" {
		slot = "app"
	}
	if err := ctx.OfficeManager.SetDevInstance(slot, req.URL, req.PID, forced); err != nil {
		// Port conflicts return 409 so CLI clients can display a precise
		// error message instead of the generic 400 used for bad input.
		if strings.Contains(err.Error(), "already held by slot") {
			request.WriteError(w, http.StatusConflict, err.Error())
			return
		}
		request.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	inst, _ := ctx.OfficeManager.GetDevInstance(slot)
	ok(w, map[string]interface{}{
		"name":           slot,
		"url":            inst.URL,
		"pid":            inst.PID,
		"forceAnnounced": inst.ForceAnnounced,
	})
}

// OfficeAgentDeleteDevInstance clears a single slot (default "app") and
// SIGTERMs the tracked process tree. ?all=true wipes every slot — used
// by `cm dev clear --all` and by team wrap-up.
func (ctx *Context) OfficeAgentDeleteDevInstance(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("all") == "true" {
		ctx.OfficeManager.ClearAllDevInstances(true)
		ok(w, map[string]interface{}{"cleared": "all"})
		return
	}
	slot := slotFromQuery(r)
	ctx.OfficeManager.ClearDevInstance(slot, true)
	ok(w, map[string]interface{}{"cleared": slot})
}

// OfficeAgentGetDevCredentials returns the named slot's login blob.
func (ctx *Context) OfficeAgentGetDevCredentials(w http.ResponseWriter, r *http.Request) {
	slot := slotFromQuery(r)
	inst, exists := ctx.OfficeManager.GetDevInstance(slot)
	if !exists {
		ok(w, map[string]interface{}{"name": slot, "credentials": ""})
		return
	}
	ok(w, map[string]interface{}{"name": slot, "credentials": inst.Credentials})
}

// OfficeAgentPostDevCredentials records the free-text login blob for a slot.
func (ctx *Context) OfficeAgentPostDevCredentials(w http.ResponseWriter, r *http.Request) {
	var req agentDevCredentialsRequest
	if !decodeBody(w, r, &req) {
		return
	}
	slot := req.Name
	if slot == "" {
		slot = "app"
	}
	if err := ctx.OfficeManager.SetDevInstanceCredentials(slot, req.Credentials); err != nil {
		request.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Echo only whether creds are set; never reflect the value back.
	ok(w, map[string]interface{}{"name": slot, "set": req.Credentials != ""})
}

// OfficeAgentDeleteDevCredentials clears the login blob for a slot.
func (ctx *Context) OfficeAgentDeleteDevCredentials(w http.ResponseWriter, r *http.Request) {
	slot := slotFromQuery(r)
	_ = ctx.OfficeManager.SetDevInstanceCredentials(slot, "")
	ok(w, map[string]interface{}{"name": slot, "set": false})
}

// OfficeAgentTaskTriage handles task triage by a leader agent.
func (ctx *Context) OfficeAgentTaskTriage(w http.ResponseWriter, r *http.Request) {
	var req agentTaskTriageRequest
	if !decodeBody(w, r, &req) {
		return
	}

	source, target, err := ctx.OfficeManager.TriageTaskBetweenAgents(req.SessionID, req.ToSessionID, req.TaskID)
	if err != nil {
		request.WriteError(w, http.StatusNotFound, err.Error())
		return
	}

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

// OfficeAgentTaskCriteria mutates a task's acceptance criteria.
// POST /api/office/agent/task/criteria
// Body: {sessionId, taskId, op ("set"|"add"|"remove"), criteria []string}
// For op="remove", criteria holds a single 0-based index string.
// Returns {criteria: []string} with the updated list on success.
// Errors: 404 when the agent or task is unknown; 400 for validation failures.
func (ctx *Context) OfficeAgentTaskCriteria(w http.ResponseWriter, r *http.Request) {
	var req agentTaskCriteriaRequest
	if !decodeBody(w, r, &req) {
		return
	}
	agent := ctx.OfficeManager.GetAgentAndTouch(req.SessionID)
	if agent == nil {
		request.WriteError(w, http.StatusNotFound, "agent not found")
		return
	}
	if err := agent.SetTaskCriteria(req.TaskID, req.Op, req.Criteria); err != nil {
		msg := err.Error()
		if msg == "task not found" {
			request.WriteError(w, http.StatusNotFound, msg)
			return
		}
		request.WriteError(w, http.StatusBadRequest, msg)
		return
	}
	ctx.SSEBroker.Broadcast("office:taskUpdated", map[string]interface{}{
		"sessionId": req.SessionID,
		"taskId":    req.TaskID,
	})
	// Return the updated criteria list so the caller can confirm the change.
	// Deep copy: AcceptanceCriteria is mutated under a.mu by SetTaskCriteria.
	var current []string
	for _, t := range agent.GetTasksCopy() {
		if t.ID == req.TaskID {
			current = append([]string(nil), t.AcceptanceCriteria...)
			break
		}
	}
	ok(w, map[string]interface{}{"criteria": current})
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
	// Validate file path: no path traversal, no absolute paths, no
	// NUL bytes. Helper lives in internal/strs.
	if !strs.IsSafeRelativePath(req.FilePath) {
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
// OfficeAgentKnowledgeSearch exposes SearchKnowledge to agents via
// `cm knowledge search <query>`. Default scope is the current team's
// agents (pulled from the live roster) + the active team's project
// workingDir. Pass ?all=true to broaden to every agent file on disk
// (cross-project search; used rarely).
func (ctx *Context) OfficeAgentKnowledgeSearch(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		request.WriteError(w, http.StatusBadRequest, "q is required")
		return
	}
	limit := 20
	if s := r.URL.Query().Get("limit"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	opts := office.KnowledgeSearchOptions{
		Query: query,
		Limit: limit,
	}
	if r.URL.Query().Get("all") != "true" {
		opts.AllowedAgents = liveTeamAgentSet(ctx)
		opts.WorkingDir = liveTeamWorkingDir(ctx)
	}
	hits, err := office.SearchKnowledge(opts)
	if err != nil {
		request.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]interface{}{"hits": hits})
}

// OfficeAgentKnowledgeTail is the non-query variant — pure recency.
func (ctx *Context) OfficeAgentKnowledgeTail(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if s := r.URL.Query().Get("limit"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	var allowed map[string]bool
	if r.URL.Query().Get("all") != "true" {
		allowed = liveTeamAgentSet(ctx)
	}
	hits, err := office.TailKnowledge(limit, allowed)
	if err != nil {
		request.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]interface{}{"hits": hits})
}

// liveTeamAgentSet builds the set of configIDs for agents currently in
// the roster. Used to default knowledge queries to the active team
// (so an agent on bonusbuddy doesn't see hits from kamban memory).
func liveTeamAgentSet(ctx *Context) map[string]bool {
	roster := ctx.OfficeManager.GetRoster()
	if len(roster) == 0 {
		return nil
	}
	out := make(map[string]bool, len(roster))
	for _, a := range roster {
		if a.ConfigID != "" {
			out[a.ConfigID] = true
		}
	}
	return out
}

// liveTeamWorkingDir returns the working directory of any live agent —
// they all share it for a single team. Empty if no team is running.
func liveTeamWorkingDir(ctx *Context) string {
	_, workingDir := ctx.OfficeManager.GetSpawnContext()
	return workingDir
}

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
	skillPath, updated, err := office.WriteSharedSkill(req.SkillName, req.Content)
	if err != nil {
		if errors.Is(err, office.ErrSkillPathTraversal) {
			request.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		request.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	action := "created"
	if updated {
		action = "updated"
	}
	ok(w, map[string]interface{}{
		"action":    action,
		"skillName": req.SkillName,
		"path":      skillPath,
	})
}

// OfficeAgentDeleteSkill deletes a shared skill by name. The file
// I/O + traversal-prevention lives in office.DeleteSharedSkill.
func (ctx *Context) OfficeAgentDeleteSkill(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if !safeName(name) {
		request.WriteError(w, http.StatusBadRequest, "invalid skill name")
		return
	}
	if err := office.DeleteSharedSkill(name); err != nil {
		if errors.Is(err, office.ErrSkillPathTraversal) {
			request.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		request.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]interface{}{"deleted": name})
}

// getSessionWorkingDir returns the working directory for a session (PTY, SDK, or Codex).
func (ctx *Context) getSessionWorkingDir(sessionID string) string {
	if s, found := ctx.SessionManager.GetSession(sessionID); found {
		return s.WorkingDir
	}
	if sdk, found := ctx.SessionManager.GetSDKSession(sessionID); found {
		return sdk.WorkingDir
	}
	if cx, found := ctx.SessionManager.GetCodexSession(sessionID); found {
		return cx.WorkingDir
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
		// R28 · Sibling endpoints (OfficeAgentPostMemoryWrite,
		// OfficeAgentPostMemoryAppend) already emit a generic message; do
		// the same here. The raw err from writeMemorySections contains
		// the temp-file path (~/.jarvis/agent-memory/<hash>/<configID>.md.tmp)
		// which leaks the internal storage layout to any caller who can
		// trigger an os.Rename or os.WriteFile failure.
		slog.Error("memory section write failed",
			"configID", agent.ConfigID, "section", req.Name, "append", req.Append, "err", err)
		request.WriteError(w, http.StatusInternalServerError, "failed to write section")
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
	// Read the config under the agent lock (SetVoice mutates a sibling field of
	// the same *TeamMemberConfig under a.mu, so an unguarded read here is a race).
	cfg := agent.SnapshotConfig()
	personality := cfg.Personality
	systemPrompt := cfg.SystemPrompt
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

// OfficePutMemberVoice sets the TTS voice for a specific member.
func (ctx *Context) OfficePutMemberVoice(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionId")
	var req memberVoiceRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if !tts.IsValidVoice(req.Voice) {
		request.WriteError(w, http.StatusBadRequest, "invalid voice")
		return
	}
	agent := ctx.OfficeManager.GetAgent(sessionID)
	if agent == nil {
		request.WriteError(w, http.StatusNotFound, "member not found")
		return
	}
	agent.SetVoice(req.Voice)
	ctx.SSEBroker.Broadcast("office:memberUpdated", map[string]interface{}{
		"sessionId": sessionID,
		"voice":     req.Voice,
	})
	ok(w, map[string]interface{}{"voice": req.Voice})
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

// ---------------------------------------------------------------------------
// Project invariants handlers
// ---------------------------------------------------------------------------

// OfficeGetProjectInvariants returns the current project invariants list.
func (ctx *Context) OfficeGetProjectInvariants(w http.ResponseWriter, r *http.Request) {
	ok(w, map[string]interface{}{"invariants": ctx.OfficeManager.GetProjectInvariants()})
}

// OfficePutProjectInvariants replaces the full invariants list.
func (ctx *Context) OfficePutProjectInvariants(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Invariants []string `json:"invariants"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if req.Invariants == nil {
		req.Invariants = []string{}
	}
	ctx.OfficeManager.SetProjectInvariants(req.Invariants)
	ok(w, map[string]interface{}{"invariants": ctx.OfficeManager.GetProjectInvariants()})
}

// OfficePostProjectInvariant appends a single invariant to the list.
func (ctx *Context) OfficePostProjectInvariant(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if req.Text == "" {
		request.WriteError(w, http.StatusBadRequest, "text is required")
		return
	}
	ctx.OfficeManager.AddProjectInvariant(req.Text)
	ok(w, map[string]interface{}{"invariants": ctx.OfficeManager.GetProjectInvariants()})
}

// OfficeDeleteProjectInvariant removes a single invariant by index.
func (ctx *Context) OfficeDeleteProjectInvariant(w http.ResponseWriter, r *http.Request) {
	idxStr := chi.URLParam(r, "index")
	idx, err := strconv.Atoi(idxStr)
	if err != nil {
		request.WriteError(w, http.StatusBadRequest, "invalid index: must be an integer")
		return
	}
	if err := ctx.OfficeManager.RemoveProjectInvariant(idx); err != nil {
		request.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	ok(w, map[string]interface{}{"invariants": ctx.OfficeManager.GetProjectInvariants()})
}
