package odin

import "testing"

// buildBlob hand-encodes: NamedStartOfStructNode "root", NamedInt "x"=7, EndOfNode.
func buildBlob() []byte {
	// The byte() truncations below are the little-endian encoding itself; the
	// inputs are short literals from this file.
	str := func(s string) []byte {
		out := []byte{0x01}                                              // utf16 flag
		n := int32(len(s))                                               //nolint:gosec // fixed-length test literal
		out = append(out, byte(n), byte(n>>8), byte(n>>16), byte(n>>24)) //nolint:gosec // little-endian byte split
		for _, c := range s {
			out = append(out, byte(c), 0x00) //nolint:gosec // ASCII test literal
		}
		return out
	}
	i32 := func(v int32) []byte { return []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)} } //nolint:gosec // little-endian byte split
	var b []byte
	b = append(b, 0x03)           // NamedStartOfStructNode
	b = append(b, str("root")...) // name
	b = append(b, 0x2E)           // UnnamedNull type marker (ReadType -> nil)
	b = append(b, 0x17)           // NamedInt
	b = append(b, str("x")...)    // name
	b = append(b, i32(7)...)      // value
	b = append(b, 0x05)           // EndOfNode
	return b
}

func TestDecoderStructIntEnd(t *testing.T) {
	d := newDecoder(buildBlob())
	d.run()
	if len(d.toks) != 3 {
		t.Fatalf("toks = %d, want 3 (%+v)", len(d.toks), d.toks)
	}
	if d.toks[0].kind != "node-start" || d.toks[0].name != "root" {
		t.Fatalf("tok0 = %+v", d.toks[0])
	}
	v, ok := d.toks[1].value.(int32)
	if d.toks[1].kind != "int" || d.toks[1].name != "x" || !ok || v != 7 {
		t.Fatalf("tok1 = %+v", d.toks[1])
	}
	if d.toks[2].kind != "node-end" {
		t.Fatalf("tok2 = %+v", d.toks[2])
	}
}
