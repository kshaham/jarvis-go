package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// Shared test harness for the cm CLI. The handlers talk to the Jarvis server
// over HTTP and terminate via fatal() -> osExit, so these helpers spin a local
// httptest server, redirect the package globals at it, capture stdout/stderr,
// and trap osExit so error branches are exercised without killing the binary.

// recordedRequest captures what the CLI sent to the server.
type recordedRequest struct {
	method string
	path   string // path + query (RequestURI) of the LAST request
	raw    []byte
	body   map[string]interface{}
	header http.Header
	hits    int
	paths   []string // RequestURI of every request, in order
	methods []string // HTTP method of every request, in order
}

// sawMethod reports whether any recorded request used the given HTTP method.
func (r *recordedRequest) sawMethod(method string) bool {
	for _, m := range r.methods {
		if m == method {
			return true
		}
	}
	return false
}

// sawPath reports whether any recorded request targeted a path with the given
// prefix (ignoring query string differences).
func (r *recordedRequest) sawPath(prefix string) bool {
	for _, p := range r.paths {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}

// cliResult is the observable outcome of invoking a handler.
type cliResult struct {
	stdout string
	stderr string
	code   int  // exit code, when exited
	exited bool // true if the handler called fatal()/osExit
}

// fatalPanic is the sentinel the osExit stub panics with so runCLI can unwind
// the handler and recover the would-be exit code.
type fatalPanic struct{ code int }

// newServer starts an httptest server that records every request and replies
// with the supplied status + body. It points the package globals at the server
// (restored on cleanup) and returns the shared request recorder.
func newServer(t *testing.T, status int, respBody string) *recordedRequest {
	t.Helper()
	rec := &recordedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.hits++
		rec.method = r.Method
		rec.path = r.URL.RequestURI()
		rec.paths = append(rec.paths, r.URL.RequestURI())
		rec.methods = append(rec.methods, r.Method)
		rec.raw, _ = io.ReadAll(r.Body)
		rec.header = r.Header.Clone()
		if len(rec.raw) > 0 {
			_ = json.Unmarshal(rec.raw, &rec.body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, respBody)
	}))
	t.Cleanup(srv.Close)
	setGlobals(t, srv.URL, "test-session")
	return rec
}

// newProbeServer starts a server that records into rec and, for GET requests,
// returns its own URL as the dev-instance url so that `cm dev url --probe`'s
// follow-up HEAD request lands back on the same server. Returns the URL.
func newProbeServer(t *testing.T, rec *recordedRequest) string {
	t.Helper()
	var self string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.hits++
		rec.method = r.Method
		rec.path = r.URL.RequestURI()
		rec.paths = append(rec.paths, r.URL.RequestURI())
		rec.methods = append(rec.methods, r.Method)
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"url":"`+self+`"}`)
	}))
	self = srv.URL
	t.Cleanup(srv.Close)
	return srv.URL
}

// setGlobals overrides apiURL/sessionID for the duration of a test.
func setGlobals(t *testing.T, url, sid string) {
	t.Helper()
	oldURL, oldSID := apiURL, sessionID
	apiURL, sessionID = url, sid
	t.Cleanup(func() { apiURL, sessionID = oldURL, oldSID })
}

// runCLI invokes fn with stdout/stderr captured and osExit trapped. A fatal()
// call inside fn is recorded as exited=true with its code rather than ending
// the test process. Any non-fatal panic is re-raised after restoring state.
func runCLI(t *testing.T, fn func()) cliResult {
	t.Helper()

	oldExit := osExit
	osExit = func(c int) { panic(fatalPanic{c}) }

	oldOut, oldErr := os.Stdout, os.Stderr
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout, os.Stderr = wOut, wErr

	outCh := make(chan string, 1)
	errCh := make(chan string, 1)
	go func() { var b bytes.Buffer; _, _ = io.Copy(&b, rOut); outCh <- b.String() }()
	go func() { var b bytes.Buffer; _, _ = io.Copy(&b, rErr); errCh <- b.String() }()

	var res cliResult
	func() {
		defer func() {
			if r := recover(); r != nil {
				if fp, ok := r.(fatalPanic); ok {
					res.code, res.exited = fp.code, true
					return
				}
				// Not our sentinel: restore state, then re-raise.
				wOut.Close()
				wErr.Close()
				os.Stdout, os.Stderr = oldOut, oldErr
				osExit = oldExit
				panic(r)
			}
		}()
		fn()
	}()

	wOut.Close()
	wErr.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	osExit = oldExit
	res.stdout = <-outCh
	res.stderr = <-errCh
	return res
}
