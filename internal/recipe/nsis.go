package recipe

import (
	"bytes"
	"compress/bzip2"
	"compress/flate"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ulikunitz/xz/lzma"

	"github.com/UberMorgott/morgue/internal/recon"
)

// nsis.go unpacks Nullsoft (NSIS) installers with a pure-Go pipeline: locate the
// firstheader, repair a tampered signature on a scratch copy, decompress the
// solid archive block (LZMA / zlib-deflate / bzip2), and walk the header's
// block/entry tables to reconstruct the installed file tree under
// <Output>/extracted/. Every extracted path is zip-slip guarded.
//
// SCOPE — verified against a real signed NSIS-3 Unicode installer (CCleaner
// 6.41, solid LZMA):
//   - Detection incl. a flipped signature byte: WORKS (repairs the tamper).
//   - Solid-LZMA decompression of the whole archive: WORKS.
//   - Header block-table parse + entry walk: WORKS. Three details matter and are
//     all verified against that installer, not guessed:
//     1. In solid mode the decompressed stream starts with a uint32 repeat of the
//     header size; every block offset in the header is relative to the byte
//     AFTER that prefix, and the file data section starts at prefix+headerSize.
//     2. Entry string params are CHARACTER indices into the string block, so a
//     Unicode (UTF-16LE) build needs them scaled by 2.
//     3. Var/shell/lang references inside strings are an escape code followed by
//     one parameter char (Unicode) or two bytes (ANSI); the parameter must be
//     consumed, not emitted, or every path turns to mojibake.
//
// Still best-effort: non-solid archives (per-block compressed sizes) are not
// decompressed — those records are skipped. When the entry walk yields no files
// the recipe falls back to dumping raw [size][data] records so the file BYTES
// still land on disk, records extract_mode="raw-fallback", and reports the step
// as Warn rather than faking success.

// NSIS firstheader / opcode constants.
const (
	nsisFirstHeaderSize = 28
	nsisBlocksNum       = 8
	nbEntries           = 2 // block index: entries
	nbStrings           = 3 // block index: strings
	nbLangtables        = 4 // block index: language tables (follows strings)
	nbData              = 7 // block index: file data
	nsisEntrySize       = 28
	ewCreateDir         = 11      // EW_CREATEDIR (also SetOutPath when param[1] != 0)
	ewExtractFile       = 20      // EW_EXTRACTFILE
	nsisMaxDecompressed = 2 << 30 // 2 GiB decompression ceiling (anti-bomb)
)

var nsisSignature = []byte{
	0xEF, 0xBE, 0xAD, 0xDE,
	'N', 'u', 'l', 'l', 's', 'o', 'f', 't', 'I', 'n', 's', 't',
}

// NSISUnpack is the recipe for Nullsoft installers.
type NSISUnpack struct{}

func init() {
	// Prepend so an NSIS installer routes here rather than falling through to the
	// generic native recipe (both match a native PE).
	RegisterFirst(&NSISUnpack{})
}

func (n *NSISUnpack) Name() string        { return "nsis" }
func (n *NSISUnpack) Description() string { return "Unpack Nullsoft (NSIS) installer" }

func (n *NSISUnpack) Match(r *recon.Result) bool { return r.Kind == recon.NSIS }

func (n *NSISUnpack) Steps() []StepInfo {
	return []StepInfo{
		{Name: "Copy original", Required: false},
		{Name: "Locate & repair NSIS header", Required: true},
		{Name: "Decompress archive", Required: true},
		{Name: "Extract files", Required: false},
	}
}

// RequiredTools: none — pure Go, works offline.
func (n *NSISUnpack) RequiredTools() []string { return nil }

// nsisManifest is written to <Output>/nsis-manifest.json.
type nsisManifest struct {
	Target            string   `json:"target"`
	FirstHeaderAt     int64    `json:"firstheader_offset"`
	SignatureRepaired bool     `json:"signature_repaired"`
	HeaderSize        uint32   `json:"header_size"`
	ArchiveSize       uint32   `json:"archive_size"`
	Compression       string   `json:"compression"`
	Unicode           bool     `json:"unicode"`
	DecompressedLen   int      `json:"decompressed_len"`
	Entries           int      `json:"entry_count"`
	FilesExtracted    int      `json:"files_extracted"`
	FilesWanted       int      `json:"files_wanted"` // EW_EXTRACTFILE records seen
	ExtractMode       string   `json:"extract_mode"` // "structured" | "raw-fallback" | "none"
	Notes             []string `json:"notes,omitempty"`
}

func (n *NSISUnpack) Execute(ctx *Context) error {
	steps := n.Steps()
	total := len(steps)
	report := func(step int, status StepStatus, dur time.Duration, err error) {
		if ctx.Progress != nil {
			ctx.Progress <- StepProgress{Step: step, Total: total, Name: steps[step].Name, Status: status, Duration: dur, Error: err}
		}
	}
	logMsg := func(m string) {
		if ctx.Log != nil {
			ctx.Log <- "[nsis] " + m
		}
	}
	reportCount := func(step int, dur time.Duration, count int, unit string, status StepStatus, err error) {
		if ctx.Progress != nil {
			ctx.Progress <- StepProgress{Step: step, Total: total, Name: steps[step].Name, Status: status, Duration: dur, Count: count, Unit: unit, Error: err}
		}
	}

	man := nsisManifest{Target: ctx.Target}

	// Step 0: copy original.
	report(0, Running, 0, nil)
	start := time.Now()
	origDir := filepath.Join(ctx.Output, "original")
	if err := os.MkdirAll(origDir, 0755); err != nil {
		report(0, Failed, time.Since(start), err)
		return err
	}
	if err := copyFile(ctx.Target, filepath.Join(origDir, filepath.Base(ctx.Target))); err != nil {
		report(0, Failed, time.Since(start), err)
		return err
	}
	report(0, Success, time.Since(start), nil)

	// Step 1: locate + repair firstheader.
	report(1, Running, 0, nil)
	start = time.Now()
	_, fhOff, ok := recon.DetectNSIS(ctx.Target, nil)
	if !ok {
		err := fmt.Errorf("NSIS firstheader not found (recon said NSIS but the recipe could not relocate it)")
		report(1, Failed, time.Since(start), err)
		return err
	}
	man.FirstHeaderAt = fhOff

	data, err := os.ReadFile(ctx.Target)
	if err != nil {
		report(1, Failed, time.Since(start), err)
		return err
	}
	if int64(len(data)) < fhOff+nsisFirstHeaderSize {
		err := fmt.Errorf("file too short for firstheader at %d", fhOff)
		report(1, Failed, time.Since(start), err)
		return err
	}

	fh := data[fhOff : fhOff+nsisFirstHeaderSize]
	man.HeaderSize = binary.LittleEndian.Uint32(fh[20:24])
	man.ArchiveSize = binary.LittleEndian.Uint32(fh[24:28])

	// Repair the 16-byte signature on a scratch copy — never mutate the input.
	repaired := make([]byte, len(data))
	copy(repaired, data)
	sigDst := repaired[fhOff+4 : fhOff+20]
	if !bytes.Equal(sigDst, nsisSignature) {
		copy(sigDst, nsisSignature)
		man.SignatureRepaired = true
		logMsg("repaired tampered firstheader signature on scratch copy")
	}
	tmpDir := filepath.Join(ctx.Output, ".tmp")
	if err := os.MkdirAll(tmpDir, 0755); err == nil {
		_ = os.WriteFile(filepath.Join(tmpDir, "repaired.bin"), repaired, 0644)
	}
	report(1, Success, time.Since(start), nil)

	// Step 2: decompress the solid archive block.
	report(2, Running, 0, nil)
	start = time.Now()
	end := fhOff + int64(man.ArchiveSize)
	if end > int64(len(data)) || man.ArchiveSize == 0 {
		end = int64(len(data))
	}
	comp := repaired[fhOff+nsisFirstHeaderSize : end]
	dec, method, derr := decompressNSIS(comp, int(man.HeaderSize))
	man.Compression = method
	if derr != nil || len(dec) == 0 {
		man.ExtractMode = "none"
		if derr != nil {
			man.Notes = append(man.Notes, "decompression failed: "+derr.Error())
			logMsg("decompression failed (" + method + "): " + derr.Error())
		}
		report(2, Failed, time.Since(start), derr)
		writeNSISManifest(ctx.Output, man, logMsg)
		// Detection + original copy already delivered value; non-fatal.
		return nil
	}
	man.DecompressedLen = len(dec)
	logMsg(fmt.Sprintf("decompressed %d bytes via %s", len(dec), method))
	reportCount(2, time.Since(start), len(dec), "bytes", Success, nil)

	// Step 3: extract files (best-effort, structured with raw fallback).
	report(3, Running, 0, nil)
	start = time.Now()
	extractDir := filepath.Join(ctx.Output, "extracted")
	if err := os.MkdirAll(extractDir, 0755); err != nil {
		report(3, Failed, time.Since(start), err)
		return err
	}

	nfiles, unicode, entryCount, wanted := extractStructured(dec, int(man.HeaderSize), extractDir, logMsg)
	man.Unicode = unicode
	man.Entries = entryCount
	if nfiles > 0 {
		man.ExtractMode = "structured"
		man.FilesExtracted = nfiles
		man.FilesWanted = wanted
		if nfiles < wanted {
			man.Notes = append(man.Notes, fmt.Sprintf(
				"%d of %d EW_EXTRACTFILE records could not be written (non-solid/compressed data blocks are not supported)",
				wanted-nfiles, wanted))
		}
	} else {
		// Structured walk found nothing usable — dump raw [size][data] records so
		// the file bytes still reach disk.
		raw := extractRawRecords(dec, int(man.HeaderSize), extractDir, logMsg)
		man.ExtractMode = "raw-fallback"
		man.FilesExtracted = raw
		man.Notes = append(man.Notes, "structured entry-walk found no files; dumped raw data records (names/paths unavailable)")
	}

	// Always dump the string table for greppability.
	if s := dumpStrings(dec, int(man.HeaderSize), unicode); len(s) > 0 {
		_ = os.WriteFile(filepath.Join(ctx.Output, "nsis-strings.txt"), []byte(strings.Join(s, "\n")), 0644)
	}

	// A fallback mode, or a structured walk that dropped records, is degraded
	// output — report WARN so the run is not read as a clean success.
	status, werr := Success, error(nil)
	if man.ExtractMode != "structured" || man.FilesExtracted < man.FilesWanted {
		status = Warn
		werr = fmt.Errorf("NSIS extraction incomplete: %d files, mode %s", man.FilesExtracted, man.ExtractMode)
	}
	reportCount(3, time.Since(start), man.FilesExtracted, "files", status, werr)
	logMsg(fmt.Sprintf("extracted %d files (%s)", man.FilesExtracted, man.ExtractMode))
	writeNSISManifest(ctx.Output, man, logMsg)
	return nil
}

func writeNSISManifest(out string, man nsisManifest, logMsg func(string)) {
	data, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(filepath.Join(out, "nsis-manifest.json"), data, 0644); err != nil {
		logMsg("failed to write manifest: " + err.Error())
	}
}

// decompressNSIS tries the NSIS solid-compression methods in order and returns
// the decompressed bytes plus the method name. It targets the SOLID case (the
// whole post-firstheader stream is one compressed blob).
func decompressNSIS(comp []byte, hdrSize int) ([]byte, string, error) {
	if len(comp) < 5 {
		return nil, "unknown", fmt.Errorf("archive too small")
	}
	var firstErr error

	// LZMA (solid). NSIS writes a 5-byte header (1 props + 4 dict size) then the
	// raw LZMA1 stream. ulikunitz expects the classic .lzma 13-byte header, so
	// splice in an 8-byte "unknown size" field (0xFF...) which relies on an EOS
	// marker at the stream end.
	if dec, err := tryLZMA(comp, hdrSize); err == nil && len(dec) >= hdrSize {
		return dec, "LZMA", nil
	} else if err != nil {
		firstErr = err
	}

	// zlib: NSIS uses raw DEFLATE (no zlib header).
	if dec, err := tryInflate(comp, hdrSize); err == nil && len(dec) >= hdrSize {
		return dec, "deflate", nil
	}

	// bzip2: NSIS uses a MODIFIED bzip2 (no "BZh" header). stdlib only decodes
	// standard bzip2, so this succeeds only on the rare standard stream.
	if dec, err := tryBzip2(comp, hdrSize); err == nil && len(dec) >= hdrSize {
		return dec, "bzip2", nil
	}

	if firstErr == nil {
		firstErr = fmt.Errorf("no supported NSIS compression matched")
	}
	return nil, "unknown", firstErr
}

func tryLZMA(comp []byte, hdrSize int) ([]byte, error) {
	hdr := make([]byte, 0, 13)
	hdr = append(hdr, comp[0])                                        // properties
	hdr = append(hdr, comp[1:5]...)                                   // dictionary size (LE)
	hdr = append(hdr, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF) // size unknown
	stream := io.MultiReader(bytes.NewReader(hdr), bytes.NewReader(comp[5:]))
	r, err := lzma.NewReader(stream)
	if err != nil {
		return nil, err
	}
	return readCapped(r, hdrSize)
}

func tryInflate(comp []byte, hdrSize int) ([]byte, error) {
	r := flate.NewReader(bytes.NewReader(comp))
	defer r.Close()
	return readCapped(r, hdrSize)
}

func tryBzip2(comp []byte, hdrSize int) ([]byte, error) {
	return readCapped(bzip2.NewReader(bytes.NewReader(comp)), hdrSize)
}

// readCapped reads up to nsisMaxDecompressed bytes. A trailing decode error is
// tolerated when at least hdrSize bytes were produced — NSIS streams may lack a
// clean terminator for the unknown-size reader.
func readCapped(r io.Reader, hdrSize int) ([]byte, error) {
	out := make([]byte, 0, hdrSize*2+4096)
	buf := make([]byte, 64*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			out = append(out, buf[:n]...)
			if len(out) > nsisMaxDecompressed {
				return out, fmt.Errorf("decompressed data exceeds cap")
			}
		}
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			if len(out) >= hdrSize && hdrSize > 0 {
				return out, nil // usable despite an unclean end
			}
			return out, err
		}
	}
}

// extractStructured parses the NSIS header block, walks the entries table, and
// writes the reconstructed file tree. Returns files written, whether the strings
// looked Unicode, and the entry count. Any parse fault returns (0,...) so the
// caller can fall back to a raw dump.
func extractStructured(dec []byte, hdrSize int, outDir string, logMsg func(string)) (files int, unicode bool, entryCount int, wanted int) {
	if hdrSize < 4+nsisBlocksNum*8 || hdrSize > len(dec) {
		return 0, false, 0, 0
	}

	// Header layout: an optional 4-byte size prefix (solid mode repeats
	// length_of_header at the head of the decompressed stream) then int32 flags,
	// then 8 block_headers of {int32 offset; int32 num}. EVERY block offset is
	// relative to hdrBase — the byte after the prefix — not to the stream start.
	hdrBase := nsisHeaderBase(dec, hdrSize)
	blockBase := hdrBase + 4

	blockOff := func(i int) (off, num int) {
		base := blockBase + i*8
		if base+8 > len(dec) {
			return 0, 0
		}
		return int(int32(binary.LittleEndian.Uint32(dec[base : base+4]))),
			int(int32(binary.LittleEndian.Uint32(dec[base+4 : base+8])))
	}

	entriesOff, entryCount := blockOff(nbEntries)
	stringsOff, _ := blockOff(nbStrings)
	langOff, _ := blockOff(nbLangtables)
	entriesOff += hdrBase
	stringsOff += hdrBase
	langOff += hdrBase

	// String table extent: [stringsOff, langOff).
	strEnd := langOff
	if strEnd <= stringsOff || strEnd > len(dec) {
		strEnd = len(dec)
	}
	var strTab []byte
	if stringsOff >= hdrBase && stringsOff < len(dec) && strEnd <= len(dec) {
		strTab = dec[stringsOff:strEnd]
	}
	unicode = looksUnicode(strTab)

	// String params are CHARACTER indices; UTF-16LE needs them doubled.
	charSize := 1
	if unicode {
		charSize = 2
	}
	resolve := func(off int) string { return resolveNSISString(strTab, off*charSize, unicode) }

	if entriesOff < hdrBase || entryCount <= 0 || entriesOff+entryCount*nsisEntrySize > len(dec) {
		return 0, unicode, entryCount, 0
	}

	dataBase := hdrBase + hdrSize // file data follows the header in the solid stream
	curDir := ""
	for i := 0; i < entryCount; i++ {
		e := dec[entriesOff+i*nsisEntrySize:]
		which := int(int32(binary.LittleEndian.Uint32(e[0:4])))
		p := func(k int) int { return int(int32(binary.LittleEndian.Uint32(e[4+k*4 : 8+k*4]))) }

		switch which {
		case ewCreateDir:
			dir := sanitizeRel(resolve(p(0)))
			if p(1) != 0 { // SetOutPath
				curDir = dir
			}
			if dir != "" {
				_ = os.MkdirAll(safeJoin(outDir, dir), 0755)
			}
		case ewExtractFile:
			name := resolve(p(1))
			pos := p(2)
			if name == "" || pos < 0 {
				continue
			}
			wanted++
			abs := dataBase + pos
			if abs+4 > len(dec) {
				continue
			}
			size := int(int32(binary.LittleEndian.Uint32(dec[abs : abs+4])))
			// High bit set => this block is individually compressed (non-solid).
			// Not handled here; skip so we don't write garbage.
			if size < 0 || abs+4+size > len(dec) {
				continue
			}
			full := name
			// A name that already carries its own var root ($INSTDIR\...) is
			// absolute — don't nest it under the current $OUTDIR.
			if !strings.HasPrefix(name, "$") {
				full = filepath.Join(curDir, name)
			}
			rel := sanitizeRel(full)
			if rel == "" {
				continue
			}
			dst := safeJoin(outDir, rel)
			if dst == "" {
				continue // zip-slip guard rejected it
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
				continue
			}
			if err := os.WriteFile(dst, dec[abs+4:abs+4+size], 0644); err == nil {
				files++
			}
		}
	}
	return files, unicode, entryCount, wanted
}

// extractRawRecords walks the data region as consecutive [int32 size][bytes]
// records and dumps each as file_NNNN.bin. A last-resort so the actual file
// bytes reach disk even when the entry table could not be parsed.
func extractRawRecords(dec []byte, hdrSize int, outDir string, logMsg func(string)) int {
	pos := hdrSize
	if pos < 0 || pos >= len(dec) {
		pos = 0
	}
	rawDir := filepath.Join(outDir, "_raw")
	_ = os.MkdirAll(rawDir, 0755)
	n := 0
	for pos+4 <= len(dec) && n < 100000 {
		size := int(int32(binary.LittleEndian.Uint32(dec[pos : pos+4])))
		size &= 0x7FFFFFFF // ignore the compression flag bit
		if size <= 0 || pos+4+size > len(dec) {
			break
		}
		name := filepath.Join(rawDir, fmt.Sprintf("file_%04d.bin", n))
		if err := os.WriteFile(name, dec[pos+4:pos+4+size], 0644); err == nil {
			n++
		}
		pos += 4 + size
	}
	if n > 0 {
		logMsg(fmt.Sprintf("raw fallback dumped %d data records to extracted/_raw/", n))
	}
	return n
}

// nsisHeaderBase returns the offset of the header proper within the decompressed
// stream: 4 when the solid-mode uint32 size prefix is present, else 0. All block
// offsets in the header are relative to this base.
func nsisHeaderBase(dec []byte, hdrSize int) int {
	if len(dec) >= 4 && int(binary.LittleEndian.Uint32(dec[0:4])) == hdrSize {
		return 4
	}
	return 0
}

// dumpStrings returns the null-terminated strings from the string block.
func dumpStrings(dec []byte, hdrSize int, unicode bool) []string {
	if hdrSize < 4+nsisBlocksNum*8 || hdrSize > len(dec) {
		return nil
	}
	hb := nsisHeaderBase(dec, hdrSize)
	bb := hb + 4
	base := bb + nbStrings*8
	stringsOff := hb + int(int32(binary.LittleEndian.Uint32(dec[base : base+4])))
	baseL := bb + nbLangtables*8
	langOff := hb + int(int32(binary.LittleEndian.Uint32(dec[baseL : baseL+4])))
	if stringsOff < hb || stringsOff >= len(dec) {
		return nil
	}
	end := langOff
	if end <= stringsOff || end > len(dec) {
		end = len(dec)
	}
	tab := dec[stringsOff:end]
	var out []string
	off := 0
	for off < len(tab) {
		s := resolveNSISString(tab, off, unicode)
		if s != "" {
			out = append(out, s)
		}
		// advance to next null terminator
		adv := nextStringLen(tab, off, unicode)
		if adv <= 0 {
			break
		}
		off += adv
	}
	return out
}

// looksUnicode heuristically decides whether the string table is UTF-16LE by the
// density of zero high-bytes.
func looksUnicode(tab []byte) bool {
	if len(tab) < 8 {
		return false
	}
	zeros := 0
	n := len(tab)
	if n > 512 {
		n = 512
	}
	for i := 1; i < n; i += 2 {
		if tab[i] == 0x00 {
			zeros++
		}
	}
	return zeros*3 > (n/2)*2 // > ~66% of odd bytes are zero
}

// NSIS string escape codes (NSIS 3, same numbers in ANSI and Unicode builds).
// Each is followed by ONE parameter: a single UTF-16 char in a Unicode build,
// two bytes in an ANSI build. Both encode the value the same way — 7 bits per
// byte/low-half, high bits in the next.
const (
	nsSkipCode  = 0x01
	nsShellCode = 0x02
	nsVarCode   = 0x03
	nsLangCode  = 0x04
)

// nsisVarName renders a variable index as its $NAME. Indices past the documented
// built-ins (user vars, $PLUGINSDIR, ...) become $VARn rather than a guess.
func nsisVarName(idx int) string {
	switch {
	case idx >= 0 && idx <= 9:
		return fmt.Sprintf("$%d", idx)
	case idx >= 10 && idx <= 19:
		return fmt.Sprintf("$R%d", idx-10)
	case idx == 20:
		return "$CMDLINE"
	case idx == 21:
		return "$INSTDIR"
	case idx == 22:
		return "$OUTDIR"
	case idx == 23:
		return "$EXEDIR"
	case idx == 24:
		return "$LANGUAGE"
	}
	return fmt.Sprintf("$VAR%d", idx)
}

// resolveNSISString reads a null-terminated string at byte offset off. Escape
// codes are expanded to a readable $TOKEN and their parameter is consumed, so
// paths stay filesystem-safe and greppable.
func resolveNSISString(tab []byte, off int, unicode bool) string {
	if off < 0 || off >= len(tab) {
		return ""
	}
	var b strings.Builder
	writeCode := func(code, param int) {
		switch code {
		case nsShellCode:
			fmt.Fprintf(&b, "$SHELL%d", param)
		case nsVarCode:
			b.WriteString(nsisVarName(param))
		case nsLangCode:
			fmt.Fprintf(&b, "$LANG%d", param)
		}
	}
	if unicode {
		for i := off; i+1 < len(tab); i += 2 {
			c := int(binary.LittleEndian.Uint16(tab[i : i+2]))
			if c == 0 {
				break
			}
			if c == nsSkipCode { // next char is literal
				if i+3 >= len(tab) {
					break
				}
				b.WriteRune(rune(binary.LittleEndian.Uint16(tab[i+2 : i+4])))
				i += 2
				continue
			}
			if c >= nsShellCode && c <= nsLangCode {
				if i+3 >= len(tab) {
					break
				}
				p := int(binary.LittleEndian.Uint16(tab[i+2 : i+4]))
				i += 2
				writeCode(c, (p&0x7F)|((p>>8&0x7F)<<7))
				continue
			}
			if c < 0x20 {
				b.WriteByte('_')
				continue
			}
			b.WriteRune(rune(c))
		}
	} else {
		for i := off; i < len(tab); i++ {
			c := int(tab[i])
			if c == 0 {
				break
			}
			if c == nsSkipCode { // next byte is literal
				if i+1 >= len(tab) {
					break
				}
				b.WriteByte(tab[i+1])
				i++
				continue
			}
			if c >= nsShellCode && c <= nsLangCode {
				if i+2 >= len(tab) {
					break
				}
				p := int(tab[i+1]&0x7F) | int(tab[i+2]&0x7F)<<7
				i += 2
				writeCode(c, p)
				continue
			}
			if c < 0x20 {
				b.WriteByte('_')
				continue
			}
			b.WriteByte(byte(c))
		}
	}
	return b.String()
}

// nextStringLen returns the byte length (including terminator) of the string at
// off, for iterating the string table.
func nextStringLen(tab []byte, off int, unicode bool) int {
	if unicode {
		for i := off; i+1 < len(tab); i += 2 {
			if binary.LittleEndian.Uint16(tab[i:i+2]) == 0 {
				return (i + 2) - off
			}
		}
		return len(tab) - off
	}
	for i := off; i < len(tab); i++ {
		if tab[i] == 0 {
			return (i + 1) - off
		}
	}
	return len(tab) - off
}

// sanitizeRel strips drive letters, leading separators and "." components,
// leaving a safe relative path.
func sanitizeRel(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimSpace(p)
	// drop a leading drive letter or root
	if len(p) >= 2 && p[1] == ':' {
		p = p[2:]
	}
	p = strings.TrimLeft(p, "/")
	var parts []string
	for _, seg := range strings.Split(p, "/") {
		seg = strings.TrimSpace(seg)
		if seg == "" || seg == "." || seg == ".." {
			continue
		}
		parts = append(parts, seg)
	}
	return filepath.Join(parts...)
}

// safeJoin joins base and rel, returning "" if the result escapes base
// (zip-slip guard).
func safeJoin(base, rel string) string {
	target := filepath.Join(base, rel)
	r, err := filepath.Rel(base, target)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(os.PathSeparator)) {
		return ""
	}
	return target
}
