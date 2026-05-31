package main

import (
	"net/http"
	"strings"
	"testing"
)

// ---- standup / brainstorm / leader-tasks ----

func TestHandleStandup_PlainText(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{}`)
	res := runCLI(t, func() { handleStandup([]string{"respond", "shipped", "auth"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.path != "/api/office/agent/standup/response" || rec.body["content"] != "shipped auth" {
		t.Errorf("path=%q body=%#v", rec.path, rec.body)
	}
}

func TestHandleStandup_JSON(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{}`)
	res := runCLI(t, func() {
		handleStandup([]string{"respond", "--json", `{"status":"done","blockers":["lead sign-off"],"next":["mobile"]}`})
	})
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.body["structured"] == nil {
		t.Errorf("expected structured payload, body=%#v", rec.body)
	}
}

func TestHandleStandup_BadJSON_Fatal(t *testing.T) {
	res := runCLI(t, func() { handleStandup([]string{"respond", "--json", `{}`}) })
	if !res.exited || !strings.Contains(res.stderr, "validation failed") {
		t.Fatalf("expected validation fatal, got %+v", res)
	}
}

func TestHandleStandup_NoRespond_Fatal(t *testing.T) {
	res := runCLI(t, func() { handleStandup([]string{"oops"}) })
	if !res.exited {
		t.Fatalf("expected fatal, got %+v", res)
	}
}

func TestHandleBrainstorm_PlainText(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{}`)
	res := runCLI(t, func() { handleBrainstorm([]string{"respond", "idea", "one"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.path != "/api/office/agent/brainstorm/response" || rec.body["proposals"] != "idea one" {
		t.Errorf("path=%q body=%#v", rec.path, rec.body)
	}
}

func TestHandleBrainstorm_JSON(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{}`)
	res := runCLI(t, func() {
		handleBrainstorm([]string{"respond", "--json", `{"proposals":[{"title":"Cache","priority":"high","owner":"bolt"}]}`})
	})
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.body["structured"] == nil {
		t.Errorf("expected structured payload, body=%#v", rec.body)
	}
}

func TestHandleBrainstorm_NoRespond_Fatal(t *testing.T) {
	res := runCLI(t, func() { handleBrainstorm([]string{"oops"}) })
	if !res.exited {
		t.Fatalf("expected fatal, got %+v", res)
	}
}

func TestHandleLeaderTasks_PlainText(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{}`)
	res := runCLI(t, func() { handleLeaderTasks([]string{"respond", "assign", "auth", "to", "vex"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.path != "/api/office/agent/leader-tasks/response" {
		t.Errorf("path=%q", rec.path)
	}
}

func TestHandleLeaderTasks_JSON_RendersFullFields(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{}`)
	body := `{"assignments":[{"agent":"alice","title":"Wire logout","description":"use JWT","priority":"high","labels":["auth","api"]}],"notes":"this sprint"}`
	res := runCLI(t, func() { handleLeaderTasks([]string{"respond", "--json", body}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	rendered, _ := rec.body["content"].(string)
	for _, want := range []string{"@alice", "Wire logout", "[high]", "use JWT", "labels: auth, api", "Notes: this sprint"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered content missing %q: %q", want, rendered)
		}
	}
	if rec.body["structured"] == nil {
		t.Errorf("expected structured payload, body=%#v", rec.body)
	}
}

func TestHandleLeaderTasks_NoRespond_Fatal(t *testing.T) {
	res := runCLI(t, func() { handleLeaderTasks([]string{"nope"}) })
	if !res.exited {
		t.Fatalf("expected fatal, got %+v", res)
	}
}

func TestHandleLeaderTasks_BadJSON_Fatal(t *testing.T) {
	res := runCLI(t, func() {
		handleLeaderTasks([]string{"respond", "--json", `{"assignments":[{"title":"x"}]}`})
	})
	if !res.exited {
		t.Fatalf("expected fatal (missing agent), got %+v", res)
	}
}

// ---- reflect ----

func TestHandleReflect(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{}`)
	res := runCLI(t, func() {
		handleReflect([]string{"learned", "something", "--category", "process", "--task-id", "T1", "--task-title", "Auth"})
	})
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.path != "/api/office/agent/reflection/response" {
		t.Errorf("path=%q", rec.path)
	}
	if rec.body["category"] != "process" || rec.body["taskId"] != "T1" || rec.body["taskTitle"] != "Auth" {
		t.Errorf("body=%#v", rec.body)
	}
}

func TestHandleReflect_NoArgs_Fatal(t *testing.T) {
	res := runCLI(t, func() { handleReflect(nil) })
	if !res.exited {
		t.Fatalf("expected fatal, got %+v", res)
	}
}

func TestHandleReflect_MissingCategory_Fatal(t *testing.T) {
	res := runCLI(t, func() { handleReflect([]string{"learned something"}) })
	if !res.exited || !strings.Contains(res.stderr, "--category is required") {
		t.Fatalf("got %+v", res)
	}
}

// ---- discover ----

func TestHandleDiscover(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{}`)
	res := runCLI(t, func() {
		handleDiscover([]string{"summary", "text", "--findings", "a", "b,c", "--self-assigned", "refactor", "--committed"})
	})
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.path != "/api/office/agent/curiosity/discovery" {
		t.Errorf("path=%q", rec.path)
	}
	disc, _ := rec.body["discoveries"].([]interface{})
	if len(disc) != 3 { // a, b, c
		t.Errorf("discoveries=%#v", rec.body["discoveries"])
	}
	if rec.body["ledToCommit"] != true || rec.body["selfAssignedTask"] != "refactor" {
		t.Errorf("body=%#v", rec.body)
	}
}

func TestHandleDiscover_NoFindings(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{}`)
	res := runCLI(t, func() { handleDiscover([]string{"just", "a", "summary"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	disc, ok := rec.body["discoveries"].([]interface{})
	if !ok || len(disc) != 0 {
		t.Errorf("expected empty discoveries slice, got %#v", rec.body["discoveries"])
	}
}

func TestHandleDiscover_NoArgs_Fatal(t *testing.T) {
	res := runCLI(t, func() { handleDiscover(nil) })
	if !res.exited {
		t.Fatalf("expected fatal, got %+v", res)
	}
}
