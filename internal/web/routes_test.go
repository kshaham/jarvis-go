package web

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/kshaham/jarvis/internal/web/handler"
)

// TestAgentMutationRoutesRegistered locks in the P1 fix: every endpoint the cm
// CLI and jarvis-mcp POST to under /api/office/agent must actually be
// registered there (previously they lived only under /claude/hooks, so they
// 404'd). Registering routes only binds method values, so a zero-value Context
// is sufficient -- no server/deps needed.
func TestAgentMutationRoutesRegistered(t *testing.T) {
	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		r.Route("/office/agent", func(r chi.Router) {
			registerAgentMutationRoutes(r, &handler.Context{})
		})
	})

	registered := map[string]bool{}
	walkErr := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		registered[method+" "+route] = true
		return nil
	})
	if walkErr != nil {
		t.Fatalf("chi.Walk: %v", walkErr)
	}

	// The exact paths cm/main.go and jarvis-mcp/tools.go POST/DELETE to.
	want := []struct{ method, path string }{
		{"POST", "/api/office/agent/task-start"},
		{"POST", "/api/office/agent/task-complete"},
		{"POST", "/api/office/agent/task-ack"},
		{"POST", "/api/office/agent/task-progress"},
		{"POST", "/api/office/agent/task/self-add"},
		{"POST", "/api/office/agent/task/triage"},
		{"POST", "/api/office/agent/task/handoff"},
		{"POST", "/api/office/agent/task/handoff/accept"},
		{"POST", "/api/office/agent/task/handoff/reject"},
		{"POST", "/api/office/agent/task/handoff/cancel"},
		{"POST", "/api/office/agent/task/screenshot"},
		{"POST", "/api/office/agent/task/labels"},
		{"POST", "/api/office/agent/task/criteria"},
		{"POST", "/api/office/agent/task/phase"},
		{"POST", "/api/office/agent/brainstorm/response"},
		{"POST", "/api/office/agent/leader-tasks/response"},
		{"POST", "/api/office/agent/reflection/response"},
		{"POST", "/api/office/agent/curiosity/discovery"},
		{"POST", "/api/office/agent/file-lock"},
		{"POST", "/api/office/agent/skill-create"},
		{"DELETE", "/api/office/agent/skills/{name}"},
		{"POST", "/api/office/agent/memory"},
		{"POST", "/api/office/agent/memory/append"},
		{"POST", "/api/office/agent/memory/section"},
		{"POST", "/api/office/agent/dev-instance"},
		{"DELETE", "/api/office/agent/dev-instance"},
		{"POST", "/api/office/agent/dev-credentials"},
		{"DELETE", "/api/office/agent/dev-credentials"},
	}
	for _, w := range want {
		if !registered[w.method+" "+w.path] {
			t.Errorf("expected route %s %s to be registered under /office/agent (cm/jarvis-mcp call it)", w.method, w.path)
		}
	}
}

// TestAgentMutationRoutesNoDuplicatePanic guards against the chi panic that
// fires if registerAgentMutationRoutes registers the same pattern twice within
// one sub-router (it must not). A panic here fails the test.
func TestAgentMutationRoutesNoDuplicatePanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("registerAgentMutationRoutes panicked (duplicate route?): %v", r)
		}
	}()
	r := chi.NewRouter()
	registerAgentMutationRoutes(r, &handler.Context{})
}
