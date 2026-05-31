package pty

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestScreenManager_SaveScreens_MarshalError drives the marshal-error early
// return in saveScreens (screen.go:290-292). A fixed []*ScreenSession value can
// never fail to marshal in production, so we inject the failure through the
// saveScreensMarshal seam and assert nothing is written (the function returns
// before touching the filesystem).
func TestScreenManager_SaveScreens_MarshalError(t *testing.T) {
	dir := t.TempDir()
	sm := NewScreenManager(dir)
	sm.mu.Lock()
	sm.screens["x"] = &ScreenSession{SessionID: "x", ScreenName: "jarvis-x"}
	sm.mu.Unlock()

	orig := saveScreensMarshal
	saveScreensMarshal = func(any, string, string) ([]byte, error) {
		return nil, errors.New("marshal boom")
	}
	t.Cleanup(func() { saveScreensMarshal = orig })

	// Must not panic and must not write the screens file (it returns on error
	// before the MkdirAll/WriteFile).
	sm.saveScreens()

	if _, err := os.Stat(filepath.Join(dir, "screens.json")); !os.IsNotExist(err) {
		t.Errorf("screens.json should not exist after a marshal failure; stat err = %v", err)
	}
}
