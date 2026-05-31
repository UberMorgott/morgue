package recipe

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// funcEntry describes a single decompiled function in functions_index.json /
// functions.ndjson. It is purely additive: it never appears in outputIndex, so
// the existing index.json shape (asserted by ghidra_test.go) is untouched.
type funcEntry struct {
	Name      string `json:"name"`
	Address   string `json:"address"`    // normalized lowercase, 0x-prefixed
	SizeBytes int64  `json:"size_bytes"` // byte length of the raw record text
	Lines     int    `json:"lines"`      // newline count within the record
	IsNamed   bool   `json:"is_named"`
	File      string `json:"file"` // slash-normalized rel path of the split file from srcDir
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
	GeneratedAt string            `json:"generated_at"`
	Counts      symbolCounts      `json:"counts"`
	Classes     []string          `json:"classes"`
	Symbols     map[string]string `json:"symbols"`
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
)

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

	res := &splitResult{}

	// Symbol accumulation (F2): single pass over the same records.
	symbols := make(map[string]string)
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
		sigPending bool // next non-blank line is the signature
		recName    string
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

		// Symbol map + class recovery (F2).
		symbols[addr0x] = name
		if owner := cppClassOwner(name); owner != "" {
			classSet[owner] = true
		}

		inRecord = false
		recBuilder.Reset()
		recBytes = 0
		recLines = 0
		recName = ""
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
		recName = ""
		recSigDone = false
		sigPending = true
		return nil
	}

	appendLine := func(rawLine string) {
		recBuilder.WriteString(rawLine)
		recBuilder.WriteByte('\n')
		recBytes += int64(len(rawLine)) + 1
		recLines++
		// The first non-blank line after the header is the signature.
		if sigPending && strings.TrimSpace(rawLine) != "" {
			recName = extractFuncName(rawLine, recAddrHex)
			recSigDone = true
			sigPending = false
		}
		_ = recSigDone
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

	// Finalize counts.
	res.NamedPct = pct(res.NamedCount, res.FunctionCount)
	res.Counts = symbolCounts{
		Total:     res.FunctionCount,
		Named:     res.NamedCount,
		Anonymous: res.AnonymousCount,
		NamedPct:  res.NamedPct,
	}
	res.Classes = sortedKeys(classSet)

	// F2: write symbols.json. MarshalIndent of the symbols map is O(n) memory;
	// acceptable for v1 (streaming fallback noted in the spec as a risk).
	sm := symbolMap{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Counts:      res.Counts,
		Classes:     res.Classes,
		Symbols:     symbols,
	}
	if data, mErr := json.MarshalIndent(&sm, "", "  "); mErr == nil {
		if wErr := os.WriteFile(filepath.Join(srcDir, "symbols.json"), data, 0644); wErr != nil {
			return nil, fmt.Errorf("write symbols.json: %w", wErr)
		}
	} else {
		return nil, fmt.Errorf("marshal symbols.json: %w", mErr)
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
func bucketFor(addrHex string) string {
	padded := addrHex
	if len(padded) < 8 {
		padded = strings.Repeat("0", 8-len(padded)) + padded
	}
	return strings.ToLower(padded[:2])
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
	head, _, ok := strings.Cut(sig, "(")
	if !ok {
		return ""
	}
	// The function name is the last identifier-ish token in head. Walk back
	// over trailing whitespace and '*' (pointer return type glued to name).
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
	// A leading '*'/'&' may remain glued (e.g. "void *foo"): trim them.
	name = strings.TrimLeft(name, "*& \t")
	if name == "" {
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
