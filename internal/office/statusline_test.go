package office

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteStatusLine_CreatesBothFiles(t *testing.T) {
	dir := t.TempDir()
	err := WriteStatusLine(dir, StatusLineParams{
		SessionID: "s-1",
		DaemonURL: "http://127.0.0.1:3555",
	})
	if err != nil {
		t.Fatalf("WriteStatusLine: %v", err)
	}
	script, err := os.ReadFile(filepath.Join(dir, ".claude", statuslineScriptName))
	if err != nil {
		t.Fatalf("read script: %v", err)
	}
	if !strings.Contains(string(script), jarvisStatusLineMarker) {
		t.Error("script missing jarvis marker")
	}
	if !strings.Contains(string(script), "/api/office/agent/statusline?sessionId=s-1") {
		t.Errorf("script doesn't embed sessionId: %q", script)
	}
	if !strings.Contains(string(script), "JARVIS_AGENT_TOKEN") {
		t.Error("script missing token env-var reference")
	}
	info, err := os.Stat(filepath.Join(dir, ".claude", statuslineScriptName))
	if err != nil {
		t.Fatalf("stat script: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("script should be executable, mode=%v", info.Mode())
	}

	style, err := os.ReadFile(filepath.Join(dir, ".claude", outputStyleFileName))
	if err != nil {
		t.Fatalf("read style: %v", err)
	}
	if !strings.Contains(string(style), `"_jarvisGenerated": true`) {
		t.Error("style missing jarvis marker")
	}
	if !strings.Contains(string(style), `"statusLine"`) {
		t.Error("style missing statusLine block")
	}
}

func TestWriteStatusLine_RequiresSessionAndDaemon(t *testing.T) {
	dir := t.TempDir()
	if err := WriteStatusLine(dir, StatusLineParams{}); err == nil {
		t.Error("expected error for missing sessionId + daemonURL")
	}
	if err := WriteStatusLine(dir, StatusLineParams{SessionID: "s"}); err == nil {
		t.Error("expected error for missing daemonURL")
	}
}

func TestWriteStatusLine_LeavesOperatorAuthored(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	operatorScript := "#!/bin/sh\n# operator's custom statusline\necho hi\n"
	os.WriteFile(filepath.Join(dir, ".claude", statuslineScriptName), []byte(operatorScript), 0o755)
	operatorStyle := `{"statusLine":{"type":"command","command":"my-thing"}}`
	os.WriteFile(filepath.Join(dir, ".claude", outputStyleFileName), []byte(operatorStyle), 0o644)

	err := WriteStatusLine(dir, StatusLineParams{
		SessionID: "s-1",
		DaemonURL: "http://127.0.0.1:3555",
	})
	if err != nil {
		t.Fatalf("WriteStatusLine: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, ".claude", statuslineScriptName))
	if !strings.Contains(string(got), "operator's custom") {
		t.Errorf("operator script was overwritten: %q", got)
	}
	gotStyle, _ := os.ReadFile(filepath.Join(dir, ".claude", outputStyleFileName))
	if !strings.Contains(string(gotStyle), "my-thing") {
		t.Errorf("operator style was overwritten: %q", gotStyle)
	}
}

func TestRemoveStatusLine_OnlyRemovesJarvisAuthored(t *testing.T) {
	dir := t.TempDir()
	if err := WriteStatusLine(dir, StatusLineParams{
		SessionID: "s-1",
		DaemonURL: "http://127.0.0.1:3555",
	}); err != nil {
		t.Fatalf("WriteStatusLine: %v", err)
	}
	if err := RemoveStatusLine(dir); err != nil {
		t.Fatalf("RemoveStatusLine: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", statuslineScriptName)); !os.IsNotExist(err) {
		t.Error("statusline script should have been removed")
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", outputStyleFileName)); !os.IsNotExist(err) {
		t.Error("output style should have been removed")
	}
}

func TestRenderStatusLineScript_UsesCustomAuthVar(t *testing.T) {
	p := StatusLineParams{SessionID: "s-1", DaemonURL: "http://localhost:3555"}
	out := renderStatusLineScript(p, "CUSTOM_TOKEN")
	if !strings.Contains(out, "${CUSTOM_TOKEN:-}") {
		t.Errorf("custom auth env var not used: %q", out)
	}
}
