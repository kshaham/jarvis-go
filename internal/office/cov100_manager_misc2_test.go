package office

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestCheckOneDevInstance_AbsentAndNotYet drives the slot-absent / empty-URL
// early return and the nextCheckAt-not-yet return.
func TestCheckOneDevInstance_AbsentAndNotYet(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	// Absent slot → early return, no panic.
	m.checkOneDevInstance("nope", time.Now())

	// Slot with empty URL → early return.
	m.mu.Lock()
	m.teamDevInstances["empty"] = &devInstance{URL: ""}
	// Slot with a future NextCheckAt → skip (not yet due).
	m.teamDevInstances["future"] = &devInstance{
		URL:         "http://jarvis:9999",
		NextCheckAt: time.Now().Add(time.Hour),
	}
	m.mu.Unlock()
	m.checkOneDevInstance("empty", time.Now())
	m.checkOneDevInstance("future", time.Now())
	// The future slot must be untouched (no LastCheckAt set).
	inst, _ := m.GetDevInstance("future")
	if !inst.LastCheckAt.IsZero() {
		t.Error("expected future-scheduled slot to be skipped")
	}
}

// TestLoadDevInstances_SkipsInvalidSlotName writes a team-dev file with an
// invalid slot name so the validDevSlotName skip runs on reload.
func TestLoadDevInstances_SkipsInvalidSlotName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".jarvis", "office-memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Slot name contains characters validDevSlotName rejects.
	badName := "team-dev-" + "this!is@invalid" + ".json"
	if err := os.WriteFile(filepath.Join(dir, badName), []byte(`{"url":"http://x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// A valid one alongside it.
	if err := os.WriteFile(filepath.Join(dir, "team-dev-app.json"), []byte(`{"url":"http://jarvis:3000"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Fresh manager reloads from disk in its constructor.
	m := newTestManager()
	if _, ok := m.GetDevInstance("app"); !ok {
		t.Error("expected the valid slot to load")
	}
	if _, ok := m.GetDevInstance("this!is@invalid"); ok {
		t.Error("expected the invalid-named slot to be skipped")
	}
}

// TestGetOfficeState_AuditTrimAndWrapUp drives the audit>50 trim and the
// wrappingUp wrapUp-status branch.
func TestGetOfficeState_AuditTrimAndWrapUp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.RegisterAgent("s1", "c1", &TeamMemberConfig{ID: "c1", Name: "C1"})
	// Generate >50 audit entries.
	for i := 0; i < 60; i++ {
		m.auditLog("office:test", "s1", "C1", map[string]interface{}{"i": i})
	}
	m.SetWrappingUp(true)

	state := m.GetOfficeState()
	audit, ok := state["recentAudit"].([]AuditEntry)
	if !ok {
		t.Fatalf("recentAudit wrong type: %T", state["recentAudit"])
	}
	if len(audit) > 50 {
		t.Errorf("expected audit trimmed to <=50, got %d", len(audit))
	}
	if _, ok := state["wrapUp"].(WrapUpStatus); !ok {
		t.Errorf("expected wrapUp status present, got %T", state["wrapUp"])
	}
}

// TestSubmitReflectionResponse_LearningWriteError drives the
// AddLearningEntry error branch by making the learnings dir unwritable.
func TestSubmitReflectionResponse_LearningWriteError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Make ~/.jarvis/learnings a FILE so AddLearningEntry's MkdirAll/write fails.
	if err := os.MkdirAll(filepath.Join(home, ".jarvis"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".jarvis", "learnings"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTestManager()
	m.RegisterAgent("s-r", "rcfg", &TeamMemberConfig{ID: "rcfg", Name: "R", Tier: "worker"})
	// Returns true even though the learning persist fails (best-effort) — the
	// error branch is logged, not surfaced.
	if !m.SubmitReflectionResponse("s-r", "a lesson", "technique", "", "") {
		t.Error("expected SubmitReflectionResponse to still return true on persist failure")
	}
}
