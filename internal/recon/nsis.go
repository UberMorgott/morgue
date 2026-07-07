package recon

import (
	"bytes"
	"encoding/binary"
	"os"

	peparser "github.com/saferwall/pe"
)

// nsis.go detects Nullsoft Scriptable Install System (NSIS) installers.
//
// An NSIS installer is a native PE with an appended overlay. The overlay begins
// with a 28-byte "firstheader":
//
//	offset  0  u32  flags
//	offset  4  [16]byte signature = {0xEF,0xBE,0xAD,0xDE} + "NullsoftInst"
//	offset 20  u32  length_of_header            (decompressed header size)
//	offset 24  u32  length_of_all_following_data (firstheader + all archive data)
//
// The 0xDEADBEEF (stored little-endian as EF BE AD DE) plus the ASCII
// "NullsoftInst" form a 16-byte magic. Some tampered installers (e.g. to defeat
// 7-Zip) flip a single signature byte, so detection tolerates a Hamming distance
// of 1 over those 16 bytes.

// nsisSignature is the canonical 16-byte NSIS firstheader signature: the
// little-endian 0xDEADBEEF followed by "NullsoftInst".
var nsisSignature = []byte{
	0xEF, 0xBE, 0xAD, 0xDE,
	'N', 'u', 'l', 'l', 's', 'o', 'f', 't', 'I', 'n', 's', 't',
}

const (
	nsisFirstHeaderSize = 28
	// nsisMaxScan bounds how many bytes we read/scan looking for the firstheader.
	// The firstheader sits at the very start of the overlay, so a modest window is
	// plenty; the cap keeps a huge installer from being slurped whole.
	nsisMaxScan = 96 << 20 // 96 MiB
)

// DetectNSIS reports whether path is an NSIS installer. It scans the PE overlay
// (falling back to a bounded scan of the whole file) for the firstheader
// signature, tolerating a single flipped signature byte. On a hit it returns the
// derived sub-type, the file offset of the firstheader, and true.
//
// f may be nil (e.g. a non-PE or a parse that only yielded a partial file); the
// scan then covers the file from the start.
func DetectNSIS(path string, f *peparser.File) (subType string, offset int64, ok bool) {
	fh, err := os.Open(path)
	if err != nil {
		return "", 0, false
	}
	defer func() { _ = fh.Close() }()

	fi, err := fh.Stat()
	if err != nil {
		return "", 0, false
	}
	fileSize := fi.Size()
	if fileSize < nsisFirstHeaderSize {
		return "", 0, false
	}

	// Prefer the overlay region; the firstheader is at its start. Fall back to a
	// whole-file scan when the overlay offset is unknown or implausible.
	var scanBase int64
	if f != nil && f.OverlayOffset > 0 && f.OverlayOffset < fileSize {
		scanBase = f.OverlayOffset
	}

	if st, off, found := scanForFirstHeader(fh, fileSize, scanBase); found {
		return st, off, true
	}
	// Overlay scan missed (or there was no overlay) — try from the top.
	if scanBase != 0 {
		if st, off, found := scanForFirstHeader(fh, fileSize, 0); found {
			return st, off, true
		}
	}
	return "", 0, false
}

// scanForFirstHeader reads up to nsisMaxScan bytes starting at scanBase and looks
// for a valid firstheader. Returns the derived sub-type and the absolute file
// offset of the firstheader on success.
func scanForFirstHeader(fh *os.File, fileSize, scanBase int64) (subType string, offset int64, ok bool) {
	readLen := fileSize - scanBase
	if readLen <= 0 {
		return "", 0, false
	}
	if readLen > nsisMaxScan {
		readLen = nsisMaxScan
	}
	buf := make([]byte, readLen)
	n, _ := fh.ReadAt(buf, scanBase)
	buf = buf[:n]

	for _, sigStart := range signatureCandidates(buf) {
		fhOffInBuf := sigStart - 4 // flags precede the 16-byte signature
		if fhOffInBuf < 0 || sigStart+24 > len(buf) {
			continue
		}
		hdrSize := binary.LittleEndian.Uint32(buf[sigStart+16 : sigStart+20])
		archiveSize := binary.LittleEndian.Uint32(buf[sigStart+20 : sigStart+24])
		fhFileOff := scanBase + int64(fhOffInBuf)
		if !saneFirstHeader(hdrSize, archiveSize, fhFileOff, fileSize) {
			continue
		}
		return deriveSubType(buf, sigStart+16), fhFileOff, true
	}
	return "", 0, false
}

// signatureCandidates returns buffer offsets at which the 16-byte NSIS signature
// begins, tolerating a single flipped byte. A single flip lands in EITHER the
// 4-byte 0xDEADBEEF OR the 12-byte "NullsoftInst" — never both — so anchoring on
// each intact half and Hamming-checking the full window catches a flip anywhere.
func signatureCandidates(buf []byte) []int {
	seen := map[int]bool{}
	var out []int
	add := func(sigStart int) {
		if sigStart < 0 || sigStart+16 > len(buf) {
			return
		}
		if seen[sigStart] {
			return
		}
		if hammingLE(buf[sigStart:sigStart+16], nsisSignature) <= 1 {
			seen[sigStart] = true
			out = append(out, sigStart)
		}
	}

	// Anchor 1: exact 0xDEADBEEF (first 4 signature bytes) → signature starts here.
	for i := 0; ; {
		idx := bytes.Index(buf[i:], nsisSignature[:4])
		if idx < 0 {
			break
		}
		add(i + idx)
		i += idx + 1
	}
	// Anchor 2: exact "NullsoftInst" (last 12 signature bytes) → signature starts 4 before.
	for i := 0; ; {
		idx := bytes.Index(buf[i:], nsisSignature[4:])
		if idx < 0 {
			break
		}
		add(i + idx - 4)
		i += idx + 1
	}
	return out
}

// hammingLE returns the number of differing bytes between a and want (compared up
// to len(want)). Used to tolerate a single flipped signature byte.
func hammingLE(a, want []byte) int {
	if len(a) < len(want) {
		return len(want) // treat as maximally different
	}
	d := 0
	for i := range want {
		if a[i] != want[i] {
			d++
		}
	}
	return d
}

// saneFirstHeader applies loose size sanity checks. The 16-byte Hamming-≤1 match
// already carries almost all of the discriminating power (a coincidental match in
// compressed data would need 15+ exact bytes), so these checks only reject
// obviously-bogus size fields.
func saneFirstHeader(hdrSize, archiveSize uint32, fhFileOff, fileSize int64) bool {
	// hdrSize is the DECOMPRESSED header size and archiveSize the COMPRESSED total,
	// so they are not directly comparable — only bound each independently. The
	// 16-byte Hamming-≤1 signature match already carries the discriminating power.
	const maxHeader = 512 << 20 // 512 MiB decompressed header ceiling
	if hdrSize == 0 || hdrSize > maxHeader {
		return false
	}
	if archiveSize <= nsisFirstHeaderSize {
		return false
	}
	// The archive (firstheader + data) must fit within the file. Signed
	// installers append an Authenticode blob AFTER the archive, so allow the
	// archive to end before EOF; it just must not overrun it.
	if fhFileOff+int64(archiveSize) > fileSize {
		return false
	}
	return true
}

// deriveSubType makes a best-effort guess at the NSIS variant from the bytes
// immediately after the firstheader (the start of the compressed header block).
// Precise NSIS-2 vs NSIS-3 / Unicode determination requires decompressing the
// header; that is refined by the unpack recipe. hdrTailStart is the buffer offset
// of length_of_header (firstheader+16).
func deriveSubType(buf []byte, hdrTailStart int) string {
	compStart := hdrTailStart + 12 // firstheader+16 -> +28 = start of compressed data
	if compStart+1 < len(buf) {
		switch buf[compStart] {
		case 0x5D: // typical LZMA properties byte (lc=3,lp=0,pb=2)
			return "NSIS (LZMA)"
		case 'B': // "BZh" bzip2 magic
			if compStart+2 < len(buf) && buf[compStart+1] == 'Z' && buf[compStart+2] == 'h' {
				return "NSIS (bzip2)"
			}
		}
	}
	return "NSIS"
}
