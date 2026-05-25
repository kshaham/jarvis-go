package claudehooks

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildSettings_Deterministic(t *testing.T) {
	a, err := BuildSettings(SettingsParams{SessionID: "sess-0001", DaemonURL: "http://127.0.0.1:3555"})
	if err != nil {
		t.Fatalf("BuildSettings failed: %v", err)
	}
	b, err := BuildSettings(SettingsParams{SessionID: "sess-0001", DaemonURL: "http://127.0.0.1:3555"})
	if err != nil {
		t.Fatalf("BuildSettings failed (second call): %v", err)
	}
	if string(a) != string(b) {
		t.Errorf("BuildSettings not deterministic.\nfirst:\n%s\nsecond:\n%s", a, b)
	}
}

func TestBuildSettings_RequiredFields(t *testing.T) {
	if _, err := BuildSettings(SettingsParams{}); err == nil {
		t.Error("expected error when SessionID + DaemonURL empty")
	}
	if _, err := BuildSettings(SettingsParams{SessionID: "x"}); err == nil {
		t.Error("expected error when DaemonURL empty")
	}
	if _, err := BuildSettings(SettingsParams{DaemonURL: "x"}); err == nil {
		t.Error("expected error when SessionID empty")
	}
}

func TestBuildSettings_Shape(t *testing.T) {
	data, err := BuildSettings(SettingsParams{
		SessionID: "sess-vex",
		DaemonURL: "http://127.0.0.1:3555",
	})
	if err != nil {
		t.Fatalf("BuildSettings: %v", err)
	}

	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("emitted invalid JSON: %v", err)
	}

	// Sentinel marker so RemoveSettings/WriteSettings can distinguish
	// our files from operator-authored ones.
	if marker, _ := doc["_jarvisGenerated"].(bool); !marker {
		t.Error("expected _jarvisGenerated=true marker at top level")
	}

	hooks, ok := doc["hooks"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected hooks map at top level, got: %T", doc["hooks"])
	}

	for _, event := range HookedEvents {
		bindings, ok := hooks[event].([]interface{})
		if !ok {
			t.Errorf("missing hook bindings for event %q", event)
			continue
		}
		if len(bindings) == 0 {
			t.Errorf("empty bindings for event %q", event)
			continue
		}
		first := bindings[0].(map[string]interface{})
		entries := first["hooks"].([]interface{})
		entry := entries[0].(map[string]interface{})

		// URL must point at the per-session route.
		wantURL := "http://127.0.0.1:3555/api/claude/hooks/sess-vex/" + event
		if got, _ := entry["url"].(string); got != wantURL {
			t.Errorf("event %q: url = %q, want %q", event, got, wantURL)
		}
		if got, _ := entry["type"].(string); got != "http" {
			t.Errorf("event %q: type = %q, want http", event, got)
		}
		// Auth header uses env-var interpolation so the token never
		// appears in the on-disk file.
		headers := entry["headers"].(map[string]interface{})
		if got, _ := headers["X-Jarvis-Agent-Token"].(string); got != "${JARVIS_AGENT_TOKEN}" {
			t.Errorf("event %q: missing/incorrect token header: %q", event, got)
		}
	}
}

func TestBuildSettings_StopHasLongerTimeout(t *testing.T) {
	// Stop tail-reads the transcript -- 30s ceiling vs the 5s for
	// observational events. Reflected in the generated config so
	// Claude Code doesn't cancel the call mid-read on a slow disk.
	data, _ := BuildSettings(SettingsParams{SessionID: "sid", DaemonURL: "http://x"})
	var doc map[string]interface{}
	_ = json.Unmarshal(data, &doc)
	hooks := doc["hooks"].(map[string]interface{})
	stopBindings := hooks["Stop"].([]interface{})
	stopEntry := stopBindings[0].(map[string]interface{})["hooks"].([]interface{})[0].(map[string]interface{})
	stopTimeout, _ := stopEntry["timeout"].(float64)
	if stopTimeout != 30 {
		t.Errorf("Stop timeout = %v, want 30", stopTimeout)
	}
	preToolBindings := hooks["PreToolUse"].([]interface{})
	preToolEntry := preToolBindings[0].(map[string]interface{})["hooks"].([]interface{})[0].(map[string]interface{})
	if t1, _ := preToolEntry["timeout"].(float64); t1 != 5 {
		t.Errorf("PreToolUse timeout = %v, want 5", t1)
	}
}

func TestWriteSettings_FreshDir(t *testing.T) {
	dir := t.TempDir()
	data, _ := BuildSettings(SettingsParams{SessionID: "sid", DaemonURL: "http://x"})
	if err := WriteSettings(dir, data); err != nil {
		t.Fatalf("WriteSettings: %v", err)
	}
	target := filepath.Join(dir, ".claude", "settings.local.json")
	contents, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("written file unreadable: %v", err)
	}
	if string(contents) != string(data) {
		t.Errorf("contents drift: got %d bytes, wanted %d", len(contents), len(data))
	}
}

func TestWriteSettings_OverwritesOwnFile(t *testing.T) {
	dir := t.TempDir()
	first, _ := BuildSettings(SettingsParams{SessionID: "sid-1", DaemonURL: "http://x"})
	second, _ := BuildSettings(SettingsParams{SessionID: "sid-2", DaemonURL: "http://x"})
	if err := WriteSettings(dir, first); err != nil {
		t.Fatalf("first write failed: %v", err)
	}
	if err := WriteSettings(dir, second); err != nil {
		t.Fatalf("second write should succeed (Jarvis file): %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
	if !strings.Contains(string(got), "sid-2") {
		t.Errorf("expected sid-2 in overwritten file, got: %s", got)
	}
}

func TestWriteSettings_RefusesOperatorFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, ".claude")
	_ = os.MkdirAll(target, 0o755)
	// Operator-authored file (no _jarvisGenerated marker).
	if err := os.WriteFile(filepath.Join(target, "settings.local.json"), []byte(`{"hooks":{}}`), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	data, _ := BuildSettings(SettingsParams{SessionID: "sid", DaemonURL: "http://x"})
	err := WriteSettings(dir, data)
	if !errors.Is(err, ErrOperatorOwned) {
		t.Errorf("expected ErrOperatorOwned, got: %v", err)
	}
}

func TestRemoveSettings_OnlyOurFiles(t *testing.T) {
	dir := t.TempDir()

	// Own file -> removed.
	data, _ := BuildSettings(SettingsParams{SessionID: "sid", DaemonURL: "http://x"})
	_ = WriteSettings(dir, data)
	if err := RemoveSettings(dir); err != nil {
		t.Fatalf("RemoveSettings (own file): %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "settings.local.json")); !os.IsNotExist(err) {
		t.Error("file should be gone after RemoveSettings")
	}

	// No-op when file doesn't exist.
	if err := RemoveSettings(dir); err != nil {
		t.Errorf("RemoveSettings (missing): %v", err)
	}

	// Operator file -> ErrOperatorOwned, file preserved.
	_ = os.MkdirAll(filepath.Join(dir, ".claude"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, ".claude", "settings.local.json"), []byte(`{"hooks":{}}`), 0o644)
	err := RemoveSettings(dir)
	if !errors.Is(err, ErrOperatorOwned) {
		t.Errorf("expected ErrOperatorOwned for operator file, got: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "settings.local.json")); err != nil {
		t.Error("operator file should be preserved")
	}
}
