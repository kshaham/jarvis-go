package handler

import "testing"

// TestResolveMaxTurns covers the per-session turn-cap precedence applied at
// both spawn sites (SDK + Codex): explicit override > perpetual ceiling >
// goal-mode default. The fix raises (never removes) the cap for perpetual
// agents so steady-state work can't deterministically retire a healthy member
// while the runaway defense (a non-zero ceiling) stays intact for every spawn.
func TestResolveMaxTurns(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		ov        *memberOverride
		perpetual bool
		want      int
	}{
		{
			name:      "goal mode keeps the 100 default",
			ov:        nil,
			perpetual: false,
			want:      defaultMaxTurnsPerSession,
		},
		{
			name:      "perpetual mode raises to the perpetual ceiling",
			ov:        nil,
			perpetual: true,
			want:      perpetualMaxTurnsPerSession,
		},
		{
			name:      "explicit override wins in goal mode",
			ov:        &memberOverride{MaxTurns: 42},
			perpetual: false,
			want:      42,
		},
		{
			name:      "explicit override wins over the perpetual ceiling",
			ov:        &memberOverride{MaxTurns: 42},
			perpetual: true,
			want:      42,
		},
		{
			name:      "zero override falls through to perpetual ceiling",
			ov:        &memberOverride{MaxTurns: 0},
			perpetual: true,
			want:      perpetualMaxTurnsPerSession,
		},
		{
			name:      "zero override falls through to goal default",
			ov:        &memberOverride{MaxTurns: 0},
			perpetual: false,
			want:      defaultMaxTurnsPerSession,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := resolveMaxTurns(tt.ov, tt.perpetual); got != tt.want {
				t.Errorf("resolveMaxTurns(%+v, %v) = %d, want %d", tt.ov, tt.perpetual, got, tt.want)
			}
		})
	}
}

// TestResolveMaxTurns_NeverUnlimited guards the invariant that every spawn path
// gets a non-zero ceiling — a 0 (unlimited) cap would disable the runaway
// defense entirely. The perpetual ceiling must also be strictly larger than
// the goal default so it actually raises the cap.
func TestResolveMaxTurns_NeverUnlimited(t *testing.T) {
	t.Parallel()
	for _, perpetual := range []bool{false, true} {
		if got := resolveMaxTurns(nil, perpetual); got <= 0 {
			t.Errorf("resolveMaxTurns(nil, %v) returned non-positive cap %d", perpetual, got)
		}
	}
	if perpetualMaxTurnsPerSession <= defaultMaxTurnsPerSession {
		t.Errorf("perpetual ceiling %d must exceed goal default %d",
			perpetualMaxTurnsPerSession, defaultMaxTurnsPerSession)
	}
}
