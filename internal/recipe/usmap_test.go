package recipe

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"
)

// realUsmap is a known-good ue4ss Mappings.usmap (version 4 / ExplicitEnumValues,
// Zstd-compressed) used for golden parsing. Skipped when absent so CI without the
// game files still passes.
const realUsmap = `D:\Steam\steamapps\common\Windrose\R5\Binaries\Win64\ue4ss\Mappings.usmap`

func TestParseUsmap_RealFile(t *testing.T) {
	if _, err := os.Stat(realUsmap); err != nil {
		t.Skipf("real usmap not present: %v", err)
	}
	m, err := ParseUsmap(realUsmap)
	if err != nil {
		t.Fatalf("ParseUsmap: %v", err)
	}
	if m.Version != 4 {
		t.Errorf("Version = %d, want 4", m.Version)
	}
	if m.CompressionMethod != usmapZStandard {
		t.Errorf("CompressionMethod = %d, want ZStandard(3)", m.CompressionMethod)
	}
	if len(m.Names) == 0 {
		t.Fatal("Names empty")
	}
	if len(m.Structs) == 0 {
		t.Fatal("Structs empty")
	}
	// A real UE mappings file has thousands of struct/class schemas.
	if len(m.Structs) < 100 {
		t.Errorf("Structs = %d, suspiciously low", len(m.Structs))
	}
	// Sanity: every struct name must be a non-empty resolved name.
	for i, s := range m.Structs {
		if s.Name == "" {
			t.Fatalf("Structs[%d] has empty name", i)
		}
		if i > 50 {
			break
		}
	}
	t.Logf("parsed usmap: ver=%d comp=%d names=%d enums=%d structs=%d",
		m.Version, m.CompressionMethod, len(m.Names), len(m.Enums), len(m.Structs))
}

// TestParseUsmap_BadMagic rejects a non-usmap file fast.
func TestParseUsmap_BadMagic(t *testing.T) {
	buf := []byte{0x00, 0x00, 0x04, 0x00}
	if _, err := parseUsmapBytes(buf); err == nil {
		t.Fatal("expected error on bad magic, got nil")
	}
}

// TestParseUsmap_FutureVersion rejects an unknown (too-new) version.
func TestParseUsmap_FutureVersion(t *testing.T) {
	buf := []byte{0xC4, 0x30, 0xFF} // magic ok, version 255
	if _, err := parseUsmapBytes(buf); err == nil {
		t.Fatal("expected error on future version, got nil")
	}
}

// TestParseUsmap_ClampNameCount is the memory-safety guard: a header that claims
// a huge name count must be rejected, NOT trigger a giant allocation. We build a
// minimal valid header (version 0, no versioning, None compression, comp==decomp)
// whose decompressed body's nameSize is absurd.
func TestParseUsmap_ClampNameCount(t *testing.T) {
	body := &bytes.Buffer{}
	// nameSize = 0x7FFFFFFF (claims ~2 billion names) but no data follows.
	binary.Write(body, binary.LittleEndian, uint32(0x7FFFFFFF))
	payload := body.Bytes()

	hdr := &bytes.Buffer{}
	hdr.Write([]byte{0xC4, 0x30})                                // magic
	hdr.WriteByte(0x00)                                          // version 0 (Initial) -> no versioning bool read
	hdr.WriteByte(0x00)                                          // CompressionMethod None
	binary.Write(hdr, binary.LittleEndian, uint32(len(payload))) // compSize
	binary.Write(hdr, binary.LittleEndian, uint32(len(payload))) // decompSize (==comp)
	hdr.Write(payload)

	_, err := parseUsmapBytes(hdr.Bytes())
	if err == nil {
		t.Fatal("expected clamp error on absurd nameSize, got nil")
	}
}

// TestParseUsmap_ClampDecompSize guards the decompressed-size allocation itself.
func TestParseUsmap_ClampDecompSize(t *testing.T) {
	hdr := &bytes.Buffer{}
	hdr.Write([]byte{0xC4, 0x30})
	hdr.WriteByte(0x00)                                        // version 0
	hdr.WriteByte(0x00)                                        // None
	binary.Write(hdr, binary.LittleEndian, uint32(8))          // compSize
	binary.Write(hdr, binary.LittleEndian, uint32(0xFFFFFFFF)) // decompSize huge
	hdr.Write(make([]byte, 8))

	if _, err := parseUsmapBytes(hdr.Bytes()); err == nil {
		t.Fatal("expected clamp error on absurd decompSize, got nil")
	}
}
