package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestGet_Success(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{"ok":true}`)
	var result interface{}
	res := runCLI(t, func() { result = get("/api/x?y=1") })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.method != http.MethodGet {
		t.Errorf("method = %q", rec.method)
	}
	if rec.path != "/api/x?y=1" {
		t.Errorf("path = %q", rec.path)
	}
	m, ok := result.(map[string]interface{})
	if !ok || m["ok"] != true {
		t.Errorf("result = %#v", result)
	}
}

func TestGet_ServerError_Fatal(t *testing.T) {
	newServer(t, http.StatusInternalServerError, `boom`)
	res := runCLI(t, func() { get("/api/x") })
	if !res.exited || res.code != 1 {
		t.Fatalf("expected fatal exit(1), got %+v", res)
	}
	if !strings.Contains(res.stderr, "Error 500") {
		t.Errorf("stderr = %q", res.stderr)
	}
}

func TestPost_SendsJSONBody(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{}`)
	res := runCLI(t, func() { post("/api/p", map[string]interface{}{"a": "b"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.method != http.MethodPost {
		t.Errorf("method = %q", rec.method)
	}
	if rec.body["a"] != "b" {
		t.Errorf("body = %#v", rec.body)
	}
	if ct := rec.header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type = %q", ct)
	}
}

func TestPostBody_ReturnsDecodedResponse(t *testing.T) {
	newServer(t, http.StatusOK, `{"warning":"heads up"}`)
	var out interface{}
	res := runCLI(t, func() { out = postBody("/api/p", map[string]interface{}{}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	m, ok := out.(map[string]interface{})
	if !ok || m["warning"] != "heads up" {
		t.Errorf("out = %#v", out)
	}
}

func TestPut_SendsBodyAndReturns(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{"invariants":["x"]}`)
	res := runCLI(t, func() { put("/api/inv", map[string]interface{}{"invariants": []string{"x"}}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.method != http.MethodPut {
		t.Errorf("method = %q", rec.method)
	}
}

func TestDel_Success(t *testing.T) {
	rec := newServer(t, http.StatusOK, ``)
	res := runCLI(t, func() { del("/api/d?name=app") })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.method != http.MethodDelete || rec.path != "/api/d?name=app" {
		t.Errorf("method=%q path=%q", rec.method, rec.path)
	}
}

func TestDelBody_ReturnsDecoded(t *testing.T) {
	newServer(t, http.StatusOK, `{"invariants":[]}`)
	var out interface{}
	res := runCLI(t, func() { out = delBody("/api/inv/0") })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if _, ok := out.(map[string]interface{}); !ok {
		t.Errorf("out = %#v", out)
	}
}

func TestDel_ServerError_Fatal(t *testing.T) {
	newServer(t, http.StatusForbidden, `nope`)
	res := runCLI(t, func() { del("/api/d") })
	if !res.exited || res.code != 1 {
		t.Fatalf("expected fatal, got %+v", res)
	}
}

func TestRequireEnv_MissingURL_Fatal(t *testing.T) {
	setGlobals(t, "", "sid")
	res := runCLI(t, func() { requireEnv() })
	if !res.exited || !strings.Contains(res.stderr, "JARVIS_API_URL") {
		t.Fatalf("expected JARVIS_API_URL fatal, got %+v", res)
	}
}

func TestRequireEnv_MissingSession_Fatal(t *testing.T) {
	setGlobals(t, "http://x", "")
	res := runCLI(t, func() { requireEnv() })
	if !res.exited || !strings.Contains(res.stderr, "JARVIS_SESSION_ID") {
		t.Fatalf("expected JARVIS_SESSION_ID fatal, got %+v", res)
	}
}

func TestAddAgentAuth_SetsHeaderWhenTokenPresent(t *testing.T) {
	t.Setenv("JARVIS_AGENT_TOKEN", "secret-tok")
	req, err := http.NewRequest(http.MethodGet, "http://x/api", nil)
	if err != nil {
		t.Fatal(err)
	}
	addAgentAuth(req)
	if got := req.Header.Get("X-Jarvis-Agent-Token"); got != "secret-tok" {
		t.Errorf("token header = %q", got)
	}
}

func TestAddAgentAuth_NoHeaderWhenAbsent(t *testing.T) {
	t.Setenv("JARVIS_AGENT_TOKEN", "")
	req, _ := http.NewRequest(http.MethodGet, "http://x/api", nil)
	addAgentAuth(req)
	if got := req.Header.Get("X-Jarvis-Agent-Token"); got != "" {
		t.Errorf("expected no token header, got %q", got)
	}
}

func TestRequireArgs_FatalWhenTooFew(t *testing.T) {
	res := runCLI(t, func() { requireArgs([]string{"a"}, 2, "cm x <a> <b>") })
	if !res.exited || !strings.Contains(res.stderr, "cm x <a> <b>") {
		t.Fatalf("expected usage fatal, got %+v", res)
	}
}

func TestPrintJSON_Pretty(t *testing.T) {
	res := runCLI(t, func() { printJSON(map[string]interface{}{"k": "v"}) })
	if res.exited {
		t.Fatalf("unexpected exit")
	}
	if !strings.Contains(res.stdout, `"k": "v"`) {
		t.Errorf("stdout = %q", res.stdout)
	}
}
