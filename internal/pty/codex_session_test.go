package pty

import (
	"os"
	"testing"
)

// drainEvents non-blockingly collects everything currently buffered on the
// session's events channel.
func drainEvents(s *CodexSession) []SessionEvent {
	var out []SessionEvent
	for {
		select {
		case ev := <-s.events:
			out = append(out, ev)
		default:
			return out
		}
	}
}

func TestCodexConsumeJSONL_Success(t *testing.T) {
	f, err := os.Open("testdata/codex_exec_success.jsonl")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()

	s := NewCodexSession(SessionConfig{Model: "gpt-5.5"})
	if saw := s.consumeJSONL(f); !saw {
		t.Fatal("expected turn.completed to be seen")
	}
	if got := s.ClaudeSessionID(); got != "019e7528-d102-7e32-875b-804e95f98e22" {
		t.Errorf("thread id = %q, want the fixture's thread_id", got)
	}

	evs := drainEvents(s)
	var (
		gotWorking bool
		gotTokens  *EventTokens
		gotOutput  string
	)
	for _, e := range evs {
		switch ev := e.(type) {
		case EventWorking:
			gotWorking = true
		case EventTokens:
			cp := ev
			gotTokens = &cp
		case EventOutput:
			gotOutput += ev.Data
		}
	}
	if !gotWorking {
		t.Error("expected EventWorking from turn.started")
	}
	if gotTokens == nil {
		t.Fatal("expected EventTokens from turn.completed")
	}
	if gotTokens.Input != 11739 || gotTokens.Output != 8 || gotTokens.Model != "gpt-5.5" {
		t.Errorf("tokens=%+v want in=11739 out=8 model=gpt-5.5", *gotTokens)
	}
	if gotOutput == "" || !contains(gotOutput, "hello from codex") {
		t.Errorf("agent_message output = %q, want it to contain 'hello from codex'", gotOutput)
	}
}

func TestCodexConsumeJSONL_Error(t *testing.T) {
	f, err := os.Open("testdata/codex_exec_error.jsonl")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()

	s := NewCodexSession(SessionConfig{Model: "gpt-5.5"})
	if saw := s.consumeJSONL(f); saw {
		t.Error("error fixture has no turn.completed; saw should be false")
	}
	var gotTurnError bool
	for _, e := range drainEvents(s) {
		if te, ok := e.(EventTurnError); ok {
			gotTurnError = true
			if te.Result == "" {
				t.Error("EventTurnError.Result should carry the failure message")
			}
		}
	}
	if !gotTurnError {
		t.Error("expected an EventTurnError from turn.failed")
	}
}

func TestCodexExecArgs_NoApprovalFlagAndShape(t *testing.T) {
	// Both turn shapes (verified against codex-cli 0.135.0) must omit
	// --ask-for-approval; resume turns must additionally omit --sandbox and
	// -C (the `resume` subcommand rejects them). Pin both invocations.
	first := codexExecArgs("", "/work/dir", "do a thing")
	wantFirst := []string{"exec", "--json", "--skip-git-repo-check", "-C", "/work/dir", "do a thing"}
	if len(first) != len(wantFirst) {
		t.Fatalf("first-turn args=%v want %v", first, wantFirst)
	}
	for i := range wantFirst {
		if first[i] != wantFirst[i] {
			t.Errorf("first-turn args[%d]=%q want %q", i, first[i], wantFirst[i])
		}
	}

	resumed := codexExecArgs("thread-123", "/work/dir", "next")
	wantResume := []string{"exec", "resume", "thread-123", "--json", "--skip-git-repo-check", "next"}
	if len(resumed) != len(wantResume) {
		t.Fatalf("resume args=%v want %v", resumed, wantResume)
	}
	for i := range wantResume {
		if resumed[i] != wantResume[i] {
			t.Errorf("resume args[%d]=%q want %q", i, resumed[i], wantResume[i])
		}
	}

	// Invariants: no --ask-for-approval anywhere; resume carries no --sandbox/-C.
	for _, a := range first {
		if a == "--ask-for-approval" {
			t.Errorf("first-turn must not pass --ask-for-approval; got %v", first)
		}
	}
	for _, a := range resumed {
		if a == "--ask-for-approval" || a == "--sandbox" || a == "-s" || a == "-C" || a == "--cd" {
			t.Errorf("resume turn must not pass %q (codex exec resume rejects it); got %v", a, resumed)
		}
	}
}

func TestCodexBuildEnv_SubscriptionStripsAPIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-should-be-stripped")
	t.Setenv("CODEX_API_KEY", "sk-should-be-stripped")

	// API-key mode: the inherited key is kept.
	apiSess := NewCodexSession(SessionConfig{Model: "gpt-5.4", CodexHome: "/tmp/h"})
	if !envHasKey(apiSess.buildEnv(), "OPENAI_API_KEY") {
		t.Error("api-key mode should keep inherited OPENAI_API_KEY")
	}

	// Subscription mode: OPENAI_API_KEY / CODEX_API_KEY are stripped so the
	// built-in provider uses auth.json (OAuth), and providerEnv can't reintroduce them.
	subSess := NewCodexSession(SessionConfig{
		Model:             "gpt-5.5",
		CodexHome:         "/tmp/h",
		CodexSubscription: true,
		ProviderEnv:       map[string]string{"OPENAI_API_KEY": "sk-from-provider", "JARVIS_API_URL": "http://localhost:3555"},
	})
	env := subSess.buildEnv()
	if envHasKey(env, "OPENAI_API_KEY") {
		t.Error("subscription mode must strip OPENAI_API_KEY from the subprocess env")
	}
	if envHasKey(env, "CODEX_API_KEY") {
		t.Error("subscription mode must strip CODEX_API_KEY from the subprocess env")
	}
	if !envHasKey(env, "JARVIS_API_URL") {
		t.Error("subscription mode must still pass non-key providerEnv (JARVIS_API_URL)")
	}
	if !envHasKey(env, "CODEX_HOME") {
		t.Error("CODEX_HOME must be set")
	}
}

// envHasKey reports whether a KEY=... entry exists in an os/exec-style env slice.
func envHasKey(env []string, key string) bool {
	for _, kv := range env {
		if len(kv) > len(key) && kv[:len(key)+1] == key+"=" {
			return true
		}
	}
	return false
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || indexOf(haystack, needle) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
