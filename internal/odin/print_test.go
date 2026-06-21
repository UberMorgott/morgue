package odin

import (
	"strings"
	"testing"
)

func TestPrintTreeStructInt(t *testing.T) {
	toks := []tok{
		{kind: "node-start", name: "root", typeStr: "", depth: 0},
		{kind: "int", name: "x", value: int32(7), depth: 1},
		{kind: "node-end", depth: 0},
	}
	got := printTree(toks)
	want := strings.Join([]string{
		"root { ",
		"  x = 7  (int)",
		"}",
		"",
	}, "\n")
	if got != want {
		t.Fatalf("printTree mismatch:\n got=%q\nwant=%q", got, want)
	}
}

func TestPrimArrayFloats(t *testing.T) {
	// two float32: 1.5, 0.25
	raw := []byte{0x00, 0x00, 0xC0, 0x3F, 0x00, 0x00, 0x80, 0x3E}
	got := decodePrimArray(raw, 4, "System.Single")
	want := "= [1.5, 0.25]  (float[] n=2)"
	if got != want {
		t.Fatalf("decodePrimArray = %q, want %q", got, want)
	}
}
