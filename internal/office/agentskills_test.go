package office

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAgentSkills_BuildCommands verifies all 7 phase commands parse with the
// expected names + descriptions + body attribution.
func TestAgentSkills_BuildCommands(t *testing.T) {
	cmds := BuildAgentSkillsCommands()
	want := map[string]bool{
		"spec": true, "plan": true, "build": true, "test": true,
		"review": true, "code-simplify": true, "ship": true,
	}
	if len(cmds) != len(want) {
		t.Fatalf("expected %d commands, got %d", len(want), len(cmds))
	}
	for _, c := range cmds {
		if !want[c.Name] {
			t.Errorf("unexpected command %q", c.Name)
		}
		if c.Description == "" {
			t.Errorf("command %q missing description", c.Name)
		}
		if !hasAgentSkillsAttribution(c.Body) {
			t.Errorf("command %q missing MIT attribution in body", c.Name)
		}
	}
}

// TestAgentSkills_BuildAgents verifies the 4 personas parse with names + attribution.
func TestAgentSkills_BuildAgents(t *testing.T) {
	agents := BuildAgentSkillsAgents()
	want := map[string]bool{
		"code-reviewer": true, "security-auditor": true,
		"test-engineer": true, "web-performance-auditor": true,
	}
	if len(agents) != len(want) {
		t.Fatalf("expected %d agents, got %d", len(want), len(agents))
	}
	for _, a := range agents {
		if !want[a.Name] {
			t.Errorf("unexpected agent %q", a.Name)
		}
		if a.Description == "" {
			t.Errorf("agent %q missing description", a.Name)
		}
		if !hasAgentSkillsAttribution(a.Body) {
			t.Errorf("agent %q missing MIT attribution in body", a.Name)
		}
	}
}

// TestAgentSkills_VendoredContentChecksumMatches re-hashes every vendored
// agent-skills file and compares against the recorded CHECKSUMS.sha256. Catches
// accidental in-tree edits that drift from upstream.
//
// To intentionally update content, regenerate via:
//
//	find skills/agent-skills internal/office/agentskills_agents internal/office/agentskills_commands \
//	  -name '*.md' -print0 | sort -z | xargs -0 shasum -a 256 \
//	  > skills/agent-skills/CHECKSUMS.sha256
func TestAgentSkills_VendoredContentChecksumMatches(t *testing.T) {
	repoRoot := repoRootFromTestFile(t)
	checksumPath := filepath.Join(repoRoot, skillsBundleRoot, "agent-skills", "CHECKSUMS.sha256")
	wantBySuffix, err := readChecksumFile(checksumPath)
	if err != nil {
		t.Fatalf("read %s: %v", checksumPath, err)
	}
	if len(wantBySuffix) == 0 {
		t.Fatalf("checksum file %s is empty", checksumPath)
	}

	// CHECKSUMS.sha256 records the bundled files under the original
	// skills/ prefix; recordedRel maps the relocated on-disk paths back so
	// content hashes match without regenerating the checksum file.
	gotBySuffix := map[string]string{}
	for _, root := range []string{
		filepath.Join(skillsBundleRoot, "agent-skills"),
		filepath.Join("internal", "office", "agentskills_agents"),
		filepath.Join("internal", "office", "agentskills_commands"),
	} {
		absRoot := filepath.Join(repoRoot, root)
		if err := filepath.WalkDir(absRoot, func(p string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
				return nil
			}
			relUnix := recordedRel(repoRoot, p)
			body, rerr := os.ReadFile(p)
			if rerr != nil {
				return rerr
			}
			sum := sha256.Sum256(body)
			gotBySuffix[relUnix] = hex.EncodeToString(sum[:])
			return nil
		}); err != nil {
			t.Fatalf("walk %s: %v", absRoot, err)
		}
	}

	for path, want := range wantBySuffix {
		got, ok := gotBySuffix[path]
		if !ok {
			t.Errorf("checksum recorded for %s but file is missing", path)
			continue
		}
		if got != want {
			t.Errorf("checksum mismatch for %s: regenerate CHECKSUMS.sha256 after intentional edits", path)
		}
	}
	for path := range gotBySuffix {
		if _, ok := wantBySuffix[path]; !ok {
			t.Errorf("vendored file %s has no recorded checksum; regenerate CHECKSUMS.sha256", path)
		}
	}
}
