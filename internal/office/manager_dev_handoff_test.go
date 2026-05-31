package office

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Dev-instance backward-compat getters/setters
// ---------------------------------------------------------------------------

// TestTeamDevInstance_SetGetRoundTrip verifies the default-slot wrappers
// round-trip URL/PID/credentials through the dev-instance map.
func TestTeamDevInstance_SetGetRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m, _, _ := newCtrlManager(t, DefaultConfig())

	// Empty before anything is set.
	if m.GetTeamDevURL() != "" {
		t.Errorf("expected empty URL initially, got %q", m.GetTeamDevURL())
	}
	if m.GetTeamDevPID() != 0 {
		t.Errorf("expected PID 0 initially, got %d", m.GetTeamDevPID())
	}
	if m.GetTeamDevCredentials() != "" {
		t.Errorf("expected empty creds initially, got %q", m.GetTeamDevCredentials())
	}

	m.SetTeamDevURL("http://localhost:3000", 4242)
	if got := m.GetTeamDevURL(); got != "http://localhost:3000" {
		t.Errorf("expected URL set, got %q", got)
	}
	if got := m.GetTeamDevPID(); got != 4242 {
		t.Errorf("expected PID 4242, got %d", got)
	}

	m.SetTeamDevCredentials("user:pass")
	if got := m.GetTeamDevCredentials(); got != "user:pass" {
		t.Errorf("expected creds set, got %q", got)
	}
}

// TestSetDevInstanceCredentials_InvalidSlot verifies an invalid slot name
// is rejected with an error.
func TestSetDevInstanceCredentials_InvalidSlot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m, _, _ := newCtrlManager(t, DefaultConfig())

	if err := m.SetDevInstanceCredentials("../../etc/passwd", "x"); err == nil {
		t.Error("expected error for invalid slot name")
	}
}

// TestSetDevInstanceCredentials_EmptyDropsSlot verifies that setting empty
// credentials on a creds-only slot drops the (now-empty) slot entirely.
func TestSetDevInstanceCredentials_EmptyDropsSlot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m, _, _ := newCtrlManager(t, DefaultConfig())

	if err := m.SetDevInstanceCredentials("app", "secret"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.GetTeamDevCredentials() != "secret" {
		t.Fatal("expected creds set")
	}
	// Clearing creds on a slot with no URL/PID drops it.
	if err := m.SetDevInstanceCredentials("app", ""); err != nil {
		t.Fatalf("unexpected error clearing creds: %v", err)
	}
	if _, ok := m.GetDevInstance("app"); ok {
		t.Error("expected creds-only slot to be dropped after clearing creds")
	}
}

// TestDevInstance_PersistsAndLoadsInFreshManager verifies a dev-instance
// slot written via SetTeamDevURL/SetTeamDevCredentials is persisted to disk
// and loaded by loadDevInstances when a fresh Manager boots against the
// same HOME.
func TestDevInstance_PersistsAndLoadsInFreshManager(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	m1, _, _ := newCtrlManager(t, DefaultConfig())
	m1.SetTeamDevURL("http://localhost:5173", 9090)
	m1.SetTeamDevCredentials("admin:hunter2")

	// Fresh manager boots and loads persisted dev instances at construction.
	m2, _, _ := newCtrlManager(t, DefaultConfig())
	if got := m2.GetTeamDevURL(); got != "http://localhost:5173" {
		t.Errorf("expected persisted URL loaded, got %q", got)
	}
	if got := m2.GetTeamDevPID(); got != 9090 {
		t.Errorf("expected persisted PID loaded, got %d", got)
	}
	if got := m2.GetTeamDevCredentials(); got != "admin:hunter2" {
		t.Errorf("expected persisted creds loaded, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// ForceAcceptHandoff / ForceRejectHandoff (operator-auth variants)
// ---------------------------------------------------------------------------

// TestForceAcceptHandoff_MovesTask verifies the operator force-accept path
// transfers the task to the receiver without the receiver's session token.
func TestForceAcceptHandoff_MovesTask(t *testing.T) {
	m, vex, bolt, task := setupTeamForHandoff(t)

	if _, err := m.InitiateHandoff(vex.SessionID(), task.ID, bolt.ConfigID, "operator override"); err != nil {
		t.Fatalf("initiate: %v", err)
	}

	h, err := m.ForceAcceptHandoff(task.ID)
	if err != nil {
		t.Fatalf("ForceAcceptHandoff: %v", err)
	}
	if h.Status != HandoffAccepted {
		t.Errorf("expected accepted status, got %s", h.Status)
	}
	if len(vex.GetTasks()) != 0 || len(bolt.GetTasks()) != 1 {
		t.Errorf("expected task moved to receiver: vex=%d bolt=%d", len(vex.GetTasks()), len(bolt.GetTasks()))
	}
	if got := bolt.GetTasks()[0].Status; got != TaskInProgress {
		t.Errorf("expected force-accepted task in_progress, got %s", got)
	}
}

// TestForceRejectHandoff_RevertsToSender verifies the operator force-reject
// path reverts the task to the sender and clears the handoff.
func TestForceRejectHandoff_RevertsToSender(t *testing.T) {
	m, vex, bolt, task := setupTeamForHandoff(t)

	if _, err := m.InitiateHandoff(vex.SessionID(), task.ID, bolt.ConfigID, "operator override"); err != nil {
		t.Fatalf("initiate: %v", err)
	}

	h, err := m.ForceRejectHandoff(task.ID, "operator says no")
	if err != nil {
		t.Fatalf("ForceRejectHandoff: %v", err)
	}
	if h.Status != HandoffRejected {
		t.Errorf("expected rejected status, got %s", h.Status)
	}
	if task.Status != TaskInProgress {
		t.Errorf("expected task reverted to in_progress, got %s", task.Status)
	}
	if task.Handoff != nil {
		t.Error("expected handoff pointer cleared after force-reject")
	}
	if len(vex.GetTasks()) != 1 {
		t.Error("expected task to remain with sender after reject")
	}
}

// TestForceAcceptHandoff_UnknownTask verifies an unknown task ID returns an
// error rather than panicking.
func TestForceAcceptHandoff_UnknownTask(t *testing.T) {
	m, _, _, _ := setupTeamForHandoff(t)
	if _, err := m.ForceAcceptHandoff("does-not-exist"); err == nil {
		t.Error("expected error for unknown task ID")
	}
}
