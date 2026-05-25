package main

// R3.3 · typed payload schemas for cm subcommands that historically
// took free-form text (standup, brainstorm, leader-tasks). Each
// command grows an optional --json <body> flag that, when set, is
// decoded against the corresponding struct here and validated
// client-side before being POSTed. The server's existing free-text
// fields remain populated (rendered from the structured form) so
// nothing on the server changes -- this is purely additive.
//
// Schemas are intentionally minimal. They model the fields operators
// + the dashboard actually consume; agents that want to attach
// extra context can either embed it in the rendered text or extend
// the struct in a follow-up.
//
// Agents see the schemas via `cm help <cmd>` (which prints the JSON
// shape) and via the system-prompt addendum jarvis injects at spawn.

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// Standup
// ---------------------------------------------------------------------------

// StandupReport is the structured shape of `cm standup respond --json`.
// The rendered Markdown view (used as the free-text Content field on
// the wire) is built by Render() so the office activity feed still
// shows a human-readable summary.
type StandupReport struct {
	Status    string   `json:"status"`
	Blockers  []string `json:"blockers,omitempty"`
	Decisions []string `json:"decisions,omitempty"`
	Next      []string `json:"next,omitempty"`
}

func (s *StandupReport) Validate() error {
	if strings.TrimSpace(s.Status) == "" {
		return errors.New("status is required")
	}
	if len(s.Status) > 2000 {
		return errors.New("status must be <= 2000 characters")
	}
	for i, b := range s.Blockers {
		if strings.TrimSpace(b) == "" {
			return fmt.Errorf("blockers[%d] is empty", i)
		}
	}
	return nil
}

func (s *StandupReport) Render() string {
	var sb strings.Builder
	sb.WriteString(s.Status)
	if len(s.Blockers) > 0 {
		sb.WriteString("\n\n**Blockers:**\n")
		for _, b := range s.Blockers {
			sb.WriteString("- ")
			sb.WriteString(b)
			sb.WriteByte('\n')
		}
	}
	if len(s.Decisions) > 0 {
		sb.WriteString("\n**Decisions:**\n")
		for _, d := range s.Decisions {
			sb.WriteString("- ")
			sb.WriteString(d)
			sb.WriteByte('\n')
		}
	}
	if len(s.Next) > 0 {
		sb.WriteString("\n**Next:**\n")
		for _, n := range s.Next {
			sb.WriteString("- ")
			sb.WriteString(n)
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

const standupJSONExample = `{
  "status":    "Finished auth refactor; tests pass.",
  "blockers":  ["Need lead's sign-off on new login UI"],
  "decisions": ["Switched from JWT to opaque tokens"],
  "next":      ["Update mobile client to match"]
}`

// ---------------------------------------------------------------------------
// Brainstorm
// ---------------------------------------------------------------------------

// BrainstormResponse is the structured shape of `cm brainstorm respond --json`.
// The server's existing "proposals" string is built by Render() so the
// office UI keeps working unchanged.
type BrainstormResponse struct {
	Proposals []BrainstormProposal `json:"proposals"`
	Notes     string               `json:"notes,omitempty"`
}

type BrainstormProposal struct {
	Title    string `json:"title"`
	Why      string `json:"why,omitempty"`
	Priority string `json:"priority,omitempty"` // "low" | "medium" | "high"
	Owner    string `json:"owner,omitempty"`    // teammate ID or "self"
}

func (b *BrainstormResponse) Validate() error {
	if len(b.Proposals) == 0 {
		return errors.New("at least one proposal is required")
	}
	if len(b.Proposals) > 20 {
		return errors.New("max 20 proposals per response")
	}
	for i, p := range b.Proposals {
		if strings.TrimSpace(p.Title) == "" {
			return fmt.Errorf("proposals[%d].title is required", i)
		}
		if p.Priority != "" && p.Priority != "low" && p.Priority != "medium" && p.Priority != "high" {
			return fmt.Errorf("proposals[%d].priority must be low|medium|high", i)
		}
	}
	return nil
}

func (b *BrainstormResponse) Render() string {
	var sb strings.Builder
	for i, p := range b.Proposals {
		fmt.Fprintf(&sb, "%d. **%s**", i+1, p.Title)
		if p.Priority != "" {
			fmt.Fprintf(&sb, " [%s]", p.Priority)
		}
		if p.Owner != "" {
			fmt.Fprintf(&sb, " @%s", p.Owner)
		}
		sb.WriteByte('\n')
		if p.Why != "" {
			sb.WriteString("   ")
			sb.WriteString(p.Why)
			sb.WriteByte('\n')
		}
	}
	if b.Notes != "" {
		sb.WriteString("\nNotes: ")
		sb.WriteString(b.Notes)
		sb.WriteByte('\n')
	}
	return sb.String()
}

const brainstormJSONExample = `{
  "proposals": [
    {"title": "Split monolith handler into 3 files", "why": "single 1200-line file is unreviewable", "priority": "high"},
    {"title": "Cache provider list", "why": "we hit the API on every spawn", "priority": "medium", "owner": "bolt"}
  ],
  "notes": "Both are doable this sprint."
}`

// ---------------------------------------------------------------------------
// Leader-tasks
// ---------------------------------------------------------------------------

// LeaderTaskAssignments is the structured shape of
// `cm leader-tasks respond --json`. Each entry is a task the leader
// is assigning to a teammate. The server still gets a flattened
// `content` string (Render() output) for the existing parser path.
type LeaderTaskAssignments struct {
	Assignments []LeaderTaskAssignment `json:"assignments"`
	Notes       string                 `json:"notes,omitempty"`
}

type LeaderTaskAssignment struct {
	Agent       string   `json:"agent"`              // teammate ID
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Priority    string   `json:"priority,omitempty"` // low|medium|high
	Labels      []string `json:"labels,omitempty"`
}

func (l *LeaderTaskAssignments) Validate() error {
	if len(l.Assignments) == 0 {
		return errors.New("at least one assignment is required")
	}
	if len(l.Assignments) > 50 {
		return errors.New("max 50 assignments per response")
	}
	for i, a := range l.Assignments {
		if strings.TrimSpace(a.Agent) == "" {
			return fmt.Errorf("assignments[%d].agent is required", i)
		}
		if strings.TrimSpace(a.Title) == "" {
			return fmt.Errorf("assignments[%d].title is required", i)
		}
		if a.Priority != "" && a.Priority != "low" && a.Priority != "medium" && a.Priority != "high" {
			return fmt.Errorf("assignments[%d].priority must be low|medium|high", i)
		}
	}
	return nil
}

func (l *LeaderTaskAssignments) Render() string {
	var sb strings.Builder
	for _, a := range l.Assignments {
		fmt.Fprintf(&sb, "@%s: %s", a.Agent, a.Title)
		if a.Priority != "" {
			fmt.Fprintf(&sb, " [%s]", a.Priority)
		}
		sb.WriteByte('\n')
		if a.Description != "" {
			sb.WriteString("  ")
			sb.WriteString(a.Description)
			sb.WriteByte('\n')
		}
		if len(a.Labels) > 0 {
			sb.WriteString("  labels: ")
			sb.WriteString(strings.Join(a.Labels, ", "))
			sb.WriteByte('\n')
		}
	}
	if l.Notes != "" {
		sb.WriteString("\nNotes: ")
		sb.WriteString(l.Notes)
		sb.WriteByte('\n')
	}
	return sb.String()
}

const leaderTasksJSONExample = `{
  "assignments": [
    {"agent": "alice", "title": "Wire up the logout endpoint", "priority": "high", "labels": ["auth"]},
    {"agent": "bob",   "title": "Add the dropdown back",      "priority": "medium"}
  ]
}`

// ---------------------------------------------------------------------------
// Decode helpers
// ---------------------------------------------------------------------------

// DecodeStandupJSON parses a JSON body into a StandupReport and
// validates it. Returns the parsed struct + the rendered free-text
// view (for callers that need both).
func DecodeStandupJSON(raw string) (*StandupReport, string, error) {
	var s StandupReport
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return nil, "", fmt.Errorf("invalid JSON: %w", err)
	}
	if err := s.Validate(); err != nil {
		return nil, "", err
	}
	return &s, s.Render(), nil
}

// DecodeBrainstormJSON / DecodeLeaderTasksJSON mirror the standup helper.
func DecodeBrainstormJSON(raw string) (*BrainstormResponse, string, error) {
	var b BrainstormResponse
	if err := json.Unmarshal([]byte(raw), &b); err != nil {
		return nil, "", fmt.Errorf("invalid JSON: %w", err)
	}
	if err := b.Validate(); err != nil {
		return nil, "", err
	}
	return &b, b.Render(), nil
}

func DecodeLeaderTasksJSON(raw string) (*LeaderTaskAssignments, string, error) {
	var l LeaderTaskAssignments
	if err := json.Unmarshal([]byte(raw), &l); err != nil {
		return nil, "", fmt.Errorf("invalid JSON: %w", err)
	}
	if err := l.Validate(); err != nil {
		return nil, "", err
	}
	return &l, l.Render(), nil
}

// SchemaExampleFor returns the documented example JSON body for a
// given subcommand. Surfaced by `cm help <cmd>`.
func SchemaExampleFor(cmd string) string {
	switch cmd {
	case "standup":
		return standupJSONExample
	case "brainstorm":
		return brainstormJSONExample
	case "leader-tasks":
		return leaderTasksJSONExample
	default:
		return ""
	}
}
