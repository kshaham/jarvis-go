package handler

// Round-2 batch: IO-error 500 arms reached HONESTLY via real filesystem faults.
//   - OfficeAgentDeleteSkill: os.RemoveAll fails when the skills root is
//     read-only -> 500.
//   - RelayList: relay inbox planted as a regular file -> os.ReadDir ENOTDIR
//     inside ensureLoaded -> 500.
//   - atomicWriteFile: os.Rename fails when the destination is a non-empty
//     directory.

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/kshaham/jarvis/internal/peers"
)

func TestOfficeAgentDeleteSkill_RemoveAllError500(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory write permissions")
	}
	ctx := newTimelineContext(t) // isolates HOME
	home, _ := os.UserHomeDir()
	skillsRoot := filepath.Join(home, ".claude", "skills")
	skillDir := filepath.Join(skillsRoot, "doomed")
	if err := os.MkdirAll(filepath.Join(skillDir, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	// Make the skills root read-only so RemoveAll of the child entry fails.
	if err := os.Chmod(skillsRoot, 0o555); err != nil {
		t.Fatalf("chmod skills root: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(skillsRoot, 0o755) })

	req := withURLParam(httptest.NewRequest("DELETE", "/api/office/agent/skills/doomed", nil), "name", "doomed")
	rec := httptest.NewRecorder()
	ctx.OfficeAgentDeleteSkill(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when RemoveAll fails, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRelayList_StoreListError500(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	relayRoot := filepath.Join(dir, "relay")
	// The relay store's inbox dir is <root>/inbox. Plant a regular FILE there so
	// ensureLoaded's os.ReadDir fails with ENOTDIR (a non-IsNotExist error).
	if err := os.MkdirAll(relayRoot, 0o755); err != nil {
		t.Fatalf("mkdir relay root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(relayRoot, "inbox"), []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("plant inbox file: %v", err)
	}
	reg, err := peers.NewRegistry(filepath.Join(dir, "peers.json"))
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	identity := peers.NewIdentityStore(filepath.Join(dir, "identity.json"))
	svc := &peers.Service{
		Identity: identity,
		Registry: reg,
		Store:    peers.NewStore(relayRoot),
		Client:   peers.NewClient(),
	}
	p := &Peers{Identity: identity, Registry: reg, Relay: svc}

	req := httptest.NewRequest("GET", "/api/relay/messages", nil)
	rec := httptest.NewRecorder()
	p.RelayList(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when relay store List fails, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAtomicWriteFile_RenameError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Destination is itself a non-empty DIRECTORY -> os.Rename(tmp, dest) fails.
	dest := filepath.Join(dir, "dest")
	if err := os.MkdirAll(filepath.Join(dest, "child"), 0o755); err != nil {
		t.Fatalf("mkdir dest: %v", err)
	}
	err := atomicWriteFile(dest, []byte("payload"), 0o644)
	if err == nil {
		t.Fatal("expected error when renaming over a non-empty directory")
	}
	// The temp file must be cleaned up (no .tmp orphans left in dir).
	matches, _ := filepath.Glob(filepath.Join(dir, ".*tmp"))
	if len(matches) != 0 {
		t.Errorf("expected no orphaned temp files, found %v", matches)
	}
}
