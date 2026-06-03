package teamtemplates

import "testing"

// The "Demo Jarvis" built-in powers the one-click demo button. It must
// stay tiny (3 members) and well-formed: the handler that spawns it
// references the template ID and the member IDs by literal string, so a
// rename here would silently break the demo's per-member overrides.
func TestBuiltInTemplates_DemoJarvis(t *testing.T) {
	var demo *TeamTemplate
	for i := range mustBuiltins(t) {
		if BuiltInTemplates()[i].ID == "demo-jarvis" {
			tpl := BuiltInTemplates()[i]
			demo = &tpl
			break
		}
	}
	if demo == nil {
		t.Fatal("demo-jarvis template not registered in BuiltInTemplates()")
	}

	if demo.Category != "demo" {
		t.Errorf("category=%q, want %q", demo.Category, "demo")
	}
	if !demo.IsBuiltIn {
		t.Error("demo-jarvis should be marked IsBuiltIn")
	}
	if len(demo.Members) != 3 {
		t.Fatalf("demo-jarvis members=%d, want 3", len(demo.Members))
	}

	// IDs are load-bearing: handler.OfficeDemoSpawn pins per-member
	// MaxTurns/Effort by these exact IDs.
	wantIDs := map[string]bool{"atlas": true, "pixel": true, "byte": true}
	strategic := 0
	for _, mbr := range demo.Members {
		if !wantIDs[mbr.ID] {
			t.Errorf("unexpected member ID %q", mbr.ID)
		}
		delete(wantIDs, mbr.ID)
		if mbr.Tier == "strategic" {
			strategic++
		}
		if mbr.SystemPrompt == "" || mbr.Personality == "" {
			t.Errorf("member %q must have a personality + system prompt", mbr.ID)
		}
		if len(mbr.Skills) == 0 {
			t.Errorf("member %q must list at least one skill", mbr.ID)
		}
	}
	if len(wantIDs) != 0 {
		t.Errorf("missing expected member IDs: %v", wantIDs)
	}
	if strategic != 1 {
		t.Errorf("demo-jarvis should have exactly one strategic lead, got %d", strategic)
	}
}

// mustBuiltins returns the builtin slice and fails if it's empty (keeps
// the index loop above honest if the slice ever regresses to empty).
func mustBuiltins(t *testing.T) []TeamTemplate {
	t.Helper()
	tpls := BuiltInTemplates()
	if len(tpls) == 0 {
		t.Fatal("BuiltInTemplates() returned no templates")
	}
	return tpls
}
