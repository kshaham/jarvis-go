package handler

// Direct unit coverage for the liveTeamAgentSet / liveTeamWorkingDir helpers
// used to scope knowledge searches to the running team. Covers the empty-roster
// (nil) branch and the populated branch where ConfigID-bearing agents are
// collected into the allow set.

import (
	"testing"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/web/sse"
)

func liveTeamCtx(t *testing.T) *Context {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	return &Context{
		SessionManager: pty.NewSessionManager(10),
		SSEBroker:      sse.NewBroker(100, 10),
		OfficeManager:  office.NewManager(stubSessions{}, func(string, interface{}) {}, nil),
	}
}

func TestLiveTeamAgentSet_EmptyRosterNil(t *testing.T) {
	ctx := liveTeamCtx(t)
	if got := liveTeamAgentSet(ctx); got != nil {
		t.Errorf("empty roster should yield nil set, got %v", got)
	}
}

func TestLiveTeamAgentSet_CollectsConfigIDs(t *testing.T) {
	ctx := liveTeamCtx(t)
	ctx.OfficeManager.RegisterAgent("sess-a", "vex", &office.TeamMemberConfig{ID: "vex", Name: "Vex"})
	ctx.OfficeManager.RegisterAgent("sess-b", "ana", &office.TeamMemberConfig{ID: "ana", Name: "Ana"})

	set := liveTeamAgentSet(ctx)
	if len(set) != 2 {
		t.Fatalf("expected 2 configIDs in allow set, got %d: %v", len(set), set)
	}
	if !set["vex"] || !set["ana"] {
		t.Errorf("expected vex+ana in set, got %v", set)
	}
}

func TestLiveTeamWorkingDir_EmptyWhenNoSpawnContext(t *testing.T) {
	ctx := liveTeamCtx(t)
	if got := liveTeamWorkingDir(ctx); got != "" {
		t.Errorf("expected empty workingDir with no spawn context, got %q", got)
	}
}
