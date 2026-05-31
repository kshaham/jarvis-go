package office

import (
	"os"
	"testing"
)

// TestPageIndexAvailable_NpxAbsent drives the npx-not-on-PATH log branch of
// PageIndexAvailable: with PATH pointing at an empty dir, NpxAvailable() is
// false so the probe is never attempted and PageIndexAvailable reports false.
// As a side effect this also exercises hooksModeMcpServers' early return.
func TestPageIndexAvailable_NpxAbsent(t *testing.T) {
	pageIndexTestMu.Lock()
	defer pageIndexTestMu.Unlock()

	// Point PATH at an empty temp dir so exec.LookPath("npx") fails.
	empty := t.TempDir()
	t.Setenv("PATH", empty)
	resetNpxCacheForTest()
	resetPageIndexCacheForTest()
	t.Cleanup(func() {
		resetNpxCacheForTest()
		resetPageIndexCacheForTest()
	})

	if NpxAvailable() {
		t.Skip("npx unexpectedly discoverable despite empty PATH; environment-specific")
	}
	if PageIndexAvailable() {
		t.Fatal("expected PageIndexAvailable false when npx is absent")
	}
	// With PageIndex unavailable, the hooks-mode MCP map must be nil.
	if got := hooksModeMcpServers(); got != nil {
		t.Fatalf("expected nil mcp map when PageIndex unavailable, got %v", got)
	}
	_ = os.Getenv("PATH")
}
