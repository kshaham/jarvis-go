package sse

import (
	"testing"
)

func TestClient_MatchesSubscription_All(t *testing.T) {
	c := &Client{subscriptions: nil}
	if !c.MatchesSubscription("anything") {
		t.Error("nil subscriptions should match all")
	}
}

func TestClient_MatchesSubscription_Exact(t *testing.T) {
	c := &Client{subscriptions: []string{"session:created"}}
	if !c.MatchesSubscription("session:created") {
		t.Error("exact match should work")
	}
	if c.MatchesSubscription("session:deleted") {
		t.Error("non-matching should not match")
	}
}

func TestClient_MatchesSubscription_Wildcard(t *testing.T) {
	c := &Client{subscriptions: []string{"session:*"}}
	if !c.MatchesSubscription("session:created") {
		t.Error("wildcard should match session:created")
	}
	if !c.MatchesSubscription("session:output") {
		t.Error("wildcard should match session:output")
	}
	if c.MatchesSubscription("office:standupStarted") {
		t.Error("wildcard should not match office events")
	}
}

func TestClient_MatchesSubscription_Multiple(t *testing.T) {
	c := &Client{subscriptions: []string{"session:*", "office:*"}}
	if !c.MatchesSubscription("session:created") {
		t.Error("should match session")
	}
	if !c.MatchesSubscription("office:standupStarted") {
		t.Error("should match office")
	}
	if c.MatchesSubscription("respawn:started") {
		t.Error("should not match respawn")
	}
}
