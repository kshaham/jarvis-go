package main

import (
	"strings"
	"testing"
)

// R3.3 · sanity coverage for the typed payload schemas + helpers.
// Production paths through standup / brainstorm / leader-tasks have
// to validate cleanly without hitting the network, so the helpers
// stay deterministic + decoupled from HTTP.

func TestDecodeStandupJSON_HappyPath(t *testing.T) {
	body := `{"status":"Done auth","blockers":["lead sign-off"],"next":["mobile sync"]}`
	report, rendered, err := DecodeStandupJSON(body)
	if err != nil {
		t.Fatalf("DecodeStandupJSON: %v", err)
	}
	if report.Status != "Done auth" {
		t.Errorf("Status=%q", report.Status)
	}
	if !strings.Contains(rendered, "lead sign-off") {
		t.Errorf("rendered should contain blockers: %q", rendered)
	}
}

func TestDecodeStandupJSON_MissingStatusFails(t *testing.T) {
	_, _, err := DecodeStandupJSON(`{}`)
	if err == nil {
		t.Fatal("expected error when status is missing")
	}
}

func TestDecodeBrainstormJSON_RequiresAtLeastOneProposal(t *testing.T) {
	_, _, err := DecodeBrainstormJSON(`{"proposals":[]}`)
	if err == nil {
		t.Fatal("expected error for empty proposals list")
	}
}

func TestDecodeBrainstormJSON_RejectsInvalidPriority(t *testing.T) {
	_, _, err := DecodeBrainstormJSON(`{"proposals":[{"title":"x","priority":"urgent"}]}`)
	if err == nil {
		t.Fatal("expected error for invalid priority")
	}
}

func TestDecodeBrainstormJSON_RendersHumanText(t *testing.T) {
	_, rendered, err := DecodeBrainstormJSON(`{"proposals":[{"title":"Cache providers","priority":"high","owner":"bolt"}]}`)
	if err != nil {
		t.Fatalf("DecodeBrainstormJSON: %v", err)
	}
	if !strings.Contains(rendered, "Cache providers") || !strings.Contains(rendered, "[high]") || !strings.Contains(rendered, "@bolt") {
		t.Errorf("rendered missing expected substrings: %q", rendered)
	}
}

func TestDecodeLeaderTasksJSON_RequiresAgent(t *testing.T) {
	_, _, err := DecodeLeaderTasksJSON(`{"assignments":[{"title":"x"}]}`)
	if err == nil {
		t.Fatal("expected error when agent is missing")
	}
}

func TestExtractFlag_FoundAndRemoved(t *testing.T) {
	args := []string{"a", "--json", `{"x":1}`, "b"}
	value, ok := extractFlag(&args, "--json")
	if !ok {
		t.Fatal("expected --json to be found")
	}
	if value != `{"x":1}` {
		t.Errorf("value=%q", value)
	}
	if len(args) != 2 || args[0] != "a" || args[1] != "b" {
		t.Errorf("args after extract: %v", args)
	}
}

func TestExtractFlag_NotPresent(t *testing.T) {
	args := []string{"a", "b"}
	value, ok := extractFlag(&args, "--json")
	if ok {
		t.Error("expected --json to be missing")
	}
	if value != "" {
		t.Errorf("value should be empty: %q", value)
	}
	if len(args) != 2 {
		t.Errorf("args mutated: %v", args)
	}
}

func TestSchemaExampleFor_ReturnsValidJSONForKnownCmds(t *testing.T) {
	for _, name := range []string{"standup", "brainstorm", "leader-tasks"} {
		ex := SchemaExampleFor(name)
		if ex == "" {
			t.Errorf("missing example for %s", name)
			continue
		}
		// Cheap structural check: must contain a brace.
		if !strings.Contains(ex, "{") {
			t.Errorf("example for %s isn't JSON-ish: %q", name, ex)
		}
	}
}
