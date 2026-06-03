package recipe

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// funcEntry describes a single decompiled function in functions_index.json /
// functions.ndjson. It is purely additive: it never appears in outputIndex, so
// the existing index.json shape (asserted by ghidra_test.go) is untouched.
type funcEntry struct {
	Name      string `json:"name"`
	Address   string `json:"address"`            // normalized lowercase, 0x-prefixed
	SizeBytes int64  `json:"size_bytes"`         // byte length of the raw record text
	Lines     int    `json:"lines"`              // newline count within the record
	IsNamed   bool   `json:"is_named"`
	File      string `json:"file"`               // slash-normalized rel path of the split file from srcDir
	Signature string `json:"signature,omitempty"` // the function's C signature line (for hookable.json)
}

// symbolEntry is one line of symbols.ndjson: a single address->name mapping.
// Streaming these (rather than buffering a map[string]string) keeps memory O(1)
// in the number of functions.
type symbolEntry struct {
	Address string `json:"address"` // normalized lowercase, 0x-prefixed
	Name    string `json:"name"`
}

// symbolCounts aggregates named/anonymous symbol statistics.
type symbolCounts struct {
	Total     int     `json:"total"`
	Named     int     `json:"named"`
	Anonymous int     `json:"anonymous"`
	NamedPct  float64 `json:"named_pct"`
}

// symbolMap is the structure written to <srcDir>/symbols.json (F2). It is an
// address->name map plus counts and the recovered C++ class list.
type symbolMap struct {
	GeneratedAt string       `json:"generated_at"`
	Counts      symbolCounts `json:"counts"`
	Classes     []string     `json:"classes"`

	// SymbolsNDJSON points at the streamed full address->name catalog
	// (symbols.ndjson, one {"address","name"} object per line). The map is NOT
	// inlined here: at the real scale (millions of functions) an inlined map
	// makes symbols.json a multi-GB blob that both MarshalIndent (write) and
	// os.ReadFile (read) materialize whole, which is the OOM this split caused.
	// symbols.json stays a small, safely-readable summary; consumers that need
	// per-address names stream symbols.ndjson.
	SymbolsNDJSON string `json:"symbols_ndjson,omitempty"`

	// Symbols is only populated when reading legacy/small symbols.json files
	// that still inline the map; it is never written by the current splitter.
	Symbols map[string]string `json:"symbols,omitempty"`
}

// splitResult carries the streaming statistics produced by splitDecompiledC.
// The full per-function list is NOT held in memory — it is streamed to
// functions.ndjson. Only a capped sample is retained for functions_index.json.
type splitResult struct {
	FunctionCount      int
	NamedCount         int
	AnonymousCount     int
	NamedPct           float64
	TotalFunctionBytes int64

	// Counts/Classes mirror what is written to symbols.json so F5 can read them
	// without re-parsing the combined .c.
	Counts  symbolCounts
	Classes []string

	// sample holds up to sampleFunctionCap entries for functions_index.json.
	sample []funcEntry
}

const (
	// splitSoftCap rolls an open bucket file to the next index once it exceeds
	// this many bytes, keeping every split file well under the 2MB LLM limit.
	splitSoftCap = 1_500_000
	// sampleFunctionCap bounds the inline sample array in functions_index.json.
	sampleFunctionCap = 50_000
	// scannerInitBuf / scannerMaxBuf size the streaming line buffer. A single
	// decompiled function can exceed 1MB, so the cap is 16MB; on overflow we
	// fall back to a bufio.Reader loop (see streamLines).
	scannerInitBuf = 1 << 20
	scannerMaxBuf  = 16 << 20
	// maxRecordBytes caps the in-memory size of a single accumulated function
	// record. A pathological record (tens/hundreds of MB under one header) is
	// flushed early once it crosses this, so recBuilder can never grow unbounded.
	maxRecordBytes = 64 << 20
)

var (
	// reFuncHeader matches a bare function-record header: an address comment.
	// Anchored so it does NOT match "// FAILED:", "// ERROR:", "// Total:" or
	// "// Decompiled by". An optional memory-space/segment prefix (e.g.
	// "ram:") is tolerated and stripped, in case this Ghidra version prefixes
	// the entry point.
	reFuncHeader = regexp.MustCompile(`^// (?:[A-Za-z_][A-Za-z0-9_]*:)?([0-9A-Fa-f]{4,16})$`)
	// reTotalLine marks the end-of-output summary written by MorgueExport.java.
	reTotalLine = regexp.MustCompile(`^// Total:`)
	// reAnonName matches Ghidra's auto-generated (anonymous) symbol prefixes.
	reAnonName = regexp.MustCompile(`^(FUN_|DAT_|LAB_|UNK_|SUB_)`)
	// reStringLit matches a C double-quoted string literal (with escapes). Used to
	// build indexes/string_refs.csv (function -> strings it references).
	reStringLit = regexp.MustCompile(`"(\\.|[^"\\])*"`)
	// reCallSite matches an identifier immediately before '(' — a candidate call.
	// Identifiers may be C++-qualified (Class::Method) or templated (TArray<int>).
	reCallSite = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*(?:(?:::|<[^>]*>)[A-Za-z0-9_~]*)*)\s*\(`)
	// rePseudoOp matches Ghidra's synthetic call-syntax builtins — CONCAT44,
	// ZEXT416, SEXT48, SUB164 — which are not real function references. These are
	// Ghidra-universal, not arch/game specific.
	rePseudoOp = regexp.MustCompile(`^(CONCAT|ZEXT|SEXT|SUB)\d`)
)

// primitiveCasts are C/Ghidra primitive type names that appear in cast syntax
// (e.g. "undefined1(x)") and must not be treated as callees.
var primitiveCasts = map[string]bool{
	"void": true, "bool": true, "char": true, "uchar": true, "wchar_t": true,
	"short": true, "ushort": true, "int": true, "uint": true, "long": true,
	"ulong": true, "longlong": true, "ulonglong": true, "float": true,
	"double": true, "byte": true, "code": true,
	"undefined": true, "undefined1": true, "undefined2": true,
	"undefined4": true, "undefined8": true,
}

// callKeywords are C/C++ control tokens that look like calls (kw(...)) but are
// not function references; excluded from the caller->callee graph.
var callKeywords = map[string]bool{
	"if": true, "for": true, "while": true, "switch": true, "return": true,
	"sizeof": true, "do": true, "else": true, "case": true, "catch": true,
	"__assert": true,
}

// extractRefs scans one decompiled function body (raw, the record text including
// its "// <addr>" comment header and signature line) and returns the unique
// string literals it references and the unique callees it invokes, in first-seen
// order. callerName is excluded from callees (its own signature is in raw).
//
// Memory: both result sets are bounded by the content of a SINGLE record, which
// the splitter already caps at maxRecordBytes; nothing here scales with the
// total function count.
func extractRefs(raw, callerName string) (strs, callees []string) {
	seenStr := map[string]bool{}
	for _, m := range reStringLit.FindAllString(raw, -1) {
		inner := m[1 : len(m)-1] // strip surrounding quotes
		if inner == "" || seenStr[inner] {
			continue
		}
		seenStr[inner] = true
		strs = append(strs, inner)
	}

	seenCall := map[string]bool{}
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue // comment / record header
		}
		for _, m := range reCallSite.FindAllStringSubmatch(line, -1) {
			callee := m[1]
			if callee == "" || callKeywords[callee] || callee == callerName || seenCall[callee] {
				continue
			}
			if primitiveCasts[callee] || rePseudoOp.MatchString(callee) {
				continue // Ghidra cast / synthetic pseudo-op, not a real call
			}
			seenCall[callee] = true
			callees = append(callees, callee)
		}
	}
	return strs, callees
}

// splitDecompiledC streams the combined decompiled .c at combinedCPath, splits
// it into per-function files under funcsDir (bucketed by address), streams a
// per-function NDJSON catalog to <srcDir>/functions.ndjson, and writes
// <srcDir>/symbols.json (F2). It NEVER loads the whole .c into memory.
//
// srcDir is the directory that contains the combined .c (and where the sibling
// JSON outputs live); funcsDir is normally <srcDir>/functions.
func splitDecompiledC(combinedCPath, srcDir, funcsDir string) (*splitResult, error) {
	in, err := os.Open(combinedCPath)
	if err != nil {
		return nil, fmt.Errorf("open combined .c: %w", err)
	}
	defer in.Close()

	inInfo, err := in.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat combined .c: %w", err)
	}

	// Make the split idempotent: clear any prior functions/ tree first. The
	// bucket writer only ever rolls to higher-numbered files, so without this a
	// re-run (e.g. re-decompiling the same target) would APPEND new bucket files
	// alongside stale ones rather than replacing them.
	if err := os.RemoveAll(funcsDir); err != nil {
		return nil, fmt.Errorf("clear functions dir: %w", err)
	}
	if err := os.MkdirAll(funcsDir, 0755); err != nil {
		return nil, fmt.Errorf("mkdir functions: %w", err)
	}

	// NDJSON sink for the full per-function catalog (never buffered as structs).
	ndjsonPath := filepath.Join(srcDir, "functions.ndjson")
	ndjsonFile, err := os.Create(ndjsonPath)
	if err != nil {
		return nil, fmt.Errorf("create functions.ndjson: %w", err)
	}
	ndjsonBuf := bufio.NewWriterSize(ndjsonFile, 64*1024)
	ndjsonEnc := json.NewEncoder(ndjsonBuf)
	defer func() {
		ndjsonBuf.Flush()
		ndjsonFile.Close()
	}()

	// NDJSON sink for the full address->name symbol catalog (F2). Streamed for
	// the same reason as functions.ndjson: an in-RAM map[string]string plus a
	// final MarshalIndent is O(n) memory and OOMs on million-function binaries.
	symNDJSONPath := filepath.Join(srcDir, "symbols.ndjson")
	symFile, err := os.Create(symNDJSONPath)
	if err != nil {
		return nil, fmt.Errorf("create symbols.ndjson: %w", err)
	}
	symBuf := bufio.NewWriterSize(symFile, 64*1024)
	symEnc := json.NewEncoder(symBuf)
	defer func() {
		symBuf.Flush()
		symFile.Close()
	}()

	// indexes/ cross-reference CSVs (B1): string_refs.csv (function -> strings it
	// references) and callers.csv (caller -> callee). Both are written streaming,
	// one row per (function, ref) as each record is processed — never an in-RAM
	// graph keyed by function count.
	indexesDir := filepath.Join(srcDir, "indexes")
	if err := os.MkdirAll(indexesDir, 0755); err != nil {
		return nil, fmt.Errorf("mkdir indexes: %w", err)
	}
	strRefsFile, err := os.Create(filepath.Join(indexesDir, "string_refs.csv"))
	if err != nil {
		return nil, fmt.Errorf("create string_refs.csv: %w", err)
	}
	strRefsBuf := bufio.NewWriterSize(strRefsFile, 64*1024)
	strRefsCSV := csv.NewWriter(strRefsBuf)
	defer func() {
		strRefsCSV.Flush()
		strRefsBuf.Flush()
		strRefsFile.Close()
	}()
	callersFile, err := os.Create(filepath.Join(indexesDir, "callers.csv"))
	if err != nil {
		return nil, fmt.Errorf("create callers.csv: %w", err)
	}
	callersBuf := bufio.NewWriterSize(callersFile, 64*1024)
	callersCSV := csv.NewWriter(callersBuf)
	defer func() {
		callersCSV.Flush()
		callersBuf.Flush()
		callersFile.Close()
	}()
	if err := strRefsCSV.Write([]string{"function", "address", "string"}); err != nil {
		return nil, fmt.Errorf("write string_refs header: %w", err)
	}
	if err := callersCSV.Write([]string{"caller", "caller_address", "callee"}); err != nil {
		return nil, fmt.Errorf("write callers header: %w", err)
	}

	res := &splitResult{}

	// Class recovery (F2): a dedup set of C++ class owners. Bounded by the number
	// of DISTINCT classes (engine classes — small relative to function count),
	// not by function count, so it does not reproduce the per-function blowup.
	classSet := make(map[string]bool)

	bw := newBucketWriter(funcsDir)
	defer bw.closeOpen()

	// recordState accumulates one function record while streaming.
	var (
		inRecord   bool
		recAddrHex string // normalized lowercase, no 0x
		recBuilder strings.Builder
		recBytes   int64
		recLines   int
		recCapped  bool // true once the record hit maxRecordBytes (body truncated in RAM)
		sigPending bool // next non-blank line is the signature
		recName    string
		recSig     string // the captured signature line (for hookable.json)
		recSigDone bool
	)

	flush := func() error {
		if !inRecord {
			return nil
		}
		raw := recBuilder.String()
		name := recName
		if name == "" {
			name = "FUN_" + recAddrHex
		}
		addr0x := "0x" + recAddrHex
		isNamed := name != "" && !reAnonName.MatchString(name)

		// Write the split file with a self-describing header.
		header := fmt.Sprintf("// === %s @ %s (size=%d lines=%d) ===\n", name, addr0x, recBytes, recLines)
		relFile, werr := bw.write(recAddrHex, header+raw)
		if werr != nil {
			return werr
		}

		entry := funcEntry{
			Name:      name,
			Address:   addr0x,
			SizeBytes: recBytes,
			Lines:     recLines,
			IsNamed:   isNamed,
			File:      relFile,
			Signature: recSig,
		}
		if encErr := ndjsonEnc.Encode(&entry); encErr != nil {
			return encErr
		}

		res.FunctionCount++
		res.TotalFunctionBytes += recBytes
		if isNamed {
			res.NamedCount++
		} else {
			res.AnonymousCount++
		}
		if len(res.sample) < sampleFunctionCap {
			res.sample = append(res.sample, entry)
		}

		// Symbol catalog + class recovery (F2): stream one entry, never buffer.
		if encErr := symEnc.Encode(&symbolEntry{Address: addr0x, Name: name}); encErr != nil {
			return encErr
		}
		if owner := cppClassOwner(name); owner != "" {
			classSet[owner] = true
		}

		// Cross-reference indexes (B1): one streamed row per (func, string) and
		// per (caller, callee). extractRefs dedups within this single record.
		refStrs, refCallees := extractRefs(raw, name)
		for _, s := range refStrs {
			if csvErr := strRefsCSV.Write([]string{name, addr0x, s}); csvErr != nil {
				return csvErr
			}
		}
		for _, callee := range refCallees {
			if csvErr := callersCSV.Write([]string{name, addr0x, callee}); csvErr != nil {
				return csvErr
			}
		}

		inRecord = false
		recBuilder.Reset()
		recBytes = 0
		recLines = 0
		recCapped = false
		recName = ""
		recSig = ""
		recSigDone = false
		sigPending = false
		return nil
	}

	startRecord := func(addrHex, rawLine string) error {
		if err := flush(); err != nil {
			return err
		}
		inRecord = true
		recAddrHex = strings.ToLower(addrHex)
		recBuilder.Reset()
		recBuilder.WriteString(rawLine)
		recBuilder.WriteByte('\n')
		recBytes = int64(len(rawLine)) + 1
		recLines = 1
		recCapped = false
		recName = ""
		recSig = ""
		recSigDone = false
		sigPending = true
		return nil
	}

	appendLine := func(rawLine string) {
		recBytes += int64(len(rawLine)) + 1
		recLines++
		// The first non-blank line after the header is the signature.
		if sigPending && strings.TrimSpace(rawLine) != "" {
			recName = extractFuncName(rawLine, recAddrHex)
			recSig = strings.TrimSpace(rawLine)
			recSigDone = true
			sigPending = false
		}
		_ = recSigDone
		// Cap the in-memory record body. recBytes/recLines keep counting (so the
		// catalog reflects the true record size), but we stop growing recBuilder
		// so a single pathological record can never exhaust RAM.
		if recCapped {
			return
		}
		if recBuilder.Len() >= maxRecordBytes {
			recBuilder.WriteString("// ... [record body truncated by Morgue: exceeded in-memory cap] ...\n")
			recCapped = true
			return
		}
		recBuilder.WriteString(rawLine)
		recBuilder.WriteByte('\n')
	}

	onLine := func(line string) error {
		// "// Total:" terminates output: flush the open record, ignore the rest.
		if reTotalLine.MatchString(line) {
			return flush()
		}
		if m := reFuncHeader.FindStringSubmatch(line); m != nil {
			return startRecord(m[1], line)
		}
		if inRecord {
			appendLine(line)
		}
		// Lines before the first record (the file header banner) are ignored.
		return nil
	}

	if err := streamLines(in, onLine); err != nil {
		return nil, err
	}
	if err := flush(); err != nil {
		return nil, err
	}

	if err := bw.closeOpen(); err != nil {
		return nil, err
	}
	if err := ndjsonBuf.Flush(); err != nil {
		return nil, err
	}
	if err := symBuf.Flush(); err != nil {
		return nil, err
	}
	strRefsCSV.Flush()
	if err := strRefsCSV.Error(); err != nil {
		return nil, fmt.Errorf("flush string_refs.csv: %w", err)
	}
	if err := strRefsBuf.Flush(); err != nil {
		return nil, err
	}
	callersCSV.Flush()
	if err := callersCSV.Error(); err != nil {
		return nil, fmt.Errorf("flush callers.csv: %w", err)
	}
	if err := callersBuf.Flush(); err != nil {
		return nil, err
	}

	// Regression guard: a non-empty combined .c that yields zero functions means
	// the Ghidra header convention drifted (reFuncHeader no longer matches). Fail
	// loudly instead of silently writing an empty, useless index.
	if res.FunctionCount == 0 && inInfo.Size() > 0 {
		return nil, fmt.Errorf("split produced 0 functions from a non-empty combined .c (%d bytes): "+
			"function-header format may have changed", inInfo.Size())
	}

	// Finalize counts.
	res.NamedPct = pct(res.NamedCount, res.FunctionCount)
	res.Counts = symbolCounts{
		Total:     res.FunctionCount,
		Named:     res.NamedCount,
		Anonymous: res.AnonymousCount,
		NamedPct:  res.NamedPct,
	}
	res.Classes = sortedKeys(classSet)

	// F2: write symbols.json as a SMALL summary (counts + classes + a pointer to
	// the streamed symbols.ndjson). It deliberately does NOT inline the full
	// address->name map, so both writing it (here) and reading it (ue5.go) stay
	// O(#classes), never O(#functions).
	sm := symbolMap{
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		Counts:        res.Counts,
		Classes:       res.Classes,
		SymbolsNDJSON: "symbols.ndjson",
	}
	data, mErr := json.MarshalIndent(&sm, "", "  ")
	if mErr != nil {
		return nil, fmt.Errorf("marshal symbols.json: %w", mErr)
	}
	if wErr := os.WriteFile(filepath.Join(srcDir, "symbols.json"), data, 0644); wErr != nil {
		return nil, fmt.Errorf("write symbols.json: %w", wErr)
	}

	return res, nil
}

// streamLines feeds each line of r to fn without loading the whole input. It
// uses a bufio.Scanner with a 16MB cap, falling back to a bufio.Reader loop on
// bufio.ErrTooLong so a single >16MB decompiled function never aborts the pass.
func streamLines(r io.Reader, fn func(line string) error) error {
	br := bufio.NewReaderSize(r, scannerInitBuf)
	scanner := bufio.NewScanner(br)
	scanner.Buffer(make([]byte, 0, scannerInitBuf), scannerMaxBuf)
	for scanner.Scan() {
		if err := fn(scanner.Text()); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		if err == bufio.ErrTooLong {
			// Fall back to a manual reader loop from where the scanner stopped.
			return streamLinesReader(br, fn)
		}
		return err
	}
	return nil
}

// streamLinesReader is the ReadString('\n') fallback for over-long lines.
func streamLinesReader(br *bufio.Reader, fn func(line string) error) error {
	for {
		line, err := br.ReadString('\n')
		if len(line) > 0 {
			if cb := fn(strings.TrimRight(line, "\n")); cb != nil {
				return cb
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// bucketWriter writes function records into address-bucketed files, rolling to
// a new file index when the open one exceeds splitSoftCap. Only one file is
// open at a time.
type bucketWriter struct {
	funcsDir   string
	curBucket  string
	curIndex   int
	curWritten int64
	curFile    *os.File
	curBuf     *bufio.Writer
	curRel     string
}

func newBucketWriter(funcsDir string) *bucketWriter {
	return &bucketWriter{funcsDir: funcsDir, curIndex: -1}
}

// bucketFor returns the 2-hex-char bucket for an address (first 2 chars of the
// address zero-padded to 8 hex digits => <=256 buckets).
// bucketFor maps a function address to a bucket directory name. It must work for
// ANY image base: taking the high hex digits (the old behavior) collapses every
// function of a single-module image into one bucket, because those digits are
// the constant load base (e.g. 0x14 for a UE5 /BASE:0x140000000 image, 0x00 for
// a native 0x400000 image). Instead we drop the lowest 12 bits (so functions in
// the same ~4KB window cluster together for navigation) and key on the next 12
// bits, giving up to 4096 buckets that distribute regardless of the base and
// keep nearby addresses adjacent. Deterministic; never sized by file input.
func bucketFor(addrHex string) string {
	h := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(addrHex)), "0x")
	v, err := strconv.ParseUint(h, 16, 64)
	if err != nil {
		// Unparseable address: deterministic fallback bucket, never crash.
		return "000"
	}
	return fmt.Sprintf("%03x", (v>>12)&0xfff)
}

// write appends block to the appropriate bucket file and returns the
// slash-normalized rel path of that file from the parent of funcsDir (srcDir).
func (b *bucketWriter) write(addrHex, block string) (string, error) {
	bucket := bucketFor(addrHex)
	// Open a new file when the bucket changes or the soft cap is exceeded.
	if b.curFile == nil || bucket != b.curBucket || b.curWritten >= splitSoftCap {
		if bucket != b.curBucket {
			b.curIndex = 0
		} else {
			b.curIndex++
		}
		if err := b.openFile(bucket, b.curIndex); err != nil {
			return "", err
		}
	}
	if _, err := b.curBuf.WriteString(block); err != nil {
		return "", err
	}
	if !strings.HasSuffix(block, "\n") {
		b.curBuf.WriteByte('\n')
		b.curWritten++
	}
	b.curWritten += int64(len(block))
	return b.curRel, nil
}

func (b *bucketWriter) openFile(bucket string, index int) error {
	if err := b.closeOpen(); err != nil {
		return err
	}
	bucketDir := filepath.Join(b.funcsDir, bucket)
	if err := os.MkdirAll(bucketDir, 0755); err != nil {
		return err
	}
	name := fmt.Sprintf("%s_%02d.c", bucket, index)
	full := filepath.Join(bucketDir, name)
	// Open with O_APPEND (not O_TRUNC) so that if an out-of-order address ever
	// causes this (bucket,index) file to be re-opened, we append to it instead
	// of truncating already-written functions. We seed curWritten with the
	// existing size so the soft-cap roll stays accurate across re-opens.
	f, err := os.OpenFile(full, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	existing := int64(0)
	if st, statErr := f.Stat(); statErr == nil {
		existing = st.Size()
	}
	b.curFile = f
	b.curBuf = bufio.NewWriterSize(f, 64*1024)
	b.curBucket = bucket
	b.curIndex = index
	b.curWritten = existing
	// rel path from srcDir (= parent of funcsDir).
	b.curRel = filepath.ToSlash(filepath.Join(filepath.Base(b.funcsDir), bucket, name))
	return nil
}

func (b *bucketWriter) closeOpen() error {
	if b.curFile == nil {
		return nil
	}
	if err := b.curBuf.Flush(); err != nil {
		b.curFile.Close()
		b.curFile = nil
		return err
	}
	err := b.curFile.Close()
	b.curFile = nil
	b.curBuf = nil
	return err
}

// extractFuncName returns the identifier token immediately before the first '('
// in a C signature line. Pointer/qualifier tokens are stripped. On no '(' or
// empty name it returns "" (the caller substitutes FUN_<addr>).
func extractFuncName(sig, _ string) string {
	// The parameter list opens at the FIRST '(' that is immediately preceded by
	// an identifier/template/qualifier character — i.e. the name attaches to it
	// with no separator ("name(args)"). This skips return-type decorations such
	// as Ghidra's pointer-to-array returns "undefined1 (*) [16]FUN_x(args)",
	// where the first '(' belongs to "(*)" (preceded by a space) and the real
	// name attaches to a later '('.
	paren := -1
	for i := 0; i < len(sig); i++ {
		if sig[i] != '(' {
			continue
		}
		if i > 0 {
			c := sig[i-1]
			if isIdentByte(c) || c == '>' {
				paren = i
				break
			}
		}
	}
	if paren < 0 {
		return ""
	}
	head := sig[:paren]

	// The function name is the last identifier-ish token in head. Walk back
	// over trailing whitespace and '*'/'&' (pointer return glued to the name).
	end := len(head)
	for end > 0 && (head[end-1] == ' ' || head[end-1] == '\t' || head[end-1] == '*' || head[end-1] == '&') {
		end--
	}
	start := end
	for start > 0 {
		c := head[start-1]
		if isIdentByte(c) || c == ':' || c == '~' || c == '<' || c == '>' || c == ',' {
			start--
			continue
		}
		break
	}
	name := strings.TrimSpace(head[start:end])
	name = strings.TrimLeft(name, "*& \t")
	if name == "" || primitiveCasts[name] {
		// A bare primitive/undefined type is a mis-parse, not a real name.
		return ""
	}
	return name
}

func isIdentByte(c byte) bool {
	return c == '_' ||
		(c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9')
}

// cppClassOwner returns the recovered C++ class for a '::'-qualified name: the
// substring before the LAST top-level '::' (bracket-depth aware, so '::' inside
// template angle brackets is not treated as a separator). Returns "" for names
// with no top-level '::'.
//
// A::B::method      -> A::B
// TArray<int>::Add  -> TArray<int>
// foo               -> ""
func cppClassOwner(name string) string {
	depth := 0
	lastSep := -1
	for i := 0; i+1 < len(name); i++ {
		switch name[i] {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		case ':':
			if depth == 0 && name[i+1] == ':' {
				lastSep = i
			}
		}
	}
	if lastSep <= 0 {
		return ""
	}
	owner := name[:lastSep]
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return ""
	}
	return owner
}

// pct returns a 2-decimal percentage of n over total (0 when total==0).
func pct(n, total int) float64 {
	if total == 0 {
		return 0
	}
	v := float64(n) * 100.0 / float64(total)
	// Round to 2 decimals.
	return float64(int64(v*100+0.5)) / 100.0
}

// functionsIndex is written to <srcDir>/functions_index.json (F1). It is a NEW
// sibling file and does NOT alter outputIndex, so ghidra_test.go is unaffected.
type functionsIndex struct {
	GeneratedAt        string      `json:"generated_at"`
	Binary             string      `json:"binary"`
	FunctionCount      int         `json:"function_count"`
	NamedCount         int         `json:"named_count"`
	AnonymousCount     int         `json:"anonymous_count"`
	NamedPct           float64     `json:"named_pct"`
	TotalFunctionBytes int64       `json:"total_function_bytes"`
	FunctionsNDJSON    string      `json:"functions_ndjson"`
	SampleFunctions    []funcEntry `json:"sample_functions"`
}

// writeNativeIndex writes <srcDir>/functions_index.json from the streaming
// result. The sample is capped (already enforced during streaming) and sorted
// by address for stable, navigable output. binary is the source binary name
// (for provenance).
func writeNativeIndex(srcDir, binary string, r *splitResult) error {
	sample := r.sample
	sort.Slice(sample, func(i, j int) bool { return sample[i].Address < sample[j].Address })
	if sample == nil {
		sample = []funcEntry{}
	}
	fi := functionsIndex{
		GeneratedAt:        time.Now().UTC().Format(time.RFC3339),
		Binary:             binary,
		FunctionCount:      r.FunctionCount,
		NamedCount:         r.NamedCount,
		AnonymousCount:     r.AnonymousCount,
		NamedPct:           r.NamedPct,
		TotalFunctionBytes: r.TotalFunctionBytes,
		FunctionsNDJSON:    "functions.ndjson",
		SampleFunctions:    sample,
	}
	data, err := json.MarshalIndent(&fi, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(srcDir, "functions_index.json"), data, 0644)
}

// splitAndIndexDecompiledC is the high-level wiring helper: given a srcDir
// containing a combined <baseName>.c (the same construction runGhidra uses), it
// locates the combined .c, splits it, and writes functions_index.json +
// symbols.json + functions.ndjson + the functions/ tree. It returns the result
// (nil if the combined .c is absent — caller treats that as a no-op). All new
// data is additive; the combined .c is left unchanged.
func splitAndIndexDecompiledC(srcDir, binaryPath string) (*splitResult, error) {
	baseName := strings.TrimSuffix(filepath.Base(binaryPath), filepath.Ext(binaryPath))
	combined := filepath.Join(srcDir, baseName+".c")
	if !fileExists(combined) {
		return nil, nil
	}
	funcsDir := filepath.Join(srcDir, "functions")
	res, err := splitDecompiledC(combined, srcDir, funcsDir)
	if err != nil {
		return nil, err
	}
	if err := writeNativeIndex(srcDir, filepath.Base(binaryPath), res); err != nil {
		return res, err
	}
	return res, nil
}
