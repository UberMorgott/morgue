package recipe

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// reQualifiedCpp matches a C++ qualified identifier with at least one "::"
// segment and no leading/trailing/double separators. Compiler __FUNCTION__ /
// __PRETTY_FUNCTION__ embedding produces these in log/assert strings, which is
// universal (a C/C++ behavior), not engine- or game-specific.
var reQualifiedCpp = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(::[A-Za-z_][A-Za-z0-9_]*)+$`)

// isQualifiedCppName reports whether s is a well-formed C++ qualified identifier
// (e.g. "UWidget::Tick") — not a path, format string, or bare word.
func isQualifiedCppName(s string) bool {
	return reQualifiedCpp.MatchString(s)
}

// resolveNameFromStrings applies the conservative single-candidate rule: if a
// function references EXACTLY ONE distinct qualified C++ identifier among its
// strings, that is a high-confidence name for the function. Zero or more than
// one → no resolution (never guess). Returns (name, true) only when confident.
func resolveNameFromStrings(strs []string) (string, bool) {
	var cand string
	seen := map[string]bool{}
	for _, s := range strs {
		if !isQualifiedCppName(s) || seen[s] {
			continue
		}
		seen[s] = true
		if len(seen) > 1 {
			return "", false // ambiguous: more than one distinct candidate
		}
		cand = s
	}
	if cand == "" {
		return "", false
	}
	return cand, true
}

// resolveStats reports the outcome of an offline name-resolution pass.
type resolveStats struct {
	Resolved int // FUN_ functions renamed from referenced strings
}

// resolveNames performs offline, conservative FUN_ -> real-name resolution over
// the streamed indexes produced by the split, and folds in the callers.csv
// intrinsic/pseudo-op cleanup. It is memory-safe: the only in-RAM structures are
// the set of real names and the rename map, both O(resolved+named) (a small
// fraction of total functions), never O(total functions). All file passes stream.
//
// It does NOT attempt address->UObject / runtime-vtable resolution: that needs
// UE4SS runtime data unavailable offline and is left to the caller's stub log.
func resolveNames(srcDir string) (resolveStats, error) {
	var st resolveStats
	symPath := filepath.Join(srcDir, "symbols.ndjson")
	strRefsPath := filepath.Join(srcDir, "indexes", "string_refs.csv")
	if !fileExists(symPath) || !fileExists(strRefsPath) {
		return st, nil // nothing to resolve against
	}

	// Pass 1: collect the set of existing real (non-anonymous) names. Bounded by
	// the named count, a small fraction of total functions.
	knownNames, err := collectRealNames(symPath)
	if err != nil {
		return st, err
	}

	// Pass 2: stream string_refs.csv (rows for one function are consecutive),
	// derive a high-confidence rename per function, write name_map.csv. The
	// rename map is keyed by address and bounded by the resolved count.
	renameByAddr, err := buildRenameMap(strRefsPath, filepath.Join(srcDir, "indexes", "name_map.csv"), knownNames)
	if err != nil {
		return st, err
	}
	st.Resolved = len(renameByAddr)

	// Pass 3: stream-apply the rename map to symbols.ndjson (temp + atomic rename).
	if st.Resolved > 0 {
		if err := applyRenamesToSymbols(symPath, renameByAddr); err != nil {
			return st, err
		}
	}

	// Pass 4: stream-filter callers.csv — keep an edge iff the callee is a real
	// reference (FUN_<hex>, C++-qualified, or a known real name); drop bare
	// intrinsics/pseudo-ops (e.g. vinsertps_avx). knownNames now includes the
	// freshly-resolved names so those survive.
	callersPath := filepath.Join(srcDir, "indexes", "callers.csv")
	if fileExists(callersPath) {
		if err := filterCallers(callersPath, knownNames); err != nil {
			return st, err
		}
	}
	return st, nil
}

// collectRealNames streams symbols.ndjson and returns the set of names that are
// not Ghidra-anonymous (FUN_/DAT_/...). O(named) memory.
func collectRealNames(symPath string) (map[string]bool, error) {
	f, err := os.Open(symPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	names := map[string]bool{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, scannerInitBuf), scannerMaxBuf)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e symbolEntry
		if json.Unmarshal(line, &e) != nil {
			continue
		}
		if e.Name != "" && !reAnonName.MatchString(e.Name) {
			names[e.Name] = true
		}
	}
	return names, sc.Err()
}

// buildRenameMap streams string_refs.csv, groups consecutive rows by address,
// and resolves a conservative name per function. It writes indexes/name_map.csv
// and returns address->newName (bounded by the resolved count). Accepted names
// are added to knownNames so duplicates are skipped (never two funcs to one name).
func buildRenameMap(strRefsPath, nameMapPath string, knownNames map[string]bool) (map[string]string, error) {
	in, err := os.Open(strRefsPath)
	if err != nil {
		return nil, err
	}
	defer in.Close()

	out, err := os.Create(nameMapPath)
	if err != nil {
		return nil, err
	}
	outBuf := bufio.NewWriterSize(out, 64*1024)
	w := csv.NewWriter(outBuf)
	defer func() {
		w.Flush()
		outBuf.Flush()
		out.Close()
	}()
	if err := w.Write([]string{"address", "old_name", "new_name"}); err != nil {
		return nil, err
	}

	renameByAddr := map[string]string{}
	r := csv.NewReader(in)
	r.FieldsPerRecord = -1
	r.ReuseRecord = true

	var curAddr, curOld string
	var curStrs []string
	flush := func() error {
		if curAddr == "" {
			return nil
		}
		defer func() { curStrs = curStrs[:0] }()
		// Only rename anonymous functions; never overwrite an existing real name.
		if !reAnonName.MatchString(curOld) {
			return nil
		}
		name, ok := resolveNameFromStrings(curStrs)
		if !ok || knownNames[name] {
			return nil // unresolved or would collide → leave anonymous
		}
		knownNames[name] = true
		renameByAddr[curAddr] = name
		return w.Write([]string{curAddr, curOld, name})
	}

	header := true
	for {
		rec, rerr := r.Read()
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return nil, rerr
		}
		if header {
			header = false
			continue // skip "function,address,string"
		}
		if len(rec) < 3 {
			continue
		}
		fn, addr, str := rec[0], rec[1], rec[2]
		if addr != curAddr {
			if err := flush(); err != nil {
				return nil, err
			}
			curAddr, curOld = addr, fn
		}
		curStrs = append(curStrs, str)
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return renameByAddr, nil
}

// applyRenamesToSymbols stream-rewrites symbols.ndjson, replacing the name of any
// entry whose address is in renameByAddr, then atomically replaces the file.
func applyRenamesToSymbols(symPath string, renameByAddr map[string]string) error {
	in, err := os.Open(symPath)
	if err != nil {
		return err
	}
	tmp := symPath + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		in.Close()
		return err
	}
	outBuf := bufio.NewWriterSize(out, 64*1024)
	enc := json.NewEncoder(outBuf)

	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, scannerInitBuf), scannerMaxBuf)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e symbolEntry
		if json.Unmarshal(line, &e) != nil {
			continue
		}
		if nn, ok := renameByAddr[e.Address]; ok {
			e.Name = nn
		}
		if encErr := enc.Encode(&e); encErr != nil {
			outBuf.Flush()
			out.Close()
			in.Close()
			return encErr
		}
	}
	if scErr := sc.Err(); scErr != nil {
		outBuf.Flush()
		out.Close()
		in.Close()
		return scErr
	}
	if err := outBuf.Flush(); err != nil {
		out.Close()
		in.Close()
		return err
	}
	out.Close()
	in.Close()
	return os.Rename(tmp, symPath)
}

// filterCallers stream-rewrites callers.csv, keeping only edges whose callee is a
// real reference: a FUN_<hex> address symbol, a C++-qualified name, or a known
// real name. Bare intrinsics/pseudo-ops are dropped. Atomic replace.
func filterCallers(callersPath string, knownNames map[string]bool) error {
	in, err := os.Open(callersPath)
	if err != nil {
		return err
	}
	tmp := callersPath + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		in.Close()
		return err
	}
	outBuf := bufio.NewWriterSize(out, 64*1024)
	w := csv.NewWriter(outBuf)

	r := csv.NewReader(in)
	r.FieldsPerRecord = -1
	r.ReuseRecord = true

	fail := func(e error) error {
		w.Flush()
		outBuf.Flush()
		out.Close()
		in.Close()
		return e
	}
	header := true
	for {
		rec, rerr := r.Read()
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return fail(rerr)
		}
		if header {
			header = false
			if err := w.Write([]string{"caller", "caller_address", "callee"}); err != nil {
				return fail(err)
			}
			continue
		}
		if len(rec) < 3 {
			continue
		}
		if !isRealCallee(rec[2], knownNames) {
			continue
		}
		if err := w.Write([]string{rec[0], rec[1], rec[2]}); err != nil {
			return fail(err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return fail(err)
	}
	if err := outBuf.Flush(); err != nil {
		return fail(err)
	}
	out.Close()
	in.Close()
	return os.Rename(tmp, callersPath)
}

// isRealCallee reports whether callee is a genuine intra-binary reference worth
// keeping in the call graph: an address-derived FUN_ symbol, a C++-qualified
// name, or a known real (resolved/named) function. Bare intrinsics/pseudo-ops
// (no "::", not FUN_, not a known name) are excluded.
func isRealCallee(callee string, knownNames map[string]bool) bool {
	// Ghidra jump-table labels (switchD_<addr>::caseD_N, switchdataD_<addr>) are
	// not function references. They contain "::", so they must be rejected before
	// the qualified-name check below would let them through. Ghidra-universal.
	if strings.HasPrefix(callee, "switchD_") ||
		strings.HasPrefix(callee, "switchdataD_") ||
		strings.HasPrefix(callee, "caseD_") {
		return false
	}
	if strings.HasPrefix(callee, "FUN_") {
		return true
	}
	if strings.Contains(callee, "::") {
		return true
	}
	return knownNames[callee]
}
