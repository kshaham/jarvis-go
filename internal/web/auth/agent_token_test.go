package auth

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestAgentTokenManager_GeneratesNewToken(t *testing.T) {
	dir := t.TempDir()
	m := NewAgentTokenManager(dir)

	tok := m.Token()
	if tok == "" {
		t.Fatal("expected a generated token, got empty string")
	}
	if len(tok) != agentTokenBytes*2 {
		t.Fatalf("token length = %d, want %d (hex of %d bytes)", len(tok), agentTokenBytes*2, agentTokenBytes)
	}

	// Token must be persisted to disk at configDir/agent-token with 0600.
	path := filepath.Join(dir, "agent-token")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("agent token file not written: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("agent token file perm = %o, want 0600", perm)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read token file: %v", err)
	}
	if strings.TrimSpace(string(data)) != tok {
		t.Errorf("persisted token %q != in-memory token %q", strings.TrimSpace(string(data)), tok)
	}
}

func TestAgentTokenManager_ValidateMatches(t *testing.T) {
	m := NewAgentTokenManager(t.TempDir())
	tok := m.Token()

	if !m.Validate(tok) {
		t.Error("Validate rejected the correct token")
	}
	if m.Validate("") {
		t.Error("Validate accepted an empty presented token")
	}
	if m.Validate("wrong") {
		t.Error("Validate accepted a wrong-length token")
	}
	// Same length but different value must fail.
	wrong := strings.Repeat("a", len(tok))
	if wrong == tok {
		wrong = strings.Repeat("b", len(tok))
	}
	if m.Validate(wrong) {
		t.Error("Validate accepted a wrong same-length token")
	}
}

func TestAgentTokenManager_ValidateEmptyCurrentToken(t *testing.T) {
	// A manager whose token never loaded (cur == "") must reject everything,
	// including an empty presented token.
	m := &AgentTokenManager{}
	if m.Validate("") {
		t.Error("Validate accepted empty token when current token is empty")
	}
	if m.Validate("anything") {
		t.Error("Validate accepted a token when current token is empty")
	}
}

func TestAgentTokenManager_PersistsAcrossReload(t *testing.T) {
	dir := t.TempDir()
	m1 := NewAgentTokenManager(dir)
	tok1 := m1.Token()

	// A fresh manager over the same dir must load the same token, not regenerate.
	m2 := NewAgentTokenManager(dir)
	tok2 := m2.Token()

	if tok1 != tok2 {
		t.Errorf("reloaded token %q != original %q (should persist)", tok2, tok1)
	}
	if !m2.Validate(tok1) {
		t.Error("reloaded manager rejects original token")
	}
}

func TestAgentTokenManager_RegeneratesOnBadLength(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent-token")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	// Write a token file with the wrong length: it should be discarded and
	// a fresh valid token generated.
	if err := os.WriteFile(path, []byte("short"), 0600); err != nil {
		t.Fatal(err)
	}

	m := NewAgentTokenManager(dir)
	tok := m.Token()
	if len(tok) != agentTokenBytes*2 {
		t.Fatalf("expected regenerated token of length %d, got %d", agentTokenBytes*2, len(tok))
	}
	if tok == "short" {
		t.Error("manager kept the malformed token")
	}
	// The regenerated token must have been written back to disk.
	data, _ := os.ReadFile(path)
	if strings.TrimSpace(string(data)) != tok {
		t.Error("regenerated token not persisted")
	}
}

func TestAgentTokenManager_TrimsWhitespaceFromFile(t *testing.T) {
	dir := t.TempDir()
	// First create a valid token, then re-write it with surrounding whitespace
	// to prove loadOrCreate trims before checking length.
	m1 := NewAgentTokenManager(dir)
	tok := m1.Token()

	path := filepath.Join(dir, "agent-token")
	if err := os.WriteFile(path, []byte("\n  "+tok+"  \n"), 0600); err != nil {
		t.Fatal(err)
	}

	m2 := NewAgentTokenManager(dir)
	if m2.Token() != tok {
		t.Errorf("whitespace-padded token not loaded cleanly: got %q want %q", m2.Token(), tok)
	}
}

func TestAgentTokenManager_MkdirFailureKeepsEmptyToken(t *testing.T) {
	// Point the manager's config dir at a path under a regular file so
	// MkdirAll(filepath.Dir(path)) fails; the token stays empty and
	// Validate rejects everything.
	parent := t.TempDir()
	fileBlocker := filepath.Join(parent, "blocker")
	if err := os.WriteFile(fileBlocker, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	m := NewAgentTokenManager(filepath.Join(fileBlocker, "nested"))
	if m.Token() != "" {
		t.Errorf("expected empty token when config dir cannot be created, got %q", m.Token())
	}
	if m.Validate("anything") {
		t.Error("Validate should reject when token never generated")
	}
}

func TestAgentTokenManager_WriteFailureStillUsableInMemory(t *testing.T) {
	dir := t.TempDir()
	// Read-only dir: MkdirAll on an existing dir succeeds, but the atomic
	// temp write fails. The in-memory token is still set even though
	// persistence failed.
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0700) })

	m := NewAgentTokenManager(dir)
	tok := m.Token()
	if len(tok) != agentTokenBytes*2 {
		t.Fatalf("expected in-memory token even when persistence fails, got len %d", len(tok))
	}
	if !m.Validate(tok) {
		t.Error("in-memory token should validate even when not persisted")
	}
}

func TestAgentTokenManager_ValidateConcurrent(t *testing.T) {
	m := NewAgentTokenManager(t.TempDir())
	tok := m.Token()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !m.Validate(tok) {
				t.Error("concurrent Validate rejected correct token")
			}
			_ = m.Token()
		}()
	}
	wg.Wait()
}
