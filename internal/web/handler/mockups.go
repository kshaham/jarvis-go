package handler

import (
	"encoding/base64"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kshaham/jarvis/internal/mockups"
	"github.com/kshaham/jarvis/internal/projects"
	"github.com/kshaham/jarvis/internal/web/request"
)

// mockupImage is one stored mockup's slot + URL.
type mockupImage struct {
	Index int    `json:"index"`
	URL   string `json:"url"`
}

// mockupProjectTile is a saved project joined with its mockup state.
type mockupProjectTile struct {
	projects.Project
	HasUI      bool           `json:"hasUi"`
	TeamActive bool           `json:"teamActive"` // a team is spawned on this project's workdir
	Status     mockups.Status `json:"status"`
	Mockups    []mockupImage  `json:"mockups"`
}

func mockupImageURL(projectID, filename string) string {
	return "/api/mockups/image/" + projectID + "/" + filename
}

// MockupsGetResults returns every saved project with UI detection, the
// currently-spawned-team match, generation status, and stored mockup URLs.
// GET /api/mockups/results
func (ctx *Context) MockupsGetResults(w http.ResponseWriter, _ *http.Request) {
	statuses := ctx.Mockups.Snapshot()
	_, workdir := ctx.OfficeManager.GetSpawnContext()
	teamUp := ctx.OfficeManager.IsTeamSpawned()
	list := ctx.Projects.List()
	tiles := make([]mockupProjectTile, 0, len(list))
	for _, p := range list {
		st := statuses[p.ID]
		if st.Status == "" {
			st.Status = mockups.StatusIdle
		}
		files, _ := mockups.List(p.ID)
		imgs := make([]mockupImage, 0, len(files))
		for _, f := range files {
			imgs = append(imgs, mockupImage{Index: int(f[0] - '0'), URL: mockupImageURL(p.ID, f)})
		}
		tiles = append(tiles, mockupProjectTile{
			Project:    p,
			HasUI:      p.Path != "" && mockups.HasUI(p.Path),
			TeamActive: teamUp && p.Path != "" && workdir == p.Path,
			Status:     st,
			Mockups:    imgs,
		})
	}
	ok(w, map[string]interface{}{"projects": tiles})
}

type mockupsGenerateRequest struct {
	ProjectID string `json:"projectId"`
}

// MockupsGenerate dispatches a 6-mockup generation run to the spawned team.
// POST /api/mockups/generate {projectId}
func (ctx *Context) MockupsGenerate(w http.ResponseWriter, r *http.Request) {
	var req mockupsGenerateRequest
	if !request.DecodeAndValidateMax(w, r, &req, 1<<12) {
		return
	}
	proj, found := findSavedProject(ctx.Projects.List(), req.ProjectID)
	if !found {
		request.WriteError(w, http.StatusNotFound, "project not found")
		return
	}
	if proj.Path == "" || !mockups.HasUI(proj.Path) {
		request.WriteError(w, http.StatusBadRequest, "project has no detectable UI")
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
	if err := ctx.Mockups.StartRun(proj.ID); err != nil {
		request.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	lead := roster[0]
	if !sessions.WriteInkPrompt(lead.SessionID(), buildMockupPrompt(proj)) {
		ctx.Mockups.Fail(proj.ID, "team lead session is not accepting prompts")
		request.WriteError(w, http.StatusConflict, "team lead is not accepting prompts")
		return
	}
	request.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"success": true, "status": "running"})
}

type mockupUploadRequest struct {
	ProjectID   string `json:"projectId"`
	Index       int    `json:"index"`
	ImageBase64 string `json:"imageBase64"`
}

// MockupsUpload receives one generated mockup PNG from a team agent.
// POST /api/office/agent/mockup-upload (agent-token auth).
func (ctx *Context) MockupsUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024*1024)
	var req mockupUploadRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if req.ProjectID == "" || req.ImageBase64 == "" {
		request.WriteError(w, http.StatusBadRequest, "projectId and imageBase64 are required")
		return
	}
	if _, found := findSavedProject(ctx.Projects.List(), req.ProjectID); !found {
		request.WriteError(w, http.StatusNotFound, "project not found")
		return
	}
	raw, err := base64.StdEncoding.DecodeString(req.ImageBase64)
	if err != nil {
		request.WriteError(w, http.StatusBadRequest, "imageBase64 is not valid base64")
		return
	}
	filename, err := ctx.Mockups.RecordUpload(req.ProjectID, req.Index, raw)
	if err != nil {
		request.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	ok(w, map[string]interface{}{"filename": filename, "url": mockupImageURL(req.ProjectID, filename)})
}

// MockupsServe serves a stored mockup PNG.
// GET /api/mockups/image/{projectId}/{filename}
func (ctx *Context) MockupsServe(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectId")
	filename := chi.URLParam(r, "filename")
	if !mockups.ValidateProjectID(projectID) || !mockups.ValidateFilename(filename) {
		request.WriteError(w, http.StatusBadRequest, "invalid mockup path")
		return
	}
	data, err := mockups.Load(projectID, filename)
	if err != nil {
		request.WriteError(w, http.StatusNotFound, "mockup not found")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func findSavedProject(list []projects.Project, id string) (projects.Project, bool) {
	for _, p := range list {
		if p.ID == id {
			return p, true
		}
	}
	return projects.Project{}, false
}

// buildMockupPrompt is the orchestration instruction written to the team
// lead's session. Kept under the 4KB inject cap.
func buildMockupPrompt(p projects.Project) string {
	return fmt.Sprintf(`[MOCKUPS] Generate 6 DISTINCT UI mockups for project %q (path: %s). Do this now, end to end:

1) Start the frontend dev server in the project path (read package.json scripts; install deps if needed, then run the dev/start script). Find the local URL (host:port).
2) Capture the current main screen:  browser-screenshot <url> /tmp/mockbase.png
3) Produce 6 mockups ONE AT A TIME (i = 0..5). For each i, FIRST write a redesign CONCEPT that is clearly DIFFERENT from every earlier concept — vary layout, color palette, typography, spacing/density and overall aesthetic (e.g. minimal, glassy, brutalist, editorial, playful, dense-pro). Then render + upload it in one command:
     mockup-render /tmp/mockbase.png "<your concept, 1-3 sentences>" %s <i>
   (mockup-render restyles the screenshot via gpt-image-1 and uploads the result automatically.)
4) Make sure all 6 are visually unique. Report when all 6 are uploaded.`, p.Name, p.Path, p.ID)
}
