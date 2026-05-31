package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestHandleDev_NoArgs_Fatal(t *testing.T) {
	res := runCLI(t, func() { handleDev(nil) })
	if !res.exited {
		t.Fatalf("expected fatal, got %+v", res)
	}
}

func TestHandleDev_UnknownSub_Fatal(t *testing.T) {
	res := runCLI(t, func() { handleDev([]string{"wat"}) })
	if !res.exited || !strings.Contains(res.stderr, "Unknown dev subcommand") {
		t.Fatalf("got %+v", res)
	}
}

func TestHandleDev_URL_DefaultSlot(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{"url":"http://jarvis:3001"}`)
	res := runCLI(t, func() { handleDev([]string{"url"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if !strings.HasPrefix(rec.path, "/api/office/agent/dev-instance?name=app") {
		t.Errorf("path=%q", rec.path)
	}
	if !strings.Contains(res.stdout, "http://jarvis:3001") {
		t.Errorf("stdout=%q", res.stdout)
	}
}

func TestHandleDev_URL_EmptyWhenNoneSet(t *testing.T) {
	newServer(t, http.StatusOK, `{}`)
	res := runCLI(t, func() { handleDev([]string{"url"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if strings.TrimSpace(res.stdout) != "" {
		t.Errorf("expected empty stdout, got %q", res.stdout)
	}
}

func TestHandleDev_URL_WithProbe(t *testing.T) {
	// Server returns its own URL so the --probe HEAD lands back on it.
	rec := &recordedRequest{}
	srv := newProbeServer(t, rec)
	setGlobals(t, srv, "test-session")
	res := runCLI(t, func() { handleDev([]string{"url", "--probe"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if !rec.sawMethod(http.MethodHead) {
		t.Errorf("expected a HEAD probe, methods=%v", rec.methods)
	}
	if !strings.Contains(res.stderr, "ready to screenshot") {
		t.Errorf("stderr=%q", res.stderr)
	}
}

func TestHandleDev_List(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{"slots":[]}`)
	res := runCLI(t, func() { handleDev([]string{"list"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.path != "/api/office/agent/dev-instance/all" {
		t.Errorf("path=%q", rec.path)
	}
}

func TestHandleDev_Announce(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{}`)
	res := runCLI(t, func() {
		handleDev([]string{"announce", "http://jarvis:3001", "--pid", "1234"})
	})
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.path != "/api/office/agent/dev-instance" {
		t.Errorf("path=%q", rec.path)
	}
	if rec.body["url"] != "http://jarvis:3001" {
		t.Errorf("body=%#v", rec.body)
	}
	if !strings.Contains(res.stdout, "announced") {
		t.Errorf("stdout=%q", res.stdout)
	}
}

func TestHandleDev_AnnounceLocalhostBlocked_Fatal(t *testing.T) {
	newServer(t, http.StatusOK, `{}`)
	res := runCLI(t, func() { handleDev([]string{"announce", "http://localhost:3001"}) })
	if !res.exited || !strings.Contains(res.stderr, "localhost") {
		t.Fatalf("expected localhost guard fatal, got %+v", res)
	}
}

func TestHandleDev_AnnounceLocalhostForce(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{}`)
	res := runCLI(t, func() { handleDev([]string{"announce", "http://localhost:3001", "--force"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if !strings.HasPrefix(rec.path, "/api/office/agent/dev-instance?force=true") {
		t.Errorf("path=%q", rec.path)
	}
	if !strings.Contains(res.stderr, "--force") {
		t.Errorf("stderr=%q", res.stderr)
	}
}

func TestHandleDev_AnnounceNoURL_Fatal(t *testing.T) {
	res := runCLI(t, func() { handleDev([]string{"announce"}) })
	if !res.exited {
		t.Fatalf("expected fatal, got %+v", res)
	}
}

func TestHandleDev_ClearOne(t *testing.T) {
	rec := newServer(t, http.StatusOK, ``)
	res := runCLI(t, func() { handleDev([]string{"clear", "frontend"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.method != http.MethodDelete || !strings.Contains(rec.path, "name=frontend") {
		t.Errorf("method=%q path=%q", rec.method, rec.path)
	}
}

func TestHandleDev_ClearAll(t *testing.T) {
	rec := newServer(t, http.StatusOK, ``)
	res := runCLI(t, func() { handleDev([]string{"clear", "--all"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if !strings.Contains(rec.path, "all=true") {
		t.Errorf("path=%q", rec.path)
	}
}

func TestHandleDevCreds_Read(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{"credentials":"admin / pw"}`)
	res := runCLI(t, func() { handleDev([]string{"creds"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.method != http.MethodGet {
		t.Errorf("method=%q", rec.method)
	}
	if !strings.Contains(res.stdout, "admin / pw") {
		t.Errorf("stdout=%q", res.stdout)
	}
}

func TestHandleDevCreds_Set(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{}`)
	res := runCLI(t, func() { handleDev([]string{"creds", "set", "admin", "/", "pw", "--name", "api"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.body["name"] != "api" || !strings.Contains(rec.body["credentials"].(string), "admin") {
		t.Errorf("body=%#v", rec.body)
	}
}

func TestHandleDevCreds_Clear(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{}`)
	res := runCLI(t, func() { handleDev([]string{"creds", "clear"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.body["credentials"] != "" {
		t.Errorf("expected empty credentials, body=%#v", rec.body)
	}
}

func TestHandleDevCreds_BadSub_Fatal(t *testing.T) {
	res := runCLI(t, func() { handleDev([]string{"creds", "frobnicate"}) })
	if !res.exited {
		t.Fatalf("expected fatal, got %+v", res)
	}
}
