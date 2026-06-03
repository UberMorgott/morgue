package recipe

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// dedupe.go: post-processing cleanup that makes the decompiled corpus readable
// for a neural net WITHOUT deleting anything.
//
//  1. dedupeFunctionBodies collapses functions whose decompiled bodies are byte-
//     identical (after stripping the address-comment header). These are dominated
//     by templated instantiations (TArray<T>/TMap<K,V> etc.) that Ghidra emits
//     once per type — thousands of near-clones that bury the unique logic. We
//     emit indexes/duplicates.json mapping a canonical function to its duplicates
//     and LOG the collapsed count. Originals stay on disk; nothing is truncated.
//
//  2. writeGameViews emits *.game.csv companions of the string-ref and caller
//     indexes that exclude pure-engine boilerplate (by isBoilerplateClass on the
//     owning C++ class), so an LLM sees game logic first. Additive: the full
//     indexes are untouched.
//
// MEMORY SAFETY: dedupe streams every split file and keeps only a fixed-size
// record per UNIQUE body hash (uint64 hash -> small group struct), never the
// bodies themselves and never an O(total) body map. The per-group duplicate
// example list is capped. Game views stream rows and write rows; no full-file
// buffering.

// dupExampleCap bounds how many duplicate members are listed per group in
// duplicates.json. The full count is always reported; only the explicit example
// list is capped to keep the report (and RAM) bounded under pathological clones.
const dupExampleCap = 64

// usmapDedupeMaxUnique is the self-imposed budget on DISTINCT function bodies the
// dedupe accumulator will track. Each tracked unique costs a small dupAccum
// (pointer + two short cloned strings) plus map overhead — on the order of ~150
// bytes resident, so 8M uniques ≈ ~1.2GB worst case, comfortably under the Job
// Object cap with headroom for the rest of the process. Real UE binaries top out
// in the hundreds of thousands of functions (Windrose: 495k total / 441k unique,
// ~110 MiB peak after the substring-clone fix), so this only trips on a
// pathological/huge corpus, where we degrade gracefully (stop tracking new
// groups, log it) instead of risking an OOM near the cap. It is a var (not const)
// solely so a test can lower it to exercise the budget path.
var usmapDedupeMaxUnique = 8_000_000

// dupMember identifies one function instance (address + best-effort name).
type dupMember struct {
	Address string `json:"address"`
	Name    string `json:"name,omitempty"`
}

// dupGroup is one set of byte-identical function bodies.
type dupGroup struct {
	CanonicalAddress string      `json:"canonical_address"`
	CanonicalName    string      `json:"canonical_name,omitempty"`
	Count            int         `json:"count"`               // total members (incl. canonical)
	Duplicates       []dupMember `json:"duplicates"`          // capped at dupExampleCap
	Truncated        bool        `json:"truncated,omitempty"` // true if Duplicates was capped
}

// duplicatesReport is indexes/duplicates.json.
type duplicatesReport struct {
	TotalFunctions     int        `json:"total_functions"`
	UniqueFunctions    int        `json:"unique_functions"`
	DuplicateFunctions int        `json:"duplicate_functions"` // collapsed (total - unique)
	DuplicateGroups    int        `json:"duplicate_groups"`
	Note               string     `json:"note"`
	Groups             []dupGroup `json:"groups"`
}

// dedupeResult summarizes a dedupe pass for logging.
type dedupeResult struct {
	TotalFunctions     int
	UniqueFunctions    int
	DuplicateFunctions int
	DuplicateGroups    int
	BudgetExceeded     bool // true if the unique-body budget was hit (partial dedupe)
}

// internal accumulator (kept in RAM keyed by body hash). Holds NO body text.
type dupAccum struct {
	canonAddr string
	canonName string
	count     int
	dups      []dupMember
	truncated bool
}

// dedupeFunctionBodies scans <srcDir>/functions/*.c, hashes each function body
// (excluding its "// <addr>" header line), and writes indexes/duplicates.json.
// Returns counts. If functions/ is absent it is a no-op (count 0). Streaming and
// memory-safe: only one uint64->dupAccum map (sized by unique bodies) is held.
func dedupeFunctionBodies(srcDir string) (dedupeResult, error) {
	var res dedupeResult
	fnDir := filepath.Join(srcDir, "functions")
	if fi, err := os.Stat(fnDir); err != nil || !fi.IsDir() {
		return res, nil
	}

	files, err := listSplitFiles(fnDir)
	if err != nil {
		return res, err
	}

	groups := make(map[uint64]*dupAccum)

	// One reused hasher across all bodies (Reset per body) — no per-body hasher
	// allocation. hashBody normalizes inline (no intermediate normalized string),
	// so the only per-body cost is the addr/name strings the callback receives.
	h := fnv.New64a()

	for _, fp := range files {
		if err := scanSplitRecords(fp, func(addr, name, body string) {
			res.TotalFunctions++

			// SAFETY NET: a self-imposed budget on distinct functions. If the
			// corpus is so large that the accumulator map would threaten the
			// process memory cap, stop accumulating NEW groups (still count totals
			// and still attribute to already-seen groups) rather than risk a
			// process-killing alloc near the Job Object cap. Logged by the caller.
			h.Reset()
			hashBody(h, body)
			key := h.Sum64()
			g, ok := groups[key]
			if ok {
				g.count++
				if len(g.dups) < dupExampleCap {
					// Clone: name is a SUBSTRING of the (large) body; storing it
					// raw would pin the whole body's backing array in the map,
					// keeping every retained body alive (the real heap-peak cause).
					g.dups = append(g.dups, dupMember{Address: addr, Name: cloneStr(name)})
				} else {
					g.truncated = true
				}
				return
			}
			if len(groups) >= usmapDedupeMaxUnique {
				// Budget exhausted: don't grow the map further. This body becomes
				// an un-tracked unique (counted in TotalFunctions only). Mark that
				// the pass was budget-capped so the report/log is honest.
				res.BudgetExceeded = true
				return
			}
			// Clone canonName for the same backing-array reason as above. addr is
			// already a freshly-built "0x..." string, but cloning it too is cheap
			// and removes any doubt about retained substrings.
			groups[key] = &dupAccum{canonAddr: cloneStr(addr), canonName: cloneStr(name), count: 1}
		}); err != nil {
			return res, err
		}
	}

	// Collapsed count = sum of (count-1) over all groups. This is exact whether
	// or not the budget was hit: un-tracked uniques (budget overflow) never enter
	// a group, so they correctly count as unique in (total - collapsed).
	collapsed := 0
	for _, g := range groups {
		collapsed += g.count - 1
	}
	res.DuplicateFunctions = collapsed
	res.UniqueFunctions = res.TotalFunctions - collapsed

	// Build the report (only groups with >1 member).
	note := "Functions whose decompiled bodies are identical after canonicalizing " +
		"Ghidra address-symbols (FUN_/DAT_/LAB_/UNK_/SUB_<hex> -> <SYM>) and " +
		"excluding the address header are collapsed here — these are dominated by " +
		"templated TArray/TMap instantiations. Originals remain on disk; this is a " +
		"map from a canonical function to its structural clones. Nothing was " +
		"deleted or truncated from the source."
	if res.BudgetExceeded {
		note += " NOTE: the corpus exceeded the dedupe memory budget (" +
			"usmapDedupeMaxUnique distinct bodies); deduplication is PARTIAL — only " +
			"the first budget-many distinct bodies were tracked, so some duplicates " +
			"may be uncollapsed. No data was lost; this only affects this report."
	}
	report := duplicatesReport{
		TotalFunctions:     res.TotalFunctions,
		UniqueFunctions:    res.UniqueFunctions,
		DuplicateFunctions: res.DuplicateFunctions,
		Note:               note,
	}
	for _, g := range groups {
		if g.count <= 1 {
			continue
		}
		report.DuplicateGroups++
		report.Groups = append(report.Groups, dupGroup{
			CanonicalAddress: g.canonAddr,
			CanonicalName:    g.canonName,
			Count:            g.count,
			Duplicates:       g.dups,
			Truncated:        g.truncated,
		})
	}
	res.DuplicateGroups = report.DuplicateGroups
	// Deterministic order: largest groups first, then by canonical address.
	sort.Slice(report.Groups, func(i, j int) bool {
		if report.Groups[i].Count != report.Groups[j].Count {
			return report.Groups[i].Count > report.Groups[j].Count
		}
		return report.Groups[i].CanonicalAddress < report.Groups[j].CanonicalAddress
	})

	idxDir := filepath.Join(srcDir, "indexes")
	if err := os.MkdirAll(idxDir, 0755); err != nil {
		return res, err
	}
	if err := writeJSONIndent(filepath.Join(idxDir, "duplicates.json"), &report); err != nil {
		return res, err
	}
	return res, nil
}

// listSplitFiles returns the .c split files under fnDir, sorted for determinism.
func listSplitFiles(fnDir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(fnDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && strings.HasSuffix(strings.ToLower(path), ".c") {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

// scanSplitRecords streams one split file and invokes fn(addr, name, body) per
// function record. A record starts at a "// <addr>" header (reFuncHeader) and
// runs until the next header or EOF. The body passed to fn EXCLUDES the header
// line so identical functions at different addresses hash equal. name is the
// best-effort function name parsed from the body's first non-empty signature
// line. Streaming: at most one record's lines are buffered at a time.
func scanSplitRecords(path string, fn func(addr, name, body string)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	br := bufio.NewReaderSize(f, scannerInitBuf)
	var curAddr string
	var body strings.Builder
	haveRec := false

	flush := func() {
		if !haveRec {
			return
		}
		text := body.String()
		fn(curAddr, funcNameFromBody(text), text)
		body.Reset()
	}

	for {
		line, rerr := br.ReadString('\n')
		if len(line) > 0 {
			// Detect a record header. Match on the line without its trailing
			// newline (reFuncHeader is anchored with $ on the address).
			trimmed := strings.TrimRight(line, "\r\n")
			if m := reFuncHeader.FindStringSubmatch(trimmed); m != nil {
				flush()
				curAddr = "0x" + strings.ToLower(m[1])
				haveRec = true
			} else if haveRec {
				// Skip the decorated record-separator line ("// === name @ 0xADDR
				// (size=.. lines=..) ===") if it appears before the next bare
				// header: it embeds the address/size, which differs between
				// otherwise-identical clones and would defeat the hash.
				if strings.HasPrefix(trimmed, "// === ") {
					continue
				}
				body.WriteString(line)
			}
		}
		if rerr != nil {
			break
		}
	}
	flush()
	return nil
}

// funcNameFromBody extracts a best-effort function name from a decompiled body
// by running extractFuncName over the first non-empty, non-comment line (the
// signature). Returns "" when nothing name-like is found. It walks lines without
// strings.Split, so it allocates no per-body line slice (×500k bodies).
func funcNameFromBody(body string) string {
	for len(body) > 0 {
		nl := strings.IndexByte(body, '\n')
		var line string
		if nl < 0 {
			line, body = body, ""
		} else {
			line, body = body[:nl], body[nl+1:]
		}
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, "//") {
			continue
		}
		// First meaningful line is the signature; deeper lines are body, not sig.
		return extractFuncName(s, "")
	}
	return ""
}

// hashBody writes a clone-canonical form of body into h WITHOUT allocating: it
// streams the bytes and, whenever it meets a Ghidra address-bearing auto-symbol
// (FUN_/DAT_/LAB_/UNK_/SUB_ followed by hex), it emits the literal "<SYM>" in
// place of the whole token. Two templated instantiations that differ ONLY in
// those embedded addresses therefore hash equal, while functions that differ in
// any real name/literal/structure stay distinct. This replaces a per-body
// regexp.ReplaceAllString (which allocated a fresh normalized string for every
// one of ~500k bodies — the dominant transient heap churn) with a zero-alloc
// scan straight into the reused hasher.
func hashBody(h io.Writer, body string) {
	const symRepl = "<SYM>"
	i := 0
	n := len(body)
	for i < n {
		// Fast path: copy a run of bytes up to the next potential symbol start.
		start := i
		for i < n && !isAddrSymStart(body, i) {
			i++
		}
		if i > start {
			io.WriteString(h, body[start:i])
		}
		if i >= n {
			break
		}
		// At a symbol start: skip the 4-char prefix + the hex run, emit <SYM>.
		i += 4 // prefix length (FUN_/DAT_/LAB_/UNK_/SUB_ are all 4 bytes)
		for i < n && isHexByte(body[i]) {
			i++
		}
		io.WriteString(h, symRepl)
	}
}

// isAddrSymStart reports whether a Ghidra address-symbol token begins at body[i]:
// one of the 4-char prefixes (FUN_/DAT_/LAB_/UNK_/SUB_) at a word boundary,
// immediately followed by at least one hex digit. The word-boundary check (prev
// byte not an identifier byte) mirrors the \b in the old regexp so we don't
// rewrite a prefix embedded mid-identifier.
func isAddrSymStart(s string, i int) bool {
	if i+5 > len(s) { // need 4 prefix bytes + >=1 hex
		return false
	}
	if s[i+3] != '_' {
		return false
	}
	switch s[i : i+3] {
	case "FUN", "DAT", "LAB", "UNK", "SUB":
	default:
		return false
	}
	if !isHexByte(s[i+4]) {
		return false
	}
	// Word boundary: the byte before the prefix must not be an identifier byte.
	if i > 0 && isIdentByte(s[i-1]) {
		return false
	}
	return true
}

// isHexByte reports whether c is an ASCII hex digit.
func isHexByte(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// cloneStr returns a copy of s backed by its own minimal allocation. Used before
// storing a body-substring (a function name) in the long-lived accumulator map,
// so the small name doesn't pin the entire decompiled body's backing array —
// which is what kept ~440k bodies resident and drove the dedupe heap peak.
func cloneStr(s string) string {
	return strings.Clone(s)
}

// gameViewResult summarizes the filtered companion outputs.
type gameViewResult struct {
	StringRefsKept    int
	StringRefsDropped int
	CallersKept       int
	CallersDropped    int
}

// writeGameViews emits *.game.csv companions of string_refs.csv and callers.csv
// that exclude rows owned by engine-boilerplate classes, so an LLM sees game
// logic first. The keyed column (string_refs: the function; callers: the caller)
// is classified via its C++ owning class (cppClassOwner + isBoilerplateClass);
// anonymous FUN_/unqualified functions are KEPT (conservative: we only drop what
// we can positively identify as engine). Streaming, additive, never deletes.
func writeGameViews(srcDir string) (gameViewResult, error) {
	var res gameViewResult
	idxDir := filepath.Join(srcDir, "indexes")

	if p := filepath.Join(idxDir, "string_refs.csv"); fileExists(p) {
		kept, dropped, err := filterCSVByFirstColClass(p, filepath.Join(idxDir, "string_refs.game.csv"))
		if err != nil {
			return res, err
		}
		res.StringRefsKept, res.StringRefsDropped = kept, dropped
	}

	if p := filepath.Join(idxDir, "callers.csv"); fileExists(p) {
		kept, dropped, err := filterCSVByFirstColClass(p, filepath.Join(idxDir, "callers.game.csv"))
		if err != nil {
			return res, err
		}
		res.CallersKept, res.CallersDropped = kept, dropped
	}

	return res, nil
}

// filterCSVByFirstColClass streams a CSV, classifies each data row by the C++
// owning class of its FIRST column, and writes only non-engine rows to out. The
// header row is preserved. Returns (kept, dropped). A row whose first column has
// no identifiable owning class (anonymous/unqualified) is KEPT. Memory-safe:
// row-at-a-time, no buffering of the whole file.
func filterCSVByFirstColClass(inPath, outPath string) (kept, dropped int, err error) {
	in, err := os.Open(inPath)
	if err != nil {
		return 0, 0, err
	}
	defer in.Close()

	out, err := os.Create(outPath)
	if err != nil {
		return 0, 0, err
	}
	outBuf := bufio.NewWriterSize(out, 64*1024)
	w := csv.NewWriter(outBuf)
	defer func() {
		w.Flush()
		outBuf.Flush()
		out.Close()
	}()

	r := csv.NewReader(in)
	r.FieldsPerRecord = -1
	r.ReuseRecord = true

	header := true
	for {
		rec, rerr := r.Read()
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return kept, dropped, rerr
		}
		if header {
			header = false
			// Copy the header through verbatim.
			if err := w.Write(rec); err != nil {
				return kept, dropped, err
			}
			continue
		}
		if len(rec) == 0 {
			continue
		}
		if isEngineFunction(rec[0]) {
			dropped++
			continue
		}
		// csv.Write needs a copy because ReuseRecord reuses the backing slice.
		row := make([]string, len(rec))
		copy(row, rec)
		if err := w.Write(row); err != nil {
			return kept, dropped, err
		}
		kept++
	}
	return kept, dropped, nil
}

// isEngineFunction reports whether a (possibly C++-qualified) function name
// belongs to an engine-boilerplate class. Unqualified or anonymous names return
// false (kept) — we only positively drop identified engine code.
func isEngineFunction(fn string) bool {
	owner := cppClassOwner(fn)
	if owner == "" {
		return false
	}
	return isBoilerplateClass(owner)
}

// runDedupeAndGameViews runs both U4 cleanup passes over srcDir, logging an
// honest summary of exactly what was collapsed/dropped. Both passes are
// logged-not-fatal so a cleanup hiccup never regresses the index build. Shared
// by the UE5 and native recipes for parity.
func runDedupeAndGameViews(srcDir string, log func(string)) {
	if dr, err := dedupeFunctionBodies(srcDir); err != nil {
		if log != nil {
			log("Function dedupe skipped (non-fatal): " + err.Error())
		}
	} else if dr.TotalFunctions > 0 && log != nil {
		log(fmt.Sprintf("Collapsed %d duplicate function bodies into %d canonical (%d groups) of %d total -> indexes/duplicates.json (originals kept)",
			dr.DuplicateFunctions, dr.UniqueFunctions, dr.DuplicateGroups, dr.TotalFunctions))
		if dr.BudgetExceeded {
			log(fmt.Sprintf("Function dedupe: corpus exceeded the %d-unique memory budget — dedupe is PARTIAL (no data lost, originals intact)", usmapDedupeMaxUnique))
		}
	}
	if gv, err := writeGameViews(srcDir); err != nil {
		if log != nil {
			log("Game-only views skipped (non-fatal): " + err.Error())
		}
	} else if log != nil && (gv.StringRefsKept+gv.StringRefsDropped+gv.CallersKept+gv.CallersDropped) > 0 {
		log(fmt.Sprintf("Game-only views: string_refs kept %d / dropped %d engine; callers kept %d / dropped %d engine -> indexes/*.game.csv",
			gv.StringRefsKept, gv.StringRefsDropped, gv.CallersKept, gv.CallersDropped))
	}
}

// writeJSONIndent marshals v with indentation and writes it to path.
func writeJSONIndent(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
