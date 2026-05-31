package main

import (
	"errors"
	"io"
	"strings"
	"testing"
)

// errReader returns data once and then a non-EOF error, exercising the
// read-error branch of serveLoop.
type errReader struct {
	data []byte
	done bool
	err  error
}

func (e *errReader) Read(p []byte) (int, error) {
	if !e.done {
		e.done = true
		n := copy(p, e.data)
		return n, nil
	}
	return 0, e.err
}

func TestServeLoop_ValidRequestDispatched(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n"
	out := captureStdout(t, func() {
		if err := serveLoop(strings.NewReader(input)); err != nil {
			t.Errorf("serveLoop returned error: %v", err)
		}
	})
	resp := decodeResponse(t, firstLine(out))
	if resp.ID != float64(1) {
		t.Fatalf("expected id 1, got %v", resp.ID)
	}
	if resp.Error != nil {
		t.Fatalf("ping should succeed, got %v", resp.Error)
	}
}

func TestServeLoop_MultipleRequests(t *testing.T) {
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	}, "\n") + "\n"
	out := captureStdout(t, func() {
		if err := serveLoop(strings.NewReader(input)); err != nil {
			t.Errorf("serveLoop error: %v", err)
		}
	})
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 responses, got %d: %q", len(lines), out)
	}
}

func TestServeLoop_ParseErrorContinues(t *testing.T) {
	// First line is malformed JSON; second is valid. The loop must emit a parse
	// error for the first and still process the second.
	input := "not json at all\n" + `{"jsonrpc":"2.0","id":9,"method":"ping"}` + "\n"
	out := captureStdout(t, func() {
		if err := serveLoop(strings.NewReader(input)); err != nil {
			t.Errorf("serveLoop error: %v", err)
		}
	})
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines (parse error + ping), got %d: %q", len(lines), out)
	}
	parseErr := decodeResponse(t, lines[0])
	if parseErr.Error == nil || parseErr.Error.Code != -32700 {
		t.Fatalf("expected -32700 parse error, got %v", parseErr.Error)
	}
	ping := decodeResponse(t, lines[1])
	if ping.ID != float64(9) {
		t.Fatalf("expected second response id 9, got %v", ping.ID)
	}
}

func TestServeLoop_BlankLinesIgnored(t *testing.T) {
	input := "\n   \n" + `{"jsonrpc":"2.0","id":3,"method":"ping"}` + "\n\n"
	out := captureStdout(t, func() {
		if err := serveLoop(strings.NewReader(input)); err != nil {
			t.Errorf("serveLoop error: %v", err)
		}
	})
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 {
		t.Fatalf("blank lines should be ignored; expected 1 response, got %d: %q", len(lines), out)
	}
}

func TestServeLoop_EOFWithoutTrailingNewline(t *testing.T) {
	// A final request with no trailing newline: ReadBytes returns the data plus
	// io.EOF in the same call. The request must still be dispatched and the loop
	// must exit cleanly.
	input := `{"jsonrpc":"2.0","id":4,"method":"ping"}`
	out := captureStdout(t, func() {
		if err := serveLoop(strings.NewReader(input)); err != nil {
			t.Errorf("serveLoop error: %v", err)
		}
	})
	resp := decodeResponse(t, firstLine(out))
	if resp.ID != float64(4) {
		t.Fatalf("expected id 4 dispatched on EOF, got %v", resp.ID)
	}
}

func TestServeLoop_EmptyInputReturnsNil(t *testing.T) {
	out := captureStdout(t, func() {
		if err := serveLoop(strings.NewReader("")); err != nil {
			t.Errorf("expected nil on empty input, got %v", err)
		}
	})
	if strings.TrimSpace(out) != "" {
		t.Fatalf("empty input should produce no output, got %q", out)
	}
}

func TestServeLoop_ReadErrorPropagates(t *testing.T) {
	sentinel := errors.New("disk gone")
	r := &errReader{data: []byte("\n"), err: sentinel}
	var got error
	captureStdout(t, func() {
		got = serveLoop(r)
	})
	if !errors.Is(got, sentinel) {
		t.Fatalf("expected sentinel read error to propagate, got %v", got)
	}
}

func TestServeLoop_ReadErrorAfterValidLine(t *testing.T) {
	// A valid request followed (in the same Read) by a non-EOF error: the request
	// is dispatched, then the error propagates.
	sentinel := errors.New("broken pipe")
	r := &errReader{data: []byte(`{"jsonrpc":"2.0","id":5,"method":"ping"}` + "\n"), err: sentinel}
	var got error
	out := captureStdout(t, func() {
		got = serveLoop(r)
	})
	// The line ended in \n, so ReadBytes returns it with nil err first; the next
	// ReadBytes call hits the sentinel. The ping should have been answered.
	if firstLine(out) == "" {
		t.Fatal("expected the valid request to be dispatched before the error")
	}
	resp := decodeResponse(t, firstLine(out))
	if resp.ID != float64(5) {
		t.Fatalf("expected id 5, got %v", resp.ID)
	}
	if !errors.Is(got, sentinel) {
		t.Fatalf("expected sentinel error, got %v", got)
	}
}

// Ensure io is referenced (errReader implements io.Reader).
var _ io.Reader = (*errReader)(nil)
