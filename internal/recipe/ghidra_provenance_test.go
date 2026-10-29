package recipe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/UberMorgott/morgue/internal/provenance"
	"github.com/UberMorgott/morgue/internal/runmodel"
)

type memSink struct{ recs []runmodel.ProvenanceRecord }

func (m *memSink) Write(r runmodel.ProvenanceRecord) error { m.recs = append(m.recs, r); return nil }

func TestGhidraProvenanceStreamsFunctionsAndRenames(t *testing.T) {
	out := t.TempDir()
	src := filepath.Join(out, "src")
	write := func(rel, content string) {
		p := filepath.Join(src, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("game.c", "// PSEUDOCODE\n")
	write(ghidraStatusFile, strings.Join([]string{
		`{"kind":"program","ghidraVersion":"12.2","imageBase":"0x140000000","pdbLoaded":false,"functionCount":4}`,
		`{"address":"0x140001000","name":"entry","source":"IMPORTED","thunk":false,"status":"decompiled","warnings":0,"undefinedReturn":false,"message":""}`,
		`{"address":"0x140001010","name":"FUN_140001010","source":"DEFAULT","thunk":false,"status":"decompiled","warnings":0,"undefinedReturn":true,"message":""}`,
		`{"address":"0x140001020","name":"GetLastError","source":"IMPORTED","thunk":true,"status":"decompiled","warnings":0,"undefinedReturn":false,"message":""}`,
		`{"address":"0x140001030","name":"FUN_140001030","source":"DEFAULT","thunk":false,"status":"failed","warnings":0,"undefinedReturn":false,"message":"timeout"}`,
		`{"kind":"summary","found":4,"decompiled":3,"errors":1}`,
	}, "\n")+"\n")
	write("functions.ndjson", strings.Join([]string{
		`{"name":"entry","address":"0x140001000","is_named":true,"file":"functions/001/001_00.c"}`,
		`{"name":"FUN_140001010","address":"0x140001010","is_named":false,"file":"functions/001/001_00.c"}`,
		`{"name":"GetLastError","address":"0x140001020","is_named":true,"file":"functions/001/001_01.c"}`,
	}, "\n")+"\n")
	write("indexes/name_map.csv", "address,old_name,new_name\n0x140001010,FUN_140001010,UWidget::Tick\n")

	sink := &memSink{}
	n, err := ghidraProvenance(sink, out, src, `C:\games\game.exe`, "fallback", false)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(sink.recs) {
		t.Fatalf("count %d != records %d", n, len(sink.recs))
	}
	byID := map[string]runmodel.ProvenanceRecord{}
	for _, r := range sink.recs {
		byID[r.ID] = r
		if r.SchemaVersion != 1 || r.Module != "game.exe" || r.Evidence == nil {
			t.Fatalf("envelope: %+v", r)
		}
	}
	fn := byID["game.exe:fn:0x140001010"]
	if fn.ArtifactPath != "src/functions/001/001_00.c" || fn.ArtifactKind != runmodel.FidelityPseudocode ||
		fn.RVA == nil || *fn.RVA != "0x1010" || fn.OriginalName != nil || fn.NameSource != "unknown" ||
		fn.Producer.Version != "12.2" || fn.Producer.StepID != "native.decompile" {
		t.Fatalf("default-name function record: %+v", fn)
	}
	// Not in a PE export table (no such file) and no PDB: a binary symbol, not proven export.
	if e := byID["game.exe:fn:0x140001000"]; e.OriginalName == nil || *e.OriginalName != "entry" ||
		e.NameSource != "debug-symbol" || e.Confidence != "medium" {
		t.Fatalf("imported name: %+v", e)
	}
	if th := byID["game.exe:fn:0x140001020"]; th.NameSource != "metadata" || th.ArtifactPath != "src/functions/001/001_01.c" {
		t.Fatalf("import thunk: %+v", th)
	}
	if f := byID["game.exe:fn:0x140001030"]; f.ArtifactPath != "src/game.c" || !strings.Contains(f.Evidence[0].Detail, "timeout") {
		t.Fatalf("failed function must point at combined .c with reason: %+v", f)
	}
	rn := byID["game.exe:rename:0x140001010"]
	if rn.RecoveredName == nil || *rn.RecoveredName != "UWidget::Tick" || rn.NameSource != "single-string-candidate" ||
		rn.Confidence != "medium" || rn.OriginalName != nil || rn.Producer.StepID != "native.rename" {
		t.Fatalf("rename record: %+v", rn)
	}
	if a := byID["game.exe:artifact:src/game.c"]; a.ArtifactKind != runmodel.FidelityPseudocode {
		t.Fatalf("combined artifact: %+v", a)
	}
	if _, err := ghidraProvenance(nil, out, src, "game.exe", "", false); err != nil {
		t.Fatalf("nil sink must be a no-op: %v", err)
	}
}

func TestSplitFilesCarryPseudocodeBanner(t *testing.T) {
	srcDir := t.TempDir()
	c := "// Decompiled by Ghidra via Morgue\n\n// 140001000\nint main(void)\n{\n  return 0;\n}\n\n// Total: 1 functions decompiled, 0 errors\n"
	if err := os.WriteFile(filepath.Join(srcDir, "game.c"), []byte(c), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := splitAndIndexDecompiledC(srcDir, "game.exe"); err != nil {
		t.Fatal(err)
	}
	var found bool
	_ = filepath.WalkDir(filepath.Join(srcDir, "functions"), func(p string, d os.DirEntry, _ error) error {
		if d != nil && !d.IsDir() {
			b, _ := os.ReadFile(p)
			if !strings.HasPrefix(string(b), "// PSEUDOCODE:") {
				t.Errorf("%s lacks banner: %q", p, string(b[:min(len(b), 80)]))
			}
			found = true
		}
		return nil
	})
	if !found {
		t.Fatal("no split files")
	}
}

// TestGhidraProvenanceRealOutput writes provenance.ndjson for a real target
// output until the engine binds Context.Provenance:
// MORGUE_PROV_DIR=<target out> MORGUE_PROV_BINARY=<module> go test -run TestGhidraProvenanceRealOutput ./internal/recipe
func TestGhidraProvenanceRealOutput(t *testing.T) {
	dir, bin := os.Getenv("MORGUE_PROV_DIR"), os.Getenv("MORGUE_PROV_BINARY")
	if dir == "" || bin == "" {
		t.Skip("MORGUE_PROV_DIR/MORGUE_PROV_BINARY not set")
	}
	w, err := provenance.Open(filepath.Join(dir, "provenance.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	n, err := ghidraProvenance(w, dir, filepath.Join(dir, "src"), bin, "", os.Getenv("MORGUE_PROV_NATIVEAOT") != "")
	if cerr := w.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %d provenance records", n)
}
