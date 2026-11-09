package logbuf

import "testing"

func TestBuffer(t *testing.T) {
	b := New(3)
	b.Write([]byte("a\nb\npart"))
	b.Write([]byte("ial\nc\n"))
	if got := b.Tail(10); len(got) != 3 || got[0] != "b" || got[1] != "partial" || got[2] != "c" {
		t.Fatalf("tail %v", got)
	}
	lines, next := b.Since(0, 0)
	if len(lines) != 3 || next != 4 {
		t.Fatalf("since %v %d", lines, next)
	}
	b.Write([]byte("d\n"))
	lines, next = b.Since(next, 0)
	if len(lines) != 1 || lines[0] != "d" || next != 5 {
		t.Fatalf("since2 %v %d", lines, next)
	}
}
