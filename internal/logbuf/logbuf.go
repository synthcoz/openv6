// Package logbuf keeps the most recent log lines in memory so they can be
// shown in the web panel and shipped from agents to the panel.
package logbuf

import (
	"strings"
	"sync"
)

type Buffer struct {
	mu    sync.Mutex
	lines []string
	max   int
	seq   uint64 // sequence number of the next line
	part  string
}

func New(max int) *Buffer { return &Buffer{max: max} }

// Write implements io.Writer (used with log.SetOutput).
func (b *Buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.part + string(p)
	parts := strings.Split(s, "\n")
	b.part = parts[len(parts)-1]
	for _, l := range parts[:len(parts)-1] {
		b.add(l)
	}
	return len(p), nil
}

func (b *Buffer) add(l string) {
	b.lines = append(b.lines, l)
	b.seq++
	if len(b.lines) > b.max {
		b.lines = b.lines[len(b.lines)-b.max:]
	}
}

// Append adds whole lines (e.g. received from an agent).
func (b *Buffer) Append(lines []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, l := range lines {
		b.add(l)
	}
}

// Since returns lines with sequence >= from (at most limit) and the next sequence.
func (b *Buffer) Since(from uint64, limit int) ([]string, uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	first := b.seq - uint64(len(b.lines))
	if from < first {
		from = first
	}
	out := append([]string(nil), b.lines[from-first:]...)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, from + uint64(len(out))
}

// Tail returns the last n lines.
func (b *Buffer) Tail(n int) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if n > len(b.lines) {
		n = len(b.lines)
	}
	return append([]string(nil), b.lines[len(b.lines)-n:]...)
}
