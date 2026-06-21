package odin

import "testing"

func TestReaderPrimitivesLE(t *testing.T) {
	// int32 = 0x04030201 LE, float32 = 1.5 (0x3FC00000 LE)
	data := []byte{0x01, 0x02, 0x03, 0x04, 0x00, 0x00, 0xC0, 0x3F}
	r := newReader(data)
	if got := r.readInt32(); got != 0x04030201 {
		t.Fatalf("readInt32 = %#x, want 0x04030201", got)
	}
	if got := r.readSingle(); got != 1.5 {
		t.Fatalf("readSingle = %v, want 1.5", got)
	}
	if !r.end() {
		t.Fatalf("expected end of buffer")
	}
}

func TestReaderStringUTF16(t *testing.T) {
	// flag=1, count=2, "Hi" UTF16LE
	data := []byte{0x01, 0x02, 0x00, 0x00, 0x00, 'H', 0x00, 'i', 0x00}
	r := newReader(data)
	if got := r.readString(); got != "Hi" {
		t.Fatalf("readString utf16 = %q, want %q", got, "Hi")
	}
}

func TestReaderString8bit(t *testing.T) {
	// flag=0, count=3, "abc" one byte each
	data := []byte{0x00, 0x03, 0x00, 0x00, 0x00, 'a', 'b', 'c'}
	r := newReader(data)
	if got := r.readString(); got != "abc" {
		t.Fatalf("readString 8bit = %q, want %q", got, "abc")
	}
}
