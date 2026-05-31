package peers

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// makeParentAFile creates a regular file at parentPath so that any attempt
// to MkdirAll(parentPath) or write a child under it fails. Returns the
// path to a would-be child file under that "directory".
func makeParentAFile(t *testing.T, parentPath string) {
	t.Helper()
	if err := os.WriteFile(parentPath, []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestIdentityStore_SaveMkdirError(t *testing.T) {
	dir := t.TempDir()
	// Put a FILE where the parent directory needs to be, so MkdirAll fails.
	blocker := filepath.Join(dir, "blocker")
	makeParentAFile(t, blocker)
	// identity path lives "inside" the blocker file.
	path := filepath.Join(blocker, "identity.json")
	store := NewIdentityStore(path)

	_, err := store.Get()
	if err == nil {
		t.Fatal("expected save error when parent dir is a file")
	}
}

func TestRegistry_LoadReadError(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	makeParentAFile(t, blocker)
	// A path under a regular file yields a "not a directory" ReadFile error
	// that is NOT os.IsNotExist, so load() must surface it.
	path := filepath.Join(blocker, "peers.json")
	if _, err := NewRegistry(path); err == nil {
		t.Fatal("expected load error when path parent is a file")
	}
}

func TestRegistry_SaveMkdirError(t *testing.T) {
	dir := t.TempDir()
	// Start from a clean, writable registry, then sabotage the parent dir
	// so the NEXT saveLocked (via Add) fails on MkdirAll.
	subdir := filepath.Join(dir, "sub")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(subdir, "peers.json")
	reg, err := NewRegistry(path)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	// Replace the subdir with a regular file so MkdirAll(subdir) fails.
	if err := os.RemoveAll(subdir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(subdir, []byte("now a file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := reg.Add(newTestPeer("x")); err == nil {
		t.Fatal("expected save error when parent dir is a file")
	}
}

func TestStore_PersistMkdirError(t *testing.T) {
	dir := t.TempDir()
	// Block the inbox path: make a file where the inbox dir should be.
	blocker := filepath.Join(dir, "inbox")
	makeParentAFile(t, blocker)
	store := NewStore(dir)
	// AppendInbound → persistLocked → MkdirAll(inbox) fails because inbox is a file.
	err := store.AppendInbound(RelayMessage{FromPeerID: "p", Content: "x"})
	if err == nil {
		t.Fatal("expected mkdir error when inbox path is a file")
	}
}

func TestStore_EnsureLoadedPropagatesError(t *testing.T) {
	dir := t.TempDir()
	// Make the inbox path a FILE so os.ReadDir returns a non-NotExist error.
	inboxFile := filepath.Join(dir, "inbox")
	if err := os.WriteFile(inboxFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewStore(dir)
	// List → ensureLoaded → loadDir(inbox) → ReadDir on a file → error.
	_, err := store.List("")
	if err == nil {
		t.Fatal("expected loadDir error when inbox is a regular file")
	}
}

func TestStore_AppendOutboundEnsureLoadedError(t *testing.T) {
	dir := t.TempDir()
	// Block the SENT dir so ensureLoaded → loadDir(sent) errors.
	sentFile := filepath.Join(dir, "sent")
	if err := os.WriteFile(sentFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewStore(dir)
	err := store.AppendOutbound(RelayMessage{ToPeerID: "p", Content: "x"})
	if err == nil {
		t.Fatal("expected ensureLoaded error to surface from AppendOutbound")
	}
}

func TestStore_MarkReadEnsureLoadedError(t *testing.T) {
	dir := t.TempDir()
	inboxFile := filepath.Join(dir, "inbox")
	if err := os.WriteFile(inboxFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewStore(dir)
	if err := store.MarkRead("any"); err == nil {
		t.Fatal("expected ensureLoaded error from MarkRead")
	}
}

func TestStore_MarkDeliveredEnsureLoadedError(t *testing.T) {
	dir := t.TempDir()
	inboxFile := filepath.Join(dir, "inbox")
	if err := os.WriteFile(inboxFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewStore(dir)
	if err := store.MarkDelivered("any", time.Now()); err == nil {
		t.Fatal("expected ensureLoaded error from MarkDelivered")
	}
}

func TestStore_UnreadInboundEnsureLoadedError(t *testing.T) {
	dir := t.TempDir()
	inboxFile := filepath.Join(dir, "inbox")
	if err := os.WriteFile(inboxFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewStore(dir)
	if _, err := store.UnreadInboundByPeer(); err == nil {
		t.Fatal("expected ensureLoaded error from UnreadInboundByPeer")
	}
}

func TestService_SendOutboundPersistError(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(filepath.Join(dir, "peers.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Block the sent dir so AppendOutbound → ensureLoaded fails.
	if err := os.WriteFile(filepath.Join(dir, "sent"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := &Service{
		Identity: NewIdentityStore(filepath.Join(dir, "identity.json")),
		Registry: reg,
		Store:    NewStore(dir),
	}
	if err := svc.Registry.Add(Peer{PeerID: "p", PublicURL: "https://x.test", PairToken: "t"}); err != nil {
		t.Fatal(err)
	}
	_, err = svc.SendOutbound("p", "hi", nil)
	if err == nil {
		t.Fatal("expected persist outbound error")
	}
}

func TestIdentityStore_UpdateGetError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.json")
	// Corrupt file → Get (called at the top of Update) fails.
	if err := os.WriteFile(path, []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewIdentityStore(path)
	if _, err := store.Update("name", "url"); err == nil {
		t.Fatal("expected Update to surface the Get error on corrupt file")
	}
}

func TestIdentityStore_UpdateSaveError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "identity.json")
	store := NewIdentityStore(path)
	// Materialize the identity first (writes the file).
	if _, err := store.Get(); err != nil {
		t.Fatal(err)
	}
	// Now replace the parent dir with a file so the next saveLocked's
	// MkdirAll fails during Update.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, []byte("file now"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update("name", ""); err == nil {
		t.Fatal("expected Update save error when parent dir is a file")
	}
}

func TestStore_PersistWriteTmpError(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	// Pre-create the sent dir as read-only so MkdirAll succeeds (dir exists)
	// but WriteFile of the tmp file fails with permission denied.
	sent := filepath.Join(dir, "sent")
	if err := os.MkdirAll(sent, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sent, 0o755) })
	err := store.AppendOutbound(RelayMessage{ToPeerID: "p", Content: "x"})
	if err == nil {
		t.Fatal("expected write-tmp permission error in read-only dir")
	}
}

func TestService_ReceiveInboundPersistError(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(filepath.Join(dir, "peers.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Block inbox so AppendInbound → ensureLoaded(inbox) fails.
	if err := os.WriteFile(filepath.Join(dir, "inbox"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := &Service{
		Identity: NewIdentityStore(filepath.Join(dir, "identity.json")),
		Registry: reg,
		Store:    NewStore(dir),
	}
	if err := svc.ReceiveInbound(RelayMessage{FromPeerID: "p", Content: "x"}); err == nil {
		t.Fatal("expected persist inbound error")
	}
}
