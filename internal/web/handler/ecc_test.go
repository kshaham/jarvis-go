package handler

import "testing"

// P10 · ECC opt-out plumbing tests for the spawn-request layer. The
// MCP gating itself is tested through the codegraph test pattern; here
// we just pin the helper contract that gates everything.

func TestEccOptOut_TableDriven(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		set  *bool
		want bool
	}{
		{"nil → opt-in (default ECC on)", nil, false},
		{"false → ECC on", boolPtr(false), false},
		{"true → ECC off", boolPtr(true), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := spawnTeamRequest{DisableEcc: tc.set}
			got := eccOptOut(req)
			if got != tc.want {
				t.Errorf("eccOptOut(DisableEcc=%v) = %v, want %v", ptrStr(tc.set), got, tc.want)
			}
		})
	}
}

func TestEccOptOut_IndependentOfCodegraph(t *testing.T) {
	t.Parallel()
	// Regression guard: gating helpers must not cross-contaminate.
	// Setting DisableCodegraph must not affect ECC, and vice versa.
	codegraphOff := boolPtr(true)
	eccOff := boolPtr(true)

	req := spawnTeamRequest{DisableCodegraph: codegraphOff}
	if eccOptOut(req) {
		t.Error("ECC should be ON when only DisableCodegraph is set")
	}
	if !codegraphOptOut(req) {
		t.Error("codegraph should be OFF when DisableCodegraph=true")
	}

	req = spawnTeamRequest{DisableEcc: eccOff}
	if !eccOptOut(req) {
		t.Error("ECC should be OFF when DisableEcc=true")
	}
	if codegraphOptOut(req) {
		t.Error("codegraph should stay ON when only DisableEcc is set")
	}
}

// ptrStr renders a *bool for assertion failure messages without
// dereferencing nil.
func ptrStr(b *bool) string {
	if b == nil {
		return "nil"
	}
	if *b {
		return "true"
	}
	return "false"
}

func boolPtr(b bool) *bool { return &b }
