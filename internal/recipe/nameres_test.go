package recipe

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestIsQualifiedCppName: only well-formed C++ qualified identifiers (with ::)
// qualify — not paths, format strings, or bare words. Universal (compiler
// __FUNCTION__ embedding), not engine-specific.
func TestIsQualifiedCppName(t *testing.T) {
	yes := []string{"UWidget::Tick", "Engine::Sub::Method", "FArchive::operator"}
	no := []string{
		"plainword", "has space::x", "Runtime/Chaos/File.ispc:374:2",
		"%s::%s", "::leadingcolon", "Trailing::", "a::b::", "12::34",
	}
	for _, s := range yes {
		if !isQualifiedCppName(s) {
			t.Errorf("isQualifiedCppName(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if isQualifiedCppName(s) {
			t.Errorf("isQualifiedCppName(%q) = true, want false", s)
		}
	}
}

// TestResolveNameFromStrings: conservative single-candidate rule. Exactly one
// distinct qualified identifier among a function's strings → resolve to it.
// Zero or more-than-one → no resolution (never guess).
func TestResolveNameFromStrings(t *testing.T) {
	cases := []struct {
		name    string
		strs    []string
		want    string
		wantOK  bool
	}{
		{"single qualified", []string{"hello", "UFoo::Bar", "%d"}, "UFoo::Bar", true},
		{"duplicate qualified counts once", []string{"UFoo::Bar", "UFoo::Bar"}, "UFoo::Bar", true},
		{"two distinct qualified → ambiguous", []string{"UFoo::Bar", "UBaz::Qux"}, "", false},
		{"none qualified", []string{"hello", "world", "path/x.cpp"}, "", false},
		{"empty", nil, "", false},
	}
	for _, c := range cases {
		got, ok := resolveNameFromStrings(c.strs)
		if got != c.want || ok != c.wantOK {
			t.Errorf("%s: resolveNameFromStrings(%v) = (%q,%v), want (%q,%v)",
				c.name, c.strs, got, ok, c.want, c.wantOK)
		}
	}
}

// TestResolveNamesIntegration: end-to-end over synthetic indexes/ + symbols.ndjson.
// A FUN_ that references exactly one qualified-id string gets renamed in
// symbols.ndjson and recorded in name_map.csv; callers.csv intrinsic noise
// (bare lowercase, no ::) is dropped while FUN_/:: edges survive.
func TestResolveNamesIntegration(t *testing.T) {
	srcDir := t.TempDir()
	idx := filepath.Join(srcDir, "indexes")
	if err := os.MkdirAll(idx, 0755); err != nil {
		t.Fatal(err)
	}
	// symbols.ndjson: one FUN_ (resolvable), one already-named, one FUN_ (no strings).
	writeFile(t, filepath.Join(srcDir, "symbols.ndjson"),
		`{"address":"0x140001000","name":"FUN_140001000"}
{"address":"0x140002000","name":"Already::Named"}
{"address":"0x140003000","name":"FUN_140003000"}
`)
	// string_refs.csv: 140001000 references exactly one qualified id.
	writeFile(t, filepath.Join(idx, "string_refs.csv"),
		`function,address,string
FUN_140001000,0x140001000,UPlayer::Jump
FUN_140001000,0x140001000,some log line
FUN_140003000,0x140003000,ambiguousA::x
FUN_140003000,0x140003000,ambiguousB::y
`)
	// callers.csv: keep FUN_ + :: edges, drop bare intrinsic.
	writeFile(t, filepath.Join(idx, "callers.csv"),
		`caller,caller_address,callee
FUN_140001000,0x140001000,FUN_140002000
FUN_140001000,0x140001000,vinsertps_avx
FUN_140001000,0x140001000,Other::Method
`)

	st, err := resolveNames(srcDir)
	if err != nil {
		t.Fatalf("resolveNames: %v", err)
	}
	if st.Resolved != 1 {
		t.Fatalf("Resolved = %d, want 1 (only 140001000; 140003000 ambiguous)", st.Resolved)
	}

	// symbols.ndjson: 140001000 renamed, 140003000 untouched.
	syms := readSymbolsNDJSON(t, filepath.Join(srcDir, "symbols.ndjson"))
	if syms["0x140001000"] != "UPlayer::Jump" {
		t.Errorf("0x140001000 = %q, want UPlayer::Jump", syms["0x140001000"])
	}
	if syms["0x140003000"] != "FUN_140003000" {
		t.Errorf("0x140003000 = %q, want unchanged FUN_140003000", syms["0x140003000"])
	}

	// name_map.csv records the rename.
	nmRows := readCSV(t, filepath.Join(idx, "name_map.csv"))
	if !csvHasRow(nmRows, "0x140001000", "UPlayer::Jump") {
		t.Errorf("name_map.csv missing rename row; got %v", nmRows)
	}

	// callers.csv: intrinsic dropped, real edges kept.
	caRows := readCSV(t, filepath.Join(idx, "callers.csv"))
	for _, r := range caRows {
		for _, c := range r {
			if c == "vinsertps_avx" {
				t.Errorf("intrinsic vinsertps_avx not filtered from callers.csv: %v", caRows)
			}
		}
	}
	if !csvHasRow(caRows, "FUN_140001000", "FUN_140002000") {
		t.Errorf("callers.csv dropped a real FUN_ edge; got %v", caRows)
	}
	if !csvHasRow(caRows, "FUN_140001000", "Other::Method") {
		t.Errorf("callers.csv dropped a real :: edge; got %v", caRows)
	}
}

// TestResolveNamesBoundedMemory proves resolveNames is memory-safe on a large
// monolith: it streams string_refs.csv / symbols.ndjson and only retains the
// real-name set + rename map (O(named+resolved)), never an O(total-functions)
// structure. ~1% of functions resolve (realistic lift), the rest are noise.
//
// Peak HeapAlloc is sampled by a goroutine (see sampleHeap); a flat peak proves
// the pass does not scale memory with the function count.
func TestResolveNamesBoundedMemory(t *testing.T) {
	const (
		n       = 1_000_000
		heapCap = 256 << 20
	)
	srcDir := t.TempDir()
	idx := filepath.Join(srcDir, "indexes")
	if err := os.MkdirAll(idx, 0755); err != nil {
		t.Fatal(err)
	}

	// symbols.ndjson: N anonymous functions.
	symF, _ := os.Create(filepath.Join(srcDir, "symbols.ndjson"))
	symW := bufio.NewWriterSize(symF, 1<<20)
	for i := 0; i < n; i++ {
		fmt.Fprintf(symW, "{\"address\":\"0x%x\",\"name\":\"FUN_%x\"}\n", 0x140000000+i*0x20, 0x140000000+i*0x20)
	}
	symW.Flush()
	symF.Close()

	// string_refs.csv: every function references a noise string; every 100th also
	// references exactly one qualified id (so ~1% resolve).
	srF, _ := os.Create(filepath.Join(idx, "string_refs.csv"))
	srW := bufio.NewWriterSize(srF, 1<<20)
	srW.WriteString("function,address,string\n")
	for i := 0; i < n; i++ {
		addr := fmt.Sprintf("0x%x", 0x140000000+i*0x20)
		fmt.Fprintf(srW, "FUN_%x,%s,log line %d\n", 0x140000000+i*0x20, addr, i)
		if i%100 == 0 {
			fmt.Fprintf(srW, "FUN_%x,%s,Pkg::Class%d::Method\n", 0x140000000+i*0x20, addr, i)
		}
	}
	srW.Flush()
	srF.Close()

	var st resolveStats
	var rerr error
	peak := sampleHeap(func() { st, rerr = resolveNames(srcDir) })
	if rerr != nil {
		t.Fatalf("resolveNames: %v", rerr)
	}
	if st.Resolved < n/100-1 || st.Resolved > n/100+1 {
		t.Fatalf("Resolved = %d, want ~%d (1%%)", st.Resolved, n/100)
	}
	t.Logf("n=%d resolved=%d peakHeap=%d MiB", n, st.Resolved, peak>>20)
	if peak > heapCap {
		t.Fatalf("peak HeapAlloc = %d MiB exceeds bound %d MiB (resolveNames not streaming)",
			peak>>20, heapCap>>20)
	}
}

// TestIsRealCallee proves the call-graph callee filter keeps genuine references
// (FUN_<hex>, C++-qualified, known real names) and drops Ghidra jump-table
// labels (switchD_/caseD_/switchdataD_) — which contain "::" and would otherwise
// slip through. Universal (Ghidra-builtin prefixes, not addr/game specific).
func TestIsRealCallee(t *testing.T) {
	known := map[string]bool{"memcpy": true}
	keep := []string{"FUN_140002000", "Other::Method", "UPlayer::Jump", "memcpy"}
	drop := []string{
		"switchD_143a2fb85::caseD_6", "switchD_1415231f1", "caseD_6",
		"switchdataD_14257bf8e", "vinsertps_avx",
	}
	for _, c := range keep {
		if !isRealCallee(c, known) {
			t.Errorf("isRealCallee(%q) = false, want true", c)
		}
	}
	for _, c := range drop {
		if isRealCallee(c, known) {
			t.Errorf("isRealCallee(%q) = true, want false", c)
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readSymbolsNDJSON(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e symbolEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("bad ndjson line %q: %v", line, err)
		}
		out[e.Address] = e.Name
	}
	return out
}
