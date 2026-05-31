package pty

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestSession_PtyReader_ErrorSendDropped drives the non-blocking error-send's
// default (drop) arm in ptyReader (session.go:661-662): when f.Read returns an
// error but readCh (cap 1) is already full, the reader must NOT block -- it
// takes the default and returns. We pre-fill readCh, then close the pipe so the
// next Read errors, and assert the reader goroutine exits promptly without
// wedging.
func TestSession_PtyReader_ErrorSendDropped(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer r.Close()

	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	readCh := make(chan readResult, 1)
	// Saturate readCh so the reader's non-blocking error send must hit default.
	readCh <- readResult{data: []byte("occupied")}

	// Close the write end so the reader's f.Read returns io.EOF/closed-error.
	_ = w.Close()

	done := make(chan struct{})
	go func() {
		s.ptyReader(context.Background(), r, readCh)
		close(done)
	}()

	select {
	case <-done:
		// Reader exited via the dropped-error default arm without blocking.
	case <-time.After(2 * time.Second):
		t.Fatal("ptyReader blocked instead of dropping the error send on a full channel")
	}

	// The pre-existing item is still buffered (the error result was dropped).
	select {
	case got := <-readCh:
		if string(got.data) != "occupied" {
			t.Errorf("buffered item = %q, want the original (error result must have been dropped)", got.data)
		}
		if got.err != nil {
			t.Errorf("buffered err = %v, want nil (the error send was dropped, not delivered)", got.err)
		}
	default:
		t.Error("expected the original buffered item to remain")
	}
}

// TestSession_PtyReader_CtxDoneOnFullSend drives the ctx.Done abort arm in
// ptyReader (session.go:667-668): after a successful Read, the reader tries to
// send the chunk to a full readCh; with ctx already cancelled, the select must
// take <-ctx.Done() and return instead of blocking forever. We cancel ctx, fill
// readCh, then write a byte so f.Read returns data with nowhere to put it.
func TestSession_PtyReader_CtxDoneOnFullSend(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer r.Close()
	defer w.Close()

	s := NewSession(SessionConfig{WorkingDir: "/tmp", Mode: ModeShell})
	readCh := make(chan readResult, 1)
	readCh <- readResult{data: []byte("occupied")} // full

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // ctx is done before the reader attempts its blocking send

	// Feed one byte so the reader's Read returns data and it proceeds to the
	// send/ctx.Done select (the send cannot complete -> ctx.Done wins).
	if _, werr := w.Write([]byte("x")); werr != nil {
		t.Fatalf("write: %v", werr)
	}

	done := make(chan struct{})
	go func() {
		s.ptyReader(ctx, r, readCh)
		close(done)
	}()

	select {
	case <-done:
		// Reader honoured ctx.Done on the full-channel send.
	case <-time.After(2 * time.Second):
		t.Fatal("ptyReader did not abort on ctx.Done when the send channel was full")
	}
}
