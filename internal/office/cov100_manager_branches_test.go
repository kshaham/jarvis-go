package office

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestInjectRelayMessage_NilConfigAgentSkipped verifies the roster loop in
// InjectRelayMessage skips an agent with a nil Config and still delivers to a
// valid target.
func TestInjectRelayMessage_NilConfigAgentSkipped(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.SetTeamSpawned(true)

	// One nil-config agent (skipped by the loop's Config==nil guard) plus a
	// strategic agent so routing is deterministic (the strategic branch
	// selects it regardless of map iteration order).
	m.RegisterAgent("sess-nil", "nilcfg", &TeamMemberConfig{ID: "nilcfg", Name: "Nil", Tier: "worker"})
	nilAgent := m.GetAgent("sess-nil")
	nilAgent.mu.Lock()
	nilAgent.Config = nil
	nilAgent.mu.Unlock()

	m.RegisterAgent("sess-ok", "okcfg", &TeamMemberConfig{ID: "okcfg", Name: "Ok", Tier: "strategic"})

	live := m.InjectRelayMessage("peer-1", "Peer Team", "hello neighbours")
	if !live {
		t.Fatal("expected relay to be delivered to a live target")
	}
	// The valid agent should have received a relay message.
	msgs := m.GetMessages()
	found := false
	for _, msg := range msgs {
		if msg.To == "sess-ok" {
			found = true
		}
	}
	if !found {
		t.Error("expected relay routed to the valid (non-nil-config) agent")
	}
}

// TestSetDevInstance_NilMapInitialised drives the lazy nil-map init in
// SetDevInstance by nilling teamDevInstances first.
func TestSetDevInstance_NilMapInitialised(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.mu.Lock()
	m.teamDevInstances = nil
	m.mu.Unlock()

	if err := m.SetDevInstance("app", "http://jarvis:3000", 1234, false); err != nil {
		t.Fatalf("SetDevInstance: %v", err)
	}
	if url := m.GetTeamDevURL(); url != "http://jarvis:3000" {
		t.Errorf("expected URL persisted, got %q", url)
	}
}

// TestSetDevInstanceCredentials_DefaultSlotAndNilInst drives the name=="" ->
// defaultDevSlot rename and the creating-a-creds-only-slot branch (inst nil).
func TestSetDevInstanceCredentials_DefaultSlotAndNilInst(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.mu.Lock()
	m.teamDevInstances = nil // force nil-map init AND nil-inst path
	m.mu.Unlock()

	// Empty name -> defaultDevSlot; no existing inst -> creds-only slot created.
	if err := m.SetDevInstanceCredentials("", "user:pass"); err != nil {
		t.Fatalf("SetDevInstanceCredentials: %v", err)
	}
	creds := m.GetTeamDevCredentials()
	if creds != "user:pass" {
		t.Errorf("expected creds stored on default slot, got %q", creds)
	}
}

// TestCollectDescendants_CycleGuard drives the visited[c] continue branch: the
// stub reports a child that, when queried, lists itself -> already-visited.
func TestCollectDescendants_CycleGuard(t *testing.T) {
	orig := execCommand
	t.Cleanup(func() { execCommand = orig })
	execCommand = func(name string, args ...string) *exec.Cmd {
		parent := ""
		if len(args) > 0 {
			parent = args[len(args)-1]
		}
		switch parent {
		case "9000":
			// root has one child 9001
			return exec.Command("printf", "%s", "9001\n")
		case "9001":
			// 9001 reports BOTH a new child 9002 and itself (9001), so the
			// self-reference exercises the visited[c] continue.
			return exec.Command("printf", "%s", "9001\n9002\n")
		default:
			return exec.Command("sh", "-c", "exit 1") // no children
		}
	}

	got := collectDescendants(9000)
	// Expect 9001 then 9002, with the duplicate 9001 skipped by the guard.
	if len(got) != 2 {
		t.Fatalf("expected 2 descendants (9001, 9002), got %v", got)
	}
	seen := map[int]bool{}
	for _, pid := range got {
		if seen[pid] {
			t.Errorf("descendant %d appeared twice; cycle guard failed: %v", pid, got)
		}
		seen[pid] = true
	}
}

// TestSubmitLeaderTasksResponse_LateAfterClosedSlot drives the late-response
// branch: the agent's LeaderTaskSlot is closed (finished) before the response
// arrives, so deliver() returns false AND closed.Load() is true.
func TestSubmitLeaderTasksResponse_LateAfterClosedSlot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.RegisterAgent("sess-late", "late", &TeamMemberConfig{ID: "late", Name: "Late", Tier: "strategic"})
	agent := m.GetAgent("sess-late")

	slot := newWaitSlot()
	slot.finish() // close it so deliver() returns false and closed==true
	agent.mu.Lock()
	agent.LeaderTaskSlot = slot
	agent.mu.Unlock()

	before := LateResponsesDropped()
	if !m.SubmitLeaderTasksResponse("sess-late", "here are the tasks") {
		t.Fatal("expected SubmitLeaderTasksResponse to return true (agent exists)")
	}
	if LateResponsesDropped() <= before {
		t.Error("expected late-response counter to increment for a closed slot")
	}
}

// TestSubmitReflectionResponse_ChannelFullHitsDefault drives the `default:`
// arm of the reflection channel select: the buffered ReflectionCh is already
// full so the non-blocking send can't proceed.
func TestSubmitReflectionResponse_ChannelFullHitsDefault(t *testing.T) {
	_, cleanup := withTempLearningsDir(t)
	defer cleanup()
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.RegisterAgent("sess-full", "full", &TeamMemberConfig{ID: "full", Name: "Full", Tier: "worker"})
	agent := m.GetAgent("sess-full")

	// A buffered channel pre-filled to capacity -> the select hits default.
	ch := make(chan string, 1)
	ch <- "already queued"
	agent.mu.Lock()
	agent.ReflectionCh = ch
	agent.mu.Unlock()

	if !m.SubmitReflectionResponse("sess-full", "new learning", "technique", "", "") {
		t.Fatal("expected reflection submit to succeed even when channel is full")
	}
	// The original queued value must remain (the new one was dropped via default).
	select {
	case v := <-ch:
		if v != "already queued" {
			t.Errorf("expected original channel value preserved, got %q", v)
		}
	default:
		t.Error("expected the pre-filled channel value to still be present")
	}
}

// TestSubmitReflectionResponse_MemoryAppendError drives the MemoryAppendSection
// error branch: lastWorkingDir is set but the agent-memory parent path is a
// regular file, so writeMemorySections' MkdirAll fails.
func TestSubmitReflectionResponse_MemoryAppendError(t *testing.T) {
	// withTempLearningsDir sets HOME to its own temp dir, so plant the
	// blocker AFTER it (and read the active HOME from the env).
	_, cleanup := withTempLearningsDir(t)
	defer cleanup()
	home := os.Getenv("HOME")

	// Make $HOME/.jarvis/agent-memory a FILE so the ReadFile/MkdirAll of the
	// per-project subdir under it fails inside MemoryAppendSection.
	jarvisDir := filepath.Join(home, ".jarvis")
	if err := os.MkdirAll(jarvisDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jarvisDir, "agent-memory"), []byte("blocker"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := newTestManager()
	m.mu.Lock()
	m.lastWorkingDir = "/proj/some-where"
	m.mu.Unlock()
	m.RegisterAgent("sess-mae", "mae", &TeamMemberConfig{ID: "mae", Name: "Mae", Tier: "worker"})

	// Should still return true: the memory-append failure is logged, not fatal.
	if !m.SubmitReflectionResponse("sess-mae", "lesson learned", "technique", "", "") {
		t.Fatal("expected reflection submit to succeed despite memory-append failure")
	}
}

// ---------------------------------------------------------------------------
// os.UserHomeDir error guards (HOME unset)
// ---------------------------------------------------------------------------

// blockTmpFile pre-creates a named tmpfile path inside office-memory AS A
// DIRECTORY so os.WriteFile(tmp, ...) fails with EISDIR. This is deterministic
// and (unlike a chmod read-only dir) doesn't race the fire-and-forget persist
// goroutines that other tests spawn.
func blockTmpFile(t *testing.T, tmpName string) {
	t.Helper()
	home := os.Getenv("HOME")
	dir := filepath.Join(home, ".jarvis", "office-memory")
	if err := os.MkdirAll(filepath.Join(dir, tmpName), 0o755); err != nil {
		t.Fatal(err)
	}
}

// TestPersistMessages_WriteTmpFails drives the WriteFile-tmpfile error branch:
// the messages.json.tmp path is a directory so the atomic tmpfile write fails.
func TestPersistMessages_WriteTmpFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	m.SendMessage("a", "b", "hi")
	blockTmpFile(t, "messages.json.tmp")
	// Must not panic; the WriteFile error path logs+returns.
	m.persistMessages()
}

// TestPersistMetrics_WriteTmpFails drives the WriteFile-tmpfile error branch in
// PersistMetrics via a metrics.json.tmp directory.
func TestPersistMetrics_WriteTmpFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	blockTmpFile(t, "metrics.json.tmp")
	m.PersistMetrics()
}

// TestPersistDevInstance_WriteTmpFails drives the WriteFile-tmpfile error
// branch in persistDevInstance via a team-dev-app.json.tmp directory.
func TestPersistDevInstance_WriteTmpFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()
	blockTmpFile(t, "team-dev-app.json.tmp")
	m.persistDevInstance("app", &devInstance{URL: "http://jarvis:3000", PID: 42})
}

// TestPersistAndLoad_HomeUnset drives the os.UserHomeDir error early-returns in
// persistMessages, loadPersistedMessages, PersistMetrics, and
// LoadPersistedMetrics by clearing HOME.
func TestPersistAndLoad_HomeUnset(t *testing.T) {
	m := newTestManager()
	// Clear HOME so os.UserHomeDir returns an error on darwin/linux.
	t.Setenv("HOME", "")

	// None of these should panic; each must hit its UserHomeDir error return.
	m.persistMessages()
	m.loadPersistedMessages()
	m.PersistMetrics()
	m.LoadPersistedMetrics()
}
