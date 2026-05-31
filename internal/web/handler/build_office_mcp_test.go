package handler

// Branch coverage for buildOfficeMCPServers' opt-out gates. When the spawn
// request disables codegraph + ECC, the codegraph / context7 /
// sequential-thinking / shadcn servers must be omitted regardless of whether
// the binaries are on PATH (the gate short-circuits before the availability
// probe). The unconditional jarvis + codegraphcontext servers stay.

import (
	"testing"

	"github.com/kshaham/jarvis/internal/office"
)

func boolPtrT(b bool) *bool { return &b }

func TestBuildOfficeMCPServers_OptOutOmitsGatedServers(t *testing.T) {
	ctx := &Context{
		OfficeManager: office.NewManager(stubSessions{}, func(string, interface{}) {}, nil),
	}
	req := spawnTeamRequest{
		WorkingDir:       "/srv/app",
		DisableCodegraph: boolPtrT(true),
		DisableEcc:       boolPtrT(true),
	}
	servers := ctx.buildOfficeMCPServers(req, "sess-optout", "tok")

	// Unconditional servers are always present.
	if _, ok := servers["jarvis"]; !ok {
		t.Error("jarvis MCP server should always be present")
	}
	if _, ok := servers["codegraphcontext"]; !ok {
		t.Error("codegraphcontext MCP server should always be present")
	}
	// Opt-out gated servers must be omitted.
	for _, gated := range []string{"codegraph", "context7", "sequential-thinking", "shadcn"} {
		if _, ok := servers[gated]; ok {
			t.Errorf("%q must be omitted when its opt-out flag is set", gated)
		}
	}
}
