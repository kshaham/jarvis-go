package office

import (
	"errors"
	"strconv"

	"github.com/kshaham/jarvis/internal/office/spawnpayload"
)

// ErrSnapshotEmpty is returned by ValidateSnapshotForRestore when
// the snapshot has zero members. The handler maps this to 400.
var ErrSnapshotEmpty = errors.New("snapshot has no members")

// ErrSnapshotMissingWorkingDir is returned when the snapshot
// doesn't carry a workingDir. Older snapshots (pre-R25) could land
// in this state if the team's SDK sessions hadn't yet reported one.
// The handler maps this to 400 with the same message text.
var ErrSnapshotMissingWorkingDir = errors.New("snapshot has no workingDir")

// ValidateSnapshotForRestore checks the prerequisites every restore
// flow needs: the snapshot must have members and a workingDir. Both
// are sentinel errors so the handler can return 400 with the right
// message without string-matching.
//
// The handler still owns the "team already spawned" precondition
// because that's an ephemeral Manager state, not a snapshot
// property.
//
// Pure. Snapshot must be non-nil (callers already null-check via
// LoadSnapshot's error return).
func ValidateSnapshotForRestore(snap *TeamSnapshot) error {
	if snap == nil {
		return ErrSnapshotEmpty
	}
	if len(snap.Members) == 0 {
		return ErrSnapshotEmpty
	}
	if snap.WorkingDir == "" {
		return ErrSnapshotMissingWorkingDir
	}
	return nil
}

// ErrInvalidForkQuery is returned by ParseForkQuery when the raw
// ?fork=… query value isn't a recognised boolean. The handler
// returns 400 with a clear message — a typo (`fork=ture`) shouldn't
// silently fall through to a non-forked restore.
var ErrInvalidForkQuery = errors.New("fork query must be a boolean (true/false)")

// ParseForkQuery interprets the operator's `?fork=…` query value.
// Empty string → false (the default; no fork). Any value that
// strconv.ParseBool accepts (1/t/T/TRUE/true/True etc.) returns
// the corresponding boolean. Anything else returns
// ErrInvalidForkQuery.
//
// The strict-parse contract is intentional: silently defaulting to
// fork=false on a typo would surprise an operator who thought
// they'd asked for a fork restore and got a fresh team instead.
func ParseForkQuery(raw string) (bool, error) {
	if raw == "" {
		return false, nil
	}
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return false, ErrInvalidForkQuery
	}
	return parsed, nil
}

// BuildRestoreOverrides projects a TeamSnapshot's members into the
// spawn-request's MemberOverride slice. When the operator asked
// for a fork (forkRequested=true), populates each member's
// ResumeClaudeSessionID + ForkResumedSession from the snapshot's
// captured ClaudeSessionID — but ONLY for members that actually
// have an ID to resume. Members without an ID fall back to fresh
// sessions so the operator still gets a working team.
//
// Returns:
//   - the override slice (always len(snap.Members) entries)
//   - forkAppliedCount: how many members actually got a resume
//     ID (so the SSE broadcast can distinguish "fork requested
//     but nothing to resume" from "fork applied to N members").
//
// Pure: no I/O, no time, no globals. Snapshot must be non-nil.
func BuildRestoreOverrides(snap *TeamSnapshot, forkRequested bool) ([]spawnpayload.MemberOverride, int) {
	if snap == nil {
		return nil, 0
	}
	overrides := make([]spawnpayload.MemberOverride, 0, len(snap.Members))
	forkAppliedCount := 0
	for _, m := range snap.Members {
		ov := spawnpayload.MemberOverride{
			ID:           m.ConfigID,
			Name:         m.Config.Name,
			Role:         m.Config.Role,
			Tier:         m.Config.Tier,
			Color:        m.Config.Color,
			Personality:  m.Config.Personality,
			Voice:        m.Config.Voice,
			SystemPrompt: m.Config.SystemPrompt,
			Skills:       m.Config.Skills,
		}
		if forkRequested && m.ClaudeSessionID != "" {
			ov.ResumeClaudeSessionID = m.ClaudeSessionID
			ov.ForkResumedSession = true
			forkAppliedCount++
		}
		overrides = append(overrides, ov)
	}
	return overrides, forkAppliedCount
}

// ForkApplied reports whether at least one member resumed via fork.
// Distinct from the raw operator request flag because a snapshot
// created before the R3.5 fork feature has no ClaudeSessionIDs
// to resume from — in which case fork=true falls back to fresh
// sessions and the UI should show "fresh team" rather than
// "forked".
//
// Trivially pure; lives alongside BuildRestoreOverrides because
// the two are always called as a pair.
func ForkApplied(requested bool, applied int) bool {
	return requested && applied > 0
}

// SDKSessionData is the minimum surface HarvestSessionData needs
// from a live SDK session. Defined here so the office package
// doesn't need to import pty (which would tangle the dependency
// graph). The handler adapts pty.SDKSession into this shape at the
// call site.
type SDKSessionData struct {
	// WorkingDir is the directory the session was spawned into.
	// All agents on a team share this value; HarvestSessionData
	// captures it from whichever agent's session it sees first.
	WorkingDir string

	// ClaudeSessionID is the Claude-side session identifier (the
	// resume token returned by `claude --print-session-id`).
	// Empty when the session hasn't completed its first turn yet
	// or the underlying CLI doesn't expose it.
	ClaudeSessionID string
}

// SDKSessionLookup is the per-sessionID adapter the handler
// provides to HarvestSessionData. Returns (zero, false) when the
// session ID isn't known to the SDK manager (PTY-only sessions,
// already-cleaned-up sessions, etc.).
type SDKSessionLookup func(sessionID string) (SDKSessionData, bool)

// HarvestSessionData walks the roster, looks up each agent's live
// SDK session, and returns:
//
//   - the team's working directory (captured from the first
//     session that has one set), and
//   - a map of configID → ClaudeSessionID for every member whose
//     session has produced a resume token.
//
// Both outputs feed the snapshot-create flow so a later restore
// can rehydrate the same working dir AND resume each member's
// Claude transcript via `--resume <id>` (fork-mode restore).
//
// Pure given a deterministic lookup. Roster ordering doesn't
// matter for the claudeSessionIDs map but does determine which
// session "wins" the workingDir tie-break (first non-empty).
//
// Returns an empty map (not nil) so callers can use len() without
// a guard.
func HarvestSessionData(roster []*AgentState, lookup SDKSessionLookup) (workingDir string, claudeSessionIDs map[string]string) {
	claudeSessionIDs = map[string]string{}
	if lookup == nil {
		return "", claudeSessionIDs
	}
	for _, a := range roster {
		if a == nil {
			continue
		}
		data, ok := lookup(a.SessionID)
		if !ok {
			continue
		}
		if workingDir == "" {
			workingDir = data.WorkingDir
		}
		if data.ClaudeSessionID != "" && a.ConfigID != "" {
			claudeSessionIDs[a.ConfigID] = data.ClaudeSessionID
		}
	}
	return workingDir, claudeSessionIDs
}
