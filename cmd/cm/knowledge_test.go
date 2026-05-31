package main

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestParseKnowledgeArgs(t *testing.T) {
	q, limit, all := parseKnowledgeArgs([]string{"auth", "bug", "--limit", "5", "--all"})
	if q != "auth bug" {
		t.Errorf("query=%q", q)
	}
	if limit != 5 {
		t.Errorf("limit=%d", limit)
	}
	if !all {
		t.Errorf("all=%v", all)
	}
}

func TestParseKnowledgeArgs_DefaultLimit(t *testing.T) {
	_, limit, all := parseKnowledgeArgs([]string{"x"})
	if limit != 20 || all {
		t.Errorf("limit=%d all=%v", limit, all)
	}
}

func TestParseKnowledgeArgs_BadLimit_Fatal(t *testing.T) {
	res := runCLI(t, func() { parseKnowledgeArgs([]string{"--limit", "zero"}) })
	if !res.exited {
		t.Fatalf("expected fatal, got %+v", res)
	}
}

func TestParseKnowledgeArgs_UnknownFlag_Fatal(t *testing.T) {
	res := runCLI(t, func() { parseKnowledgeArgs([]string{"--wat"}) })
	if !res.exited {
		t.Fatalf("expected fatal, got %+v", res)
	}
}

func TestHandleKnowledge_NoArgs_Fatal(t *testing.T) {
	res := runCLI(t, func() { handleKnowledge(nil) })
	if !res.exited {
		t.Fatalf("expected fatal, got %+v", res)
	}
}

func TestHandleKnowledge_UnknownSub_Fatal(t *testing.T) {
	res := runCLI(t, func() { handleKnowledge([]string{"frobnicate"}) })
	if !res.exited || !strings.Contains(res.stderr, "Unknown knowledge subcommand") {
		t.Fatalf("got %+v", res)
	}
}

func TestHandleKnowledge_Search(t *testing.T) {
	ts := time.Now().Add(-90 * time.Second).Format(time.RFC3339Nano)
	body := `{"hits":[{"source":"memory","agentName":"vex","section":"notes","excerpt":"found it","timestamp":"` + ts + `"}]}`
	rec := newServer(t, http.StatusOK, body)
	res := runCLI(t, func() { handleKnowledge([]string{"search", "auth", "--limit", "3", "--all"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if !strings.Contains(rec.path, "q=auth") || !strings.Contains(rec.path, "limit=3") || !strings.Contains(rec.path, "all=true") {
		t.Errorf("path=%q", rec.path)
	}
	if !strings.Contains(res.stdout, "found it") || !strings.Contains(res.stdout, "memory/vex") {
		t.Errorf("stdout=%q", res.stdout)
	}
}

func TestHandleKnowledge_SearchEmptyQuery_Fatal(t *testing.T) {
	res := runCLI(t, func() { handleKnowledge([]string{"search"}) })
	if !res.exited {
		t.Fatalf("expected fatal, got %+v", res)
	}
}

func TestHandleKnowledge_SearchNoHits(t *testing.T) {
	newServer(t, http.StatusOK, `{"hits":[]}`)
	res := runCLI(t, func() { handleKnowledge([]string{"search", "nothing"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if !strings.Contains(res.stdout, "no matches") {
		t.Errorf("stdout=%q", res.stdout)
	}
}

func TestHandleKnowledge_Tail(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{"hits":[]}`)
	res := runCLI(t, func() { handleKnowledge([]string{"tail", "--limit", "7"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if !strings.HasPrefix(rec.path, "/api/office/agent/knowledge/tail") || !strings.Contains(rec.path, "limit=7") {
		t.Errorf("path=%q", rec.path)
	}
}

func TestFormatAge(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{5 * time.Minute, "5m"},
		{3 * time.Hour, "3h"},
		{50 * time.Hour, "2d"},
	}
	for _, tc := range cases {
		if got := formatAge(tc.d); got != tc.want {
			t.Errorf("formatAge(%v)=%q want %q", tc.d, got, tc.want)
		}
	}
}
