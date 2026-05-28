package peers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client speaks to peer jarvis instances. Two operations matter:
// PostInbox (deliver a message), and GetIdentity (verify a peer is
// who they claim to be during paste-token registration).
type Client struct {
	HTTP    *http.Client
	Timeout time.Duration
}

// NewClient returns a Client configured with a sensible 15s timeout
// per call. Operators can tune via NewClientWithTimeout for slow links.
func NewClient() *Client {
	return NewClientWithTimeout(15 * time.Second)
}

func NewClientWithTimeout(timeout time.Duration) *Client {
	return &Client{
		HTTP: &http.Client{
			Timeout: timeout,
			// Don't follow redirects — peer URLs should be exact. A
			// redirect probably means the operator pasted a wrong URL.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		Timeout: timeout,
	}
}

// PostInbox delivers a RelayMessage to a peer's /api/relay/inbox
// endpoint. Returns nil on 2xx; otherwise an error describing the
// failure (network, timeout, 4xx, 5xx). The error message embeds the
// status code so the caller can map back to PeerStatus.
func (c *Client) PostInbox(peer Peer, msg RelayMessage) error {
	url := joinURL(peer.PublicURL, "/api/relay/inbox")
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+peer.PairToken)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("post inbox: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	// Surface 1KB of the response body so the operator can debug.
	preview := readPreview(resp.Body, 1024)
	return fmt.Errorf("peer %d: %s", resp.StatusCode, preview)
}

// Identity is the shape of /api/identity. Unauthenticated so a fresh
// "Add peer" flow can pre-verify the URL before persisting the
// registration.
type Identity struct {
	PeerID      string `json:"peerId"`
	DisplayName string `json:"displayName"`
}

// GetIdentity round-trips a peer's /api/identity endpoint and returns
// what the peer claims to be. Used in the Add-Peer flow to confirm
// the URL the operator pasted resolves to a real jarvis.
func (c *Client) GetIdentity(publicURL string) (Identity, error) {
	url := joinURL(publicURL, "/api/identity")
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Identity{}, fmt.Errorf("new request: %w", err)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Identity{}, fmt.Errorf("get identity: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Identity{}, fmt.Errorf("peer %d: %s", resp.StatusCode, readPreview(resp.Body, 256))
	}
	var out Identity
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&out); err != nil {
		return Identity{}, fmt.Errorf("decode identity: %w", err)
	}
	if out.PeerID == "" {
		return Identity{}, errors.New("peer returned an empty identity")
	}
	return out, nil
}

// Ping is GetIdentity sugar — returns just success/failure for the
// "is this peer reachable" health check.
func (c *Client) Ping(peer Peer) error {
	_, err := c.GetIdentity(peer.PublicURL)
	return err
}

// joinURL trims a trailing slash on base and prepends a leading slash
// on path. Avoids the URL class which had iffy RN behaviour and is
// equally not worth bringing in here.
func joinURL(base, path string) string {
	for strings.HasSuffix(base, "/") {
		base = base[:len(base)-1]
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}

// readPreview reads up to maxBytes from r and returns a trimmed string.
// Used to embed a snippet of an error body without unbounded reads.
func readPreview(r io.Reader, maxBytes int) string {
	buf := make([]byte, maxBytes)
	n, _ := io.ReadFull(io.LimitReader(r, int64(maxBytes)), buf)
	return strings.TrimSpace(string(buf[:n]))
}
