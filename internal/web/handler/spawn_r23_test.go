package handler

// R23 · Smoke tests for the spawn-modal payload fields that survived
// R24's removal of dollar-based caps. Covers:
//   - memberOverride JSON decode for maxTurns / spawnDelayMs
//   - firstNonEmpty helper used when synthesizing override-only
//     members into TeamTemplateMember stubs
//
// R24 removed: memberOverride.MaxCostUSD + spawnTeamRequest.DailyCapUSD
// and their corresponding assertions.

import (
	"encoding/json"
	"testing"
)

func TestMemberOverride_DecodesR23Fields(t *testing.T) {
	body := `{
		"id": "lead-1",
		"provider": "claude-api",
		"model": "claude-opus-4-7",
		"maxTurns": 50,
		"spawnDelayMs": 750
	}`
	var ov memberOverride
	if err := json.Unmarshal([]byte(body), &ov); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ov.MaxTurns != 50 {
		t.Errorf("MaxTurns: got %d, want 50", ov.MaxTurns)
	}
	if ov.SpawnDelayMs != 750 {
		t.Errorf("SpawnDelayMs: got %d, want 750", ov.SpawnDelayMs)
	}
}

func TestMemberOverride_OmittedR23FieldsZero(t *testing.T) {
	body := `{"id":"worker-1","provider":"claude-subscription","model":"sonnet"}`
	var ov memberOverride
	if err := json.Unmarshal([]byte(body), &ov); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ov.MaxTurns != 0 {
		t.Errorf("MaxTurns should default to 0, got %d", ov.MaxTurns)
	}
	if ov.SpawnDelayMs != 0 {
		t.Errorf("SpawnDelayMs should default to 0, got %d", ov.SpawnDelayMs)
	}
}

// R24 regression guard · Legacy clients may still send maxCostUSD /
// dailyCapUSD in the JSON payload. With the fields removed from the
// structs, json.Unmarshal should silently ignore them (DisallowUnknown
// is not set on the decoder). Verify the request decodes cleanly.
func TestR24_LegacyCostFieldsIgnored(t *testing.T) {
	body := `{
		"id": "lead-1",
		"provider": "claude-api",
		"model": "sonnet",
		"maxCostUSD": 2.5,
		"maxTurns": 80
	}`
	var ov memberOverride
	if err := json.Unmarshal([]byte(body), &ov); err != nil {
		t.Fatalf("should ignore maxCostUSD, got err: %v", err)
	}
	if ov.MaxTurns != 80 {
		t.Errorf("MaxTurns: got %d, want 80", ov.MaxTurns)
	}
}

func TestR24_LegacyDailyCapIgnored(t *testing.T) {
	body := `{"workingDir":"/tmp","dailyCapUSD":25.5,"mission":"hi"}`
	var req spawnTeamRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("should ignore dailyCapUSD, got err: %v", err)
	}
	if req.WorkingDir != "/tmp" {
		t.Errorf("WorkingDir: got %q, want /tmp", req.WorkingDir)
	}
	if req.Mission != "hi" {
		t.Errorf("Mission: got %q, want hi", req.Mission)
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
