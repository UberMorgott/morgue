package recon

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// buildNSISFile writes a synthetic file whose first bytes are a valid NSIS
// firstheader (flags + 16-byte signature + hdrSize + archiveSize) followed by
// filler so archiveSize fits within the file. flips is a list of byte offsets
// WITHIN the 16-byte signature (0..15) to corrupt, simulating tampering.
func buildNSISFile(t *testing.T, flips ...int) string {
	t.Helper()
	const hdrSize = 100
	const fillLen = 200
	archiveSize := nsisFirstHeaderSize + fillLen

	buf := make([]byte, 0, archiveSize)
	buf = append(buf, 0, 0, 0, 0) // flags
	sig := make([]byte, 16)
	copy(sig, nsisSignature)
	for _, off := range flips {
		sig[off] ^= 0xFF // flip the byte
	}
	buf = append(buf, sig...)
	hs := make([]byte, 4)
	binary.LittleEndian.PutUint32(hs, hdrSize)
	buf = append(buf, hs...)
	as := make([]byte, 4)
	binary.LittleEndian.PutUint32(as, uint32(archiveSize))
	buf = append(buf, as...)
	buf = append(buf, make([]byte, fillLen)...) // filler

	path := filepath.Join(t.TempDir(), "installer.bin")
	if err := os.WriteFile(path, buf, 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDetectNSIS(t *testing.T) {
	tests := []struct {
		name    string
		flips   []int
		wantOK  bool
		wantOff int64
	}{
		{"clean signature", nil, true, 0},
		{"1-byte flip in magic (NullsoftInst)", []int{8}, true, 0},        // "N" -> flipped; DEADBEEF intact
		{"1-byte flip in DEADBEEF", []int{1}, true, 0},                    // 0xBE flipped; NullsoftInst intact
		{"2-byte flip rejected", []int{8, 9}, false, 0},                   // Hamming 2 over intact-DEADBEEF anchor
		{"2-byte flip split rejected", []int{1, 8}, false, 0},             // both anchors broken
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := buildNSISFile(t, tt.flips...)
			_, off, ok := DetectNSIS(path, nil)
			if ok != tt.wantOK {
				t.Fatalf("DetectNSIS ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && off != tt.wantOff {
				t.Errorf("DetectNSIS offset = %d, want %d", off, tt.wantOff)
			}
		})
	}
}

// TestDetectNSIS_PlainPEMiss ensures a file with no NSIS signature is not a
// false positive.
func TestDetectNSIS_PlainPEMiss(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plain.bin")
	// 4 KiB of a repeating non-signature pattern.
	data := make([]byte, 4096)
	for i := range data {
		data[i] = byte(i % 251)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := DetectNSIS(path, nil); ok {
		t.Error("DetectNSIS returned true for a non-NSIS file")
	}
}

func TestHammingLE(t *testing.T) {
	base := []byte{1, 2, 3, 4}
	if d := hammingLE([]byte{1, 2, 3, 4}, base); d != 0 {
		t.Errorf("hamming identical = %d, want 0", d)
	}
	if d := hammingLE([]byte{1, 9, 3, 4}, base); d != 1 {
		t.Errorf("hamming one-diff = %d, want 1", d)
	}
	if d := hammingLE([]byte{1, 2}, base); d != len(base) {
		t.Errorf("hamming short input = %d, want %d", d, len(base))
	}
}
