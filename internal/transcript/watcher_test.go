package transcript

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDetectHookEvent(t *testing.T) {
	tests := []struct {
		input    string
		expected HookEvent
	}{
		{"permission_prompt", HookPermissionPrompt},
		{"elicitation_dialog", HookElicitation},
		{"idle_prompt", HookIdlePrompt},
		{"stop", HookStop},
		{"random_type", ""},
	}
	for _, tc := range tests {
		if got := detectHookEvent(tc.input); got != tc.expected {
			t.Errorf("detectHookEvent(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestWatcher_NewAndStop(t *testing.T) {
	w := NewWatcher(context.Background(), "test-session")
	w.Stop()
}

func TestWatcher_ReadNewContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	os.WriteFile(path, []byte(`{"type":"stop"}`+"\n"), 0644)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w := NewWatcher(ctx, "test-session")
	w.transcriptPath = path
	w.readNewContent()

	select {
	case event := <-w.Events():
		if e, ok := event.(HookDetectedEvent); !ok || e.Hook != HookStop {
			t.Errorf("expected HookStop, got %v", event)
		}
	case <-time.After(1 * time.Second):
		t.Error("no event received")
	}
}
