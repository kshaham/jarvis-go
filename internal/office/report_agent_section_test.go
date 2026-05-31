package office

import (
	"strings"
	"testing"
	"time"
)

// TestWriteAgentSection_FullDetails drives a wrap-up report through an agent
// that has recorded usage plus both completed and in-flight tasks, exercising
// the usage block, task-history, and in-flight sections of writeAgentSection.
func TestWriteAgentSection_FullDetails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.SetTeamSpawned(true)

	m.RegisterAgent("sess-a", "alpha", &TeamMemberConfig{
		ID: "alpha", Name: "Alpha", Role: "Backend Engineer", Tier: "worker",
	})
	a := m.GetAgent("sess-a")
	// Recorded usage so the usage block renders.
	a.RecordTurnUsage(1500, 800, 0.0123)
	// A completed task (with a completion time) + an in-flight task.
	a.AddTask(&Task{ID: "c1", Title: "Ship login", Status: TaskCompleted, CompletedAt: time.Now()})
	a.AddTask(&Task{ID: "p1", Title: "Refactor auth", Status: TaskInProgress})

	meta, err := GenerateWrapUpReport(m, "manual", "")
	if err != nil {
		t.Fatalf("GenerateWrapUpReport: %v", err)
	}
	body, err := LoadWrapUpReport(meta.Filename)
	if err != nil {
		t.Fatalf("LoadWrapUpReport: %v", err)
	}

	wants := []string{
		"### Alpha (Backend Engineer, worker tier)",
		"1 task completed",
		"**Usage:**",
		"input / ",
		"**Task history:**",
		"✅ Ship login",
		"**Still in flight at wrap-up:**",
		"Refactor auth",
	}
	for _, w := range wants {
		if !strings.Contains(body, w) {
			t.Errorf("report missing %q\n---\n%s", w, body)
		}
	}

	// The list metadata (readReportMeta / parseFilenameTimestamp) parses the
	// trigger back.
	reports, err := ListWrapUpReports()
	if err != nil {
		t.Fatalf("ListWrapUpReports: %v", err)
	}
	if len(reports) == 0 || reports[0].Trigger != "manual" {
		t.Errorf("expected the report listed with trigger 'manual', got %+v", reports)
	}
	if reports[0].GeneratedAt.IsZero() {
		t.Error("expected a parsed GeneratedAt from the filename timestamp")
	}
}

// TestGenerateAndBroadcastWrapUpReport_Broadcasts drives the broadcast wrapper
// (success path) and asserts the office:reportGenerated event fires with meta.
func TestGenerateAndBroadcastWrapUpReport_Broadcasts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.SetTeamSpawned(true)
	m.RegisterAgent("sess-a", "alpha", &TeamMemberConfig{ID: "alpha", Name: "Alpha"})

	var gotEvent string
	var gotMeta *WrapUpReportMeta
	GenerateAndBroadcastWrapUpReport(m, "auto", "idle wrap-up", func(event string, data interface{}) {
		gotEvent = event
		if meta, ok := data.(*WrapUpReportMeta); ok {
			gotMeta = meta
		}
	})

	if gotEvent != "office:reportGenerated" {
		t.Errorf("expected office:reportGenerated, got %q", gotEvent)
	}
	if gotMeta == nil || gotMeta.Trigger != "auto" || gotMeta.Reason != "idle wrap-up" {
		t.Errorf("unexpected report meta: %+v", gotMeta)
	}
}
