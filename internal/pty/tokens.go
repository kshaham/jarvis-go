package pty

import (
	"regexp"
	"strconv"
	"strings"
)

var tokenPattern = regexp.MustCompile(`\b(\d+(?:\.\d+)?)\s*([kKmM])?\s*tokens\b`)

// ParseTokenCount extracts a token count from a string like "123.4k tokens".
func ParseTokenCount(s string) (int64, bool) {
	if !strings.Contains(s, "token") {
		return 0, false
	}

	match := tokenPattern.FindStringSubmatch(s)
	if match == nil {
		return 0, false
	}

	count, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return 0, false
	}

	switch strings.ToLower(match[2]) {
	case "k":
		count *= 1000
	case "m":
		count *= 1_000_000
	}

	tokenCount := int64(count)
	if tokenCount > MaxSessionTokens || tokenCount <= 0 {
		return 0, false
	}

	return tokenCount, true
}
