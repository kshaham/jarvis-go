package pty

import "strings"

type BufferAccumulator struct {
	builder  strings.Builder
	maxSize  int
	trimSize int
}

func NewBufferAccumulator(maxSize, trimSize int) *BufferAccumulator {
	b := &BufferAccumulator{maxSize: maxSize, trimSize: trimSize}
	b.builder.Grow(trimSize)
	return b
}

func (b *BufferAccumulator) Append(data string) {
	if len(data) == 0 {
		return
	}
	b.builder.WriteString(data)
	if b.builder.Len() > b.maxSize {
		b.trim()
	}
}

func (b *BufferAccumulator) Value() string { return b.builder.String() }
func (b *BufferAccumulator) Len() int      { return b.builder.Len() }

func (b *BufferAccumulator) Clear() { b.builder.Reset() }

func (b *BufferAccumulator) Set(value string) {
	b.builder.Reset()
	b.builder.WriteString(value)
}

func (b *BufferAccumulator) Tail(n int) string {
	s := b.builder.String()
	if n >= len(s) {
		return s
	}
	return s[len(s)-n:]
}

func (b *BufferAccumulator) trim() {
	full := b.builder.String()
	keep := full[len(full)-b.trimSize:]
	b.builder.Reset()
	b.builder.WriteString(keep)
}
