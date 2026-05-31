package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestHandleMemory_NoArgs_Fatal(t *testing.T) {
	res := runCLI(t, func() { handleMemory(nil) })
	if !res.exited {
		t.Fatalf("expected fatal, got %+v", res)
	}
}

func TestHandleMemory_UnknownSub_Fatal(t *testing.T) {
	res := runCLI(t, func() { handleMemory([]string{"frobnicate"}) })
	if !res.exited || !strings.Contains(res.stderr, "Unknown memory subcommand") {
		t.Fatalf("got %+v", res)
	}
}

func TestHandleMemory_Read(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{"content":"hi"}`)
	res := runCLI(t, func() { handleMemory([]string{"read"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.method != http.MethodGet || !strings.HasPrefix(rec.path, "/api/office/agent/memory?") {
		t.Errorf("method=%q path=%q", rec.method, rec.path)
	}
}

func TestHandleMemory_Write(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{}`)
	res := runCLI(t, func() { handleMemory([]string{"write", "remember", "this"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.path != "/api/office/agent/memory" || rec.body["content"] != "remember this" {
		t.Errorf("path=%q body=%#v", rec.path, rec.body)
	}
}

func TestHandleMemory_Append(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{}`)
	res := runCLI(t, func() { handleMemory([]string{"append", "another", "entry"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.path != "/api/office/agent/memory/append" || rec.body["entry"] != "another entry" {
		t.Errorf("path=%q body=%#v", rec.path, rec.body)
	}
}

func TestHandleMemory_SectionRead(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{"content":"x"}`)
	res := runCLI(t, func() { handleMemory([]string{"section", "task_history"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.method != http.MethodGet || !strings.Contains(rec.path, "name=task_history") {
		t.Errorf("method=%q path=%q", rec.method, rec.path)
	}
}

func TestHandleMemory_SectionWrite(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{}`)
	res := runCLI(t, func() { handleMemory([]string{"section", "notes", "the content"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.body["name"] != "notes" || rec.body["append"] != false {
		t.Errorf("body=%#v", rec.body)
	}
	if !strings.Contains(res.stdout, "written") {
		t.Errorf("stdout=%q", res.stdout)
	}
}

func TestHandleMemory_SectionAppend(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{}`)
	res := runCLI(t, func() { handleMemory([]string{"section", "--append", "notes", "new line"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.body["append"] != true || rec.body["content"] != "new line" {
		t.Errorf("body=%#v", rec.body)
	}
	if !strings.Contains(res.stdout, "appended") {
		t.Errorf("stdout=%q", res.stdout)
	}
}

func TestHandleMemory_SectionMissingName_Fatal(t *testing.T) {
	res := runCLI(t, func() { handleMemory([]string{"section", "--append"}) })
	if !res.exited {
		t.Fatalf("expected fatal, got %+v", res)
	}
}

func TestHandleMemory_Query(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{"matches":[]}`)
	res := runCLI(t, func() { handleMemory([]string{"query", "auth", "flow"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.method != http.MethodGet || !strings.Contains(rec.path, "q=auth") {
		t.Errorf("method=%q path=%q", rec.method, rec.path)
	}
}
