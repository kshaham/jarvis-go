package handler

// Round-2 branch fills for buildOfficeMCPServers' INCLUDED arms: when the
// availability probes report the binaries present (and the operator hasn't
// opted out), the codegraph / context7 / sequential-thinking / shadcn /
// pageindex servers must be wired. The probes are indirected through package
// function variables (seams) so these branches are exercisable without the
// host toolchain. We override them for the duration of the test and restore.

import (
	"testing"

	"github.com/kshaham/jarvis/internal/office"
)

func TestBuildOfficeMCPServers_AllAvailableWiresEverything(t *testing.T) {
	// Save + restore the availability seams.
	prevCG, prevNpx, prevPI, prevPICfg := codegraphBinaryAvailable, npxAvailable, pageIndexAvailable, pageIndexMcpConfig
	t.Cleanup(func() {
		codegraphBinaryAvailable = prevCG
		npxAvailable = prevNpx
		pageIndexAvailable = prevPI
		pageIndexMcpConfig = prevPICfg
	})
	codegraphBinaryAvailable = func() bool { return true }
	npxAvailable = func() bool { return true }
	pageIndexAvailable = func() bool { return true }
	pageIndexMcpConfig = func(string) map[string]any {
		return map[string]any{"type": "stdio", "command": "npx", "args": []string{"-y", "@pageindex/mcp"}}
	}

	ctx := &Context{
		OfficeManager: office.NewManager(stubSessions{}, func(string, interface{}) {}, nil),
	}
	req := spawnTeamRequest{WorkingDir: "/srv/app"} // no opt-outs
	servers := ctx.buildOfficeMCPServers(req, "sess-all", "tok-all")

	for _, name := range []string{"jarvis", "codegraphcontext", "codegraph", "context7", "sequential-thinking", "shadcn", "pageindex"} {
		if _, ok := servers[name]; !ok {
			t.Errorf("expected %q MCP server to be wired when all probes report available", name)
		}
	}

	// The codegraph entry must point at the team's working dir.
	cg, _ := servers["codegraph"].(map[string]any)
	args, _ := cg["args"].([]string)
	foundWorkdir := false
	for _, a := range args {
		if a == "/srv/app" {
			foundWorkdir = true
		}
	}
	if !foundWorkdir {
		t.Errorf("codegraph args should include the working dir, got %v", args)
	}

	// jarvis env must carry the agent token we passed.
	jv, _ := servers["jarvis"].(map[string]any)
	env, _ := jv["env"].(map[string]string)
	if env["JARVIS_AGENT_TOKEN"] != "tok-all" {
		t.Errorf("jarvis env JARVIS_AGENT_TOKEN=%q, want tok-all", env["JARVIS_AGENT_TOKEN"])
	}
}

// TestBuildOfficeMCPServers_PageIndexNilConfigSkipped covers the inner branch:
// PageIndexAvailable()==true but PageIndexMcpConfig returns nil -> no pageindex
// entry is added.
func TestBuildOfficeMCPServers_PageIndexNilConfigSkipped(t *testing.T) {
	prevPI, prevPICfg := pageIndexAvailable, pageIndexMcpConfig
	prevCG, prevNpx := codegraphBinaryAvailable, npxAvailable
	t.Cleanup(func() {
		pageIndexAvailable = prevPI
		pageIndexMcpConfig = prevPICfg
		codegraphBinaryAvailable = prevCG
		npxAvailable = prevNpx
	})
	codegraphBinaryAvailable = func() bool { return false }
	npxAvailable = func() bool { return false }
	pageIndexAvailable = func() bool { return true }
	pageIndexMcpConfig = func(string) map[string]any { return nil } // nil config -> skip

	ctx := &Context{
		OfficeManager: office.NewManager(stubSessions{}, func(string, interface{}) {}, nil),
	}
	servers := ctx.buildOfficeMCPServers(spawnTeamRequest{WorkingDir: "/srv/app"}, "sess-pi", "")
	if _, ok := servers["pageindex"]; ok {
		t.Error("pageindex must be skipped when PageIndexMcpConfig returns nil")
	}
}
