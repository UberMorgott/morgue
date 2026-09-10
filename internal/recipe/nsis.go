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
	"sort"
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
// SCOPE — verified against a real signed NSIS-3 Unicode installer from a
// commercial vendor (solid LZMA, ~2024 build):
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
// Still best-effort: an individually-compressed (non-solid) data block is
// decompressed with the archive's own method, and a block that fails is counted
// in the manifest notes rather than aborting the walk. When the entry walk
// yields no files the recipe falls back to dumping raw [size][data] records so
// the file BYTES still land on disk, records extract_mode="raw-fallback", and
// reports the step as Warn rather than faking success.
//
// The entry walk is table-driven (nsisOps) and LINEAR — see the nsisWalk doc for
// what that costs. File-tree opcodes mutate <Output>/extracted/; metadata
// opcodes (registry, shortcuts, plugin/DLL registration, exec, ini) are recorded
// to <Output>/nsis-actions.txt instead of being executed.

// NSIS firstheader / opcode constants.
const (
	nsisFirstHeaderSize = 28
	nsisBlocksNum       = 8
	nbEntries           = 2 // block index: entries
	nbStrings           = 3 // block index: strings
	nbLangtables        = 4 // block index: language tables (follows strings)
	nbData              = 7 // block index: file data
	nsisEntrySize       = 28
	nsisMaxDecompressed = 2 << 30 // 2 GiB decompression ceiling (anti-bomb)
)

// NSIS instruction opcodes (exehead/fileform.h, stock NSIS 3 makensis).
// CAVEAT: the enum shifts when NSIS is built with non-default NSIS_SUPPORT_*
// defines, so a custom-built installer can misnumber. An opcode we do not know
// is counted and skipped, never fatal — see nsisOps.
const (
	ewRet                = 1
	ewJmp                = 2
	ewAbort              = 3
	ewQuit               = 4
	ewCall               = 5
	ewUpdateText         = 6
	ewSleep              = 7
	ewBringToFront       = 8
	ewChDetailsView      = 9
	ewSetFileAttributes  = 10
	ewCreateDir          = 11 // also SetOutPath when param[1] != 0
	ewIfFileExists       = 12
	ewSetFlag            = 13
	ewIfFlag             = 14
	ewGetFlag            = 15
	ewRename             = 16
	ewGetFullPathName    = 17
	ewSearchPath         = 18
	ewGetTempFileName    = 19
	ewExtractFile        = 20
	ewDeleteFile         = 21
	ewMessageBox         = 22
	ewRmDir              = 23
	ewStrLen             = 24
	ewAssignVar          = 25 // StrCpy
	ewStrCmp             = 26
	ewReadEnvStr         = 27
	ewIntCmp             = 28
	ewIntOp              = 29
	ewIntFmt             = 30
	ewPushPop            = 31
	ewFindWindow         = 32
	ewSendMessage        = 33
	ewIsWindow           = 34
	ewGetDlgItem         = 35
	ewSetCtlColors       = 36
	ewSetBrandingImage   = 37
	ewCreateFont         = 38
	ewShowWindow         = 39
	ewShellExec          = 40
	ewExecute            = 41
	ewGetFileTime        = 42
	ewGetDLLVersion      = 43
	ewRegisterDLL        = 44
	ewCreateShortcut     = 45
	ewCopyFiles          = 46
	ewReboot             = 47
	ewWriteINI           = 48
	ewReadINIStr         = 49
	ewDelReg             = 50
	ewWriteReg           = 51
	ewReadRegStr         = 52
	ewRegEnum            = 53
	ewFClose             = 54
	ewFOpen              = 55
	ewFPuts              = 56
	ewFGets              = 57
	ewFSeek              = 58
	ewFindClose          = 59
	ewFindNext           = 60
	ewFindFirst          = 61
	ewWriteUninstaller   = 62
	ewLog                = 63
	ewSectionSet         = 64
	ewInstTypeSet        = 65
	ewGetOSInfo          = 66
	ewReservedOpcode     = 67
	ewLockWindow         = 68
	ewFPutWS             = 69
	ewFGetWS             = 70
	ewGetFunctionAddress = 71
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
	report(1, Success, time.Since(start), nil)

	// Step 2: decompress the solid archive block.
	report(2, Running, 0, nil)
	start = time.Now()
	end := fhOff + int64(man.ArchiveSize)
	if end > int64(len(data)) || man.ArchiveSize == 0 {
		// Fall back to the file tail, but stop before the Authenticode overlay —
		// the certificate table is appended AFTER the NSIS archive.
		end = int64(len(data))
		if cert := peCertOffset(data); cert > fhOff+nsisFirstHeaderSize && cert < end {
			end = cert
			man.Notes = append(man.Notes, fmt.Sprintf("archive tail trimmed at Authenticode overlay (offset %d)", cert))
		}
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

	walk := extractStructured(dec, int(man.HeaderSize), method, extractDir, logMsg)
	man.Unicode = walk.Unicode
	man.Entries = walk.Entries
	if walk.Entries > 0 {
		man.Notes = append(man.Notes, fmt.Sprintf(
			"entry walk: %d instructions, %d handled, %d unknown opcodes skipped, %d control-flow instructions counted but not followed (linear walk — every branch is taken)",
			walk.Entries, walk.Known, walk.Unknown, walk.Control))
	}
	if len(walk.Meta) > 0 {
		_ = os.WriteFile(filepath.Join(ctx.Output, "nsis-actions.txt"),
			[]byte(strings.Join(walk.Meta, "\n")+"\n"), 0644)
		logMsg(fmt.Sprintf("recorded %d registry/shortcut/plugin actions to nsis-actions.txt", len(walk.Meta)))
	}
	if walk.Files > 0 {
		man.ExtractMode = "structured"
		man.FilesExtracted = walk.Files
		man.FilesWanted = walk.Wanted
		if walk.Files < walk.Wanted {
			man.Notes = append(man.Notes, fmt.Sprintf(
				"%d of %d file records could not be written (unreadable or undecompressable data block)",
				walk.Wanted-walk.Files, walk.Wanted))
		}
	} else {
		// Structured walk found nothing usable — dump raw [size][data] records so
		// the file bytes still reach disk.
		raw, skipped := extractRawRecords(dec, int(man.HeaderSize), extractDir, logMsg)
		man.ExtractMode = "raw-fallback"
		man.FilesExtracted = raw
		man.Notes = append(man.Notes, "structured entry-walk found no files; dumped raw data records (names/paths unavailable)")
		if skipped > 0 {
			man.Notes = append(man.Notes, fmt.Sprintf("raw fallback resynchronised past %d unreadable record header(s)", skipped))
		}
	}

	// Always dump the string table for greppability (encoding detected on its own,
	// so a failed entry-walk cannot turn the dump into mojibake).
	if s := dumpStrings(dec, int(man.HeaderSize)); len(s) > 0 {
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
	defer func() { _ = r.Close() }()
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

// nsisWalkResult is what one pass over the entries table produced.
type nsisWalkResult struct {
	Files   int
	Wanted  int      // EW_EXTRACTFILE records seen
	Entries int      // instructions in the table
	Unicode bool     // string table encoding
	Known   int      // instructions with a handler
	Control int      // branch/compare instructions, counted not executed
	Unknown int      // opcodes without a handler
	Meta    []string // registry / shortcut / plugin lines for nsis-actions.txt
}

// nsisWalk carries the state one linear pass mutates.
//
// LIMITATION (deliberate): the walk is LINEAR — instructions execute in table
// order and every branch is taken as if it were straight-line code. There is no
// control flow: EW_JMP / EW_IFFILEEXISTS / EW_INTCMP / EW_STRCMP are counted and
// reported, never followed. So a path assembled inside a conditional may reflect
// a branch the installer would not have taken. That is the right trade for a
// static unpacker — the goal is to see every file the installer CAN write, not
// to reproduce one particular run.
type nsisWalk struct {
	dec      []byte
	dataBase int
	method   string
	outDir   string
	resolve  func(int) string
	vars     map[string]string // "$INSTDIR" -> resolved value, from EW_ASSIGNVAR
	varKeys  []string          // longest-first so $VAR25 never eats $VAR250
	curDir   string
	log      func(string)
	res      nsisWalkResult
}

// str resolves a string param and substitutes every variable the walk has seen
// assigned. Unresolved variables survive as $TOKEN and are handled downstream by
// nsisVarSegment, which maps them onto a safe fixed segment.
func (w *nsisWalk) str(off int) string {
	s := w.resolve(off)
	if !strings.Contains(s, "$") {
		return s
	}
	for _, k := range w.varKeys {
		s = strings.ReplaceAll(s, k, w.vars[k])
	}
	return s
}

func (w *nsisWalk) setVar(idx int, val string) {
	name := nsisVarName(idx)
	if _, ok := w.vars[name]; !ok {
		w.varKeys = append(w.varKeys, name)
		sort.Slice(w.varKeys, func(i, j int) bool { return len(w.varKeys[i]) > len(w.varKeys[j]) })
	}
	w.vars[name] = val
}

// dst turns a resolved NSIS path into an absolute path under outDir, or "" if it
// is empty or the zip-slip guard rejects it.
func (w *nsisWalk) dst(name string) string {
	if name == "" {
		return ""
	}
	full := name
	// A rooted name ($INSTDIR\…, C:\…, \…) carries its own root — don't nest it
	// under the current $OUTDIR.
	rooted := strings.HasPrefix(name, "$") || strings.HasPrefix(name, "\\") ||
		strings.HasPrefix(name, "/") || (len(name) > 1 && name[1] == ':')
	if !rooted {
		full = filepath.Join(w.curDir, name)
	}
	rel := sanitizeRel(full)
	if rel == "" {
		return ""
	}
	return safeJoin(w.outDir, rel)
}

// writeData writes the data-block record at pos to dst, inflating it first when
// it is individually compressed (non-solid).
func (w *nsisWalk) writeData(pos int, dst string) bool {
	abs := w.dataBase + pos
	if pos < 0 || abs+4 > len(w.dec) {
		return false
	}
	raw := binary.LittleEndian.Uint32(w.dec[abs : abs+4])
	size := int(raw &^ 0x80000000)
	if size <= 0 || abs+4+size > len(w.dec) {
		return false
	}
	payload := w.dec[abs+4 : abs+4+size]
	if raw&0x80000000 != 0 { // high bit => this block carries its own compression
		p, err := decompressBlock(payload, w.method)
		if err != nil {
			w.log("non-solid block decompression failed for " + dst + ": " + err.Error())
			return false
		}
		payload = p
	}
	if os.MkdirAll(filepath.Dir(dst), 0755) != nil {
		return false
	}
	return os.WriteFile(dst, payload, 0644) == nil
}

func (w *nsisWalk) meta(format string, a ...any) {
	w.res.Meta = append(w.res.Meta, fmt.Sprintf(format, a...))
}

var nsisRegRoots = []string{"HKCR", "HKCU", "HKLM", "HKU", "HKPD", "HKCC", "HKDD"}

func nsisRegRoot(v int) string {
	if v >= 0 && v < len(nsisRegRoots) {
		return nsisRegRoots[v]
	}
	return fmt.Sprintf("HK?%d", v)
}

// countControl is the handler for branch/compare opcodes: recorded, never taken.
func countControl(w *nsisWalk, p func(int) int) { w.res.Control++ }

// nsisNoop is the handler for opcodes that cannot touch the extracted tree and
// carry nothing an operator wants (UI, stack, integer and string arithmetic).
// Having them here rather than in the unknown bucket keeps the unknown counter
// meaningful: it then means "opcode we could not identify", not "opcode we did
// not bother to list".
func nsisNoop(w *nsisWalk, p func(int) int) {}

// nsisOps maps an opcode to its handler. Adding an instruction is one line here.
// File-tree opcodes mutate the extracted tree; metadata opcodes only record a
// line for nsis-actions.txt; control opcodes are counted by countControl.
var nsisOps = map[int]func(w *nsisWalk, p func(int) int){
	// --- file tree ---
	ewCreateDir: func(w *nsisWalk, p func(int) int) {
		dir := sanitizeRel(w.str(p(0)))
		if p(1) != 0 { // SetOutPath
			w.curDir = dir
		}
		if dir != "" {
			_ = os.MkdirAll(safeJoin(w.outDir, dir), 0755)
		}
	},
	ewExtractFile: func(w *nsisWalk, p func(int) int) {
		dst := w.dst(w.str(p(1)))
		if dst == "" {
			return
		}
		w.res.Wanted++
		if w.writeData(p(2), dst) {
			w.res.Files++
		}
	},
	ewWriteUninstaller: func(w *nsisWalk, p func(int) int) {
		dst := w.dst(w.str(p(0)))
		if dst == "" {
			return
		}
		w.res.Wanted++
		// ponytail: writes only the appended data block. The real uninstaller also
		// gets a copy of the installer's exehead stub prepended; add that if anyone
		// actually needs to RUN the reconstructed uninstaller.
		if w.writeData(p(1), dst) {
			w.res.Files++
			w.meta("uninstaller: %s (data block only, exe stub not prepended)", dst)
		}
	},
	ewRename: func(w *nsisWalk, p func(int) int) {
		from, to := w.dst(w.str(p(0))), w.dst(w.str(p(1)))
		if from == "" || to == "" {
			return
		}
		if os.MkdirAll(filepath.Dir(to), 0755) == nil {
			_ = os.Rename(from, to)
		}
	},
	ewDeleteFile: func(w *nsisWalk, p func(int) int) {
		if d := w.dst(w.str(p(0))); d != "" {
			_ = os.Remove(d)
		}
	},
	ewRmDir: func(w *nsisWalk, p func(int) int) {
		if d := w.dst(w.str(p(0))); d != "" {
			_ = os.RemoveAll(d)
		}
	},

	// --- variables: this is what makes $INSTDIR & co resolve for real ---
	ewAssignVar: func(w *nsisWalk, p func(int) int) { w.setVar(p(0), w.str(p(1))) },

	// --- metadata: recorded, never executed ---
	ewWriteReg: func(w *nsisWalk, p func(int) int) {
		val := w.str(p(3))
		if p(4) == 4 { // REG_DWORD: param3 is the integer itself
			val = fmt.Sprintf("0x%X", p(3))
		}
		w.meta("reg write: %s\\%s [%s] = %s", nsisRegRoot(p(0)), w.str(p(1)), w.str(p(2)), val)
	},
	ewDelReg: func(w *nsisWalk, p func(int) int) {
		w.meta("reg delete: %s\\%s [%s]", nsisRegRoot(p(0)), w.str(p(1)), w.str(p(2)))
	},
	ewCreateShortcut: func(w *nsisWalk, p func(int) int) {
		w.meta("shortcut: %s -> %s %s (icon %s)", w.str(p(0)), w.str(p(1)), w.str(p(2)), w.str(p(3)))
	},
	ewRegisterDLL: func(w *nsisWalk, p func(int) int) {
		w.meta("plugin/dll: %s!%s", w.str(p(0)), w.str(p(1)))
	},
	ewShellExec: func(w *nsisWalk, p func(int) int) {
		w.meta("shellexec: %s %s %s", w.str(p(0)), w.str(p(1)), w.str(p(2)))
	},
	ewExecute: func(w *nsisWalk, p func(int) int) { w.meta("exec: %s", w.str(p(0))) },
	ewWriteINI: func(w *nsisWalk, p func(int) int) {
		w.meta("ini write: %s [%s] %s = %s", w.str(p(3)), w.str(p(0)), w.str(p(1)), w.str(p(2)))
	},
	ewReadINIStr: func(w *nsisWalk, p func(int) int) {
		w.meta("ini read: %s [%s] %s -> %s", w.str(p(3)), w.str(p(1)), w.str(p(2)), nsisVarName(p(0)))
	},
	ewReadRegStr: func(w *nsisWalk, p func(int) int) {
		w.meta("reg read: %s\\%s [%s] -> %s", nsisRegRoot(p(1)), w.str(p(2)), w.str(p(3)), nsisVarName(p(0)))
	},
	ewCopyFiles: func(w *nsisWalk, p func(int) int) {
		w.meta("copy files: %s -> %s", w.str(p(0)), w.str(p(1)))
	},
	ewSetFileAttributes: func(w *nsisWalk, p func(int) int) {
		w.meta("attributes: %s = 0x%X", w.str(p(0)), p(1))
	},
	ewMessageBox: func(w *nsisWalk, p func(int) int) { w.meta("messagebox: %s", w.str(p(1))) },
	ewReboot:     func(w *nsisWalk, p func(int) int) { w.meta("reboot requested") },

	// --- control flow: counted, not followed (see nsisWalk doc) ---
	ewRet:          countControl,
	ewJmp:          countControl,
	ewCall:         countControl,
	ewIfFileExists: countControl,
	ewIfFlag:       countControl,
	ewStrCmp:       countControl,
	ewIntCmp:       countControl,

	// --- no effect on the tree, nothing worth recording: identified, then dropped.
	// Everything from ewLog up is UI/bookkeeping and its exact numbering drifts
	// most between custom NSIS builds — listing it costs nothing since the handler
	// does nothing either way.
	ewAbort:              nsisNoop,
	ewQuit:               nsisNoop,
	ewUpdateText:         nsisNoop,
	ewSleep:              nsisNoop,
	ewBringToFront:       nsisNoop,
	ewChDetailsView:      nsisNoop,
	ewSetFlag:            nsisNoop,
	ewGetFlag:            nsisNoop,
	ewGetFullPathName:    nsisNoop,
	ewSearchPath:         nsisNoop,
	ewGetTempFileName:    nsisNoop,
	ewStrLen:             nsisNoop,
	ewReadEnvStr:         nsisNoop,
	ewIntOp:              nsisNoop,
	ewIntFmt:             nsisNoop,
	ewPushPop:            nsisNoop,
	ewFindWindow:         nsisNoop,
	ewSendMessage:        nsisNoop,
	ewIsWindow:           nsisNoop,
	ewGetDlgItem:         nsisNoop,
	ewSetCtlColors:       nsisNoop,
	ewSetBrandingImage:   nsisNoop,
	ewCreateFont:         nsisNoop,
	ewShowWindow:         nsisNoop,
	ewGetFileTime:        nsisNoop,
	ewGetDLLVersion:      nsisNoop,
	ewRegEnum:            nsisNoop,
	ewFClose:             nsisNoop,
	ewFOpen:              nsisNoop,
	ewFPuts:              nsisNoop,
	ewFGets:              nsisNoop,
	ewFSeek:              nsisNoop,
	ewFindClose:          nsisNoop,
	ewFindNext:           nsisNoop,
	ewFindFirst:          nsisNoop,
	ewLog:                nsisNoop,
	ewSectionSet:         nsisNoop,
	ewInstTypeSet:        nsisNoop,
	ewGetOSInfo:          nsisNoop,
	ewReservedOpcode:     nsisNoop,
	ewLockWindow:         nsisNoop,
	ewFPutWS:             nsisNoop,
	ewFGetWS:             nsisNoop,
	ewGetFunctionAddress: nsisNoop,
}

// extractStructured parses the NSIS header block, walks the entries table, and
// writes the reconstructed file tree. Any parse fault returns a zero-Files
// result so the caller can fall back to a raw dump.
func extractStructured(dec []byte, hdrSize int, method, outDir string, logMsg func(string)) nsisWalkResult {
	var res nsisWalkResult
	if hdrSize < 4+nsisBlocksNum*8 || hdrSize > len(dec) {
		return res
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
		//nolint:gosec // G115: NSIS block offsets/counts are signed int32 on the wire; the int32() is a deliberate two's-complement reinterpretation
		return int(int32(binary.LittleEndian.Uint32(dec[base : base+4]))),
			int(int32(binary.LittleEndian.Uint32(dec[base+4 : base+8])))
	}

	entriesOff, entryCount := blockOff(nbEntries)
	res.Entries = entryCount
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
	res.Unicode = looksUnicode(strTab)

	// String params are CHARACTER indices; UTF-16LE needs them doubled.
	charSize := 1
	if res.Unicode {
		charSize = 2
	}

	if entriesOff < hdrBase || entryCount <= 0 || entriesOff+entryCount*nsisEntrySize > len(dec) {
		return res
	}

	w := &nsisWalk{
		dec:      dec,
		dataBase: hdrBase + hdrSize, // file data follows the header in the solid stream
		method:   method,
		outDir:   outDir,
		resolve:  func(off int) string { return resolveNSISString(strTab, off*charSize, res.Unicode) },
		vars:     map[string]string{},
		log:      logMsg,
		res:      res,
	}
	for i := range entryCount {
		e := dec[entriesOff+i*nsisEntrySize:]
		//nolint:gosec // G115: NSIS entry opcode and params are signed int32 on the wire; the int32() is a deliberate two's-complement reinterpretation
		which := int(int32(binary.LittleEndian.Uint32(e[0:4])))
		p := func(k int) int { return int(int32(binary.LittleEndian.Uint32(e[4+k*4 : 8+k*4]))) } //nolint:gosec // G115: same signed int32 wire format as `which` above

		fn, ok := nsisOps[which]
		if !ok {
			w.res.Unknown++
			continue
		}
		w.res.Known++
		fn(w, p)
	}
	return w.res
}

// extractRawRecords walks the data region as consecutive [int32 size][bytes]
// records and dumps each as file_NNNN.bin. A last-resort so the actual file
// bytes reach disk even when the entry table could not be parsed. A bogus size
// no longer ends the walk: the cursor slides forward 4 bytes and retries, so one
// corrupt header does not cost every record behind it. Returns records written
// and how many headers were skipped during resynchronisation.
func extractRawRecords(dec []byte, hdrSize int, outDir string, logMsg func(string)) (int, int) {
	pos := hdrSize
	if pos < 0 || pos >= len(dec) {
		pos = 0
	}
	rawDir := filepath.Join(outDir, "_raw")
	_ = os.MkdirAll(rawDir, 0755)
	n, skipped := 0, 0
	for pos+4 <= len(dec) && n < 100000 && skipped < 100000 {
		size := int(binary.LittleEndian.Uint32(dec[pos:pos+4]) &^ 0x80000000)
		if size <= 0 || pos+4+size > len(dec) {
			// ponytail: naive 4-byte resync, no content sniffing — good enough
			// for a last-resort dump; upgrade only if real archives need it.
			pos += 4
			skipped++
			continue
		}
		name := filepath.Join(rawDir, fmt.Sprintf("file_%04d.bin", n))
		if err := os.WriteFile(name, dec[pos+4:pos+4+size], 0644); err == nil { //nolint:gosec // G703: name is a generated file_NNNN.bin under outDir/_raw; no archive-controlled component reaches it
			n++
		}
		pos += 4 + size
	}
	if n > 0 {
		logMsg(fmt.Sprintf("raw fallback dumped %d data records to extracted/_raw/ (%d header(s) skipped)", n, skipped))
	}
	return n, skipped
}

// decompressBlock inflates one individually-compressed (non-solid) data block
// using the method already identified for the archive.
func decompressBlock(b []byte, method string) ([]byte, error) {
	switch method {
	case "LZMA":
		return tryLZMA(b, 0)
	case "deflate":
		return tryInflate(b, 0)
	case "bzip2":
		return tryBzip2(b, 0)
	}
	return nil, fmt.Errorf("unsupported block compression %q", method)
}

// peCertOffset returns the file offset of the Authenticode certificate table
// (PE security data directory), or -1 when the file is not a PE or is unsigned.
func peCertOffset(data []byte) int64 {
	if len(data) < 0x40 || data[0] != 'M' || data[1] != 'Z' {
		return -1
	}
	pe := int(binary.LittleEndian.Uint32(data[0x3C:0x40]))
	if pe < 0 || pe+0x18 > len(data) || string(data[pe:pe+4]) != "PE\x00\x00" {
		return -1
	}
	opt := pe + 0x18
	if opt+2 > len(data) {
		return -1
	}
	// Security directory is entry 4; the directory array starts at 0x60 (PE32) or
	// 0x70 (PE32+) into the optional header.
	dirs := opt + 0x60
	if binary.LittleEndian.Uint16(data[opt:opt+2]) == 0x20B {
		dirs = opt + 0x70
	}
	e := dirs + 4*8
	if e+8 > len(data) {
		return -1
	}
	off := int64(binary.LittleEndian.Uint32(data[e : e+4]))
	size := int64(binary.LittleEndian.Uint32(data[e+4 : e+8]))
	if off <= 0 || size <= 0 || off > int64(len(data)) {
		return -1
	}
	return off
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

// dumpStrings returns the null-terminated strings from the string block. The
// encoding is detected from the table itself, independently of whether the entry
// walk succeeded.
func dumpStrings(dec []byte, hdrSize int) []string {
	if hdrSize < 4+nsisBlocksNum*8 || hdrSize > len(dec) {
		return nil
	}
	hb := nsisHeaderBase(dec, hdrSize)
	bb := hb + 4
	base := bb + nbStrings*8
	stringsOff := hb + int(int32(binary.LittleEndian.Uint32(dec[base:base+4]))) //nolint:gosec // G115: NSIS block offsets are signed int32 on the wire; deliberate two's-complement reinterpretation
	baseL := bb + nbLangtables*8
	langOff := hb + int(int32(binary.LittleEndian.Uint32(dec[baseL:baseL+4]))) //nolint:gosec // G115: NSIS block offsets are signed int32 on the wire; deliberate two's-complement reinterpretation
	if stringsOff < hb || stringsOff >= len(dec) {
		return nil
	}
	end := langOff
	if end <= stringsOff || end > len(dec) {
		end = len(dec)
	}
	tab := dec[stringsOff:end]
	unicode := looksUnicode(tab)
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
	n := min(len(tab), 512)
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

// nsisShellCSIDL maps a Windows CSIDL onto the NSIS constant that carries it.
// A shell reference stores TWO CSIDLs — the per-user folder and its all-users
// twin — so both ids of a pair map to the same NSIS name (e.g. CSIDL_PROGRAMS
// 0x02 and CSIDL_COMMON_PROGRAMS 0x17 are both $SMPROGRAMS).
//
// $QUICKLAUNCH is deliberately absent: NSIS builds it from $APPDATA plus a fixed
// subpath rather than giving it a CSIDL of its own, so it never reaches here.
var nsisShellCSIDL = map[int]string{
	0x00: "DESKTOP", 0x10: "DESKTOP", 0x19: "DESKTOP",
	0x02: "SMPROGRAMS", 0x17: "SMPROGRAMS",
	0x05: "DOCUMENTS", 0x2E: "DOCUMENTS",
	0x06: "FAVORITES", 0x1F: "FAVORITES",
	0x07: "SMSTARTUP", 0x18: "SMSTARTUP",
	0x08: "RECENT",
	0x09: "SENDTO",
	0x0B: "STARTMENU", 0x16: "STARTMENU",
	0x0D: "MUSIC", 0x35: "MUSIC",
	0x0E: "VIDEOS", 0x37: "VIDEOS",
	0x13: "NETHOOD",
	0x14: "FONTS",
	0x15: "TEMPLATES", 0x2D: "TEMPLATES",
	0x1A: "APPDATA", 0x23: "APPDATA",
	0x1B: "PRINTHOOD",
	0x1C: "LOCALAPPDATA",
	0x1D: "ALTSTARTUP",
	0x20: "INTERNET_CACHE",
	0x21: "COOKIES",
	0x22: "HISTORY",
	0x24: "WINDIR",
	0x25: "SYSDIR",
	0x26: "PROGRAMFILES",
	0x27: "PICTURES", 0x36: "PICTURES",
	0x28: "PROFILE",
	0x2B: "COMMONFILES",
	0x2F: "ADMINTOOLS", 0x30: "ADMINTOOLS",
	0x38: "RESOURCES",
	0x39: "RESOURCES_LOCALIZED",
	0x3B: "CDBURN_AREA",
}

// nsisShellNames is the set of names nsisShellCSIDL can produce, so a path
// segment can be recognised as a shell folder rather than an opaque variable.
var nsisShellNames = func() map[string]bool {
	m := make(map[string]bool, len(nsisShellCSIDL))
	for _, v := range nsisShellCSIDL {
		m[v] = true
	}
	return m
}()

// nsisShellName renders a NS_SHELL_CODE parameter as its NSIS constant.
//
// The parameter packs two CSIDLs, seven bits each: the low half is the
// per-user folder, the high half its all-users twin (0x17<<7|0x02 = 2946 is
// $SMPROGRAMS — the pair that produced the "$SHELL2946" this replaces). Seven
// bits is enough because every CSIDL NSIS emits is below 0x80. The per-user id
// names the folder; if it is a marker NSIS uses for "no such variant", the
// all-users id does.
func nsisShellName(param int) string {
	cur, all := param&0x7F, param>>7
	if n, ok := nsisShellCSIDL[cur]; ok {
		return "$" + n
	}
	if n, ok := nsisShellCSIDL[all]; ok {
		return "$" + n
	}
	return fmt.Sprintf("$SHELL%d_%d", cur, all)
}

// nsisVarName renders a variable index as its $NAME. Indices past the documented
// built-ins (user vars, $PLUGINSDIR, ...) become $VARn rather than a guess.
//
// A $VARn that survives to the extracted tree as a _varN directory is EXPECTED,
// not a parse failure: indices ≥25 are script-declared user variables, and a
// linear walk can only resolve one whose value came from a literal EW_ASSIGNVAR
// (StrCpy). Anything filled at RUN time — ReadRegStr, GetTempFileName, a
// System::Call, a plugin, or a Pop off the stack — has no static value, so the
// placeholder is the honest answer rather than an invented path.
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
			b.WriteString(nsisShellName(param))
		case nsVarCode:
			b.WriteString(nsisVarName(param))
		case nsLangCode:
			fmt.Fprintf(&b, "$LANG%d", param)
		}
	}
	if unicode {
		for i := off; i+1 < len(tab); i += 2 {
			u := binary.LittleEndian.Uint16(tab[i : i+2])
			c := int(u)
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
			b.WriteRune(rune(u))
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
			b.WriteByte(tab[i])
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

// nsisVarSegment maps a resolved NSIS variable token ($INSTDIR, $PLUGINSDIR, …)
// occupying a whole path segment onto a safe relative segment. "" means the
// segment collapses to the output root. The result is always a single plain
// name, so a variable can never contribute a traversal component.
func nsisVarSegment(seg string) string {
	switch strings.ToUpper(seg) {
	case "$INSTDIR", "$OUTDIR", "$EXEDIR":
		return "" // installation root == extraction root
	case "$PLUGINSDIR":
		return "_plugins"
	case "$TEMP", "$PLUGINSDIR_TEMP":
		return "_temp"
	}
	name := strings.TrimPrefix(seg, "$")
	// A shell folder gets its own readable segment ($SMPROGRAMS → _shell_SMPROGRAMS).
	// The name comes from nsisShellCSIDL, so it is always a plain [A-Z_] token and
	// cannot contribute a traversal component.
	if nsisShellNames[strings.ToUpper(name)] {
		return "_shell_" + strings.ToUpper(name)
	}
	// $VAR25 → _var25; anything else ($0, $R1, $SHELL23_2, …) → _var_<name>.
	if rest := strings.TrimPrefix(name, "VAR"); rest != name && rest != "" {
		name = rest
	} else {
		name = "_" + name
	}
	var b strings.Builder
	b.WriteString("_var")
	for _, r := range name {
		if r == '.' || r == '/' || r == '\\' || r == ':' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// sanitizeRel strips drive letters, leading separators and "." components, maps
// NSIS variable segments to safe names, and leaves a relative path that cannot
// escape the output directory.
func sanitizeRel(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimSpace(p)
	// drop a leading drive letter or root
	if len(p) >= 2 && p[1] == ':' {
		p = p[2:]
	}
	p = strings.TrimLeft(p, "/")
	var parts []string
	for seg := range strings.SplitSeq(p, "/") {
		seg = strings.TrimSpace(seg)
		if seg == "" || seg == "." || seg == ".." {
			continue
		}
		if strings.HasPrefix(seg, "$") {
			if seg = nsisVarSegment(seg); seg == "" {
				continue
			}
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
