package office

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildKickstartPrompt_AllSections drives the optional-section
// branches of BuildKickstartPrompt that fire only when their inputs are
// non-empty: the Team Mission block (kickstart.go ~50-54), the project
// invariants block (~63-66), the Project Context block (~70-74), and the
// Teammate Insights cross-agent block (~100-104).
func TestBuildKickstartPrompt_AllSections(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Seed a project doc so BuildProjectContext returns non-empty.
	repo := t.TempDir()
	// Ensure this repo path isn't cached from a prior test.
	projectDocsMu.Lock()
	delete(projectDocsCache, repo)
	projectDocsMu.Unlock()
	if err := os.WriteFile(filepath.Join(repo, "CLAUDE.md"),
		[]byte("# Repo\n\nThis project does important things.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Seed a teammate's learning file so BuildCrossAgentContextCompact
	// returns non-empty for our agent (configID "me").
	learnDir := filepath.Join(home, ".jarvis", "learnings")
	if err := os.MkdirAll(learnDir, 0o755); err != nil {
		t.Fatal(err)
	}
	peer := AgentLearningData{
		ConfigID:  "peer",
		Name:      "Peer",
		Learnings: []AgentLearning{{Category: "tech", Content: "peer insight worth sharing"}},
	}
	pdata, _ := json.Marshal(peer)
	if err := os.WriteFile(filepath.Join(learnDir, "peer.json"), pdata, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &TeamMemberConfig{ID: "me", Name: "Me", Role: "Backend", Tier: "worker"}
	prompt := BuildKickstartPrompt(
		cfg,
		repo,
		nil,
		"Ship the platform", // teamMission
		"",                  // agentMemory
		true,                // perpetualMode
		[]string{"No force-push to main", "Always run tests"}, // invariants
		false, // sevenStepMode
	)

	for _, want := range []string{
		"## Team Mission",
		"Ship the platform",
		"No force-push to main", // invariants block
		"## Project Context",
		"important things", // from seeded CLAUDE.md
		"## Teammate Insights",
		"peer insight worth sharing",
		"## Identity",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("kickstart prompt missing %q\n---\n%s", want, prompt)
		}
	}
}
