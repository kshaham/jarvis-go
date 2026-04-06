package pty

import "testing"

func TestBufferAccumulator_Append(t *testing.T) {
	b := NewBufferAccumulator(100, 50)
	b.Append("hello")
	if b.Len() != 5 {
		t.Errorf("expected len 5, got %d", b.Len())
	}
	if b.Value() != "hello" {
		t.Errorf("expected 'hello', got %q", b.Value())
	}
}

func TestBufferAccumulator_AutoTrim(t *testing.T) {
	b := NewBufferAccumulator(20, 10)
	b.Append("12345678901234567890X") // 21 bytes > max 20
	if b.Len() != 10 {
		t.Errorf("expected len 10 after trim, got %d", b.Len())
	}
	// Should keep last 10 bytes: "234567890X" -- wait, trim keeps last trimSize bytes
	// "12345678901234567890X" has 21 chars, keep last 10 = "1234567890X"[11:] = "34567890X" -- no
	// Actually: full[len(full)-trimSize:] = full[21-10:] = full[11:] = "0X" -- wait
	// Let me recalculate: "12345678901234567890X" = 21 chars
	// full[21-10:] = full[11:] = "34567890X" -- that's 9 chars? No.
	// "12345678901234567890X"[11:] = "4567890X" -- let me count properly
	// 0:"1" 1:"2" 2:"3" 3:"4" 4:"5" 5:"6" 6:"7" 7:"8" 8:"9" 9:"0" 10:"1" 11:"2" 12:"3" 13:"4" 14:"5" 15:"6" 16:"7" 17:"8" 18:"9" 19:"0" 20:"X"
	// full[11:] = "234567890X" -- that's 10 chars. Correct!
	expected := "234567890X"
	if b.Value() != expected {
		t.Errorf("expected %q after trim, got %q", expected, b.Value())
	}
}

func TestBufferAccumulator_Clear(t *testing.T) {
	b := NewBufferAccumulator(100, 50)
	b.Append("data")
	b.Clear()
	if b.Len() != 0 {
		t.Errorf("expected len 0, got %d", b.Len())
	}
}

func TestBufferAccumulator_Tail(t *testing.T) {
	b := NewBufferAccumulator(100, 50)
	b.Append("abcdefghij")
	if tail := b.Tail(5); tail != "fghij" {
		t.Errorf("expected 'fghij', got %q", tail)
	}
}

func TestBufferAccumulator_TailExceedsLength(t *testing.T) {
	b := NewBufferAccumulator(100, 50)
	b.Append("abc")
	if tail := b.Tail(100); tail != "abc" {
		t.Errorf("expected 'abc', got %q", tail)
	}
}

func TestBufferAccumulator_Set(t *testing.T) {
	b := NewBufferAccumulator(100, 50)
	b.Append("old")
	b.Set("new")
	if b.Value() != "new" {
		t.Errorf("expected 'new', got %q", b.Value())
	}
}

func TestBufferAccumulator_EmptyAppend(t *testing.T) {
	b := NewBufferAccumulator(100, 50)
	b.Append("")
	if b.Len() != 0 {
		t.Errorf("expected len 0, got %d", b.Len())
	}
}

func BenchmarkBufferAppend(b *testing.B) {
	buf := NewBufferAccumulator(MaxTerminalBufferSize, TrimTerminalTo)
	data := "\x1b[32mSome colored output\x1b[0m with text\n"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Append(data)
	}
}
