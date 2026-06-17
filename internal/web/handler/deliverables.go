package handler

import (
	"net/http"
	"strconv"

	"github.com/kshaham/jarvis/internal/deliverables"
	"github.com/kshaham/jarvis/internal/projects"
	"github.com/kshaham/jarvis/internal/web/request"
)

// deliverableProjectTile is a saved project joined with its deliverables state.
type deliverableProjectTile struct {
	projects.Project
	TeamActive   bool                       `json:"teamActive"`
	Drafting     *deliverables.Drafting     `json:"drafting,omitempty"`
	Deliverables []deliverables.Deliverable `json:"deliverables"`
}

// MockupsGetResults' sibling: list every project with its deliverables.
// GET /api/deliverables/results
func (ctx *Context) DeliverablesGetResults(w http.ResponseWriter, _ *http.Request) {
	_, workdir := ctx.OfficeManager.GetSpawnContext()
	teamUp := ctx.OfficeManager.IsTeamSpawned()
	list := ctx.Projects.List()
	tiles := make([]deliverableProjectTile, 0, len(list))
	for _, p := range list {
		docs, _ := deliverables.List(p.ID)
		// A nil slice marshals to JSON `null`; the API contract is an array, so
		// normalize to [] (clients type `deliverables` as a non-null array).
		if docs == nil {
			docs = []deliverables.Deliverable{}
		}
		var drafting *deliverables.Drafting
		if d := ctx.Deliverables.Drafting(p.ID); d.Type != "" {
			drafting = &d
		}
		tiles = append(tiles, deliverableProjectTile{
			Project:      p,
			TeamActive:   teamUp && p.Path != "" && workdir == p.Path,
			Drafting:     drafting,
			Deliverables: docs,
		})
	}
	ok(w, map[string]interface{}{"projects": tiles})
}

// DeliverablesGetDoc returns one document's markdown (latest if no version).
// GET /api/deliverables/doc?projectId=&slug=&version=
func (ctx *Context) DeliverablesGetDoc(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("projectId")
	slug := r.URL.Query().Get("slug")
	var version int64
	if v := r.URL.Query().Get("version"); v != "" {
		version, _ = strconv.ParseInt(v, 10, 64)
	}
	content, meta, err := deliverables.Get(projectID, slug, version)
	if err != nil {
		request.WriteError(w, http.StatusNotFound, "deliverable not found")
		return
	}
	ok(w, map[string]interface{}{
		"title":   meta.Title,
		"type":    meta.Type,
		"content": content,
	})
}

type deliverablesGenerateRequest struct {
	ProjectID string `json:"projectId"`
	Type      string `json:"type"`
	Title     string `json:"title"`
}

// DeliverablesGenerate dispatches a "write a <type> deliverable" prompt to the
// spawned team. POST /api/deliverables/generate {projectId, type, title?}
func (ctx *Context) DeliverablesGenerate(w http.ResponseWriter, r *http.Request) {
	var req deliverablesGenerateRequest
	if !request.DecodeAndValidateMax(w, r, &req, 1<<12) {
		return
	}
	spec, ok2 := deliverableTypes[req.Type]
	if !ok2 {
		request.WriteError(w, http.StatusBadRequest, "unknown deliverable type")
		return
	}
	proj, found := findSavedProject(ctx.Projects.List(), req.ProjectID)
	if !found {
		request.WriteError(w, http.StatusNotFound, "project not found")
		return
	}
	if proj.Path == "" {
		request.WriteError(w, http.StatusBadRequest, "project has no local path yet")
		return
	}
	_, workdir := ctx.OfficeManager.GetSpawnContext()
	if !ctx.OfficeManager.IsTeamSpawned() || workdir != proj.Path {
		request.WriteError(w, http.StatusConflict, "spawn a team for this project first")
		return
	}
	roster := ctx.OfficeManager.GetRoster()
	if len(roster) == 0 {
		request.WriteError(w, http.StatusConflict, "the team has no agents")
		return
	}
	sessions := ctx.OfficeManager.Sessions()
	if sessions == nil {
		request.WriteError(w, http.StatusInternalServerError, "session store unavailable")
		return
	}
	title := req.Title
	if title == "" {
		title = spec.DefaultTitle
	}
	ctx.Deliverables.StartDrafting(proj.ID, req.Type, title)
	lead := roster[0]
	if !sessions.WriteInkPrompt(lead.SessionID(), buildDeliverablePrompt(proj, req.Type, title, spec)) {
		request.WriteError(w, http.StatusConflict, "team lead is not accepting prompts")
		return
	}
	request.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"success": true, "status": "drafting"})
}

type deliverableUploadRequest struct {
	ProjectID string `json:"projectId"`
	Title     string `json:"title"`
	Type      string `json:"type"`
	Content   string `json:"content"`
}

// DeliverablesUpload saves one deliverable version from a team agent.
// POST /api/office/agent/deliverable-upload (agent-token auth).
func (ctx *Context) DeliverablesUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8*1024*1024)
	var req deliverableUploadRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if req.ProjectID == "" || req.Title == "" || req.Content == "" {
		request.WriteError(w, http.StatusBadRequest, "projectId, title and content are required")
		return
	}
	if _, found := findSavedProject(ctx.Projects.List(), req.ProjectID); !found {
		request.WriteError(w, http.StatusNotFound, "project not found")
		return
	}
	slug, err := ctx.Deliverables.SaveDoc(req.ProjectID, req.Title, req.Type, req.Content)
	if err != nil {
		request.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	ok(w, map[string]interface{}{"slug": slug})
}

// ── workflow action types (research + ops) ───────────────────────────────

type deliverableSpec struct {
	Label        string
	DefaultTitle string
	Guidance     string // what a good doc of this type contains
}

var deliverableTypes = map[string]deliverableSpec{
	"research-brief": {"Research brief", "Research brief",
		"objective, background, scope, method, key findings (with evidence), open questions, and recommended next steps"},
	"competitive-analysis": {"Competitive analysis", "Competitive analysis",
		"the landscape, a comparison of the main players/options (a table of dimensions vs options), strengths/weaknesses, gaps/opportunities, and a recommendation"},
	"findings-memo": {"Findings memo", "Findings memo",
		"context, the headline finding up top, supporting evidence/data, implications, and concrete recommendations"},
	"literature-review": {"Literature review", "Literature review",
		"the question, the sources reviewed (cited), themes/consensus and disagreements across sources, and a synthesis"},
	"project-plan": {"Project plan", "Project plan",
		"goal & success criteria, scope, milestones/timeline, workstreams & owners, risks & mitigations, and dependencies"},
	"sop": {"SOP", "Standard operating procedure",
		"purpose, when to use it, prerequisites, numbered step-by-step procedure, roles/responsibilities, and common pitfalls"},
	"decision-memo": {"Decision memo", "Decision memo",
		"the decision to be made, options considered with trade-offs, the recommendation with rationale, and the proposed next step"},
	"status-report": {"Status report", "Status report",
		"overall status (on-track/at-risk), what shipped this period, what's next, blockers/risks, and metrics if relevant"},
	"custom": {"Custom document", "Document",
		"a clear, well-structured document appropriate to the requested title"},
}

func buildDeliverablePrompt(p projects.Project, docType, title string, spec deliverableSpec) string {
	return "[DELIVERABLE] Write a **" + spec.Label + "** for project \"" + p.Name + "\" (path: " + p.Path + ").\n\n" +
		"1) Read the project's docs/research/context (README, *.md, reports, key files) to ground the document in real content.\n" +
		"2) Write a thorough, well-structured Markdown document titled \"" + title + "\". A strong " + spec.Label +
		" includes: " + spec.Guidance + ". Use clear headings and concise prose; cite specifics from the project where possible.\n" +
		"3) Save it to a file, then publish it:\n" +
		"     deliverable-save " + p.ID + " " + docType + " \"" + title + "\" <file.md>\n" +
		"It will appear on the Deliverables page. Report when done."
}
