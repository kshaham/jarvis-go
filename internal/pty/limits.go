package pty

const (
	MaxTerminalBufferSize = 2 * 1024 * 1024
	TrimTerminalTo        = 1536 * 1024
	MaxTextOutputSize     = 1 * 1024 * 1024
	TrimTextTo            = 768 * 1024
	MaxMessages           = 1000
	TrimMessagesTo        = 800
	MaxLineBufferSize     = 64 * 1024
	MaxConcurrentSessions = 50
	MaxSessionTokens      = 2_000_000
	MaxDeltaPerUpdate     = 1_000_000
)
