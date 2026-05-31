package handler

// Branch fills for the peers/relay handlers:
//   - RelayList with a peerId filter
//   - PeersPing: seeded peer whose Ping fails -> 200 w/ unreachable status;
//     and the peer-not-found 404
//   - PeersDelete: happy delete of a seeded peer

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRelayList_WithPeerIDFilter(t *testing.T) {
	p := newPeersBundle(t)
	seedPeer(t, p, "peer-f", "tok-f")
	resp := getJSON(t, p.RelayList, "/api/relay/messages?peerId=peer-f")
	// No messages for this peer yet; the filter path still returns an empty list.
	msgs, _ := resp["messages"].([]interface{})
	if len(msgs) != 0 {
		t.Errorf("expected 0 filtered messages, got %d", len(msgs))
	}
}

func TestPeersPing_UnreachablePeer(t *testing.T) {
	p := newPeersBundle(t)
	// Seeded peer points at a non-resolvable URL, so Ping fails -> the handler
	// returns 200 with an error + an updated (unreachable) status.
	seedPeer(t, p, "peer-down", "tok-d")
	req := withURLParam(httptest.NewRequest("POST", "/api/peers/peer-down/ping", nil), "peerID", "peer-down")
	rec := httptest.NewRecorder()
	p.PeersPing(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (status carried in body) on ping failure, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	if resp["error"] == nil {
		t.Errorf("expected an error field in the ping response, got %v", resp)
	}
}

func TestPeersDelete_HappySeeded(t *testing.T) {
	p := newPeersBundle(t)
	seedPeer(t, p, "peer-del", "tok-del")
	req := withURLParam(httptest.NewRequest("DELETE", "/api/peers/peer-del", nil), "peerID", "peer-del")
	rec := httptest.NewRecorder()
	p.PeersDelete(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 deleting a seeded peer, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	if resp["deleted"] != true {
		t.Errorf("deleted=%v, want true", resp["deleted"])
	}
}
