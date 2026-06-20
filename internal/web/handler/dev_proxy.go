package handler

// dev_proxy.go reverse-proxies the operator's browser through to a team's
// running dev instance. The dev servers leads announce (e.g.
// http://jarvis:3001) bind to a docker-internal hostname on an UNPUBLISHED
// port: the sibling headless-shell container can reach them, but the
// operator's browser cannot (only 3555/3556 are published, and "jarvis"
// resolves only on the docker network). Routing the request through the
// jarvis process — which CAN reach http://jarvis:<port> (the same path the
// health checker pings) — gives the operator a working live preview through
// the published jarvis port, including through the server TLS proxy.

import (
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
)

// devProxyTransport fails fast on a dead upstream so the operator gets a
// 502 instead of a hung tab. The short DialContext timeout is the key knob;
// the rest mirror http.DefaultTransport defaults.
var devProxyTransport = &http.Transport{
	Proxy: http.ProxyFromEnvironment,
	DialContext: (&net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext,
	ForceAttemptHTTP2:     true,
	MaxIdleConns:          100,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   5 * time.Second,
	ExpectContinueTimeout: time.Second,
}

// OfficeDevProxy proxies /api/office/dev-proxy/{slot}/* to the announced URL
// of the named dev slot. WebSocket Upgrade (Vite / Next HMR) passes through
// because httputil.ReverseProxy forwards the Upgrade handshake natively.
func (ctx *Context) OfficeDevProxy(w http.ResponseWriter, r *http.Request) {
	slot := chi.URLParam(r, "slot")
	if !validDevSlot(slot) {
		http.Error(w, "invalid dev slot name", http.StatusBadRequest)
		return
	}

	inst, ok := ctx.OfficeManager.GetDevInstance(slot)
	if !ok || inst.URL == "" {
		http.Error(w, "dev instance '"+slot+"' is not running (no announced URL)", http.StatusBadGateway)
		return
	}

	target, err := url.Parse(inst.URL)
	if err != nil || target.Host == "" {
		http.Error(w, "dev instance '"+slot+"' has an unparseable URL", http.StatusBadGateway)
		return
	}

	prefix := "/api/office/dev-proxy/" + slot
	rest := chi.URLParam(r, "*")

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = devProxyTransport
	// FlushInterval -1 streams responses immediately rather than buffering,
	// which keeps SSE / HMR / chunked dev-server output flowing.
	proxy.FlushInterval = -1

	// Wrap the default single-host director: it sets the scheme/host on the
	// outbound request; we then rewrite the path to strip the route prefix
	// (so the upstream sees "/<rest>", not "/api/office/dev-proxy/<slot>/...")
	// and set Host + X-Forwarded-Prefix so subpath-aware dev servers can
	// resolve their asset URLs.
	baseDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		baseDirector(req)
		req.Host = target.Host
		req.URL.Path = "/" + rest
		if req.URL.RawPath != "" {
			req.URL.RawPath = "/" + rest
		}
		req.Header.Set("X-Forwarded-Prefix", prefix+"/")
	}

	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		http.Error(w, "dev server unreachable: "+err.Error(), http.StatusBadGateway)
	}

	// Force no-store on every proxied response (overriding whatever the dev
	// server / any intermediary set) and strip validators, so the operator's
	// browser can NEVER cache the live preview -- each load (and the SPA's
	// auto-refresh) always reflects the team's latest edits.
	proxy.ModifyResponse = func(resp *http.Response) error {
		resp.Header.Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		resp.Header.Set("Pragma", "no-cache")
		resp.Header.Del("ETag")
		resp.Header.Del("Last-Modified")
		resp.Header.Del("Expires")
		return nil
	}

	proxy.ServeHTTP(w, r)
}

// validDevSlot mirrors office.validDevSlotName (unexported in that package)
// so the proxy rejects unsafe slot names before touching the manager.
func validDevSlot(name string) bool {
	if name == "" || len(name) > 32 {
		return false
	}
	for _, c := range name {
		isLower := c >= 'a' && c <= 'z'
		isUpper := c >= 'A' && c <= 'Z'
		isDigit := c >= '0' && c <= '9'
		if !isLower && !isUpper && !isDigit && c != '_' && c != '-' {
			return false
		}
	}
	return true
}
