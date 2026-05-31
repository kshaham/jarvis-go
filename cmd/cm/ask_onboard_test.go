package main

import (
	"net/http"
	"strings"
	"testing"
)

// ---- ask ----

func TestHandleAsk_NoArgs_Fatal(t *testing.T) {
	res := runCLI(t, func() { handleAsk(nil) })
	if !res.exited {
		t.Fatalf("expected fatal, got %+v", res)
	}
}

func TestHandleAsk_Answer(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{}`)
	res := runCLI(t, func() { handleAsk([]string{"answer", "Q1", "here", "is", "the", "answer"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.path != "/api/office/agent/ask/answer" || rec.body["questionId"] != "Q1" {
		t.Errorf("path=%q body=%#v", rec.path, rec.body)
	}
}

func TestHandleAsk_AnswerMissingArgs_Fatal(t *testing.T) {
	res := runCLI(t, func() { handleAsk([]string{"answer", "Q1"}) })
	if !res.exited {
		t.Fatalf("expected fatal, got %+v", res)
	}
}

func TestHandleAsk_List(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{"questions":[]}`)
	res := runCLI(t, func() { handleAsk([]string{"list"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.method != http.MethodGet || !strings.HasPrefix(rec.path, "/api/office/agent/ask/list") {
		t.Errorf("method=%q path=%q", rec.method, rec.path)
	}
}

func TestHandleAsk_BlockingAnswered(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{"success":true,"questionId":"Q9","status":"answered","answer":"use JWT","answeredBy":"lead"}`)
	res := runCLI(t, func() { handleAsk([]string{"how", "do", "I", "auth", "--about", "T1", "--wait", "1s"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.path != "/api/office/agent/ask" || rec.body["content"] != "how do I auth" {
		t.Errorf("path=%q body=%#v", rec.path, rec.body)
	}
	if rec.body["aboutTaskId"] != "T1" {
		t.Errorf("aboutTaskId=%#v", rec.body["aboutTaskId"])
	}
	if !strings.Contains(res.stdout, "use JWT") || !strings.Contains(res.stdout, "answered") {
		t.Errorf("stdout=%q", res.stdout)
	}
}

func TestHandleAsk_BlockingTimeout_Exit2(t *testing.T) {
	newServer(t, http.StatusOK, `{"questionId":"Q9","status":"timeout","tried":["lead","vex"]}`)
	res := runCLI(t, func() { handleAsk([]string{"question?"}) })
	if !res.exited || res.code != 2 {
		t.Fatalf("expected exit(2), got %+v", res)
	}
	if !strings.Contains(res.stdout, "timeout") {
		t.Errorf("stdout=%q", res.stdout)
	}
}

func TestHandleAsk_BlockingNoRecipient_Exit2(t *testing.T) {
	newServer(t, http.StatusOK, `{"questionId":"Q9","status":"no-recipient"}`)
	res := runCLI(t, func() { handleAsk([]string{"question?"}) })
	if !res.exited || res.code != 2 {
		t.Fatalf("expected exit(2), got %+v", res)
	}
	if !strings.Contains(res.stdout, "no-recipient") {
		t.Errorf("stdout=%q", res.stdout)
	}
}

func TestHandleAsk_BlockingOtherStatus(t *testing.T) {
	newServer(t, http.StatusOK, `{"questionId":"Q9","status":"queued"}`)
	res := runCLI(t, func() { handleAsk([]string{"question?"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if !strings.Contains(res.stdout, "queued") {
		t.Errorf("stdout=%q", res.stdout)
	}
}

func TestHandleAsk_BlockingBadWait_Fatal(t *testing.T) {
	newServer(t, http.StatusOK, `{}`)
	res := runCLI(t, func() { handleAsk([]string{"question?", "--wait", "notaduration"}) })
	if !res.exited || !strings.Contains(res.stderr, "invalid --wait") {
		t.Fatalf("got %+v", res)
	}
}

func TestHandleAsk_BlockingServerError_Fatal(t *testing.T) {
	newServer(t, http.StatusBadRequest, `bad`)
	res := runCLI(t, func() { handleAsk([]string{"question?", "--wait", "1s"}) })
	if !res.exited {
		t.Fatalf("expected fatal, got %+v", res)
	}
}

// ---- onboard ----

func TestHandleOnboard_Happy(t *testing.T) {
	rec := newServer(t, http.StatusOK, "PROTOCOLS\nbe nice\n")
	res := runCLI(t, func() { handleOnboard([]string{"protocols"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if !strings.Contains(rec.path, "topic=protocols") {
		t.Errorf("path=%q", rec.path)
	}
	if !strings.Contains(res.stdout, "be nice") {
		t.Errorf("stdout=%q", res.stdout)
	}
}

func TestHandleOnboard_NoTopic_Fatal(t *testing.T) {
	res := runCLI(t, func() { handleOnboard(nil) })
	if !res.exited {
		t.Fatalf("expected fatal, got %+v", res)
	}
}

func TestHandleOnboard_ServerError_Fatal(t *testing.T) {
	newServer(t, http.StatusNotFound, `unknown topic`)
	res := runCLI(t, func() { handleOnboard([]string{"bogus"}) })
	if !res.exited || !strings.Contains(res.stderr, "Error 404") {
		t.Fatalf("got %+v", res)
	}
}
