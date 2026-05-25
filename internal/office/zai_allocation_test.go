package office

import "testing"

// countByModel produces a model→count tally from an allocation map.
func countByModel(allocated map[string]string) map[string]int {
	out := make(map[string]int, len(allocated))
	for _, m := range allocated {
		out[m]++
	}
	return out
}

func TestAllocateZAIModels_SingleAgentHonoursRequest(t *testing.T) {
	got := AllocateZAIModels(map[string]string{"a": "glm-5.1"}, []string{"a"})
	if got["a"] != "glm-5.1" {
		t.Errorf("a got %q want glm-5.1", got["a"])
	}
}

func TestAllocateZAIModels_EmptyRequestDefaultsToTopTier(t *testing.T) {
	got := AllocateZAIModels(map[string]string{}, []string{"a"})
	if got["a"] != zaiDefaultModel {
		t.Errorf("default got %q want %q", got["a"], zaiDefaultModel)
	}
}

func TestAllocateZAIModels_FillsTopTierBeforeCascading(t *testing.T) {
	// glm-5.1 limit is 2; three agents all requesting glm-5.1 should
	// produce 2× glm-5.1 + 1× the next tier (glm-5).
	got := AllocateZAIModels(
		map[string]string{"a": "glm-5.1", "b": "glm-5.1", "c": "glm-5.1"},
		[]string{"a", "b", "c"},
	)
	tally := countByModel(got)
	if tally["glm-5.1"] != 2 {
		t.Errorf("glm-5.1 count=%d want 2 (rate-limit floor)", tally["glm-5.1"])
	}
	if tally["glm-5"] != 1 {
		t.Errorf("glm-5 cascade count=%d want 1, full alloc=%+v", tally["glm-5"], got)
	}
}

func TestAllocateZAIModels_CascadeStartsFromRequestedTier(t *testing.T) {
	// Five agents requesting glm-4.7. Limit = 3; remainder cascades
	// down to glm-4.7-flash (limit 5). All 5 should land in either
	// glm-4.7 or glm-4.7-flash, not in higher tiers.
	got := AllocateZAIModels(
		map[string]string{"a": "glm-4.7", "b": "glm-4.7", "c": "glm-4.7", "d": "glm-4.7", "e": "glm-4.7"},
		[]string{"a", "b", "c", "d", "e"},
	)
	tally := countByModel(got)
	for _, higherTier := range []string{"glm-5.1", "glm-5"} {
		if tally[higherTier] != 0 {
			t.Errorf("higher tier %s used unexpectedly: alloc=%+v", higherTier, got)
		}
	}
	if tally["glm-4.7"] != 3 {
		t.Errorf("glm-4.7 count=%d want 3", tally["glm-4.7"])
	}
	if tally["glm-4.7-flash"] != 2 {
		t.Errorf("glm-4.7-flash count=%d want 2", tally["glm-4.7-flash"])
	}
}

func TestAllocateZAIModels_UnknownModelStartsFromTop(t *testing.T) {
	// "mystery-model" isn't in the downgrade path; the allocator
	// should silently start from the top tier.
	got := AllocateZAIModels(
		map[string]string{"a": "mystery-model"},
		[]string{"a"},
	)
	if got["a"] != "glm-5.1" {
		t.Errorf("unknown model got %q want glm-5.1 (top of chain)", got["a"])
	}
}

func TestAllocateZAIModels_OverflowFallback(t *testing.T) {
	// Saturate every tier: total slot budget across all tiers is
	// 2 + 2 + 3 + 5 + 5 = 17. The 18th agent triggers the
	// zaiOverflowFallback (glm-4.7).
	ids := make([]string, 18)
	req := make(map[string]string, 18)
	for i := range ids {
		ids[i] = "agent-" + string(rune('A'+i))
		req[ids[i]] = "glm-5.1"
	}
	got := AllocateZAIModels(req, ids)
	if len(got) != 18 {
		t.Fatalf("len=%d want 18", len(got))
	}
	tally := countByModel(got)
	// Verify the budget was exhausted before overflow.
	for _, model := range ZAIDowngradePath {
		expected := ZAIRateLimits[model]
		// glm-4.7 gets one extra from the overflow path.
		if model == zaiOverflowFallback {
			expected++
		}
		if tally[model] != expected {
			t.Errorf("model %s count=%d want %d", model, tally[model], expected)
		}
	}
}

func TestAllocateZAIModels_DeterministicForGivenMemberOrder(t *testing.T) {
	// Same input must yield same output (no map-iteration nondeterminism).
	req := map[string]string{"a": "glm-5.1", "b": "glm-5.1", "c": "glm-5"}
	ids := []string{"a", "b", "c"}
	first := AllocateZAIModels(req, ids)
	for i := 0; i < 10; i++ {
		next := AllocateZAIModels(req, ids)
		for k, v := range first {
			if next[k] != v {
				t.Fatalf("nondeterministic alloc on iter %d: %s went from %q to %q", i, k, v, next[k])
			}
		}
	}
}

func TestAllocateZAIModels_EmptyMemberListReturnsEmpty(t *testing.T) {
	got := AllocateZAIModels(map[string]string{}, []string{})
	if len(got) != 0 {
		t.Errorf("expected empty allocation, got %+v", got)
	}
}
