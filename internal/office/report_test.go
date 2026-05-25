package office

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestValidateReportFilename(t *testing.T) {
	cases := []struct {
		name string
		ok   bool
	}{
		{"wrap-up-20260421-013000.md", true},
		{"wrap-up-20260101-235959.md", true},
		{"../../etc/passwd", false},
		{"report.md", false},
		{"wrap-up-20260421-013000.md.exe", false},
		{"wrap-up-20260421.md", false},
		{"../wrap-up-20260421-013000.md", false},
		{"", false},
		{"wrap-up-BADBADBA-000000.md", false}, // non-digit year
	}
	for _, c := range cases {
		got := ValidateReportFilename(c.name)
		if got != c.ok {
			t.Errorf("ValidateReportFilename(%q) = %v, want %v", c.name, got, c.ok)
		}
	}
}

func TestGenerateAndBroadcastWrapUpReport_BroadcastsOnSuccess(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	var captured struct {
		event string
		meta  *WrapUpReportMeta
	}
	broadcast := func(event string, data interface{}) {
		captured.event = event
		captured.meta, _ = data.(*WrapUpReportMeta)
	}
	GenerateAndBroadcastWrapUpReport(m, "manual", "test", broadcast)
	if captured.event != "office:reportGenerated" {
		t.Errorf("event=%q want office:reportGenerated", captured.event)
	}
	if captured.meta == nil {
		t.Fatal("no metadata broadcast")
	}
	if !ValidateReportFilename(captured.meta.Filename) {
		t.Errorf("bad filename: %s", captured.meta.Filename)
	}
}

func TestGenerateAndBroadcastWrapUpReport_NilBroadcastNoCrash(t *testing.T) {
	// Documented contract: nil broadcast is safe (test path).
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	// Must not panic.
	GenerateAndBroadcastWrapUpReport(m, "manual", "", nil)
}

func TestGenerateReport_EmptyTeam(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	meta, err := GenerateWrapUpReport(m, "manual", "")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !ValidateReportFilename(meta.Filename) {
		t.Errorf("bad filename: %s", meta.Filename)
	}
	if meta.SizeBytes <= 0 {
		t.Error("report should have non-zero size")
	}
	body, err := LoadWrapUpReport(meta.Filename)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !strings.Contains(body, "# Team Wrap-Up Report") {
		t.Error("body missing header")
	}
	if !strings.Contains(body, "**Trigger:** manual") {
		t.Error("body missing trigger line")
	}
	if !strings.Contains(body, "_No agents in roster._") {
		t.Error("empty-team body should say no agents")
	}
}

func TestGenerateReport_PopulatedTeam(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.SetTeamMission("Ship the Q2 reliability initiative.")
	m.RegisterAgent("s-vex", "vex", &TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend", Tier: "worker"})
	m.RegisterAgent("s-bolt", "bolt", &TeamMemberConfig{ID: "bolt", Name: "Bolt", Role: "Backend", Tier: "worker"})
	vex := m.GetAgent("s-vex")
	vex.AddTask(&Task{
		ID: "t-a", Title: "ship auth page", Status: TaskCompleted,
		CompletedAt: time.Now(),
	})
	vex.AddTask(&Task{
		ID: "t-b", Title: "refactor nav", Status: TaskInProgress,
	})

	meta, err := GenerateWrapUpReport(m, "wrap-up", "operator closing team")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	body, err := LoadWrapUpReport(meta.Filename)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// Mission
	if !strings.Contains(body, "Q2 reliability") {
		t.Error("body missing mission")
	}
	// Per-agent section
	if !strings.Contains(body, "### Vex") {
		t.Error("body missing Vex agent section")
	}
	if !strings.Contains(body, "### Bolt") {
		t.Error("body missing Bolt agent section")
	}
	// Completed task appears
	if !strings.Contains(body, "ship auth page") {
		t.Error("completed task not listed")
	}
	// In-flight task appears
	if !strings.Contains(body, "refactor nav") {
		t.Error("in-flight task not listed")
	}
	// Metadata extracted into the meta struct.
	if meta.Trigger != "wrap-up" {
		t.Errorf("meta.Trigger = %q, want 'wrap-up'", meta.Trigger)
	}
}

func TestListReports_RoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	// Generate three reports; each gets a distinct timestamp. The
	// filename uses seconds precision, so sleep 1s between to avoid
	// collisions.
	for i := 0; i < 3; i++ {
		if _, err := GenerateWrapUpReport(m, "manual", ""); err != nil {
			t.Fatalf("generate %d: %v", i, err)
		}
		time.Sleep(1100 * time.Millisecond)
	}
	list, err := ListWrapUpReports()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 reports, got %d", len(list))
	}
	// Newest first -- filenames should be in descending order.
	for i := 1; i < len(list); i++ {
		if list[i-1].Filename <= list[i].Filename {
			t.Errorf("expected newest-first ordering; got %s then %s",
				list[i-1].Filename, list[i].Filename)
		}
	}
	// Each should be loadable.
	for _, m := range list {
		body, err := LoadWrapUpReport(m.Filename)
		if err != nil {
			t.Errorf("load %s: %v", m.Filename, err)
		}
		if !strings.Contains(body, "# Team Wrap-Up Report") {
			t.Errorf("body of %s missing header", m.Filename)
		}
	}
}

func TestPruneOldReports(t *testing.T) {
	dir := t.TempDir()
	// Create 7 valid-looking report files.
	for _, name := range []string{
		"wrap-up-20260421-000001.md",
		"wrap-up-20260421-000002.md",
		"wrap-up-20260421-000003.md",
		"wrap-up-20260421-000004.md",
		"wrap-up-20260421-000005.md",
		"wrap-up-20260421-000006.md",
		"wrap-up-20260421-000007.md",
	} {
		if err := os.WriteFile(dir+"/"+name, []byte("x"), 0o644); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	if err := pruneOldReports(dir, 3); err != nil {
		t.Fatalf("prune: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 3 {
		t.Fatalf("expected 3 after prune, got %d", len(entries))
	}
	// The 3 survivors must be the newest (highest timestamp suffix).
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	for _, want := range []string{
		"wrap-up-20260421-000005.md",
		"wrap-up-20260421-000006.md",
		"wrap-up-20260421-000007.md",
	} {
		found := false
		for _, n := range names {
			if n == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %s to survive, got %v", want, names)
		}
	}
}
