package office

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
)

// New-project flow helpers.
//
// When the operator clicks "New project" in the spawn modal,
// jarvis validates the slug, creates /app/workspace/<slug> if it
// doesn't exist, seeds a CLAUDE.md so agents have onboarding
// context, and (on creation only) broadcasts an SSE event so the
// web UI can refresh the workdir picker without a hard reload.
//
// Extracted from handler.spawnTeamFromRequest so the orchestrator
// can run this without holding an http.ResponseWriter. The HTTP
// shell still maps PrepareNewProject errors to 400/500 responses.

// ProjectNameRe constrains the slug an operator can supply. Letters,
// digits, underscore, hyphen only — 1-64 chars. Excludes dots and
// slashes so no request can traverse out of NewProjectRoot.
var ProjectNameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// NewProjectRoot is the fixed parent directory for freshly-created
// projects. Already in the workingDirAllowlist for production
// deploys (docker-compose.yml: /app/workspace is bind-mounted into
// the jarvis container). Exposed so tests can override via a
// runtime-injected helper or stub.
const NewProjectRoot = "/app/workspace"

// ErrInvalidProjectName is returned by PrepareNewProject when the
// slug fails ProjectNameRe. The handler maps it to 400.
var ErrInvalidProjectName = errors.New("project name must match [a-zA-Z0-9_-]{1,64} (no dots or slashes)")

// SSEBroadcaster is the minimal SSE surface PrepareNewProject needs.
// Implemented by the real sse.Broker; tests can pass a no-op or a
// recording stub.
type SSEBroadcaster func(event string, data interface{})

// PrepareNewProject validates a new-project slug, creates the
// target directory if missing, seeds a CLAUDE.md (best-effort),
// and broadcasts an SSE event on first creation.
//
// Returns the resolved target directory path on success — the
// caller assigns it to req.WorkingDir so the rest of the spawn
// flow uses the freshly-created project root.
//
// Behaviour split:
//   - Slug validation errors are returned as ErrInvalidProjectName
//     (the handler returns 400).
//   - mkdir failures are returned wrapped with the path so the
//     handler can surface them (500).
//   - CLAUDE.md seed failures are logged at warn level and
//     swallowed — a missing seed file isn't worth failing the
//     spawn over.
//   - SSE broadcast only fires when the directory was actually
//     created this call (existing-dir reuse is silent).
//
// Pass broadcast=nil from tests / contexts without an SSE broker.
func PrepareNewProject(projectName, mission string, broadcast SSEBroadcaster) (string, error) {
	if !ProjectNameRe.MatchString(projectName) {
		return "", ErrInvalidProjectName
	}
	target := filepath.Join(NewProjectRoot, projectName)
	created := false
	if _, statErr := os.Stat(target); os.IsNotExist(statErr) {
		if mkErr := os.MkdirAll(target, 0o755); mkErr != nil {
			slog.Error("new project: mkdir failed", "path", target, "err", mkErr)
			return "", fmt.Errorf("failed to create project directory: %w", mkErr)
		}
		created = true
	}
	if seedErr := SeedNewProjectCLAUDEMd(target, projectName, mission); seedErr != nil {
		// Non-fatal — the team can still spawn, but log loudly so
		// the operator can fix perms / disk issues before the next
		// one.
		slog.Warn("new project: CLAUDE.md seed failed", "path", target, "err", seedErr)
	}
	if created && broadcast != nil {
		broadcast("office:projectCreated", map[string]interface{}{
			"name": projectName,
			"path": target,
		})
		slog.Info("new project created", "name", projectName, "path", target)
	}
	return target, nil
}
