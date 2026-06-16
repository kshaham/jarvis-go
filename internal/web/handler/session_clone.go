package handler

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/state"
	"github.com/kshaham/jarvis/internal/web/request"
)

// maxCloneCount caps how many clones can be spawned in one call so a
// misconfigured script can't flood the session manager.
const maxCloneCount = 20

// CloneSession duplicates a session's configuration (working dir, mode, name,
// color) into one or more fresh sessions. Does NOT copy terminal history --
// clones are parallel workers, not snapshots.
// POST /api/sessions/{id}/clone?count=N
func (ctx *Context) CloneSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	source, ok := ctx.SessionManager.GetSession(id)
	if !ok {
		request.WriteError(w, http.StatusNotFound, "session not found")
		return
	}

	count := 1
	if v := r.URL.Query().Get("count"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= maxCloneCount {
			count = n
		}
	}

	providerEnv := ctx.getProviderEnvVars()
	baseName := source.Name()
	if baseName == "" {
		baseName = "session"
	}

	clones := make([]sessionJSON, 0, count)
	errs := make([]string, 0)
	for i := 0; i < count; i++ {
		cloneName := fmt.Sprintf("%s (clone)", baseName)
		if count > 1 {
			cloneName = fmt.Sprintf("%s (clone %d)", baseName, i+1)
		}
		session, err := ctx.SessionManager.CreateSession(
			context.Background(),
			pty.SessionConfig{
				WorkingDir:  source.WorkingDir,
				Mode:        source.Mode,
				Name:        cloneName,
				ProviderEnv: providerEnv,
			},
		)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}

		go forwardSessionEvents(session, ctx)
		ctx.emit("session:created", sessionToJSON(session))

		if ctx.TelegramHandler != nil {
			ctx.TelegramHandler.NotifySessionStart(session.ID, session.Name(), session.WorkingDir)
		}
		ctx.StateStore.SaveSession(&state.SessionState{
			ID:         session.ID,
			Name:       session.Name(),
			WorkingDir: session.WorkingDir,
			Mode:       string(session.Mode),
			CreatedAt:  session.CreatedAt.Format(time.RFC3339),
			Status:     string(session.Status()),
		})

		// Lifecycle trackers mirror the CreateSession path so clones respawn
		// + ralph-track the same way as originals.
		ctx.initSessionTrackers(session.ID)

		rc := ctx.GetOrCreateRespawnController(session.ID)
		rc.SetSessionWriter(&respawnSessionWriter{session: session})

		clones = append(clones, sessionToJSON(session))
	}

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"sessions": clones,
		"errors":   errs,
	})
}
