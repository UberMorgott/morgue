package recipe

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// syntheticCombinedC builds a combined Ghidra .c like MorgueExport.java emits:
// a banner header, then per-function records of `// <addr>` + signature + body
// + blank line, and a final `// Total:` summary. It mixes a named function, an
// anonymous FUN_ function, and a C++ method (A::B::method).
const syntheticCombinedC = `// Decompiled by Ghidra via Morgue
// Binary: C:\\x\\wur.exe
// Architecture: x86:LE:64:default

// 00401000
void NamedFunc(int param_1)

{
  return;
}

// 00401100
undefined8 FUN_00401100(void)

{
  return 0;
}

// 00408abc
void __thiscall A::B::method(A::B *this,int param_1)

{
  this->field = param_1;
  return;
}

// 0050ffff
int TArray<int>::Add(int param_1)

{
  return param_1;
}

// Total: 4 functions decompiled, 0 errors
`

func TestSplitDecompiledC(t *testing.T) {
	srcDir := t.TempDir()
	combined := filepath.Join(srcDir, "wur.c")
	if err := os.WriteFile(combined, []byte(syntheticCombinedC), 0644); err != nil {
		t.Fatal(err)
	}

	// binaryPath must use the OS-native separator: splitAndIndexDecompiledC
	// derives the combined-.c base name via filepath.Base, which only treats the
	// running OS's separator as a path delimiter. A hard-coded Windows path
	// (C:\x\wur.exe) makes filepath.Base a no-op on Linux, so the base name comes
	// out as "C:\x\wur", the "wur.c" lookup misses, and the splitter returns a nil
	// result (the CI-only failure). Build the path with filepath.Join so the test
	// is hermetic on every platform; production always passes native paths.
	res, err := splitAndIndexDecompiledC(srcDir, filepath.Join(`C:\x`, "wur.exe"))
	if err != nil {
		t.Fatalf("splitAndIndexDecompiledC: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}

	// Function count: 4 records.
	if res.FunctionCount != 4 {
		t.Errorf("FunctionCount = %d, want 4", res.FunctionCount)
	}
	// Named: NamedFunc, A::B::method, TArray<int>::Add = 3; anonymous: FUN_ = 1.
	if res.NamedCount != 3 {
		t.Errorf("NamedCount = %d, want 3", res.NamedCount)
	}
	if res.AnonymousCount != 1 {
		t.Errorf("AnonymousCount = %d, want 1", res.AnonymousCount)
	}

	// functions/ tree created with split files, each well under 2MB.
	funcsDir := filepath.Join(srcDir, "functions")
	var splitFiles []string
	filepath.WalkDir(funcsDir, func(path string, d os.DirEntry, werr error) error {
		if werr != nil || d.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".c") {
			splitFiles = append(splitFiles, path)
			info, _ := d.Info()
			if info != nil && info.Size() >= 2_000_000 {
				t.Errorf("split file %s is %d bytes (>=2MB)", path, info.Size())
			}
		}
		return nil
	})
	if len(splitFiles) == 0 {
		t.Fatal("no split .c files created under functions/")
	}

	// functions.ndjson: one JSON object per line, count == FunctionCount.
	ndjsonPath := filepath.Join(srcDir, "functions.ndjson")
	nf, err := os.Open(ndjsonPath)
	if err != nil {
		t.Fatalf("open functions.ndjson: %v", err)
	}
	defer nf.Close()
	lines := 0
	sc := bufio.NewScanner(nf)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) == "" {
			continue
		}
		var fe funcEntry
		if err := json.Unmarshal(sc.Bytes(), &fe); err != nil {
			t.Errorf("ndjson line not valid funcEntry: %v", err)
		}
		lines++
	}
	if lines != res.FunctionCount {
		t.Errorf("functions.ndjson lines = %d, want %d", lines, res.FunctionCount)
	}

	// functions_index.json: counts match.
	fiData, err := os.ReadFile(filepath.Join(srcDir, "functions_index.json"))
	if err != nil {
		t.Fatalf("read functions_index.json: %v", err)
	}
	var fi functionsIndex
	if err := json.Unmarshal(fiData, &fi); err != nil {
		t.Fatalf("functions_index.json invalid: %v", err)
	}
	if fi.FunctionCount != 4 || fi.NamedCount != 3 || fi.AnonymousCount != 1 {
		t.Errorf("functions_index counts = (%d,%d,%d), want (4,3,1)",
			fi.FunctionCount, fi.NamedCount, fi.AnonymousCount)
	}
	if fi.FunctionsNDJSON != "functions.ndjson" {
		t.Errorf("functions_ndjson = %q", fi.FunctionsNDJSON)
	}

	// symbols.json: a small summary (counts + classes + ndjson pointer). The full
	// address->name map is streamed to symbols.ndjson, NOT inlined here (memory
	// safety). The summary must NOT carry the giant map.
	smData, err := os.ReadFile(filepath.Join(srcDir, "symbols.json"))
	if err != nil {
		t.Fatalf("read symbols.json: %v", err)
	}
	var sm symbolMap
	if err := json.Unmarshal(smData, &sm); err != nil {
		t.Fatalf("symbols.json invalid: %v", err)
	}
	if len(sm.Symbols) != 0 {
		t.Errorf("symbols.json must not inline the address map; got %d entries", len(sm.Symbols))
	}
	if sm.SymbolsNDJSON != "symbols.ndjson" {
		t.Errorf("symbols.json symbols_ndjson = %q, want symbols.ndjson", sm.SymbolsNDJSON)
	}

	// symbols.ndjson: one {address,name} per line; addresses present, names right.
	syms := map[string]string{}
	sf, err := os.Open(filepath.Join(srcDir, "symbols.ndjson"))
	if err != nil {
		t.Fatalf("open symbols.ndjson: %v", err)
	}
	defer sf.Close()
	ssc := bufio.NewScanner(sf)
	ssc.Buffer(make([]byte, 0, 1<<20), 1<<20)
	for ssc.Scan() {
		if strings.TrimSpace(ssc.Text()) == "" {
			continue
		}
		var se symbolEntry
		if err := json.Unmarshal(ssc.Bytes(), &se); err != nil {
			t.Errorf("symbols.ndjson line not valid symbolEntry: %v", err)
			continue
		}
		syms[se.Address] = se.Name
	}
	for _, addr := range []string{"0x00401000", "0x00401100", "0x00408abc", "0x0050ffff"} {
		if _, ok := syms[addr]; !ok {
			t.Errorf("symbols.ndjson missing address %s (have %v)", addr, syms)
		}
	}
	if got := syms["0x00401000"]; got != "NamedFunc" {
		t.Errorf("0x00401000 name = %q, want NamedFunc", got)
	}
	// Classes: A::B recovered; TArray<int> recovered (Add). FUN_ has no class.
	classSet := map[string]bool{}
	for _, c := range sm.Classes {
		classSet[c] = true
	}
	if !classSet["A::B"] {
		t.Errorf("classes missing A::B; got %v", sm.Classes)
	}
	if !classSet["TArray<int>"] {
		t.Errorf("classes missing TArray<int>; got %v", sm.Classes)
	}

	// Combined .c must be left unchanged.
	if !fileExists(combined) {
		t.Error("combined .c was removed; it must be preserved")
	}
}

func TestExtractFuncName(t *testing.T) {
	cases := []struct {
		sig, want string
	}{
		{"void NamedFunc(int param_1)", "NamedFunc"},
		{"undefined8 FUN_00401100(void)", "FUN_00401100"},
		{"void __thiscall A::B::method(A::B *this,int p)", "A::B::method"},
		{"int TArray<int>::Add(int p)", "TArray<int>::Add"},
		{"void *getptr(void)", "getptr"},
		{"int noparen", ""},
		// Ghidra pointer-to-array return: the real name (FUN_x) is AFTER the
		// "(*) [N]" return-type decoration; the return type must NOT be taken
		// as the name. Regression for the hookable.json "undefined1" garbage.
		{"undefined1 (*) [16]FUN_14000f3c0(longlong param_1,uint param_7)", "FUN_14000f3c0"},
		{"undefined1 (*) [32]Foo::Bar(longlong p)", "Foo::Bar"},
		// A bare primitive/undefined type with params is not a real name.
		{"undefined1(longlong param_1)", ""},
	}
	for _, c := range cases {
		got := extractFuncName(c.sig, "deadbeef")
		if got != c.want {
			t.Errorf("extractFuncName(%q) = %q, want %q", c.sig, got, c.want)
		}
	}
}

func TestCppClassOwner(t *testing.T) {
	cases := []struct {
		name, want string
	}{
		{"A::B::method", "A::B"},
		{"TArray<int>::Add", "TArray<int>"},
		{"plainfunc", ""},
		{"Foo::bar", "Foo"},
		{"NS::TMap<int,FString>::Find", "NS::TMap<int,FString>"},
	}
	for _, c := range cases {
		got := cppClassOwner(c.name)
		if got != c.want {
			t.Errorf("cppClassOwner(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestSplitDecompiledCSegmentPrefix(t *testing.T) {
	// Tolerate a memory-space prefix on the entry-point comment (e.g. "ram:").
	const withPrefix = `// Decompiled by Ghidra via Morgue

// ram:00401000
void OnlyFunc(void)

{
  return;
}

// Total: 1 functions decompiled, 0 errors
`
	srcDir := t.TempDir()
	combined := filepath.Join(srcDir, "seg.c")
	if err := os.WriteFile(combined, []byte(withPrefix), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := splitAndIndexDecompiledC(srcDir, "seg.bin")
	if err != nil {
		t.Fatal(err)
	}
	if res.FunctionCount != 1 {
		t.Errorf("FunctionCount = %d, want 1 (segment-prefixed address)", res.FunctionCount)
	}
}

func TestSplitMissingCombined(t *testing.T) {
	srcDir := t.TempDir()
	res, err := splitAndIndexDecompiledC(srcDir, "nope.exe")
	if err != nil {
		t.Errorf("expected nil error for missing combined .c, got %v", err)
	}
	if res != nil {
		t.Errorf("expected nil result for missing combined .c, got %+v", res)
	}
}
