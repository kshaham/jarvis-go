package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setArgs swaps os.Args for the duration of a test.
func setArgs(t *testing.T, args ...string) {
	t.Helper()
	old := os.Args
	os.Args = append([]string{"cm"}, args...)
	t.Cleanup(func() { os.Args = old })
}

func TestMain_NoArgs_PrintsUsageExit0(t *testing.T) {
	setArgs(t)
	res := runCLI(t, main)
	if !res.exited || res.code != 0 {
		t.Fatalf("expected exit(0), got %+v", res)
	}
	if !strings.Contains(res.stdout, "Jarvis agent CLI") {
		t.Errorf("stdout=%q", res.stdout)
	}
}

func TestMain_Help(t *testing.T) {
	for _, h := range []string{"help", "--help", "-h"} {
		t.Run(h, func(t *testing.T) {
			setArgs(t, h)
			res := runCLI(t, main)
			if res.exited {
				t.Fatalf("help should not exit: %+v", res)
			}
			if !strings.Contains(res.stdout, "Commands:") {
				t.Errorf("stdout=%q", res.stdout)
			}
		})
	}
}

func TestMain_UnknownCommand_Exit1(t *testing.T) {
	setArgs(t, "bogus")
	res := runCLI(t, main)
	if !res.exited || res.code != 1 {
		t.Fatalf("expected exit(1), got %+v", res)
	}
	if !strings.Contains(res.stderr, "Unknown command") {
		t.Errorf("stderr=%q", res.stderr)
	}
}

func TestMain_Lock(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{}`)
	setArgs(t, "lock", "/path/to/file.go")
	res := runCLI(t, main)
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.path != "/api/office/agent/file-lock" || rec.body["action"] != "lock" {
		t.Errorf("path=%q body=%#v", rec.path, rec.body)
	}
	if !strings.Contains(res.stdout, "Locked:") {
		t.Errorf("stdout=%q", res.stdout)
	}
}

func TestMain_Unlock(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{}`)
	setArgs(t, "unlock", "/path/to/file.go")
	res := runCLI(t, main)
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.body["action"] != "unlock" {
		t.Errorf("body=%#v", rec.body)
	}
	if !strings.Contains(res.stdout, "Unlocked:") {
		t.Errorf("stdout=%q", res.stdout)
	}
}

func TestMain_Locks(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{"locks":[]}`)
	setArgs(t, "locks")
	res := runCLI(t, main)
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.method != http.MethodGet || rec.path != "/api/office/agent/file-locks" {
		t.Errorf("method=%q path=%q", rec.method, rec.path)
	}
}

func TestMain_Roster(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{"members":[]}`)
	setArgs(t, "roster")
	res := runCLI(t, main)
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.path != "/api/office/agent/team-roster" {
		t.Errorf("path=%q", rec.path)
	}
}

func TestMain_Message(t *testing.T) {
	rec := newServer(t, http.StatusOK, `{}`)
	setArgs(t, "message", "other-sess", "ping", "you")
	res := runCLI(t, main)
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if rec.path != "/api/office/agent/message" || rec.body["toSessionId"] != "other-sess" || rec.body["content"] != "ping you" {
		t.Errorf("path=%q body=%#v", rec.path, rec.body)
	}
}

func TestMain_LockMissingArg_Fatal(t *testing.T) {
	newServer(t, http.StatusOK, `{}`)
	setArgs(t, "lock")
	res := runCLI(t, main)
	if !res.exited {
		t.Fatalf("expected fatal, got %+v", res)
	}
}

// TestMain_DispatchAllArms routes one minimal invocation through every switch
// arm so the dispatch table itself is covered. Handlers may fatal on the given
// args; that still executes the arm, which is the point here.
func TestMain_DispatchAllArms(t *testing.T) {
	commands := [][]string{
		{"standup", "respond", "x"},
		{"brainstorm", "respond", "x"},
		{"leader-tasks", "respond", "x"},
		{"reflect", "learned", "--category", "process"},
		{"discover", "summary", "--findings", "a"},
		{"task", "ack", "T1"},
		{"ask", "list"},
		{"skill", "list"},
		{"memory", "read"},
		{"dev", "list"},
		{"knowledge", "tail"},
		{"know", "tail"},
		{"onboard", "protocols"},
		{"onboarding", "skills"},
		{"project", "invariants"},
	}
	for _, cmd := range commands {
		t.Run(cmd[0], func(t *testing.T) {
			newServer(t, http.StatusOK, `{}`)
			setArgs(t, cmd...)
			// We don't assert success/exit here -- just that the arm runs
			// without an unexpected (non-fatal) panic.
			_ = runCLI(t, main)
		})
	}
}

// ---- uploadTaskScreenshot mime sniffing ----

func TestUploadTaskScreenshot_MimeSniff(t *testing.T) {
	cases := []struct {
		name     string
		bytes    []byte
		wantMime string
	}{
		{"png", []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, "image/png"},
		{"jpeg", []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10}, "image/jpeg"},
		{"webp", append([]byte("RIFF"), append([]byte{0, 0, 0, 0}, []byte("WEBP")...)...), "image/webp"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := newServer(t, http.StatusOK, `{}`)
			dir := t.TempDir()
			path := filepath.Join(dir, "shot."+tc.name)
			if err := os.WriteFile(path, tc.bytes, 0o644); err != nil {
				t.Fatal(err)
			}
			var err error
			res := runCLI(t, func() { err = uploadTaskScreenshot("T1", path) })
			if res.exited {
				t.Fatalf("unexpected exit: %+v", res)
			}
			if err != nil {
				t.Fatalf("uploadTaskScreenshot: %v", err)
			}
			if rec.body["mimeType"] != tc.wantMime {
				t.Errorf("mime=%v want %q", rec.body["mimeType"], tc.wantMime)
			}
			if rec.body["dataBase64"] == "" {
				t.Errorf("expected base64 data, body=%#v", rec.body)
			}
		})
	}
}

func TestUploadTaskScreenshot_ReadError(t *testing.T) {
	newServer(t, http.StatusOK, `{}`)
	var err error
	res := runCLI(t, func() { err = uploadTaskScreenshot("T1", "/no/such/file.png") })
	if res.exited {
		t.Fatalf("unexpected exit: %+v", res)
	}
	if err == nil {
		t.Fatalf("expected read error")
	}
}
