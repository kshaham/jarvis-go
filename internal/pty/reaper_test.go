package pty

import "testing"

// TestIsReapTarget covers the pure comm/uid match that decides whether a
// /proc entry is an orphaned agent subprocess the shutdown backstop should
// SIGKILL. Only the claude/codex CLIs owned by our own euid qualify.
func TestIsReapTarget(t *testing.T) {
	const selfUID = 1000

	tests := []struct {
		name    string
		comm    string
		uid     int
		selfUID int
		want    bool
	}{
		{name: "claude owned by us", comm: "claude", uid: selfUID, selfUID: selfUID, want: true},
		{name: "codex owned by us", comm: "codex", uid: selfUID, selfUID: selfUID, want: true},
		{name: "claude owned by another uid", comm: "claude", uid: 0, selfUID: selfUID, want: false},
		{name: "codex owned by another uid", comm: "codex", uid: 42, selfUID: selfUID, want: false},
		{name: "unrelated comm owned by us", comm: "bash", uid: selfUID, selfUID: selfUID, want: false},
		{name: "empty comm", comm: "", uid: selfUID, selfUID: selfUID, want: false},
		{name: "comm substring is not a match", comm: "claude-helper", uid: selfUID, selfUID: selfUID, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act.
			got := isReapTarget(tt.comm, tt.uid, tt.selfUID)

			// Assert.
			if got != tt.want {
				t.Errorf("isReapTarget(%q, %d, %d) = %v, want %v", tt.comm, tt.uid, tt.selfUID, got, tt.want)
			}
		})
	}
}

// TestReapAgentSubprocesses_SafeNoOp asserts the backstop is harmless when
// there are no claude/codex processes to reap: on darwin it returns 0
// immediately; on linux it scans /proc, finds no matching agent CLIs (this
// test never spawns one), and returns 0 without panicking.
func TestReapAgentSubprocesses_SafeNoOp(t *testing.T) {
	// Act.
	got := ReapAgentSubprocesses()

	// Assert.
	if got != 0 {
		t.Errorf("ReapAgentSubprocesses() = %d, want 0 when no agent subprocesses are present", got)
	}
}
