package main

import (
	"strings"
	"testing"
)

func TestParseAnnounceArgsE(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantSlot  string
		wantURL   string
		wantPID   int
		wantForce bool
		wantErr   string // substring; empty = expect success
	}{
		{
			name:     "bare url defaults to app slot",
			args:     []string{"http://jarvis:3001"},
			wantSlot: "app",
			wantURL:  "http://jarvis:3001",
		},
		{
			name:     "pid flag before url",
			args:     []string{"--pid", "1234", "http://jarvis:3001"},
			wantSlot: "app",
			wantURL:  "http://jarvis:3001",
			wantPID:  1234,
		},
		{
			name:     "pid flag after url",
			args:     []string{"http://jarvis:3001", "--pid", "1234"},
			wantSlot: "app",
			wantURL:  "http://jarvis:3001",
			wantPID:  1234,
		},
		{
			name:      "all flags mixed order",
			args:      []string{"--name", "backend", "http://jarvis:8080", "--pid", "5678", "--force"},
			wantSlot:  "backend",
			wantURL:   "http://jarvis:8080",
			wantPID:   5678,
			wantForce: true,
		},
		{
			name:      "force-only with url",
			args:      []string{"http://localhost:3001", "--force"},
			wantSlot:  "app",
			wantURL:   "http://localhost:3001",
			wantForce: true,
		},
		{
			name:     "named slot, no pid",
			args:     []string{"--name", "api", "http://jarvis:4000"},
			wantSlot: "api",
			wantURL:  "http://jarvis:4000",
		},
		{
			name:    "non-integer pid fails",
			args:    []string{"--pid", "abc", "http://jarvis:3001"},
			wantErr: "--pid must be a non-negative integer",
		},
		{
			name:    "negative pid fails",
			args:    []string{"--pid", "-5", "http://jarvis:3001"},
			wantErr: "--pid must be a non-negative integer",
		},
		{
			name:    "pid without value fails",
			args:    []string{"--pid"},
			wantErr: "--pid requires a value",
		},
		{
			name:    "name without value fails",
			args:    []string{"--name"},
			wantErr: "--name requires a value",
		},
		{
			name:    "unknown flag fails",
			args:    []string{"--verbose", "http://jarvis:3001"},
			wantErr: "unknown flag: --verbose",
		},
		{
			name:     "empty args yields empty url and app slot",
			args:     []string{},
			wantSlot: "app",
		},
		{
			name:     "only first positional becomes url",
			args:     []string{"http://jarvis:3001", "http://jarvis:4000"},
			wantSlot: "app",
			wantURL:  "http://jarvis:3001",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			slot, url, pid, force, err := parseAnnounceArgsE(tc.args)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tc.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if slot != tc.wantSlot {
				t.Errorf("slot: want %q, got %q", tc.wantSlot, slot)
			}
			if url != tc.wantURL {
				t.Errorf("url: want %q, got %q", tc.wantURL, url)
			}
			if pid != tc.wantPID {
				t.Errorf("pid: want %d, got %d", tc.wantPID, pid)
			}
			if force != tc.wantForce {
				t.Errorf("force: want %v, got %v", tc.wantForce, force)
			}
		})
	}
}
