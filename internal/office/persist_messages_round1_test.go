package office

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestLoadPersistedMessages_SanitizesAndCaps writes a messages.json with a
// zero-timestamp message (dropped), a far-future one (clamped), and more than
// MaxMessages entries (front-dropped), then loads it and asserts the result.
func TestLoadPersistedMessages_SanitizesAndCaps(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".jarvis", "office-memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	msgs := []OfficeMessage{
		{ID: "zero", Content: "no timestamp"},                                  // zero -> dropped
		{ID: "future", Content: "from the future", Timestamp: now.Add(48 * time.Hour)}, // clamped
	}
	// Add enough valid messages to exceed the default MaxMessages (50).
	for i := 0; i < 60; i++ {
		msgs = append(msgs, OfficeMessage{
			ID:        "m" + string(rune('A'+i%26)) + string(rune('0'+i/26)),
			Content:   "msg",
			Timestamp: now.Add(-time.Duration(60-i) * time.Minute),
		})
	}
	data, _ := json.Marshal(msgs)
	if err := os.WriteFile(filepath.Join(dir, "messages.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	m := newTestManager() // clears messages after construct
	m.loadPersistedMessages()

	loaded := m.GetMessages()
	maxMsgs := m.GetConfig().MaxMessages
	if len(loaded) > maxMsgs {
		t.Errorf("expected at most %d messages after cap, got %d", maxMsgs, len(loaded))
	}
	// The zero-timestamp message must have been dropped.
	for _, msg := range loaded {
		if msg.ID == "zero" {
			t.Error("zero-timestamp message should have been dropped")
		}
		// No message should carry a far-future timestamp post-sanitize.
		if msg.Timestamp.After(now.Add(time.Hour)) {
			t.Errorf("future timestamp not clamped: %v", msg.Timestamp)
		}
	}
}

// TestPersistMessages_RoundTrip sends messages, persists, and reloads them in a
// fresh manager.
func TestPersistMessages_RoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.RegisterAgent("sess-a", "alpha", &TeamMemberConfig{ID: "alpha", Name: "Alpha"})
	m.RegisterAgent("sess-b", "beta", &TeamMemberConfig{ID: "beta", Name: "Beta"})

	m.SendMessage("sess-a", "sess-b", "hello beta")
	m.persistMessages()

	m2 := newTestManager()
	m2.loadPersistedMessages()
	got := m2.GetMessages()
	found := false
	for _, msg := range got {
		if msg.Content == "hello beta" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected persisted message reloaded, got %d messages", len(got))
	}
}

// TestHandleAgentDeath_ReassignsPendingTasksToLead verifies a dying worker's
// pending tasks are reassigned to a strategic-tier agent with Source=reassigned.
func TestHandleAgentDeath_ReassignsPendingTasksToLead(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.RegisterAgent("sess-lead", "lead", &TeamMemberConfig{ID: "lead", Name: "Lead", Tier: "strategic"})
	m.RegisterAgent("sess-worker", "worker", &TeamMemberConfig{ID: "worker", Name: "Worker", Tier: "worker"})
	worker := m.GetAgent("sess-worker")
	lead := m.GetAgent("sess-lead")
	worker.AddTask(&Task{ID: "p1", Title: "Pending work", Status: TaskPending})
	worker.AddTask(&Task{ID: "done", Title: "Completed", Status: TaskCompleted}) // not reassigned

	m.HandleAgentDeath("sess-worker")

	// Worker is unregistered.
	if m.GetAgent("sess-worker") != nil {
		t.Error("expected the dead worker to be unregistered")
	}
	// Lead received the reassigned pending task with the reassigned source.
	var reassigned *Task
	for _, tk := range lead.GetTasksCopy() {
		if tk.Title == "Pending work" {
			reassigned = tk
		}
	}
	if reassigned == nil {
		t.Fatal("expected the pending task reassigned to the lead")
	}
	if reassigned.Source != "reassigned" {
		t.Errorf("expected Source 'reassigned', got %q", reassigned.Source)
	}
	// The completed task must NOT have been reassigned.
	for _, tk := range lead.GetTasksCopy() {
		if tk.Title == "Completed" {
			t.Error("completed task should not be reassigned")
		}
	}
}

// TestHandleAgentDeath_UnknownSessionNoOp covers the early-return for an
// unknown session and the idempotent ClaimDeath double-call guard.
func TestHandleAgentDeath_UnknownSessionNoOp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	// Unknown session -> safe no-op.
	m.HandleAgentDeath("ghost")

	m.RegisterAgent("sess-a", "alpha", &TeamMemberConfig{ID: "alpha", Name: "Alpha", Tier: "worker"})
	// First death runs; a second death on the now-unregistered session is a no-op.
	m.HandleAgentDeath("sess-a")
	m.HandleAgentDeath("sess-a")
}
