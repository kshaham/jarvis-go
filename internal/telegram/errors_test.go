package telegram

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func TestRedactTokenErr_URLError(t *testing.T) {
	token := "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11"
	orig := &url.Error{
		Op:  "Post",
		URL: "https://api.telegram.org/bot" + token + "/sendMessage",
		Err: errors.New("dial tcp: lookup api.telegram.org: no such host"),
	}
	got := redactTokenErr(orig, token)
	if strings.Contains(got.Error(), token) {
		t.Fatalf("token leaked in redacted error: %q", got.Error())
	}
	if !strings.Contains(got.Error(), "<REDACTED>") {
		t.Fatalf("expected <REDACTED> in output, got %q", got.Error())
	}
}

func TestRedactTokenErr_WrappedNonURL(t *testing.T) {
	token := "987654:XYZ"
	inner := fmt.Errorf("telegram said no to token %s, please retry", token)
	wrapped := fmt.Errorf("queue: %w", inner)
	got := redactTokenErr(wrapped, token)
	if strings.Contains(got.Error(), token) {
		t.Fatalf("token leaked: %q", got.Error())
	}
}

func TestRedactTokenErr_NoTokenPresent(t *testing.T) {
	token := "secrettoken"
	orig := errors.New("some other failure without the secret")
	got := redactTokenErr(orig, token)
	if got.Error() != orig.Error() {
		t.Fatalf("error should pass through unchanged; got %q", got.Error())
	}
}

func TestRedactTokenErr_EmptyToken(t *testing.T) {
	orig := errors.New("whatever")
	got := redactTokenErr(orig, "")
	if got != orig {
		t.Fatal("empty token should be a no-op (same error instance)")
	}
}

func TestRedactTokenErr_NilErr(t *testing.T) {
	if got := redactTokenErr(nil, "tok"); got != nil {
		t.Fatalf("nil err should stay nil, got %v", got)
	}
}
