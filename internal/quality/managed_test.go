package quality

import (
	"context"
	"github.com/UberMorgott/morgue/internal/recon"
	"github.com/UberMorgott/morgue/internal/runmodel"
	pe "github.com/saferwall/pe"
	"os"
	"path/filepath"
	"testing"
)

func TestManagedInventoryExcludesNoBody(t *testing.T) {
	f := &pe.File{}
	f.CLR.MetadataStreams = map[string][]byte{"#Strings": []byte("\x00<Module>\x00Demo\x00Run\x00")}
	f.CLR.MetadataTables = map[int]*pe.MetadataTable{
		pe.TypeDef:   {Content: []pe.TypeDefTableRow{{TypeName: 1, MethodList: 1}, {TypeName: 10, MethodList: 1}}},
		pe.MethodDef: {Content: []pe.MethodDefTableRow{{Name: 15, RVA: 100}, {Name: 15, Flags: 0x400}, {Name: 15, Flags: 0x2000}}},
	}
	inv, err := managedRows(f, nil)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Types != 1 || inv.Methods != 1 || inv.ExcludedMethods != 2 {
		t.Fatalf("%+v", inv)
	}
}

func TestManagedDiagnosticsAndNoPUAEncryption(t *testing.T) {
	out := t.TempDir()
	os.MkdirAll(filepath.Join(out, "src"), 0755)
	os.WriteFile(filepath.Join(out, "src", "Demo.cs"), []byte("class Demo {\n// Unknown result type (might be due to invalid IL or missing references)\n// Could not resolve type System.Missing\nstring Run() { return Decrypt(123); }\n}"), 0644)
	r, err := (Managed{}).Collect(context.Background(), Input{Output: out, Target: runmodel.TargetRecord{InputPath: "missing.dll"}})
	if err != nil {
		t.Fatal(err)
	}
	if r.UnknownResultTypes.Value == nil || *r.UnknownResultTypes.Value != 1 || *r.UnresolvedReferences.Value != 1 {
		t.Fatalf("%+v", r)
	}
	if r.StringDecryption.Ratio != nil || r.StringDecryption.Found.Status != "unknown" {
		t.Fatal("absence of PUA is not decryption proof")
	}
	if len(r.UnknownResultTypes.Evidence) != 1 || *r.UnknownResultTypes.Evidence[0].Line != 2 {
		t.Fatal("missing line evidence")
	}
	if r.Functions.Emitted.Status != "estimated" || r.Functions.Ratio != nil {
		t.Fatal("source estimates cannot produce exact coverage")
	}
}

func TestStringCoveragePartialAndZero(t *testing.T) {
	for _, tc := range []struct {
		body string
		want *float64
	}{
		{"SITES:2\nSITE:0x06000001:10:replaced\nSITE:0x06000001:20:residual\n", floatPtr(.5)},
		{"SITES:0\n", nil},
		{"REWROTE:5\nRESIDUAL:0\n", nil},
	} {
		c := stringCoverage(tc.body, "logs/cfxstrings.log")
		if tc.want == nil {
			if c.Ratio != nil {
				t.Fatal("invented ratio")
			}
		} else if c.Ratio == nil || *c.Ratio != *tc.want {
			t.Fatalf("%+v", c)
		}
	}
}

func floatPtr(n float64) *float64 { return &n }

func TestRawDiagnosticDriftRemainsUnknown(t *testing.T) {
	out := t.TempDir()
	os.MkdirAll(filepath.Join(out, "logs"), 0755)
	os.WriteFile(filepath.Join(out, "logs", "ilspycmd-test.log"), []byte("unrecognized future diagnostic format"), 0644)
	os.WriteFile(filepath.Join(out, "logs", "ilspycmd-test.log.json"), []byte(`{"schemaVersion":1,"tool":"ilspycmd","exitCode":1}`), 0644)
	r := emptyFamily("managed")
	collectInvocationDiagnostics(Input{Output: out}, "ilspycmd", &r)
	finishFamily(&r)
	if r.DecompilerErrors.Value != nil || r.DecompilerErrors.Status != "unknown" {
		t.Fatal("parser drift became reassuring zero")
	}
	if r.Status != "degraded" {
		t.Fatal("failed invocation was hidden")
	}
}

func TestApplicationLiteralsAreNotDiagnostics(t *testing.T) {
	out := t.TempDir()
	os.MkdirAll(filepath.Join(out, "src"), 0755)
	os.WriteFile(filepath.Join(out, "src", "Demo.cs"), []byte("class Demo { string text = \"Unknown result type; could not resolve\"; }"), 0644)
	r := emptyFamily("managed")
	if err := collectSource(context.Background(), Input{Output: out}, ".cs", &r); err != nil {
		t.Fatal(err)
	}
	if r.UnknownResultTypes.Value != nil || r.UnresolvedReferences.Value != nil {
		t.Fatal("application string became compiler diagnostic")
	}
}

func TestUnattributedResourcesAreNotResidualObfuscation(t *testing.T) {
	if n := residualIndicators(recon.Result{ObfuscatorFeatures: []string{"resources"}}); n != 0 {
		t.Fatal("ordinary resources became residual obfuscation")
	}
	if n := residualIndicators(recon.Result{Obfuscator: "ConfuserEx", ObfuscatorFeatures: []string{"constants"}}); n != 2 {
		t.Fatal(n)
	}
}
