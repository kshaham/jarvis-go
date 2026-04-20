package telegram

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

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
