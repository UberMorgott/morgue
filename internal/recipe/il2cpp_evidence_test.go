package recipe

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/UberMorgott/morgue/internal/quality"
	"github.com/UberMorgott/morgue/internal/runmodel"
	"github.com/UberMorgott/morgue/internal/util"
)

type unityMemSink struct{ rows []runmodel.ProvenanceRecord }

func (s *unityMemSink) Write(r runmodel.ProvenanceRecord) error {
	s.rows = append(s.rows, r)
	return nil
}

func TestIL2CPPProvenanceCarriesTokenAndRVA(t *testing.T) {
	out := t.TempDir()
	layout := newIL2CPPLayout(out, "")
	if err := layout.mkdirAll(); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(layout.DllDir, "Game.dll"), []byte("MZ"), 0644)
	src := filepath.Join(out, "src", "Game")
	_ = os.MkdirAll(src, 0755)
	_ = os.WriteFile(filepath.Join(src, "A.cs"), []byte("using Il2CppInspector.DLL;\nnamespace N;\n[Token(Token = \"0x02000002\")]\npublic class A\n{\n\t[Token(Token = \"0x06000001\")]\n\t[Address(RVA = \"0x00001000\", Offset = \"0x400\", VA = \"0x0000000180001000\")]\n\tpublic void M()\n\t{\n\t}\n}\n"), 0644)
	sink := &unityMemSink{}
	ctx := &Context{Ctx: context.Background(), Output: out, Provenance: sink}
	rows, err := emitIL2CPPProvenance(ctx, layout, filepath.Join(out, "src"), "abc", "il2cppinspector", "dump/cs/il2cpp.cs")
	if err != nil || rows != 2 {
		t.Fatalf("rows %d err %v", rows, err)
	}
	var method *runmodel.ProvenanceRecord
	for i := range sink.rows {
		r := &sink.rows[i]
		if r.ArtifactKind != runmodel.FidelityStub || r.Producer.StepID != unityStepIL2CPPStubs || r.InputSHA256 != "abc" {
			t.Fatalf("row %+v", r)
		}
		if r.Token != nil && *r.Token == "0x6000001" {
			method = r
		}
	}
	if method == nil || *method.RVA != "0x1000" || *method.Address != "0x180001000" || *method.OriginalName != "N.A::public void M()" ||
		method.ArtifactPath != "src/Game/A.cs" || *method.Evidence[0].Line != 8 || method.NameSource != "metadata" {
		t.Fatalf("method row %+v", method)
	}
}

func TestUnityDataEvidenceUnsetStateIsFailure(t *testing.T) {
	out := t.TempDir()
	ctx := &Context{Ctx: context.Background(), Output: out}
	ev := newUnityDataEvidence(ctx, "unity-mono", "", filepath.Join(filepath.Dir(out), "_assets", "x"), true)
	writeUnityDataEvidence(ctx, &ev, func(string, string) {})
	r, err := quality.UnityData{}.Collect(context.Background(), quality.Input{Output: out})
	if err != nil || r.Status != "degraded" {
		t.Fatalf("status %s err %v", r.Status, err)
	}
	if ev.State != quality.UnityAssetsFailed || ev.ExportDir != "../_assets/x" {
		t.Fatalf("%+v", ev)
	}
}

func TestILSpyPartialProjectIsKeptNotDiscarded(t *testing.T) {
	dir := t.TempDir()
	res := &util.CmdResult{ExitCode: 70, Stderr: "While decompiling x.dll:\n4 error(s) occurred; the affected code was replaced by the error text in the output."}
	if _, ok := ilspyPartialErrors(res, nil, dir); ok {
		t.Fatal("no source written: not partial")
	}
	_ = os.WriteFile(filepath.Join(dir, "Empty.cs"), nil, 0644)
	if _, ok := ilspyPartialErrors(res, nil, dir); ok {
		t.Fatal("empty source is not output")
	}
	_ = os.WriteFile(filepath.Join(dir, "A.cs"), []byte("class A {}"), 0644)
	if n, ok := ilspyPartialErrors(res, nil, dir); !ok || n != 4 {
		t.Fatalf("n=%d ok=%v", n, ok)
	}
	if _, ok := ilspyPartialErrors(&util.CmdResult{ExitCode: 70, Stderr: "System.BadImageFormatException"}, nil, dir); ok {
		t.Fatal("crash without the partial marker is a failure")
	}
}
