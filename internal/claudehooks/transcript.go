package claudehooks

// Tail-read the Claude Code transcript JSONL to extract per-turn token
// usage. Hooks events do not carry usage natively, so we open the
// transcript file passed in the event payload (every event has it),
// read the last 64 KB, and parse the most recent assistant message's
// `usage` block. Cost is computed against a model->price table.
//
// Why 64 KB: a single turn's JSONL is typically <4 KB; 64 KB covers
// even the longest turns without paying for a full-file scan. For a
// transcript that has grown to MBs over a long session, this keeps
// the read O(1) per Stop event.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// TurnUsage is the structured result handed to AgentState.RecordTurnUsage.
// Cost is computed locally; the transcript only carries token counts.
type TurnUsage struct {
	InputTokens             int64
	OutputTokens            int64
	CacheCreationInputTokens int64
	CacheReadInputTokens    int64
	Model                   string
	CostUSD                 float64
}

// transcriptTailBytes caps how much of the transcript file we read on
// each Stop event. A single turn rarely exceeds 4 KB of JSONL; 64 KB
// is comfortably larger while staying microsecond-fast even on slow
// filesystems.
const transcriptTailBytes = 64 * 1024

// ReadLatestUsage opens the transcript JSONL at path, reads the last
// transcriptTailBytes, and returns the usage block from the most
// recent assistant message. Returns ErrNoUsage if no assistant
// message with a usage block was found in the tail (legitimate when
// the turn produced no output, e.g. a tool-only turn).
func ReadLatestUsage(path string) (*TurnUsage, error) {
	if path == "" {
		return nil, errors.New("claudehooks: transcript path is empty")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("claudehooks: open transcript: %w", err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("claudehooks: stat transcript: %w", err)
	}
	size := stat.Size()
	offset := int64(0)
	if size > transcriptTailBytes {
		offset = size - transcriptTailBytes
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, fmt.Errorf("claudehooks: seek: %w", err)
	}

	tail, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("claudehooks: read tail: %w", err)
	}

	// If we seeked into the middle of a JSON line, the first partial
	// line is unparseable -- drop everything up to the first newline.
	if offset > 0 {
		if i := bytes.IndexByte(tail, '\n'); i >= 0 {
			tail = tail[i+1:]
		}
	}

	// Walk lines in reverse, returning the first that parses as an
	// assistant message with a usage block. Most transcripts end with
	// the assistant's final message + a result line, so we usually
	// match on the second-to-last line.
	scanner := bufio.NewScanner(bytes.NewReader(tail))
	scanner.Buffer(make([]byte, 0, 8192), 1<<20)
	var lines [][]byte
	for scanner.Scan() {
		// Copy the slice -- scanner reuses its buffer between Scan calls.
		ln := make([]byte, len(scanner.Bytes()))
		copy(ln, scanner.Bytes())
		lines = append(lines, ln)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("claudehooks: scan: %w", err)
	}

	for i := len(lines) - 1; i >= 0; i-- {
		usage, ok := parseAssistantUsage(lines[i])
		if !ok {
			continue
		}
		return usage, nil
	}
	return nil, ErrNoUsage
}

// ErrNoUsage signals that no assistant message with a usage block
// was found in the transcript tail. Caller should treat this as "no
// cost to record this turn" rather than a hard error.
var ErrNoUsage = errors.New("claudehooks: no usage block in transcript tail")

// parseAssistantUsage tries to interpret one JSONL line as an
// assistant message and pulls out the usage / model fields. Returns
// (nil, false) for any other line shape.
func parseAssistantUsage(line []byte) (*TurnUsage, bool) {
	// Lenient probe -- the transcript schema has shifted between
	// Claude Code releases. We accept either a top-level
	// {type:"assistant", message:{model, usage:{...}}} OR a flatter
	// {role:"assistant", model, usage:{...}}.
	var probe struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		Message *struct {
			Model string `json:"model"`
			Usage *struct {
				InputTokens              int64 `json:"input_tokens"`
				OutputTokens             int64 `json:"output_tokens"`
				CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
				CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
			} `json:"usage"`
		} `json:"message"`
		Model string `json:"model"`
		Usage *struct {
			InputTokens              int64 `json:"input_tokens"`
			OutputTokens             int64 `json:"output_tokens"`
			CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(line, &probe); err != nil {
		return nil, false
	}
	isAssistant := probe.Type == "assistant" || probe.Role == "assistant"
	if !isAssistant {
		return nil, false
	}
	model := probe.Model
	var usage *struct {
		InputTokens              int64 `json:"input_tokens"`
		OutputTokens             int64 `json:"output_tokens"`
		CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	}
	if probe.Message != nil {
		if probe.Message.Model != "" {
			model = probe.Message.Model
		}
		usage = probe.Message.Usage
	}
	if usage == nil {
		usage = probe.Usage
	}
	if usage == nil {
		return nil, false
	}
	out := &TurnUsage{
		InputTokens:              usage.InputTokens,
		OutputTokens:             usage.OutputTokens,
		CacheCreationInputTokens: usage.CacheCreationInputTokens,
		CacheReadInputTokens:     usage.CacheReadInputTokens,
		Model:                    model,
	}
	out.CostUSD = ComputeCost(out)
	return out, true
}

// ModelPrice is the per-million-token rates Anthropic publishes for
// each Claude model. Cache pricing follows the documented multipliers
// (cache writes are ~25% above input; reads are ~10% of input).
type ModelPrice struct {
	InputPerMTok       float64
	OutputPerMTok      float64
	CacheWritePerMTok  float64
	CacheReadPerMTok   float64
}

// modelPrices is a static table of public list rates as of the model
// family this codebase targets (Claude 4.x). Unknown models fall back
// to Sonnet pricing -- a sane median that keeps cost reporting in the
// right order of magnitude even for new model IDs we haven't seen.
//
// Update this table when Anthropic ships new prices. Single source of
// truth for cost computation in hooks mode -- the SDK path gets
// per-turn cost from the SDK directly and bypasses this table.
var modelPrices = map[string]ModelPrice{
	"claude-opus-4-7":    {InputPerMTok: 15.0, OutputPerMTok: 75.0, CacheWritePerMTok: 18.75, CacheReadPerMTok: 1.5},
	"claude-opus-4-6":    {InputPerMTok: 15.0, OutputPerMTok: 75.0, CacheWritePerMTok: 18.75, CacheReadPerMTok: 1.5},
	"claude-sonnet-4-6":  {InputPerMTok: 3.0, OutputPerMTok: 15.0, CacheWritePerMTok: 3.75, CacheReadPerMTok: 0.3},
	"claude-sonnet-4-7":  {InputPerMTok: 3.0, OutputPerMTok: 15.0, CacheWritePerMTok: 3.75, CacheReadPerMTok: 0.3},
	"claude-haiku-4-5":   {InputPerMTok: 0.8, OutputPerMTok: 4.0, CacheWritePerMTok: 1.0, CacheReadPerMTok: 0.08},
}

// fallbackPrice is what we use when the model ID isn't in the table.
// Sonnet rates -- middle of the road; over-reports for Haiku and
// under-reports for Opus. Better than zero.
var fallbackPrice = modelPrices["claude-sonnet-4-6"]

// ComputeCost converts a TurnUsage into USD using the model price
// table. Exposed for tests; usually called via parseAssistantUsage.
func ComputeCost(u *TurnUsage) float64 {
	price, ok := modelPrices[u.Model]
	if !ok {
		// Many model IDs include date suffixes (claude-opus-4-7-20260101).
		// Strip and re-lookup.
		base := stripModelSuffix(u.Model)
		price, ok = modelPrices[base]
		if !ok {
			price = fallbackPrice
		}
	}
	const perMillion = 1_000_000.0
	cost := float64(u.InputTokens)/perMillion*price.InputPerMTok +
		float64(u.OutputTokens)/perMillion*price.OutputPerMTok +
		float64(u.CacheCreationInputTokens)/perMillion*price.CacheWritePerMTok +
		float64(u.CacheReadInputTokens)/perMillion*price.CacheReadPerMTok
	return cost
}

// stripModelSuffix drops a trailing "-YYYYMMDD" from a Claude model
// ID. Defensive against Anthropic adding date suffixes to model IDs.
func stripModelSuffix(model string) string {
	parts := strings.Split(model, "-")
	if len(parts) < 2 {
		return model
	}
	last := parts[len(parts)-1]
	if len(last) == 8 && allDigits(last) {
		return strings.Join(parts[:len(parts)-1], "-")
	}
	return model
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
