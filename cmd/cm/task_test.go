package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandleTask_NoArgs_Fatal(t *testing.T) {
	res := runCLI(t, func() { handleTask(nil) })
	if !res.exited || !strings.Contains(res.stderr, "cm task") {
		t.Fatalf("expected usage fatal, got %+v", res)
	}
}

func TestHandleTask_UnknownSubcommand_Fatal(t *testing.T) {
	res := runCLI(t, func() { handleTask([]string{"wibble"}) })
	if !res.exited || !strings.Contains(res.stderr, "Unknown task subcommand") {
		t.Fatalf("got %+v", res)
	}
}

func TestHandleTask_Start(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{}`)
	res := runCLI(t, func() { handleTask([]string{"start", "T1"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.path != "/api/office/agent/task-start" || rec.body["taskId"] != "T1" {
		t.Errorf("path=%q body=%#v", rec.path, rec.body)
	}
	if !strings.Contains(res.stdout, "in-progress") {
		t.Errorf("stdout=%q", res.stdout)
	}
}

func TestHandleTask_Start_MissingArg_Fatal(t *testing.T) {
	res := runCLI(t, func() { handleTask([]string{"start"}) })
	if !res.exited {
		t.Fatalf("expected fatal, got %+v", res)
	}
}

func TestHandleTask_CompleteWithSummary_AppendsMemoryThenCompletes(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{}`)
	res := runCLI(t, func() {
		handleTask([]string{"complete", "T9", "--summary", "did the thing"})
	})
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if !rec.sawPath("/api/office/agent/memory/section") {
		t.Errorf("expected memory/section append, paths=%v", rec.paths)
	}
	if !rec.sawPath("/api/office/agent/task-complete") {
		t.Errorf("expected task-complete, paths=%v", rec.paths)
	}
	if !strings.Contains(res.stdout, "marked as complete") {
		t.Errorf("stdout=%q", res.stdout)
	}
}

func TestHandleTask_CompleteWithoutSummary_WarnsAndCompletes(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{}`)
	res := runCLI(t, func() { handleTask([]string{"complete", "T9"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.sawPath("/api/office/agent/memory/section") {
		t.Errorf("should not append memory without summary, paths=%v", rec.paths)
	}
	if !strings.Contains(res.stderr, "without --summary") {
		t.Errorf("stderr=%q", res.stderr)
	}
}

func TestHandleTask_CompleteSurfacesServerWarning(t *testing.T) {
	newServer(t, http.StatusOK, `{"warning":"UI task missing verification","alreadyComplete":true}`)
	res := runCLI(t, func() {
		handleTask([]string{"complete", "T9", "--summary", "x"})
	})
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if !strings.Contains(res.stderr, "UI task missing verification") {
		t.Errorf("expected surfaced warning, stderr=%q", res.stderr)
	}
	if !strings.Contains(res.stderr, "already complete") {
		t.Errorf("expected already-complete note, stderr=%q", res.stderr)
	}
}

func TestHandleTask_CompleteWithScreenshot(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{}`)
	dir := t.TempDir()
	png := filepath.Join(dir, "shot.png")
	// minimal PNG signature bytes
	if err := os.WriteFile(png, []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, 0o644); err != nil {
		t.Fatal(err)
	}
	res := runCLI(t, func() {
		handleTask([]string{"complete", "T9", "--summary", "x", "--screenshot", png})
	})
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if !rec.sawPath("/api/office/agent/task/screenshot") {
		t.Errorf("expected screenshot upload, paths=%v", rec.paths)
	}
}

func TestHandleTask_CompleteScreenshotMissingFile_WarnsButContinues(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{}`)
	res := runCLI(t, func() {
		handleTask([]string{"complete", "T9", "--summary", "x", "--screenshot", "/no/such/file.png"})
	})
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if !strings.Contains(res.stderr, "screenshot upload failed") {
		t.Errorf("stderr=%q", res.stderr)
	}
	if !rec.sawPath("/api/office/agent/task-complete") {
		t.Errorf("should still complete, paths=%v", rec.paths)
	}
}

func TestHandleTask_CompleteNoTaskID_Fatal(t *testing.T) {
	newServer(t, http.StatusOK, `{}`)
	res := runCLI(t, func() { handleTask([]string{"complete"}) })
	if !res.exited {
		t.Fatalf("expected fatal, got %+v", res)
	}
}

func TestHandleTask_AckProgressBlocked(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		wantPath string
		wantOut  string
	}{
		{"ack", []string{"ack", "T1"}, "/api/office/agent/task-ack", "acknowledged"},
		{"progress", []string{"progress", "T1", "halfway", "there"}, "/api/office/agent/task-progress", "Progress reported"},
		{"blocked", []string{"blocked", "T1", "waiting on review"}, "/api/office/agent/task-progress", "marked as blocked"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := newServer(t, http.StatusOK, `{}`)
			res := runCLI(t, func() { handleTask(tc.args) })
			if res.exited {
				t.Fatalf("unexpected exit: %+v", res)
			}
			if rec.path != tc.wantPath {
				t.Errorf("path=%q want %q", rec.path, tc.wantPath)
			}
			if !strings.Contains(res.stdout, tc.wantOut) {
				t.Errorf("stdout=%q want substr %q", res.stdout, tc.wantOut)
			}
		})
	}
}

func TestHandleTask_BlockedSetsFlag(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{}`)
	runCLI(t, func() { handleTask([]string{"blocked", "T1", "reason here"}) })
	if rec.body["isBlocked"] != true {
		t.Errorf("isBlocked not set: %#v", rec.body)
	}
}

func TestHandleTask_List(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{"tasks":[]}`)
	res := runCLI(t, func() { handleTask([]string{"list"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.method != http.MethodGet || !strings.HasPrefix(rec.path, "/api/office/agent/tasks") {
		t.Errorf("method=%q path=%q", rec.method, rec.path)
	}
}

func TestHandleTask_SelfAddWithLabelsAndCriteria(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{}`)
	res := runCLI(t, func() {
		handleTask([]string{"self-add", "Title", "Description", "high",
			"--label", "ui", "--label", "bug", "--criterion", "passes tests"})
	})
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.body["title"] != "Title" || rec.body["priority"] != "high" {
		t.Errorf("body=%#v", rec.body)
	}
	labels, _ := rec.body["labels"].([]interface{})
	if len(labels) != 2 {
		t.Errorf("labels=%#v", rec.body["labels"])
	}
	crit, _ := rec.body["acceptanceCriteria"].([]interface{})
	if len(crit) != 1 {
		t.Errorf("criteria=%#v", rec.body["acceptanceCriteria"])
	}
}

func TestHandleTask_SelfAddTooFew_Fatal(t *testing.T) {
	res := runCLI(t, func() { handleTask([]string{"self-add", "OnlyTitle"}) })
	if !res.exited {
		t.Fatalf("expected fatal, got %+v", res)
	}
}

func TestHandleTask_LabelAddRemove(t *testing.T) {
	for _, action := range []string{"add", "remove"} {
		t.Run(action, func(t *testing.T) {
			rec := newServer(t, http.StatusOK, `{}`)
			res := runCLI(t, func() { handleTask([]string{"label", action, "T1", "ui"}) })
			if res.exited {
				t.Fatalf("unexpected exit: %+v", res)
			}
			if rec.path != "/api/office/agent/task/labels" {
				t.Errorf("path=%q", rec.path)
			}
			if _, ok := rec.body[action]; !ok {
				t.Errorf("expected %q key in body=%#v", action, rec.body)
			}
		})
	}
}

func TestHandleTask_LabelBadAction_Fatal(t *testing.T) {
	res := runCLI(t, func() { handleTask([]string{"label", "frobnicate", "T1", "ui"}) })
	if !res.exited {
		t.Fatalf("expected fatal, got %+v", res)
	}
}

func TestHandleTask_Criteria(t *testing.T) {
	for _, op := range []string{"set", "add", "remove"} {
		t.Run(op, func(t *testing.T) {
			rec := newServer(t, http.StatusOK, `{}`)
			res := runCLI(t, func() { handleTask([]string{"criteria", "T1", op, "value"}) })
			if res.exited {
				t.Fatalf("unexpected exit: %+v", res)
			}
			if rec.path != "/api/office/agent/task/criteria" || rec.body["op"] != op {
				t.Errorf("path=%q body=%#v", rec.path, rec.body)
			}
		})
	}
}

func TestHandleTask_CriteriaBadOp_Fatal(t *testing.T) {
	res := runCLI(t, func() { handleTask([]string{"criteria", "T1", "nope", "v"}) })
	if !res.exited {
		t.Fatalf("expected fatal, got %+v", res)
	}
}

func TestHandleTask_Triage(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{}`)
	res := runCLI(t, func() { handleTask([]string{"triage", "T1", "other-sess", "needs", "domain", "expert"}) })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.path != "/api/office/agent/task/triage" || rec.body["toSessionId"] != "other-sess" {
		t.Errorf("path=%q body=%#v", rec.path, rec.body)
	}
}

func TestHandleTask_HandoffFlow(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		wantPath string
	}{
		{"handoff", []string{"handoff", "T1", "cfg2", "here is context"}, "/api/office/agent/task/handoff"},
		{"accept", []string{"handoff-accept", "T1"}, "/api/office/agent/task/handoff/accept"},
		{"reject", []string{"handoff-reject", "T1", "too busy"}, "/api/office/agent/task/handoff/reject"},
		{"reject-noreason", []string{"handoff-reject", "T1"}, "/api/office/agent/task/handoff/reject"},
		{"cancel", []string{"handoff-cancel", "T1"}, "/api/office/agent/task/handoff/cancel"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := newServer(t, http.StatusOK, `{}`)
			res := runCLI(t, func() { handleTask(tc.args) })
			if res.exited {
				t.Fatalf("unexpected exit: %+v", res)
			}
			if rec.path != tc.wantPath {
				t.Errorf("path=%q want %q", rec.path, tc.wantPath)
			}
		})
	}
}
