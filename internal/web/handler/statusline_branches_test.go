package handler

// Branch-fill tests for OfficeAgentStatusLine. The existing statusline_test.go
// covers the placeholder arms (no session / no manager / unknown agent) and a
// single registered-agent render. These exercise the remaining composition
// arms: the roster "skip non-matching agent" continue, the SDK-working status,
// the active-task title, team-wide blocker counting (singular + plural), and
// the paused suffix.

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/office"
)

func TestOfficeAgentStatusLine_WorkingWithActiveTaskAndBlockers(t *testing.T) {
	ctx := newTimelineContext(t)
	mgr := ctx.OfficeManager

	// Register a leading "decoy" agent whose session ID does NOT match so the
	// roster loop hits the `continue` (line 37) before reaching our target.
	mgr.RegisterAgent("decoy-0001", "decoy", &office.TeamMemberConfig{ID: "decoy", Name: "Decoy"})

	// Target agent: working SDK + a worker role/tier + an in-progress task.
	mgr.RegisterAgent("sess-target", "vex", &office.TeamMemberConfig{
		ID: "vex", Name: "Vex", Role: "engineer", Tier: "worker",
	})
	target := mgr.GetAgent("sess-target")
	target.SetSDKWorking(true)
	target.AddTask(&office.Task{
		ID: "t-1", Title: "ship the rollout", Status: office.TaskInProgress,
		AssignedAt: time.Now(),
	})

	// Two blocked tasks across the roster -> plural "blockers team-wide".
	target.AddTask(&office.Task{ID: "t-2", Title: "blocked one", Status: office.TaskBlocked})
	mgr.GetAgent("decoy-0001").AddTask(&office.Task{ID: "t-3", Title: "blocked two", Status: office.TaskBlocked})

	rec := statuslineGet(t, ctx, "sessionId=sess-target")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := strings.TrimSpace(rec.Body.String())

	if !strings.Contains(body, "Vex") {
		t.Errorf("name missing: %q", body)
	}
	if !strings.Contains(body, "engineer") {
		t.Errorf("role missing: %q", body)
	}
	if !strings.Contains(body, "working") {
		t.Errorf("SDK-working status missing: %q", body)
	}
	if !strings.Contains(body, `on "ship the rollout"`) {
		t.Errorf("active task title missing: %q", body)
	}
	if !strings.Contains(body, "2 blockers team-wide") {
		t.Errorf("expected plural blocker count, got: %q", body)
	}
	// worker tier (not strategic) must NOT add the "lead" marker.
	if strings.Contains(body, "· lead") {
		t.Errorf("worker tier should not be marked lead: %q", body)
	}
}

func TestOfficeAgentStatusLine_IdleSingleBlockerAndPaused(t *testing.T) {
	ctx := newTimelineContext(t)
	mgr := ctx.OfficeManager

	mgr.RegisterAgent("sess-idle", "ana", &office.TeamMemberConfig{
		ID: "ana", Name: "Ana", Role: "reviewer", Tier: "worker",
	})
	idle := mgr.GetAgent("sess-idle")
	// SDK not working -> "idle" status arm.
	idle.SetSDKWorking(false)
	// Exactly one blocked task -> singular "1 blocker team-wide".
	idle.AddTask(&office.Task{ID: "b-1", Title: "the one blocker", Status: office.TaskBlocked})

	// Pause the office -> "⏸ paused" suffix.
	mgr.SetPaused(true, "operator coffee break")

	rec := statuslineGet(t, ctx, "sessionId=sess-idle")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := strings.TrimSpace(rec.Body.String())

	if !strings.Contains(body, "idle") {
		t.Errorf("idle status missing: %q", body)
	}
	if !strings.Contains(body, "1 blocker team-wide") {
		t.Errorf("expected singular blocker count, got: %q", body)
	}
	if strings.Contains(body, "blockers team-wide") {
		t.Errorf("singular case must not pluralize: %q", body)
	}
	if !strings.Contains(body, "paused") {
		t.Errorf("paused suffix missing: %q", body)
	}
}

func TestOfficeAgentStatusLine_StrategicTierMarkedLead(t *testing.T) {
	ctx := newTimelineContext(t)
	mgr := ctx.OfficeManager
	mgr.RegisterAgent("sess-lead", "boss", &office.TeamMemberConfig{
		ID: "boss", Name: "Boss", Role: "", Tier: "strategic",
	})
	rec := statuslineGet(t, ctx, "sessionId=sess-lead")
	body := strings.TrimSpace(rec.Body.String())
	// Empty role -> role arm skipped; strategic -> "lead" appended.
	if !strings.Contains(body, "lead") {
		t.Errorf("strategic tier must be marked lead: %q", body)
	}
	if strings.Contains(body, "engineer") {
		t.Errorf("no role expected: %q", body)
	}
}
