package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/kshaham/jarvis/internal/web/request"
)

// ListProjects returns the operator's saved projects, most-recently-used
// first. GET /api/projects
func (ctx *Context) ListProjects(w http.ResponseWriter, r *http.Request) {
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"projects": ctx.Projects.List(),
	})
}

// DeleteProject removes one saved project by id. DELETE /api/projects/{id}
func (ctx *Context) DeleteProject(w http.ResponseWriter, r *http.Request) {
	ctx.deleteProjectWithID(w, r, chi.URLParam(r, "id"))
}

func (ctx *Context) deleteProjectWithID(w http.ResponseWriter, _ *http.Request, id string) {
	if id == "" {
		request.WriteError(w, http.StatusBadRequest, "project id required")
		return
	}
	ctx.Projects.Delete(id)
	request.WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}
