package handler

import (
	"errors"
	"net/http"

	"github.com/kshaham/jarvis/internal/projects"
	"github.com/kshaham/jarvis/internal/quality"
	"github.com/kshaham/jarvis/internal/web/request"
)

// projectTile is one saved project joined with its latest coverage, vuln, and
// react-doctor results — the shape the Tests page renders.
type projectTile struct {
	projects.Project
	Coverage quality.CoverageResult `json:"coverage"`
	Vuln     quality.VulnResult     `json:"vuln"`
	React    quality.ReactResult    `json:"react"`
}

// TestsGetResults returns every saved project with its cached coverage + vuln
// results (live "running" status overlaid for in-flight jobs).
// GET /api/tests/results
func (ctx *Context) TestsGetResults(w http.ResponseWriter, _ *http.Request) {
	snap := ctx.Quality.Snapshot()
	list := ctx.Projects.List()
	tiles := make([]projectTile, 0, len(list))
	for _, p := range list {
		pr := snap[p.ID]
		if pr.Coverage.Status == "" {
			pr.Coverage.Status = quality.StatusIdle
		}
		if pr.Vuln.Status == "" {
			pr.Vuln.Status = quality.StatusIdle
		}
		// React-doctor only applies to React projects; mark non-React ones
		// Unsupported (cheap package.json read) so the UI hides the badge,
		// and default React projects to Idle until first run.
		if pr.React.Status == "" {
			if quality.IsReactProject(p.Path) {
				pr.React.Status = quality.StatusIdle
			} else {
				pr.React.Status = quality.StatusUnsupported
			}
		}
		tiles = append(tiles, projectTile{Project: p, Coverage: pr.Coverage, Vuln: pr.Vuln, React: pr.React})
	}
	ok(w, map[string]interface{}{"projects": tiles})
}

type testsJobRequest struct {
	ProjectID string `json:"projectId"`
}

// TestsRunCoverage starts an async coverage run for one project.
// POST /api/tests/coverage
func (ctx *Context) TestsRunCoverage(w http.ResponseWriter, r *http.Request) {
	ctx.startTestsJob(w, r, ctx.Quality.StartCoverage)
}

// TestsRunScan starts an async Trivy scan for one project.
// POST /api/tests/scan
func (ctx *Context) TestsRunScan(w http.ResponseWriter, r *http.Request) {
	ctx.startTestsJob(w, r, ctx.Quality.StartScan)
}

// TestsRunReact starts an async react-doctor scan for one project.
// POST /api/tests/react
func (ctx *Context) TestsRunReact(w http.ResponseWriter, r *http.Request) {
	ctx.startTestsJob(w, r, ctx.Quality.StartReact)
}

func (ctx *Context) startTestsJob(w http.ResponseWriter, r *http.Request, start func(string) error) {
	var req testsJobRequest
	if !request.DecodeAndValidateMax(w, r, &req, 1<<12) {
		return
	}
	if req.ProjectID == "" {
		request.WriteError(w, http.StatusBadRequest, "projectId required")
		return
	}
	if err := start(req.ProjectID); err != nil {
		if errors.Is(err, quality.ErrProjectNotFound) {
			request.WriteError(w, http.StatusNotFound, "project not found")
			return
		}
		request.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	request.WriteJSON(w, http.StatusAccepted, map[string]interface{}{
		"success": true,
		"status":  quality.StatusRunning,
	})
}
