package notifications

import (
	"path/filepath"
	"testing"
)

func TestStore_CreateAndGet(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "routes.json"))
	r := s.Create(Route{
		Name:    "stuck-to-slack",
		Enabled: true,
		Match:   MatchRule{EventPattern: "office:agentStuck"},
		Channels: []ChannelSpec{
			{Type: ChannelWebhook, Target: "https://hooks.example.com/x"},
		},
	})
	if r.ID == "" {
		t.Fatal("expected ID assigned")
	}
	got := s.Get(r.ID)
	if got == nil || got.Name != "stuck-to-slack" {
		t.Fatalf("get returned %+v", got)
	}
}

func TestStore_PersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routes.json")
	s1 := NewStore(path)
	r := s1.Create(Route{
		Name:     "keeper",
		Enabled:  true,
		Match:    MatchRule{EventPattern: "session:*"},
		Channels: []ChannelSpec{{Type: ChannelLog, Target: "standup"}},
	})

	s2 := NewStore(path)
	if s2.Get(r.ID) == nil {
		t.Fatal("expected route to survive reopen")
	}
	if len(s2.List()) != 1 {
		t.Errorf("expected 1 route after reopen, got %d", len(s2.List()))
	}
}

func TestStore_Update(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "routes.json"))
	r := s.Create(Route{
		Name: "orig", Enabled: true,
		Match:    MatchRule{EventPattern: "session:idle"},
		Channels: []ChannelSpec{{Type: ChannelLog, Target: "a"}},
	})
	updated, err := s.Update(r.ID, Route{
		Name: "renamed", Enabled: false,
		Match:    MatchRule{EventPattern: "session:*"},
		Channels: []ChannelSpec{{Type: ChannelLog, Target: "b"}},
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "renamed" || updated.Enabled {
		t.Fatalf("update did not apply: %+v", updated)
	}
}

func TestStore_DeleteNotFound(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "routes.json"))
	if err := s.Delete("no-such"); err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}
