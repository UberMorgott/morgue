package recipe

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func u16(s string) []byte {
	b := make([]byte, 0, len(s)*2)
	for _, r := range s {
		var c [2]byte
		binary.LittleEndian.PutUint16(c[:], uint16(r))
		b = append(b, c[:]...)
	}
	return b
}

// varRef encodes a NS_VAR_CODE reference the way a Unicode NSIS build does:
// the code char, then one char holding the index 7 bits per byte-half.
func varRef(idx int) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint16(b[0:2], nsVarCode)
	binary.LittleEndian.PutUint16(b[2:4], uint16((idx&0x7F)|0x80|((idx>>7)&0x7F)<<8|0x8000))
	return b
}

// TestExtractStructured_UnicodePrefixed covers the real NSIS-3 solid layout that
// a real signed commercial installer uses: a uint32 header-size prefix ahead of the header
// (all block offsets relative to the byte AFTER it), UTF-16LE strings addressed
// by CHARACTER index, and a $VAR escape inside the path.
func TestExtractStructured_UnicodePrefixed(t *testing.T) {
	const content = "Hello NSIS!"

	entriesOff := 68 // relative to the header start
	stringsOff := entriesOff + nsisEntrySize

	var strTab []byte
	strTab = append(strTab, 0, 0) // char 0: empty string
	nameChar := len(strTab) / 2
	strTab = append(strTab, varRef(21)...) // $INSTDIR
	strTab = append(strTab, u16("\\sub\\hello.txt")...)
	strTab = append(strTab, 0, 0)

	langOff := stringsOff + len(strTab)
	hdrSize := langOff

	header := make([]byte, hdrSize)
	putBlock := func(idx, off, num int) {
		base := 4 + idx*8
		binary.LittleEndian.PutUint32(header[base:base+4], uint32(off))
		binary.LittleEndian.PutUint32(header[base+4:base+8], uint32(num))
	}
	putBlock(nbEntries, entriesOff, 1)
	putBlock(nbStrings, stringsOff, 0)
	putBlock(nbLangtables, langOff, 0)

	e := header[entriesOff:]
	binary.LittleEndian.PutUint32(e[0:4], ewExtractFile)
	binary.LittleEndian.PutUint32(e[8:12], uint32(nameChar)) // param1 = name (char index)
	binary.LittleEndian.PutUint32(e[12:16], 0)               // param2 = data position
	copy(header[stringsOff:], strTab)

	// Solid stream: [uint32 hdrSize][header][data records].
	stream := make([]byte, 4)
	binary.LittleEndian.PutUint32(stream, uint32(hdrSize))
	stream = append(stream, header...)
	rec := make([]byte, 4)
	binary.LittleEndian.PutUint32(rec, uint32(len(content)))
	stream = append(stream, append(rec, []byte(content)...)...)

	out := t.TempDir()
	files, unicode, entries, wanted := extractStructured(stream, hdrSize, out, func(string) {})
	if !unicode {
		t.Error("string table should be detected as Unicode")
	}
	if entries != 1 || wanted != 1 || files != 1 {
		t.Fatalf("entries=%d wanted=%d files=%d, want 1/1/1", entries, wanted, files)
	}
	got, err := os.ReadFile(filepath.Join(out, "$INSTDIR", "sub", "hello.txt"))
	if err != nil {
		t.Fatalf("expected $INSTDIR/sub/hello.txt: %v", err)
	}
	if string(got) != content {
		t.Errorf("content = %q, want %q", got, content)
	}
}
