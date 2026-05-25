package office

import (
	"strings"
	"testing"
)

func TestPageIndexMcpConfig_HappyPath(t *testing.T) {
	got := PageIndexMcpConfig("/tmp/library")
	if got == nil {
		t.Fatal("expected non-nil config")
	}
	// Shape must match the codegraph/cgc pattern that spawn_agent.go
	// already merges into mcpServers.
	if got["type"] != "stdio" {
		t.Errorf("type=%v want stdio", got["type"])
	}
	if got["command"] != "npx" {
		t.Errorf("command=%v want npx", got["command"])
	}
	args, ok := got["args"].([]string)
	if !ok || len(args) < 2 || args[0] != "-y" {
		t.Errorf("args=%v want [-y, @pageindex/mcp@...]", got["args"])
	}
	if !strings.HasPrefix(args[1], "@pageindex/mcp@") {
		t.Errorf("args[1]=%q want @pageindex/mcp@<version>", args[1])
	}
	env, ok := got["env"].(map[string]string)
	if !ok {
		t.Fatalf("env=%v want map[string]string", got["env"])
	}
	if env["PAGEINDEX_LIBRARY_DIR"] != "/tmp/library" {
		t.Errorf("PAGEINDEX_LIBRARY_DIR=%q want /tmp/library",
			env["PAGEINDEX_LIBRARY_DIR"])
	}
}

func TestPageIndexMcpConfig_EmptyDirReturnsNil(t *testing.T) {
	// Defensive: without a library dir there's nothing for the MCP
	// to index. Returning nil lets the spawn handler skip the
	// mcpServers entry instead of wiring up a broken server.
	if PageIndexMcpConfig("") != nil {
		t.Error("expected nil config for empty libraryDir")
	}
}

func TestLibraryDir_NonEmptyOnSupportedHost(t *testing.T) {
	// On any host with a HOME directory, LibraryDir should return a
	// non-empty path. CI hosts always have HOME; failing here means
	// library.Dir() got broken or HOME is unset.
	if got := LibraryDir(); got == "" {
		t.Error("LibraryDir() returned empty — HOME unset?")
	}
}

func TestPageIndexAvailable_GracefulWithoutNpx(t *testing.T) {
	// We can't easily test the "npx present" branch in CI without
	// network, but the "npx absent" branch is deterministic:
	// PageIndexAvailable must return false (and not panic) when the
	// NpxAvailable() gate fails.
	//
	// This test exercises the cached-once path. After reset, the
	// outcome depends on the test host. We don't assert true/false
	// — just that it doesn't crash and is idempotent.
	resetPageIndexCacheForTest()
	first := PageIndexAvailable()
	second := PageIndexAvailable()
	if first != second {
		t.Errorf("cache not stable: first=%v second=%v", first, second)
	}
}
