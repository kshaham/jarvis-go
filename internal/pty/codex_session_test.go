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
	// `codex exec` (>= the deployed 0.135.0) hard-errors on --ask-for-approval;
	// approval comes from config.toml approval_policy. Guard against anyone
	// re-adding the flag, and pin the rest of the invocation shape.
	first := codexExecArgs("", "/work/dir", "do a thing")
	for _, a := range first {
		if a == "--ask-for-approval" {
			t.Fatalf("codexExecArgs must NOT pass --ask-for-approval (rejected by codex exec); got %v", first)
		}
	}
	if first[0] != "exec" {
		t.Errorf("args[0]=%q want exec", first[0])
	}
	want := []string{"exec", "--json", "--skip-git-repo-check", "--sandbox", "danger-full-access", "-C", "/work/dir", "do a thing"}
	if len(first) != len(want) {
		t.Fatalf("first-turn args=%v want %v", first, want)
	}
	for i := range want {
		if first[i] != want[i] {
			t.Errorf("first-turn args[%d]=%q want %q", i, first[i], want[i])
		}
	}
	// Resume turns insert `resume <threadID>` right after the subcommand.
	resumed := codexExecArgs("thread-123", "/work/dir", "next")
	if resumed[1] != "resume" || resumed[2] != "thread-123" {
		t.Errorf("resume args=%v want [exec resume thread-123 ...]", resumed)
	}
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
