package office

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kshaham/jarvis/internal/testutil/stubbin"
)

// installHeadroomStub puts a fake `headroom` on PATH that succeeds on
// `--version`, and resets the availability cache so the probe re-runs.
func installHeadroomStub(t *testing.T) {
	t.Helper()
	stub := stubbin.Install(t, "headroom", "#!/bin/sh\necho 'headroom 0.5.0'\nexit 0\n")
	t.Setenv("PATH", filepath.Dir(stub))
	resetHeadroomCacheForTest()
	t.Cleanup(resetHeadroomCacheForTest)
}

func TestHeadroomAvailable_StubPresent(t *testing.T) {
	installHeadroomStub(t)
	if !HeadroomAvailable() {
		t.Error("expected HeadroomAvailable() true when a working `headroom` stub is on PATH")
	}
}

func TestHeadroomAvailable_Absent(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // empty dir => LookPath fails
	resetHeadroomCacheForTest()
	t.Cleanup(resetHeadroomCacheForTest)
	if HeadroomAvailable() {
		t.Error("expected HeadroomAvailable() false when `headroom` is not on PATH")
	}
}

func TestHeadroomAvailable_ProbeFailure(t *testing.T) {
	stub := stubbin.Install(t, "headroom", "#!/bin/sh\nexit 3\n") // --version fails
	t.Setenv("PATH", filepath.Dir(stub))
	resetHeadroomCacheForTest()
	t.Cleanup(resetHeadroomCacheForTest)
	if HeadroomAvailable() {
		t.Error("expected HeadroomAvailable() false when the version probe exits non-zero")
	}
}

// TestHeadroomAvailable_RetriesAfterTransientFailure verifies a negative probe
// is NOT cached permanently: if `headroom` was absent on the first call but
// appears later (container startup, mid-process install), HeadroomAvailable()
// detects it on a subsequent call WITHOUT a manual cache reset.
func TestHeadroomAvailable_RetriesAfterTransientFailure(t *testing.T) {
	// First call: empty PATH -> not found -> false, and the negative must NOT cache.
	emptyDir := t.TempDir()
	t.Setenv("PATH", emptyDir)
	resetHeadroomCacheForTest()
	t.Cleanup(resetHeadroomCacheForTest)
	if HeadroomAvailable() {
		t.Fatal("precondition: expected false when headroom is absent")
	}

	// Now headroom appears on PATH. No resetHeadroomCacheForTest() call: the fix
	// means the prior false was never cached, so the probe re-runs and succeeds.
	stub := stubbin.Install(t, "headroom", "#!/bin/sh\necho 'headroom 0.5.0'\nexit 0\n")
	t.Setenv("PATH", filepath.Dir(stub))
	if !HeadroomAvailable() {
		t.Error("expected HeadroomAvailable() to self-heal once `headroom` appears on PATH")
	}
}

func TestHeadroomMcpConfig_Shape(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	installHeadroomStub(t)
	cfg := HeadroomMcpConfig()
	if cfg == nil {
		t.Fatal("expected non-nil config when headroom is available")
	}
	if cfg["type"] != "stdio" {
		t.Errorf("type=%v want stdio", cfg["type"])
	}
	if cfg["command"] != "headroom" {
		t.Errorf("command=%v want headroom", cfg["command"])
	}
	args, ok := cfg["args"].([]string)
	if !ok || len(args) != 2 || args[0] != "mcp" || args[1] != "serve" {
		t.Errorf("args=%v want [mcp serve]", cfg["args"])
	}
	env, ok := cfg["env"].(map[string]string)
	if !ok || env["HEADROOM_HOME"] == "" {
		t.Errorf("env=%v want HEADROOM_HOME set", cfg["env"])
	}
}

func TestHeadroomMcpConfig_UnavailableNil(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	resetHeadroomCacheForTest()
	t.Cleanup(resetHeadroomCacheForTest)
	if cfg := HeadroomMcpConfig(); cfg != nil {
		t.Errorf("expected nil config when headroom unavailable, got %v", cfg)
	}
}

func TestHeadroomHome_CreatesDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := HeadroomHome()
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Errorf("expected HeadroomHome() to create a directory, got dir=%q err=%v", dir, err)
	}
}

func TestHooksModeMcpServers_IncludesHeadroom(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	installHeadroomStub(t) // headroom available; pageindex (npx) absent in this PATH
	servers := hooksModeMcpServers()
	if servers == nil {
		t.Fatal("expected non-nil hooks-mode MCP map when headroom is available")
	}
	if _, ok := servers["headroom"]; !ok {
		t.Errorf("expected hooks-mode servers to include headroom, got %v", servers)
	}
}
