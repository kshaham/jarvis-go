package claudehooks

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadLatestUsage_TopLevelUsage(t *testing.T) {
	// Some Claude Code transcript formats put usage at the top level
	// of an assistant line (older format). Verify we handle it.
	jsonl := strings.Join([]string{
		`{"type":"user","content":"hello"}`,
		`{"type":"assistant","model":"claude-sonnet-4-6","usage":{"input_tokens":1000,"output_tokens":2000,"cache_creation_input_tokens":500,"cache_read_input_tokens":3000}}`,
		`{"type":"result","subtype":"success"}`,
	}, "\n") + "\n"

	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := os.WriteFile(path, []byte(jsonl), 0o644); err != nil {
		t.Fatalf("seed transcript: %v", err)
	}

	usage, err := ReadLatestUsage(path)
	if err != nil {
		t.Fatalf("ReadLatestUsage: %v", err)
	}
	if usage.InputTokens != 1000 || usage.OutputTokens != 2000 {
		t.Errorf("token counts wrong: %+v", usage)
	}
	if usage.Model != "claude-sonnet-4-6" {
		t.Errorf("model = %q, want claude-sonnet-4-6", usage.Model)
	}
	// Sonnet-4-6: $3/MTok input, $15/MTok output, $3.75 cache write, $0.30 cache read
	// = (1000*3 + 2000*15 + 500*3.75 + 3000*0.30) / 1_000_000
	// = (3000 + 30000 + 1875 + 900) / 1_000_000 = 0.035775
	if usage.CostUSD < 0.0357 || usage.CostUSD > 0.0358 {
		t.Errorf("cost USD = %v, want ~0.035775", usage.CostUSD)
	}
}

func TestReadLatestUsage_NestedMessageUsage(t *testing.T) {
	// Newer transcript shape: message: {model, usage}.
	jsonl := strings.Join([]string{
		`{"type":"user","content":"hi"}`,
		`{"type":"assistant","message":{"model":"claude-opus-4-7","usage":{"input_tokens":2000,"output_tokens":4000}}}`,
		`{"type":"result"}`,
	}, "\n") + "\n"
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	_ = os.WriteFile(path, []byte(jsonl), 0o644)

	usage, err := ReadLatestUsage(path)
	if err != nil {
		t.Fatalf("ReadLatestUsage: %v", err)
	}
	if usage.Model != "claude-opus-4-7" {
		t.Errorf("model = %q, want claude-opus-4-7", usage.Model)
	}
	// Opus-4-7: $15 in, $75 out, no cache tokens.
	// = (2000*15 + 4000*75) / 1_000_000 = (30000 + 300000) / 1M = 0.33
	if usage.CostUSD < 0.32 || usage.CostUSD > 0.34 {
		t.Errorf("cost USD = %v, want ~0.33", usage.CostUSD)
	}
}

func TestReadLatestUsage_ReturnsLatest(t *testing.T) {
	// Multiple assistant messages: should pick the most recent one.
	jsonl := strings.Join([]string{
		`{"type":"assistant","model":"claude-sonnet-4-6","usage":{"input_tokens":100,"output_tokens":100}}`,
		`{"type":"assistant","model":"claude-sonnet-4-6","usage":{"input_tokens":999,"output_tokens":999}}`,
		`{"type":"result"}`,
	}, "\n") + "\n"
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	_ = os.WriteFile(path, []byte(jsonl), 0o644)

	usage, err := ReadLatestUsage(path)
	if err != nil {
		t.Fatalf("ReadLatestUsage: %v", err)
	}
	if usage.InputTokens != 999 {
		t.Errorf("expected latest assistant entry, got input=%d", usage.InputTokens)
	}
}

func TestReadLatestUsage_NoUsage(t *testing.T) {
	// Tool-only turns may not produce an assistant message with usage.
	jsonl := `{"type":"user"}` + "\n" + `{"type":"result"}` + "\n"
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	_ = os.WriteFile(path, []byte(jsonl), 0o644)

	_, err := ReadLatestUsage(path)
	if !errors.Is(err, ErrNoUsage) {
		t.Errorf("expected ErrNoUsage, got: %v", err)
	}
}

func TestReadLatestUsage_LargeTranscript_TailOnly(t *testing.T) {
	// 200 KB of filler before the assistant line; ensure tail-read
	// finds the latest line without scanning the whole file.
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	for i := 0; i < 2000; i++ {
		// Each filler line ~100 bytes; 2000 lines ~200 KB.
		_, _ = f.WriteString(`{"type":"user","content":"some long filler content that takes up bytes ` + strings.Repeat("x", 50) + `"}` + "\n")
	}
	_, _ = f.WriteString(`{"type":"assistant","model":"claude-haiku-4-5","usage":{"input_tokens":10,"output_tokens":20}}` + "\n")
	_, _ = f.WriteString(`{"type":"result"}` + "\n")
	_ = f.Close()

	usage, err := ReadLatestUsage(path)
	if err != nil {
		t.Fatalf("ReadLatestUsage: %v", err)
	}
	if usage.Model != "claude-haiku-4-5" || usage.InputTokens != 10 {
		t.Errorf("expected haiku 10/20, got %+v", usage)
	}
}

func TestReadLatestUsage_MissingFile(t *testing.T) {
	_, err := ReadLatestUsage(filepath.Join(t.TempDir(), "does-not-exist.jsonl"))
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestComputeCost_UnknownModelFallsBackToSonnet(t *testing.T) {
	got := ComputeCost(&TurnUsage{
		InputTokens:  1_000_000,
		OutputTokens: 0,
		Model:        "claude-some-future-model",
	})
	// Sonnet input rate: $3/MTok.
	if got < 2.99 || got > 3.01 {
		t.Errorf("expected ~$3 for unknown model fallback, got %v", got)
	}
}

func TestComputeCost_ModelDateSuffixStripped(t *testing.T) {
	got := ComputeCost(&TurnUsage{
		InputTokens:  1_000_000,
		OutputTokens: 0,
		Model:        "claude-opus-4-7-20260101",
	})
	// Opus input rate: $15/MTok.
	if got < 14.9 || got > 15.1 {
		t.Errorf("expected ~$15 (opus rate after date strip), got %v", got)
	}
}
