package recipe

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSplitFile is a tiny helper to lay down a functions/ bucket file in the
// same record format the splitter emits: each function is "// <addr>\n<body>".
func writeSplitFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestDedupeFunctionBodies(t *testing.T) {
	srcDir := t.TempDir()
	fnDir := filepath.Join(srcDir, "functions")

	// Two functions with IDENTICAL bodies (different addresses + names) — a
	// templated-instantiation duplicate. One unique function. Neither the bare
	// "// <addr>" header NOR the decorated "// === ... ===" record separator
	// (which carries the address/size) may count toward the body hash, else
	// nothing would ever match. We use the real split format the splitter emits.
	rec := func(addr, body string) string {
		return "// === f @ 0x" + addr + " (size=10 lines=5) ===\n// " + addr + "\n" + body
	}
	body := "void __thiscall foo(int *this)\n{\n  *this = 1;\n  return;\n}\n"
	bodyB := "void __thiscall bar(int *this)\n{\n  *this = 2;\n  return;\n}\n"

	writeSplitFile(t, filepath.Join(fnDir, "00.c"),
		rec("140001000", body)+
			rec("140002000", body)+ // exact duplicate of the first
			rec("140003000", bodyB)) // unique

	res, err := dedupeFunctionBodies(srcDir)
	if err != nil {
		t.Fatalf("dedupeFunctionBodies: %v", err)
	}
	if res.TotalFunctions != 3 {
		t.Errorf("TotalFunctions = %d, want 3", res.TotalFunctions)
	}
	if res.UniqueFunctions != 2 {
		t.Errorf("UniqueFunctions = %d, want 2", res.UniqueFunctions)
	}
	if res.DuplicateFunctions != 1 {
		t.Errorf("DuplicateFunctions = %d, want 1 (collapsed)", res.DuplicateFunctions)
	}
	if res.DuplicateGroups != 1 {
		t.Errorf("DuplicateGroups = %d, want 1", res.DuplicateGroups)
	}

	// duplicates.json must list the one group with its canonical + duplicate.
	data, err := os.ReadFile(filepath.Join(srcDir, "indexes", "duplicates.json"))
	if err != nil {
		t.Fatalf("read duplicates.json: %v", err)
	}
	var dj duplicatesReport
	if err := json.Unmarshal(data, &dj); err != nil {
		t.Fatalf("duplicates.json invalid: %v", err)
	}
	if len(dj.Groups) != 1 {
		t.Fatalf("Groups = %d, want 1", len(dj.Groups))
	}
	g := dj.Groups[0]
	if g.Count != 2 {
		t.Errorf("group Count = %d, want 2", g.Count)
	}
	if len(g.Duplicates) != 1 {
		t.Errorf("group Duplicates = %d, want 1", len(g.Duplicates))
	}
	// Canonical is the first-seen address.
	if g.CanonicalAddress != "0x140001000" {
		t.Errorf("CanonicalAddress = %q, want 0x140001000", g.CanonicalAddress)
	}
}

// TestDedupeFunctionBodies_SymbolNormalized verifies that two bodies differing
// ONLY in their embedded Ghidra address-symbols (FUN_/DAT_<hex>) collapse — the
// templated-clone case that dominates real binaries.
func TestDedupeFunctionBodies_SymbolNormalized(t *testing.T) {
	srcDir := t.TempDir()
	fnDir := filepath.Join(srcDir, "functions")
	rec := func(addr, body string) string {
		return "// === f @ 0x" + addr + " (size=10 lines=5) ===\n// " + addr + "\n" + body
	}
	// Same structure; only the FUN_/DAT_ addresses differ.
	bodyA := "void FUN_140001000(void)\n{\n  DAT_150000000 = 1;\n  return;\n}\n"
	bodyB := "void FUN_140002000(void)\n{\n  DAT_150000abc = 1;\n  return;\n}\n"
	// A genuinely different function (references a different NAMED symbol).
	bodyC := "void FUN_140004000(void)\n{\n  GWorld = 1;\n  return;\n}\n"

	writeSplitFile(t, filepath.Join(fnDir, "00.c"),
		rec("140001000", bodyA)+rec("140002000", bodyB)+rec("140004000", bodyC))

	res, err := dedupeFunctionBodies(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	if res.TotalFunctions != 3 {
		t.Errorf("TotalFunctions = %d, want 3", res.TotalFunctions)
	}
	// bodyA/bodyB collapse; bodyC stays unique => 2 unique, 1 collapsed.
	if res.UniqueFunctions != 2 {
		t.Errorf("UniqueFunctions = %d, want 2", res.UniqueFunctions)
	}
	if res.DuplicateFunctions != 1 {
		t.Errorf("DuplicateFunctions = %d, want 1", res.DuplicateFunctions)
	}
}

func TestDedupeFunctionBodies_NoFunctionsDir(t *testing.T) {
	srcDir := t.TempDir()
	res, err := dedupeFunctionBodies(srcDir)
	if err != nil {
		t.Fatalf("dedupeFunctionBodies (no dir): %v", err)
	}
	if res.TotalFunctions != 0 {
		t.Errorf("TotalFunctions = %d, want 0", res.TotalFunctions)
	}
}

func TestWriteGameViews(t *testing.T) {
	srcDir := t.TempDir()
	idxDir := filepath.Join(srcDir, "indexes")
	if err := os.MkdirAll(idxDir, 0755); err != nil {
		t.Fatal(err)
	}

	// string_refs.csv: function,address,string. A game class + an engine class.
	stringRefs := "function,address,string\n" +
		"AMyGameChar::Tick,0x1000,\"hello\"\n" +
		"UObject::PostLoad,0x2000,\"engine\"\n" +
		"FUN_140001234,0x3000,\"anon\"\n"
	if err := os.WriteFile(filepath.Join(idxDir, "string_refs.csv"), []byte(stringRefs), 0644); err != nil {
		t.Fatal(err)
	}

	// callers.csv: caller,callee (engine callee filtered).
	callers := "caller,callee\n" +
		"AMyGameChar::Tick,AMyGameChar::Move\n" +
		"UObject::PostLoad,UClass::GetName\n"
	if err := os.WriteFile(filepath.Join(idxDir, "callers.csv"), []byte(callers), 0644); err != nil {
		t.Fatal(err)
	}

	res, err := writeGameViews(srcDir)
	if err != nil {
		t.Fatalf("writeGameViews: %v", err)
	}

	// string_refs.game.csv must keep the game class, drop the engine class row.
	sg, err := os.ReadFile(filepath.Join(idxDir, "string_refs.game.csv"))
	if err != nil {
		t.Fatalf("read string_refs.game.csv: %v", err)
	}
	sgs := string(sg)
	if !strings.Contains(sgs, "AMyGameChar::Tick") {
		t.Error("string_refs.game.csv should keep AMyGameChar::Tick")
	}
	if strings.Contains(sgs, "UObject::PostLoad") {
		t.Error("string_refs.game.csv should drop UObject::PostLoad (engine)")
	}
	if res.StringRefsKept == 0 || res.StringRefsDropped == 0 {
		t.Errorf("expected nonzero kept(%d)/dropped(%d)", res.StringRefsKept, res.StringRefsDropped)
	}

	// callers.game.csv must drop the edge whose caller is engine.
	cg, err := os.ReadFile(filepath.Join(idxDir, "callers.game.csv"))
	if err != nil {
		t.Fatalf("read callers.game.csv: %v", err)
	}
	cgs := string(cg)
	if !strings.Contains(cgs, "AMyGameChar::Tick") {
		t.Error("callers.game.csv should keep game caller")
	}
	if strings.Contains(cgs, "UObject::PostLoad") {
		t.Error("callers.game.csv should drop engine caller")
	}
}
