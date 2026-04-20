package handler

// R23 · Smoke tests for the new spawn-modal payload fields. Full
// end-to-end spawn exercises dependencies (SessionManager, provider
// env, template loading) that aren't practical to stand up in a unit
// test — these focus on the pure-function surface R23 added:
//   - memberOverride JSON decode for maxCostUSD / maxTurns /
//     spawnDelayMs
//   - spawnTeamRequest JSON decode for dailyCapUSD
//   - firstNonEmpty helper used when synthesizing override-only
//     members into TeamTemplateMember stubs

import (
	"encoding/json"
	"testing"
)

func TestMemberOverride_DecodesR23Fields(t *testing.T) {
	body := `{
		"id": "lead-1",
		"provider": "claude-api",
		"model": "claude-opus-4-7",
		"maxCostUSD": 1.5,
		"maxTurns": 50,
		"spawnDelayMs": 750
	}`
	var ov memberOverride
	if err := json.Unmarshal([]byte(body), &ov); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ov.MaxCostUSD != 1.5 {
		t.Errorf("MaxCostUSD: got %v, want 1.5", ov.MaxCostUSD)
	}
	if ov.MaxTurns != 50 {
		t.Errorf("MaxTurns: got %d, want 50", ov.MaxTurns)
	}
	if ov.SpawnDelayMs != 750 {
		t.Errorf("SpawnDelayMs: got %d, want 750", ov.SpawnDelayMs)
	}
}

func TestMemberOverride_OmittedR23FieldsZero(t *testing.T) {
	// Modal sends no cap fields when operator leaves them blank —
	// backend should see zero values and honor the defaults.
	body := `{"id":"worker-1","provider":"claude-subscription","model":"sonnet"}`
	var ov memberOverride
	if err := json.Unmarshal([]byte(body), &ov); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ov.MaxCostUSD != 0 {
		t.Errorf("MaxCostUSD should default to 0, got %v", ov.MaxCostUSD)
	}
	if ov.MaxTurns != 0 {
		t.Errorf("MaxTurns should default to 0, got %d", ov.MaxTurns)
	}
	if ov.SpawnDelayMs != 0 {
		t.Errorf("SpawnDelayMs should default to 0, got %d", ov.SpawnDelayMs)
	}
}

func TestSpawnTeamRequest_DecodesDailyCapUSD(t *testing.T) {
	body := `{"workingDir":"/tmp","dailyCapUSD":25.5}`
	var req spawnTeamRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.DailyCapUSD != 25.5 {
		t.Errorf("DailyCapUSD: got %v, want 25.5", req.DailyCapUSD)
	}
}

func TestSpawnTeamRequest_OmittedDailyCapIsZero(t *testing.T) {
	body := `{"workingDir":"/tmp"}`
	var req spawnTeamRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.DailyCapUSD != 0 {
		t.Errorf("DailyCapUSD should default to 0 (meaning leave global cap unchanged), got %v", req.DailyCapUSD)
	}
}

func TestFirstNonEmpty(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{"first wins", []string{"a", "b"}, "a"},
		{"skip empties", []string{"", "", "c", "d"}, "c"},
		{"all empty", []string{"", ""}, ""},
		{"no args", nil, ""},
		{"single non-empty", []string{"only"}, "only"},
	}
	for _, tc := range cases {
		got := firstNonEmpty(tc.in...)
		if got != tc.want {
			t.Errorf("%s: firstNonEmpty%v = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}
