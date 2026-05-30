package office

import (
	"errors"
	"testing"
)

func TestValidateSnapshotForRestore_NilSnapshot(t *testing.T) {
	if err := ValidateSnapshotForRestore(nil); !errors.Is(err, ErrSnapshotEmpty) {
		t.Errorf("nil snap: got %v want ErrSnapshotEmpty", err)
	}
}

func TestValidateSnapshotForRestore_EmptyMembers(t *testing.T) {
	snap := &TeamSnapshot{Members: nil, WorkingDir: "/x"}
	if err := ValidateSnapshotForRestore(snap); !errors.Is(err, ErrSnapshotEmpty) {
		t.Errorf("empty members: got %v want ErrSnapshotEmpty", err)
	}
}

func TestValidateSnapshotForRestore_MissingWorkingDir(t *testing.T) {
	snap := &TeamSnapshot{
		Members:    []SnapshotMemberEntry{{ConfigID: "a"}},
		WorkingDir: "",
	}
	if err := ValidateSnapshotForRestore(snap); !errors.Is(err, ErrSnapshotMissingWorkingDir) {
		t.Errorf("missing workdir: got %v want ErrSnapshotMissingWorkingDir", err)
	}
}

func TestValidateSnapshotForRestore_HappyPath(t *testing.T) {
	snap := &TeamSnapshot{
		Members:    []SnapshotMemberEntry{{ConfigID: "a"}},
		WorkingDir: "/work",
	}
	if err := ValidateSnapshotForRestore(snap); err != nil {
		t.Errorf("happy path: got %v", err)
	}
}

func TestParseForkQuery_EmptyMeansFalse(t *testing.T) {
	got, err := ParseForkQuery("")
	if err != nil {
		t.Errorf("empty: err=%v", err)
	}
	if got {
		t.Error("empty should yield false")
	}
}

func TestParseForkQuery_TruthyValues(t *testing.T) {
	for _, raw := range []string{"1", "t", "T", "true", "TRUE", "True"} {
		got, err := ParseForkQuery(raw)
		if err != nil {
			t.Errorf("%q: err=%v", raw, err)
			continue
		}
		if !got {
			t.Errorf("%q yielded false", raw)
		}
	}
}

func TestParseForkQuery_FalsyValues(t *testing.T) {
	for _, raw := range []string{"0", "f", "F", "false", "FALSE", "False"} {
		got, err := ParseForkQuery(raw)
		if err != nil {
			t.Errorf("%q: err=%v", raw, err)
			continue
		}
		if got {
			t.Errorf("%q yielded true", raw)
		}
	}
}

func TestParseForkQuery_InvalidReturnsSentinelError(t *testing.T) {
	for _, raw := range []string{"ture", "yes", "no", "2", "abc"} {
		_, err := ParseForkQuery(raw)
		if !errors.Is(err, ErrInvalidForkQuery) {
			t.Errorf("%q: got %v want ErrInvalidForkQuery", raw, err)
		}
	}
}

func TestBuildRestoreOverrides_NilSnapshotReturnsNil(t *testing.T) {
	got, count := BuildRestoreOverrides(nil, false)
	if got != nil {
		t.Errorf("expected nil slice, got %+v", got)
	}
	if count != 0 {
		t.Errorf("expected count=0, got %d", count)
	}
}

func TestBuildRestoreOverrides_NoForkLeavesResumeEmpty(t *testing.T) {
	snap := &TeamSnapshot{Members: []SnapshotMemberEntry{
		{ConfigID: "alpha", Config: TeamMemberConfig{Name: "Alpha"}, ClaudeSessionID: "claude-a"},
		{ConfigID: "beta", Config: TeamMemberConfig{Name: "Beta"}, ClaudeSessionID: "claude-b"},
	}}
	got, count := BuildRestoreOverrides(snap, false)
	if count != 0 {
		t.Errorf("count=%d want 0 (no fork)", count)
	}
	for i, ov := range got {
		if ov.ResumeClaudeSessionID != "" {
			t.Errorf("[%d] ResumeClaudeSessionID=%q want empty", i, ov.ResumeClaudeSessionID)
		}
		if ov.ForkResumedSession {
			t.Errorf("[%d] ForkResumedSession=true want false", i)
		}
	}
}

func TestBuildRestoreOverrides_ForkPopulatesWhenIDsPresent(t *testing.T) {
	snap := &TeamSnapshot{Members: []SnapshotMemberEntry{
		{ConfigID: "alpha", Config: TeamMemberConfig{Name: "A"}, ClaudeSessionID: "claude-a"},
		{ConfigID: "beta", Config: TeamMemberConfig{Name: "B"}, ClaudeSessionID: "claude-b"},
	}}
	got, count := BuildRestoreOverrides(snap, true)
	if count != 2 {
		t.Errorf("count=%d want 2", count)
	}
	if got[0].ResumeClaudeSessionID != "claude-a" || !got[0].ForkResumedSession {
		t.Errorf("alpha fork fields wrong: %+v", got[0])
	}
}

func TestBuildRestoreOverrides_MixedForkSkipsBlankIDs(t *testing.T) {
	// A snapshot created mid-spawn may have some members without a
	// ClaudeSessionID yet. Fork should apply only to those that have one.
	snap := &TeamSnapshot{Members: []SnapshotMemberEntry{
		{ConfigID: "alpha", Config: TeamMemberConfig{}, ClaudeSessionID: "claude-a"},
		{ConfigID: "beta", Config: TeamMemberConfig{}, ClaudeSessionID: ""},
	}}
	got, count := BuildRestoreOverrides(snap, true)
	if count != 1 {
		t.Errorf("count=%d want 1 (only alpha has CSID)", count)
	}
	if got[1].ForkResumedSession {
		t.Errorf("beta should NOT be marked forked: %+v", got[1])
	}
}

func TestBuildRestoreOverrides_CopiesConfigFields(t *testing.T) {
	snap := &TeamSnapshot{Members: []SnapshotMemberEntry{
		{
			ConfigID: "alpha",
			Config: TeamMemberConfig{
				Name: "Mia", Role: "lead", Tier: "strategic",
				Color: "purple", Personality: "calm",
				SystemPrompt: "system text", Skills: []string{"s1", "s2"},
			},
		},
	}}
	got, _ := BuildRestoreOverrides(snap, false)
	if got[0].ID != "alpha" || got[0].Name != "Mia" || got[0].Role != "lead" ||
		got[0].Tier != "strategic" || got[0].Color != "purple" ||
		got[0].Personality != "calm" || got[0].SystemPrompt != "system text" {
		t.Errorf("config fields not preserved: %+v", got[0])
	}
	if len(got[0].Skills) != 2 || got[0].Skills[0] != "s1" {
		t.Errorf("Skills not preserved: %+v", got[0].Skills)
	}
}

func TestForkApplied(t *testing.T) {
	cases := []struct {
		name      string
		requested bool
		applied   int
		want      bool
	}{
		{"not requested, none applied", false, 0, false},
		{"not requested, count nonzero (shouldn't happen)", false, 3, false},
		{"requested, none applied (snapshot too old)", true, 0, false},
		{"requested, one applied", true, 1, true},
		{"requested, many applied", true, 10, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ForkApplied(tc.requested, tc.applied); got != tc.want {
				t.Errorf("ForkApplied(%v, %d) = %v want %v", tc.requested, tc.applied, got, tc.want)
			}
		})
	}
}


// fakeLookup returns a SDKSessionLookup backed by a fixed table.
// Missing keys yield (zero, false) as the contract requires.
func fakeLookup(table map[string]SDKSessionData) SDKSessionLookup {
	return func(sid string) (SDKSessionData, bool) {
		d, ok := table[sid]
		return d, ok
	}
}

func TestHarvestSessionData_PicksFirstWorkingDir(t *testing.T) {
	roster := []*AgentState{
		NewAgentState("sess-aaa-0001", "alpha", nil),
		NewAgentState("sess-bbb-0002", "beta", nil),
	}
	lookup := fakeLookup(map[string]SDKSessionData{
		"sess-aaa-0001": {WorkingDir: "/work/team-dir"},
		"sess-bbb-0002": {WorkingDir: "/should-be-ignored"},
	})
	wd, _ := HarvestSessionData(roster, lookup)
	if wd != "/work/team-dir" {
		t.Errorf("workingDir=%q want /work/team-dir", wd)
	}
}

func TestHarvestSessionData_SkipsMissingSessions(t *testing.T) {
	// Member registered in office but no live SDK session — handler
	// uses this path for mid-spawn snapshots and snapshots taken
	// while one agent is restarting.
	roster := []*AgentState{
		NewAgentState("alive-0001", "alpha", nil),
		NewAgentState("ghost-0002", "beta", nil), // not in lookup
	}
	lookup := fakeLookup(map[string]SDKSessionData{
		"alive-0001": {WorkingDir: "/x", ClaudeSessionID: "claude-1"},
	})
	wd, ids := HarvestSessionData(roster, lookup)
	if wd != "/x" {
		t.Errorf("workingDir=%q", wd)
	}
	if len(ids) != 1 || ids["alpha"] != "claude-1" {
		t.Errorf("claudeSessionIDs=%+v want {alpha:claude-1}", ids)
	}
}

func TestHarvestSessionData_BlankClaudeSessionIDSkipped(t *testing.T) {
	// Some members may have a live SDK session but not yet a
	// resume token (first-turn-in-flight). Those should NOT land
	// in the map — the fork-mode restore handler treats absence
	// as "spawn fresh".
	roster := []*AgentState{NewAgentState("sess-001", "alpha", nil)}
	lookup := fakeLookup(map[string]SDKSessionData{
		"sess-001": {WorkingDir: "/x", ClaudeSessionID: ""},
	})
	_, ids := HarvestSessionData(roster, lookup)
	if _, ok := ids["alpha"]; ok {
		t.Errorf("blank ClaudeSessionID landed in map: %+v", ids)
	}
}

func TestHarvestSessionData_BlankConfigIDSkipped(t *testing.T) {
	// A member without a ConfigID can't be re-spawned from the
	// snapshot (no template anchor), so skip them.
	roster := []*AgentState{NewAgentState("sess-001", "", nil)}
	lookup := fakeLookup(map[string]SDKSessionData{
		"sess-001": {WorkingDir: "/x", ClaudeSessionID: "claude-1"},
	})
	_, ids := HarvestSessionData(roster, lookup)
	if len(ids) != 0 {
		t.Errorf("blank ConfigID landed in map: %+v", ids)
	}
}

func TestHarvestSessionData_NilLookupReturnsEmpty(t *testing.T) {
	// Tests sometimes pass nil to verify defensive code paths.
	wd, ids := HarvestSessionData([]*AgentState{NewAgentState("x", "", nil)}, nil)
	if wd != "" {
		t.Errorf("workingDir=%q want empty", wd)
	}
	if len(ids) != 0 {
		t.Errorf("claudeSessionIDs=%+v want empty", ids)
	}
}

func TestHarvestSessionData_NilAgentInRosterSkipped(t *testing.T) {
	// Defensive: nil entries in the roster (shouldn't happen but
	// the production roster slice is shared) don't panic.
	roster := []*AgentState{
		nil,
		NewAgentState("sess-001", "alpha", nil),
		nil,
	}
	lookup := fakeLookup(map[string]SDKSessionData{
		"sess-001": {WorkingDir: "/x", ClaudeSessionID: "c1"},
	})
	wd, ids := HarvestSessionData(roster, lookup)
	if wd != "/x" || ids["alpha"] != "c1" {
		t.Errorf("nil-skip path: wd=%q ids=%+v", wd, ids)
	}
}

func TestHarvestSessionData_EmptyRosterEmptyOutputs(t *testing.T) {
	wd, ids := HarvestSessionData(nil, fakeLookup(map[string]SDKSessionData{}))
	if wd != "" {
		t.Errorf("workingDir=%q want empty", wd)
	}
	if ids == nil {
		t.Error("expected empty map, got nil")
	}
}
