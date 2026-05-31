package peers

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestStore_AppendOutboundAndList(t *testing.T) {
	store := NewStore(t.TempDir())
	msg := RelayMessage{
		ToPeerID: "peerA",
		Content:  "hello A",
	}
	if err := store.AppendOutbound(msg); err != nil {
		t.Fatalf("AppendOutbound: %v", err)
	}
	all, err := store.List("")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 message, got %d", len(all))
	}
	got := all[0]
	if got.ID == "" {
		t.Fatal("AppendOutbound should assign an ID")
	}
	if got.Direction != "outbound" {
		t.Fatalf("expected outbound direction, got %q", got.Direction)
	}
	if got.CreatedAt.IsZero() {
		t.Fatal("CreatedAt should be defaulted")
	}
	if got.Content != "hello A" {
		t.Fatalf("content mismatch: %q", got.Content)
	}
}

func TestStore_AppendOutboundKeepsProvidedID(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.AppendOutbound(RelayMessage{ID: "fixed-out", ToPeerID: "p"}); err != nil {
		t.Fatalf("AppendOutbound: %v", err)
	}
	all, _ := store.List("")
	if all[0].ID != "fixed-out" {
		t.Fatalf("provided outbound ID not kept: %q", all[0].ID)
	}
}

func TestStore_AppendInboundAssignsOwnID(t *testing.T) {
	store := NewStore(t.TempDir())
	// Malicious peer-supplied ID must be overwritten with a fresh UUID.
	msg := RelayMessage{
		ID:         "../peers/peers",
		FromPeerID: "peerB",
		Content:    "inbound body",
	}
	if err := store.AppendInbound(msg); err != nil {
		t.Fatalf("AppendInbound: %v", err)
	}
	all, _ := store.List("")
	if len(all) != 1 {
		t.Fatalf("expected 1 message, got %d", len(all))
	}
	got := all[0]
	if got.ID == "../peers/peers" {
		t.Fatal("AppendInbound must NOT trust peer-supplied ID")
	}
	if got.ID == "" {
		t.Fatal("AppendInbound should assign a fresh ID")
	}
	if got.Direction != "inbound" {
		t.Fatalf("expected inbound direction, got %q", got.Direction)
	}
}

func TestStore_ListFilterByPeer(t *testing.T) {
	store := NewStore(t.TempDir())
	// Outbound to A, outbound to B, inbound from A.
	_ = store.AppendOutbound(RelayMessage{ToPeerID: "A", Content: "to A"})
	_ = store.AppendOutbound(RelayMessage{ToPeerID: "B", Content: "to B"})
	_ = store.AppendInbound(RelayMessage{FromPeerID: "A", Content: "from A"})

	onlyA, err := store.List("A")
	if err != nil {
		t.Fatalf("List(A): %v", err)
	}
	if len(onlyA) != 2 {
		t.Fatalf("expected 2 messages for A (one each direction), got %d", len(onlyA))
	}
	for _, m := range onlyA {
		isToA := m.Direction == "outbound" && m.ToPeerID == "A"
		isFromA := m.Direction == "inbound" && m.FromPeerID == "A"
		if !isToA && !isFromA {
			t.Fatalf("List(A) returned unrelated message: %+v", m)
		}
	}

	all, _ := store.List("")
	if len(all) != 3 {
		t.Fatalf("expected 3 total messages, got %d", len(all))
	}
}

func TestStore_ListSortedByCreatedAt(t *testing.T) {
	store := NewStore(t.TempDir())
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	_ = store.AppendOutbound(RelayMessage{ID: "late", ToPeerID: "p", CreatedAt: base.Add(3 * time.Hour)})
	_ = store.AppendInbound(RelayMessage{FromPeerID: "p", CreatedAt: base.Add(1 * time.Hour)})
	_ = store.AppendOutbound(RelayMessage{ID: "mid", ToPeerID: "p", CreatedAt: base.Add(2 * time.Hour)})

	all, _ := store.List("")
	if len(all) != 3 {
		t.Fatalf("expected 3, got %d", len(all))
	}
	for i := 1; i < len(all); i++ {
		if all[i].CreatedAt.Before(all[i-1].CreatedAt) {
			t.Fatalf("List not sorted ascending at index %d", i)
		}
	}
}

func TestStore_MarkReadInbound(t *testing.T) {
	store := NewStore(t.TempDir())
	_ = store.AppendInbound(RelayMessage{FromPeerID: "p", Content: "x"})
	all, _ := store.List("")
	id := all[0].ID

	if err := store.MarkRead(id); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	all, _ = store.List("")
	if all[0].ReadAt == nil {
		t.Fatal("ReadAt should be set after MarkRead")
	}
}

func TestStore_MarkReadOutbound(t *testing.T) {
	store := NewStore(t.TempDir())
	_ = store.AppendOutbound(RelayMessage{ID: "out1", ToPeerID: "p"})
	if err := store.MarkRead("out1"); err != nil {
		t.Fatalf("MarkRead outbound: %v", err)
	}
	all, _ := store.List("")
	if all[0].ReadAt == nil {
		t.Fatal("outbound ReadAt should be set")
	}
}

func TestStore_MarkReadNotFound(t *testing.T) {
	store := NewStore(t.TempDir())
	err := store.MarkRead("does-not-exist")
	if err == nil {
		t.Fatal("expected not-found error from MarkRead")
	}
}

func TestStore_MarkDelivered(t *testing.T) {
	store := NewStore(t.TempDir())
	_ = store.AppendOutbound(RelayMessage{ID: "deliverme", ToPeerID: "p"})
	when := time.Date(2024, 7, 4, 12, 0, 0, 0, time.UTC)
	if err := store.MarkDelivered("deliverme", when); err != nil {
		t.Fatalf("MarkDelivered: %v", err)
	}
	all, _ := store.List("")
	if all[0].DeliveredAt == nil || !all[0].DeliveredAt.Equal(when) {
		t.Fatalf("DeliveredAt not set correctly: %v", all[0].DeliveredAt)
	}
}

func TestStore_MarkDeliveredNotFound(t *testing.T) {
	store := NewStore(t.TempDir())
	// Even an inbound message can't be marked delivered (outbound-only).
	_ = store.AppendInbound(RelayMessage{FromPeerID: "p"})
	err := store.MarkDelivered("nope", time.Now())
	if err == nil {
		t.Fatal("expected not-found from MarkDelivered")
	}
}

func TestStore_UnreadInboundByPeer(t *testing.T) {
	store := NewStore(t.TempDir())
	_ = store.AppendInbound(RelayMessage{FromPeerID: "A"})
	_ = store.AppendInbound(RelayMessage{FromPeerID: "A"})
	_ = store.AppendInbound(RelayMessage{FromPeerID: "B"})
	// Outbound messages don't count toward unread inbound.
	_ = store.AppendOutbound(RelayMessage{ToPeerID: "A"})

	counts, err := store.UnreadInboundByPeer()
	if err != nil {
		t.Fatalf("UnreadInboundByPeer: %v", err)
	}
	if counts["A"] != 2 {
		t.Fatalf("expected 2 unread from A, got %d", counts["A"])
	}
	if counts["B"] != 1 {
		t.Fatalf("expected 1 unread from B, got %d", counts["B"])
	}

	// Mark one of A's messages read; count should drop.
	allA, _ := store.List("A")
	var inboundID string
	for _, m := range allA {
		if m.Direction == "inbound" {
			inboundID = m.ID
			break
		}
	}
	if err := store.MarkRead(inboundID); err != nil {
		t.Fatal(err)
	}
	counts, _ = store.UnreadInboundByPeer()
	if counts["A"] != 1 {
		t.Fatalf("expected 1 unread from A after MarkRead, got %d", counts["A"])
	}
}

func TestStore_PersistAcrossInstances(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)
	_ = store.AppendOutbound(RelayMessage{ToPeerID: "p", Content: "persisted out"})
	_ = store.AppendInbound(RelayMessage{FromPeerID: "p", Content: "persisted in"})

	// Files on disk.
	if _, err := os.Stat(filepath.Join(root, "sent")); err != nil {
		t.Fatalf("sent dir not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "inbox")); err != nil {
		t.Fatalf("inbox dir not created: %v", err)
	}

	// Fresh store reads from the same root.
	store2 := NewStore(root)
	all, err := store2.List("")
	if err != nil {
		t.Fatalf("reloaded List: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 persisted messages, got %d", len(all))
	}
}

func TestStore_LoadFixesDirectionFromBucket(t *testing.T) {
	root := t.TempDir()
	inbox := filepath.Join(root, "inbox")
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		t.Fatal(err)
	}
	// A record claiming to be outbound, but living in the inbox dir.
	raw := `{"id":"mislabelled","fromPeerId":"p","direction":"outbound","content":"x"}`
	if err := os.WriteFile(filepath.Join(inbox, "mislabelled.json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewStore(root)
	all, err := store.List("")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 loaded message, got %d", len(all))
	}
	if all[0].Direction != "inbound" {
		t.Fatalf("direction should be corrected to inbound from bucket, got %q", all[0].Direction)
	}
}

func TestStore_LoadSkipsCorruptAndNonJSON(t *testing.T) {
	root := t.TempDir()
	inbox := filepath.Join(root, "inbox")
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		t.Fatal(err)
	}
	// Valid record.
	good := `{"id":"good","fromPeerId":"p","content":"ok"}`
	if err := os.WriteFile(filepath.Join(inbox, "good.json"), []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}
	// Corrupt JSON (skipped).
	if err := os.WriteFile(filepath.Join(inbox, "bad.json"), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Record with empty ID (skipped).
	if err := os.WriteFile(filepath.Join(inbox, "empty.json"), []byte(`{"id":"","content":"y"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Non-JSON extension (skipped).
	if err := os.WriteFile(filepath.Join(inbox, "notes.txt"), []byte("ignore me"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A subdirectory (skipped).
	if err := os.MkdirAll(filepath.Join(inbox, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}

	store := NewStore(root)
	all, err := store.List("")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 1 || all[0].ID != "good" {
		t.Fatalf("expected only the good record, got %+v", all)
	}
}

func TestStore_EmptyRootDegradedMode(t *testing.T) {
	store := NewStore("")
	if err := store.AppendOutbound(RelayMessage{ToPeerID: "p", Content: "x"}); err != nil {
		t.Fatalf("AppendOutbound degraded: %v", err)
	}
	if err := store.AppendInbound(RelayMessage{FromPeerID: "p", Content: "y"}); err != nil {
		t.Fatalf("AppendInbound degraded: %v", err)
	}
	all, err := store.List("")
	if err != nil {
		t.Fatalf("List degraded: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 in-memory messages, got %d", len(all))
	}
	// MarkRead/MarkDelivered should still operate on the in-memory index.
	var outID string
	for _, m := range all {
		if m.Direction == "outbound" {
			outID = m.ID
		}
	}
	if err := store.MarkDelivered(outID, time.Now().UTC()); err != nil {
		t.Fatalf("MarkDelivered degraded: %v", err)
	}
}

func TestStore_PersistLockedRejectsTraversalID(t *testing.T) {
	// Directly exercise the persistLocked guard via AppendOutbound with a
	// crafted ID that contains a path separator. persistLocked should
	// reject it rather than escaping the bucket.
	root := t.TempDir()
	store := NewStore(root)
	err := store.AppendOutbound(RelayMessage{ID: "evil/../escape", ToPeerID: "p"})
	if err == nil {
		t.Fatal("expected error for traversal-containing ID")
	}
}

func TestStore_ConcurrentAppendAndList(t *testing.T) {
	store := NewStore(t.TempDir())
	var wg sync.WaitGroup
	const n = 24
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			if idx%2 == 0 {
				_ = store.AppendOutbound(RelayMessage{ToPeerID: "p", Content: "o"})
			} else {
				_ = store.AppendInbound(RelayMessage{FromPeerID: "p", Content: "i"})
			}
			_, _ = store.List("")
			_, _ = store.UnreadInboundByPeer()
		}(i)
	}
	wg.Wait()
	all, err := store.List("")
	if err != nil {
		t.Fatalf("final List: %v", err)
	}
	if len(all) != n {
		t.Fatalf("expected %d messages, got %d", n, len(all))
	}
}

func TestDefaultRelayDir(t *testing.T) {
	got := DefaultRelayDir()
	if got != "" {
		if filepath.Base(got) != "relay" {
			t.Fatalf("unexpected default relay dir: %q", got)
		}
	}
}
