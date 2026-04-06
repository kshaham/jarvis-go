package respawn

import "errors"

var (
	// ErrNoSessionWriter is returned when sendToSession is called without a writer.
	ErrNoSessionWriter = errors.New("respawn: no session writer configured")

	// ErrSendFailed is returned when all send retries are exhausted.
	ErrSendFailed = errors.New("respawn: all send attempts failed")
)
