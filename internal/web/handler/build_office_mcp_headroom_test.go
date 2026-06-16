package handler

import (
	"testing"

	"github.com/kshaham/jarvis/internal/office"
)

// Headroom MCP wiring: present when the probe reports available and the team
// hasn't opted out; absent otherwise. Seams are overridden so the branch is
// exercisable without the `headroom` binary on the host.

func withHeadroomSeams(t *testing.T, available bool) {
	prevAvail, prevCfg := headroomAvailable, headroomMcpConfig
	t.Cleanup(func() {
		headroomAvailable = prevAvail
		headroomMcpConfig = prevCfg
	})
	headroomAvailable = func() bool { return available }
	headroomMcpConfig = func() map[string]any {
		return map[string]any{"type": "stdio", "command": "headroom", "args": []string{"mcp", "serve"}}
	}
}

func newHeadroomTestCtx() *Context {
	return &Context{OfficeManager: office.NewManager(stubSessions{}, func(string, interface{}) {}, nil)}
}

func TestBuildOfficeMCPServers_HeadroomWiredWhenAvailable(t *testing.T) {
	withHeadroomSeams(t, true)
	ctx := newHeadroomTestCtx()
	servers := ctx.buildOfficeMCPServers(spawnTeamRequest{WorkingDir: "/srv/app"}, "sess-hr", "tok-hr")
	if _, ok := servers["headroom"]; !ok {
		t.Errorf("expected headroom MCP server wired when available, got %v", mcpKeysOf(servers))
	}
}

func TestBuildOfficeMCPServers_HeadroomAbsentWhenUnavailable(t *testing.T) {
	withHeadroomSeams(t, false)
	ctx := newHeadroomTestCtx()
	servers := ctx.buildOfficeMCPServers(spawnTeamRequest{WorkingDir: "/srv/app"}, "sess-hr2", "tok-hr2")
	if _, ok := servers["headroom"]; ok {
		t.Error("expected no headroom MCP server when probe reports unavailable")
	}
}

func TestBuildOfficeMCPServers_HeadroomOptOutWins(t *testing.T) {
	withHeadroomSeams(t, true) // available...
	ctx := newHeadroomTestCtx()
	disable := true
	req := spawnTeamRequest{WorkingDir: "/srv/app", DisableHeadroom: &disable} // ...but opted out
	servers := ctx.buildOfficeMCPServers(req, "sess-hr3", "tok-hr3")
	if _, ok := servers["headroom"]; ok {
		t.Error("expected headroom MCP server omitted when the team disabled it")
	}
}

func TestCodexAllowlist_IncludesHeadroom(t *testing.T) {
	if !codexMCPAllowlist["headroom"] {
		t.Error("expected headroom in the Codex MCP allowlist")
	}
	full := map[string]any{
		"headroom": map[string]any{"command": "headroom"},
		"context7": map[string]any{"command": "npx"}, // not allowlisted
	}
	lean := leanCodexMCPServers(full)
	if _, ok := lean["headroom"]; !ok {
		t.Error("expected headroom to survive the Codex allowlist filter")
	}
	if _, ok := lean["context7"]; ok {
		t.Error("expected context7 to be filtered out for Codex")
	}
}

func mcpKeysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
