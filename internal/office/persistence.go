package office

// R20 · Persistence hardening.
//
// Every load path in manager.go used to do `_ = json.Unmarshal(...)` or
// silently swallow read errors. A corrupted metrics.json became empty
// state; a malformed team-daily-budget.txt became 0 (unlimited). The
// operator found out days later when numbers didn't add up.
//
// loadJSONFile + loadTextFile centralize the pattern:
//   - parse error -> log ERROR, rename <file> to <file>.corrupt-<unix>,
//     broadcast office:stateCorruption, return zero value + false
//   - missing file -> return zero value + false (not an error, just
//     "no state yet")
//   - success -> return parsed value + true
//
// Quarantining the bad file instead of deleting it preserves forensic
// data for the operator to inspect. The broadcast is one-shot per
// load attempt so the dashboard toasts exactly once.

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// quarantineCorruptFile renames a corrupt state file to a timestamped
// suffix so it can't be reloaded next boot but is still available for
// the operator to inspect. Logs at WARN if the rename fails — no point
// panicking, the caller will still recover with zero value.
func quarantineCorruptFile(path string, reason error) string {
	ts := time.Now().Unix()
	corruptPath := fmt.Sprintf("%s.corrupt-%d", path, ts)
	if err := os.Rename(path, corruptPath); err != nil {
		slog.Warn("failed to quarantine corrupt state file",
			"path", path, "renameErr", err, "reason", reason)
		return ""
	}
	slog.Error("quarantined corrupt state file",
		"path", path, "corruptPath", corruptPath, "reason", reason)
	return corruptPath
}

// loadJSONFile reads a JSON file and unmarshals into dst. Returns
// (present, loaded): present=true if the file existed, loaded=true if
// it was parsed successfully. On parse failure the file is quarantined
// and a stateCorruption broadcast fires (if broadcast != nil).
func loadJSONFile(path string, dst interface{}, broadcast BroadcastFunc) (present, loaded bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, false
		}
		// Read error other than missing — log but treat as missing so
		// startup proceeds. The operator sees the log + a broadcast.
		slog.Warn("failed to read state file", "path", path, "err", err)
		if broadcast != nil {
			broadcast("office:stateCorruption", map[string]interface{}{
				"file":  path,
				"error": err.Error(),
				"kind":  "read",
			})
		}
		return true, false
	}
	if err := json.Unmarshal(data, dst); err != nil {
		corruptPath := quarantineCorruptFile(path, err)
		if broadcast != nil {
			broadcast("office:stateCorruption", map[string]interface{}{
				"file":         path,
				"corruptedPath": corruptPath,
				"error":        err.Error(),
				"kind":         "parse",
			})
		}
		return true, false
	}
	return true, true
}

// loadTextFile reads a text file and trims whitespace. Same
// present/loaded semantics as loadJSONFile. Used for the small
// mission + budget txt files that aren't JSON.
func loadTextFile(path string, broadcast BroadcastFunc) (value string, present, loaded bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, false
		}
		slog.Warn("failed to read state file", "path", path, "err", err)
		if broadcast != nil {
			broadcast("office:stateCorruption", map[string]interface{}{
				"file":  path,
				"error": err.Error(),
				"kind":  "read",
			})
		}
		return "", true, false
	}
	return strings.TrimSpace(string(data)), true, true
}

// loadFloat64File parses a text file as a non-negative float. Negative
// values are clamped to 0 with a WARN (they're almost certainly a
// copy/paste or edit accident). Parse failures are quarantined.
func loadFloat64File(path string, broadcast BroadcastFunc) (value float64, loaded bool) {
	raw, present, ok := loadTextFile(path, broadcast)
	if !present || !ok || raw == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		quarantineCorruptFile(path, err)
		if broadcast != nil {
			broadcast("office:stateCorruption", map[string]interface{}{
				"file":  path,
				"error": err.Error(),
				"kind":  "parse",
			})
		}
		return 0, false
	}
	if v < 0 {
		slog.Warn("state file contains negative value — clamping to 0",
			"path", path, "original", v)
		return 0, true
	}
	return v, true
}

// sanitizeMessageTimestamps drops messages with zero-time timestamps
// and clamps future timestamps to now. Rare but real: clock skew, a
// manual edit of messages.json, or a legacy unsanitized writer can
// leak invalid times that break dashboard sorting.
func sanitizeMessageTimestamps(msgs []OfficeMessage) (clean []OfficeMessage, dropped, clamped int) {
	now := time.Now()
	clean = make([]OfficeMessage, 0, len(msgs))
	for _, msg := range msgs {
		if msg.Timestamp.IsZero() {
			dropped++
			continue
		}
		if msg.Timestamp.After(now.Add(time.Hour)) {
			msg.Timestamp = now
			clamped++
		}
		clean = append(clean, msg)
	}
	return clean, dropped, clamped
}
