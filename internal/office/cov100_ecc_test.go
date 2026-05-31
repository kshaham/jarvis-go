package office

import (
	"embed"
	"testing"
)

//go:embed testdata_ecc/ecc_agents/*.md testdata_ecc/ecc_agents/*.txt testdata_ecc/ecc_commands/*.md testdata_ecc/ecc_commands/*.txt
var testECCFS embed.FS

// TestParseEmbeddedAgents_SkipsAndRejects drives the non-.md skip and the
// safeBasename rejection (the evil.md fixture has a path-traversal name in
// its frontmatter) inside parseEmbeddedAgents.
func TestParseEmbeddedAgents_SkipsAndRejects(t *testing.T) {
	agents := parseEmbeddedAgents(testECCFS, "testdata_ecc/ecc_agents")
	// Only the good agent should survive: evil rejected, notes.txt skipped.
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent (good), got %d: %+v", len(agents), agents)
	}
	if agents[0].Name != "goodagent" {
		t.Errorf("expected goodagent, got %q", agents[0].Name)
	}
	if agents[0].Model != "sonnet" {
		t.Errorf("expected model sonnet, got %q", agents[0].Model)
	}
	if len(agents[0].Tools) != 2 {
		t.Errorf("expected 2 tools, got %v", agents[0].Tools)
	}
}

// TestParseEmbeddedCommands_SkipsNonMarkdown drives the non-.md skip in
// parseEmbeddedCommands.
func TestParseEmbeddedCommands_SkipsNonMarkdown(t *testing.T) {
	cmds := parseEmbeddedCommands(testECCFS, "testdata_ecc/ecc_commands")
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %d: %+v", len(cmds), cmds)
	}
	if cmds[0].Name != "cmd" {
		t.Errorf("expected cmd, got %q", cmds[0].Name)
	}
	// splitFrontmatter keeps the raw scalar (including surrounding quotes).
	if cmds[0].Model != "haiku" || cmds[0].ArgumentHint != `"<arg>"` {
		t.Errorf("unexpected command metadata: %+v", cmds[0])
	}
}

// TestParseEmbedded_MissingRoot drives the ReadDir error branch for a root
// that doesn't exist in the embed FS.
func TestParseEmbedded_MissingRoot(t *testing.T) {
	if got := parseEmbeddedAgents(testECCFS, "no/such/dir"); got != nil {
		t.Errorf("expected nil for missing agents root, got %v", got)
	}
	if got := parseEmbeddedCommands(testECCFS, "no/such/dir"); got != nil {
		t.Errorf("expected nil for missing commands root, got %v", got)
	}
}

// TestSplitFrontmatter_NoClosingDelimiter drives the end==-1 branch: the
// content opens a frontmatter block but never closes it.
func TestSplitFrontmatter_NoClosingDelimiter(t *testing.T) {
	fm, body := splitFrontmatter("---\nname: x\nno closing delimiter here\n")
	// With no closing "---", the function returns the original content as body
	// and an empty map (frontmatter is treated as absent / unterminated).
	if len(fm) != 0 {
		t.Errorf("expected empty frontmatter map for unterminated block, got %v", fm)
	}
	if body == "" {
		t.Error("expected body to retain the original content")
	}
}
