package office

import (
	"strings"
	"testing"

	"github.com/kshaham/jarvis/internal/library"
)

// TestRenderAntiPatternHintsAsMarkdown_TitleFallbackAndAvoidCap drives the
// empty-title → slug fallback and the WhatToAvoid >2 cap (only first 2 are
// rendered).
func TestRenderAntiPatternHintsAsMarkdown_TitleFallbackAndAvoidCap(t *testing.T) {
	hits := []library.AntiPatternHit{
		{
			Slug:        "race-on-shared-map",
			Title:       "", // empty → falls back to Slug
			Path:        "library/anti-patterns/race-on-shared-map.md",
			WhyItFails:  "concurrent writes to a map panic",
			WhatToAvoid: []string{"avoid one", "avoid two", "avoid three", "avoid four"},
		},
	}
	out := renderAntiPatternHintsAsMarkdown(hits)
	if !strings.Contains(out, "race-on-shared-map") {
		t.Errorf("expected slug used as title fallback, got:\n%s", out)
	}
	if !strings.Contains(out, "avoid one") || !strings.Contains(out, "avoid two") {
		t.Errorf("expected first two avoid items, got:\n%s", out)
	}
	if strings.Contains(out, "avoid three") {
		t.Errorf("expected avoid items capped at 2, got:\n%s", out)
	}
}

// TestRenderAntiPatternHintsAsMarkdown_Empty returns "" for no hits.
func TestRenderAntiPatternHintsAsMarkdown_Empty(t *testing.T) {
	if got := renderAntiPatternHintsAsMarkdown(nil); got != "" {
		t.Errorf("expected empty for no hits, got %q", got)
	}
}

// TestBuildLibraryHintsSection_NilConfig covers the config==nil guard.
func TestBuildLibraryHintsSection_NilConfigGuard(t *testing.T) {
	if got := BuildLibraryHintsSection(nil, "/wd"); got != "" {
		t.Errorf("expected empty for nil config, got %q", got)
	}
	if got := BuildAntiPatternHintsSection(nil, "/wd"); got != "" {
		t.Errorf("expected empty for nil config, got %q", got)
	}
}
