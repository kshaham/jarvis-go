package office

import "testing"

// Sanity check on the per-host PageIndexAvailable() probe: in a CI
// environment with npx present, hooksModeMcpServers should return a
// map containing exactly "pageindex". When npx is absent it returns
// nil. The test asserts only the shape (key set + entry shape),
// not the underlying network probe — the probe itself is exercised
// by pageindex_test.go.

func TestHooksModeMcpServers_ShapeMatchesSDKModePattern(t *testing.T) {
	got := hooksModeMcpServers()
	if got == nil {
		// PageIndex not available on this host (no npx, or the
		// probe failed). Nothing to assert; the integration test
		// in TestBuildSettings_McpServersOmittedWhenNil covers
		// the "absent" branch.
		t.Skip("PageIndex MCP not available on this host")
	}
	pi, ok := got["pageindex"]
	if !ok {
		t.Fatalf("expected 'pageindex' entry, got keys: %v", keysOf(got))
	}
	m, ok := pi.(map[string]any)
	if !ok {
		t.Fatalf("pageindex entry wrong shape: %T", pi)
	}
	// Stdio MCP shape — same as the SDK-mode wiring in
	// internal/web/handler/spawn_agent.go so hooks-mode + SDK-mode
	// agents see an identical tool surface.
	if m["type"] != "stdio" {
		t.Errorf("type=%v want stdio", m["type"])
	}
	if m["command"] != "npx" {
		t.Errorf("command=%v want npx", m["command"])
	}
	env, ok := m["env"].(map[string]string)
	if !ok || env["PAGEINDEX_LIBRARY_DIR"] == "" {
		t.Errorf("env missing PAGEINDEX_LIBRARY_DIR: %v", m["env"])
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
