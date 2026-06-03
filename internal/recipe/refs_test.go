package recipe

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExtractRefs unit-tests the string-literal + call-site extraction on a
// synthetic decompiled function body with known content.
func TestExtractRefs(t *testing.T) {
	raw := `// 140001000
void Engine::Widget::Tick(int param_1)

{
  puts("hello world");
  FUN_140002000(param_1);
  iVar1 = Other::Helper(param_1,"second string");
  if (param_1 != 0) {
    FUN_140002000(0);
  }
  return;
}
`
	strs, callees := extractRefs(raw, "Engine::Widget::Tick")

	wantStrs := map[string]bool{"hello world": true, "second string": true}
	if len(strs) != len(wantStrs) {
		t.Fatalf("strings = %v, want keys %v", strs, wantStrs)
	}
	for _, s := range strs {
		if !wantStrs[s] {
			t.Errorf("unexpected string %q", s)
		}
	}

	// Callees: FUN_140002000 (deduped to one), Other::Helper. NOT puts? puts is a
	// real call too — accept it. Must NOT include keywords (if/return) or self.
	cset := map[string]bool{}
	for _, c := range callees {
		cset[c] = true
	}
	if !cset["FUN_140002000"] {
		t.Errorf("missing callee FUN_140002000; got %v", callees)
	}
	if !cset["Other::Helper"] {
		t.Errorf("missing callee Other::Helper; got %v", callees)
	}
	if cset["if"] || cset["return"] {
		t.Errorf("control keyword leaked into callees: %v", callees)
	}
	if cset["Engine::Widget::Tick"] {
		t.Errorf("self (signature) leaked into callees: %v", callees)
	}
	// FUN_140002000 appears twice in body but must be deduped to one row.
	dupCount := 0
	for _, c := range callees {
		if c == "FUN_140002000" {
			dupCount++
		}
	}
	if dupCount != 1 {
		t.Errorf("FUN_140002000 not deduped: appears %d times", dupCount)
	}
}

// TestExtractRefsFiltersGhidraPseudoOps proves the caller->callee graph excludes
// Ghidra synthetic pseudo-ops (CONCAT/ZEXT/SEXT/SUB + digits) and primitive type
// casts, which are call-syntax noise, not real function references. Universal:
// these are Ghidra builtins, not arch- or game-specific.
func TestExtractRefsFiltersGhidraPseudoOps(t *testing.T) {
	raw := `// 140005000
void FUN_140005000(void)
{
  uVar1 = ZEXT416(param_1);
  uVar2 = CONCAT44(a,b);
  uVar3 = SUB164(x,0);
  uVar4 = undefined1(y);
  iVar5 = int(z);
  RealClass::RealMethod(uVar1);
  FUN_140006000();
}
`
	_, callees := extractRefs(raw, "FUN_140005000")
	cset := map[string]bool{}
	for _, c := range callees {
		cset[c] = true
	}
	for _, noise := range []string{"ZEXT416", "CONCAT44", "SUB164", "undefined1", "int"} {
		if cset[noise] {
			t.Errorf("Ghidra pseudo-op %q leaked into callees: %v", noise, callees)
		}
	}
	if !cset["RealClass::RealMethod"] {
		t.Errorf("real qualified call dropped; got %v", callees)
	}
	if !cset["FUN_140006000"] {
		t.Errorf("real FUN_ call dropped; got %v", callees)
	}
}

// TestSplitEmitsIndexCSVs is the integration test: splitAndIndexDecompiledC must
// produce indexes/string_refs.csv (function→string) and indexes/callers.csv
// (caller→callee) with rows for the synthetic functions.
func TestSplitEmitsIndexCSVs(t *testing.T) {
	srcDir := t.TempDir()
	binPath := filepath.Join(srcDir, "game.exe")
	combined := filepath.Join(srcDir, "game.c")
	body := `// Decompiled by Ghidra via Morgue
// Binary: game.exe
// Architecture: x86:LE:64:default
// 140001000
void Engine::Widget::Tick(void)
{
  puts("alpha");
  FUN_140002000();
}
// 140002000
void FUN_140002000(void)
{
  iVar1 = Other::Helper("beta");
}
`
	if err := os.WriteFile(combined, []byte(body), 0644); err != nil {
		t.Fatalf("write combined: %v", err)
	}

	if _, err := splitAndIndexDecompiledC(srcDir, binPath); err != nil {
		t.Fatalf("split: %v", err)
	}

	// string_refs.csv
	srRows := readCSV(t, filepath.Join(srcDir, "indexes", "string_refs.csv"))
	if !csvHasRow(srRows, "Engine::Widget::Tick", "alpha") {
		t.Errorf("string_refs missing (Engine::Widget::Tick, alpha); got %v", srRows)
	}
	if !csvHasRow(srRows, "FUN_140002000", "beta") {
		t.Errorf("string_refs missing (FUN_140002000, beta); got %v", srRows)
	}

	// callers.csv
	caRows := readCSV(t, filepath.Join(srcDir, "indexes", "callers.csv"))
	if !csvHasRow(caRows, "Engine::Widget::Tick", "FUN_140002000") {
		t.Errorf("callers missing (Engine::Widget::Tick -> FUN_140002000); got %v", caRows)
	}
	if !csvHasRow(caRows, "FUN_140002000", "Other::Helper") {
		t.Errorf("callers missing (FUN_140002000 -> Other::Helper); got %v", caRows)
	}
}

// readCSV reads a CSV file and returns all data rows (header skipped).
func readCSV(t *testing.T, path string) [][]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	all, err := r.ReadAll()
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if len(all) == 0 {
		return nil
	}
	return all[1:] // skip header
}

// csvHasRow reports whether any row contains col0==a and any later col==b.
func csvHasRow(rows [][]string, a, b string) bool {
	for _, r := range rows {
		if len(r) >= 2 && r[0] == a {
			for _, c := range r[1:] {
				if c == b {
					return true
				}
			}
		}
	}
	return false
}

var _ = strings.TrimSpace
