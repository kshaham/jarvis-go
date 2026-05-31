package office

import (
	"embed"
	"testing"
)

// testECCCmdsDotFS embeds a commands dir containing a dotfile fixture
// (.hidden.md). The `all:` prefix is required because the default
// go:embed matcher skips entries whose name begins with `.` or `_`.
// The .hidden.md basename (minus .md) is ".hidden", which safeBasename
// rejects (leading dot), driving the previously-uncovered rejection
// branch of parseEmbeddedCommands (the !ok continue at ecc.go:140-143).
//
//go:embed all:testdata_ecc/ecc_cmds_dot
var testECCCmdsDotFS embed.FS

func TestParseEmbeddedCommands_RejectsUnsafeBasename(t *testing.T) {
	cmds := parseEmbeddedCommands(testECCCmdsDotFS, "testdata_ecc/ecc_cmds_dot")
	// Only valid.md should survive; .hidden.md is rejected by safeBasename.
	if len(cmds) != 1 {
		t.Fatalf("expected exactly 1 command (valid), got %d: %+v", len(cmds), cmds)
	}
	if cmds[0].Name != "valid" {
		t.Errorf("expected surviving command 'valid', got %q", cmds[0].Name)
	}
	// Confirm the rejected entry truly didn't slip through under any name.
	for _, c := range cmds {
		if c.Name == ".hidden" || c.Name == "hidden" {
			t.Errorf("unsafe-basename command leaked through: %q", c.Name)
		}
	}
}
