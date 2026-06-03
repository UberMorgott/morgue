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
	"regexp"
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

// reAddrSymbol matches Ghidra auto-generated address-bearing symbols
// (FUN_140.., DAT_15.., LAB_.., UNK_.., SUB_..). Their embedded address makes two
// otherwise-identical templated clones differ only in these tokens, so we
// canonicalize them to a placeholder before hashing. This is what turns "exact
// duplicate" into "structurally identical clone", the case that actually matters
// for templated TArray<T>/TMap<K,V> instantiations.
var reAddrSymbol = regexp.MustCompile(`\b(?:FUN_|DAT_|LAB_|UNK_|SUB_)[0-9A-Fa-f]+\b`)

// dupExampleCap bounds how many duplicate members are listed per group in
// duplicates.json. The full count is always reported; only the explicit example
// list is capped to keep the report (and RAM) bounded under pathological clones.
const dupExampleCap = 64

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

	for _, fp := range files {
		if err := scanSplitRecords(fp, func(addr, name, body string) {
			res.TotalFunctions++
			h := fnv.New64a()
			io.WriteString(h, normalizeBody(body))
			key := h.Sum64()
			g, ok := groups[key]
			if !ok {
				groups[key] = &dupAccum{canonAddr: addr, canonName: name, count: 1}
				return
			}
			g.count++
			if len(g.dups) < dupExampleCap {
				g.dups = append(g.dups, dupMember{Address: addr, Name: name})
			} else {
				g.truncated = true
			}
		}); err != nil {
			return res, err
		}
	}

	res.UniqueFunctions = len(groups)
	res.DuplicateFunctions = res.TotalFunctions - res.UniqueFunctions

	// Build the report (only groups with >1 member).
	report := duplicatesReport{
		TotalFunctions:     res.TotalFunctions,
		UniqueFunctions:    res.UniqueFunctions,
		DuplicateFunctions: res.DuplicateFunctions,
		Note: "Functions whose decompiled bodies are identical after canonicalizing " +
			"Ghidra address-symbols (FUN_/DAT_/LAB_/UNK_/SUB_<hex> -> <SYM>) and " +
			"excluding the address header are collapsed here — these are dominated by " +
			"templated TArray/TMap instantiations. Originals remain on disk; this is a " +
			"map from a canonical function to its structural clones. Nothing was " +
			"deleted or truncated from the source.",
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
// signature). Returns "" when nothing name-like is found.
func funcNameFromBody(body string) string {
	for _, line := range strings.Split(body, "\n") {
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, "//") {
			continue
		}
		if name := extractFuncName(s, ""); name != "" {
			return name
		}
		// Only inspect the first meaningful line; deeper lines are body, not sig.
		return ""
	}
	return ""
}

// normalizeBody canonicalizes a decompiled body for clone detection: it rewrites
// Ghidra address-bearing auto-symbols (FUN_/DAT_/LAB_/UNK_/SUB_<hex>) to a single
// "<SYM>" token so two templated instantiations that differ ONLY in those
// embedded addresses hash equal. It deliberately does NOT touch real names,
// literals, or structure: functions that reference different NAMED symbols, or
// have any other textual difference, stay distinct. This keeps the collapse
// conservative (structurally-identical clones only) while catching the templated
// duplicates that dominate a UE binary.
func normalizeBody(body string) string {
	return reAddrSymbol.ReplaceAllString(body, "<SYM>")
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
