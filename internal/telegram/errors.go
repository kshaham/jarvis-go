package telegram

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync/atomic"
)

// apiBaseURLValue holds the base URL for all Telegram Bot API calls. It is an
// atomic so background goroutines (poll loop, command registration, queue
// sends) can read it concurrently while tests swap it to a local httptest
// server without a data race. Production code never reassigns it. Always
// combined with a "/bot<token>/<method>" suffix at the call sites.
var apiBaseURLValue atomic.Value

func init() {
	apiBaseURLValue.Store("https://api.telegram.org")
}

// apiBaseURL returns the current Telegram API base URL.
func apiBaseURL() string {
	v, _ := apiBaseURLValue.Load().(string)
	if v == "" {
		return "https://api.telegram.org"
	}
	return v
}

// redactTokenErr returns a copy of err with every occurrence of token
// replaced by "<REDACTED>". Handles *url.Error specially so the URL
// field (which Go embeds as-is in Error()) is redacted at its source;
// all other errors get a string-replace pass.
//
// Go's net/http wraps transport errors as *url.Error{Op, URL, Err}
// whose Error() returns 'Op "URL": Err'. Our Telegram URLs are of the
// form https://api.telegram.org/bot<TOKEN>/<method>, so a DNS or TLS
// failure would otherwise dump the bot token into any log or response
// body that stringifies the error.
//
// An empty token or nil error is a no-op.
func redactTokenErr(err error, token string) error {
	if err == nil || token == "" {
		return err
	}
	var uerr *url.Error
	if errors.As(err, &uerr) {
		uerr.URL = strings.ReplaceAll(uerr.URL, token, "<REDACTED>")
		return err
	}
	if strings.Contains(err.Error(), token) {
		return fmt.Errorf("%s", strings.ReplaceAll(err.Error(), token, "<REDACTED>"))
	}
	return err
}
